package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// app is the hosted multi-user server: LinkedIn sign-in, per-user API keys,
// MCP OAuth, $1/month Razorpay billing, and the MCP endpoint itself.
type app struct {
	base                            string // public URL, no trailing slash
	liID, liSec                     string
	store                           *Store
	signKey                         []byte
	free                            map[string]bool // LinkedIn user ids that skip billing
	rzpID, rzpSec, rzpHook, rzpPlan string
	limiter                         *limiter
}

func hosted() error {
	need := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			log.Fatalf("%s is required in hosted mode", k)
		}
		return v
	}
	mk, err := hex.DecodeString(need("LMCP_MASTER_KEY"))
	if err != nil || len(mk) != 32 {
		return errors.New("LMCP_MASTER_KEY must be 64 hex chars (openssl rand -hex 32)")
	}
	dbPath := os.Getenv("LMCP_DB")
	if dbPath == "" {
		dbPath = "linkedin-mcp.db"
	}
	st, err := openStore(dbPath, mk)
	if err != nil {
		return err
	}
	sk := sha256.Sum256(append([]byte("session:"), mk...))
	a := &app{
		base: strings.TrimRight(need("PUBLIC_URL"), "/"),
		liID: need("LINKEDIN_CLIENT_ID"), liSec: need("LINKEDIN_CLIENT_SECRET"),
		store: st, signKey: sk[:], free: map[string]bool{},
		rzpID: os.Getenv("RAZORPAY_KEY_ID"), rzpSec: os.Getenv("RAZORPAY_KEY_SECRET"),
		rzpHook: os.Getenv("RAZORPAY_WEBHOOK_SECRET"), rzpPlan: os.Getenv("RAZORPAY_PLAN_ID"),
		limiter: &limiter{max: 60, per: time.Hour, hits: map[string][]time.Time{}},
	}
	for _, f := range strings.Split(os.Getenv("LMCP_FREE_USERS"), ",") {
		if f = strings.TrimSpace(f); f != "" {
			a.free[f] = true // LinkedIn ids are case-sensitive
		}
	}
	go func() {
		for range time.Tick(time.Hour) {
			st.Sweep()
			a.limiter.gc()
		}
	}()

	addr := os.Getenv("LMCP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8767"
	}
	log.Printf("linkedin-mcp hosted on %s (public %s)", addr, a.base)
	srv := &http.Server{Addr: addr, Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("GET /login", a.login)
	mux.HandleFunc("GET /callback", a.callback)
	mux.HandleFunc("POST /logout", a.post(a.logout))
	mux.HandleFunc("GET /dashboard", a.dashboard)
	mux.HandleFunc("POST /key", a.post(a.newKey))
	mux.HandleFunc("POST /subscribe", a.post(a.subscribe))
	mux.HandleFunc("POST /cancel", a.post(a.cancel))
	mux.HandleFunc("POST /delete", a.post(a.deleteAccount))
	mux.HandleFunc("POST /webhooks/razorpay", a.razorpayHook)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	a.oauthRoutes(mux)

	mh := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return newServer(a.clientFor(mcpauth.TokenInfoFromContext(r.Context()).UserID), true)
	}, &mcp.StreamableHTTPOptions{DisableLocalhostProtection: true}) // behind nginx; bearer auth required
	mux.Handle("/mcp", mcpauth.RequireBearerToken(a.verifyBearer, &mcpauth.RequireBearerTokenOptions{
		ResourceMetadataURL:    a.base + "/.well-known/oauth-protected-resource",
		AllowMissingExpiration: true, // API keys don't expire, they are rotated
	})(mh))
	return secureHeaders(mux)
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// verifyBearer accepts a personal API key (lmk_...) or an OAuth access token.
func (a *app) verifyBearer(ctx context.Context, tok string, r *http.Request) (*mcpauth.TokenInfo, error) {
	if strings.HasPrefix(tok, "lmk_") {
		uid, err := a.store.UserByAPIKey(tok)
		if err != nil {
			return nil, mcpauth.ErrInvalidToken
		}
		return &mcpauth.TokenInfo{UserID: uid}, nil
	}
	_, uid, exp, err := a.store.Token(tok, "access", false)
	if err != nil {
		return nil, mcpauth.ErrInvalidToken
	}
	return &mcpauth.TokenInfo{UserID: uid, Expiration: exp}, nil
}

func (a *app) paid(u *User) bool {
	// free access by LinkedIn id only: email is not proof of identity
	return a.free[u.ID] || time.Now().Before(u.PaidUntil)
}

// clientFor returns the per-call LinkedIn client getter for a user. Billing,
// LinkedIn token expiry and rate limit are checked on every tool call.
func (a *app) clientFor(uid string) func(context.Context) (*Client, error) {
	return func(context.Context) (*Client, error) {
		u, err := a.store.User(uid)
		if err != nil {
			return nil, fmt.Errorf("account not found, sign in again at %s", a.base)
		}
		if !a.paid(u) {
			return nil, fmt.Errorf("no active subscription. Subscribe ($1/month) at %s/dashboard", a.base)
		}
		if time.Now().After(u.LiExpires) {
			return nil, fmt.Errorf("LinkedIn connection expired %s. Reconnect at %s/login", u.LiExpires.Format(time.DateOnly), a.base)
		}
		if !a.limiter.allow(uid) {
			return nil, errors.New("rate limit: max 60 calls per hour")
		}
		log.Printf("tool call user=%s", uid)
		return &Client{tok: &Token{AccessToken: u.LiToken, ExpiresAt: u.LiExpires,
			PersonURN: "urn:li:person:" + u.ID, Name: u.Name}}, nil
	}
}

type limiter struct {
	mu   sync.Mutex
	max  int
	per  time.Duration
	hits map[string][]time.Time
}

// gc drops keys with no hits in the window, so the map can't grow forever.
func (l *limiter) gc() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-l.per)
	for k, h := range l.hits {
		if len(h) == 0 || h[len(h)-1].Before(cut) {
			delete(l.hits, k)
		}
	}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-l.per)
	h := l.hits[key]
	for len(h) > 0 && h[0].Before(cut) {
		h = h[1:]
	}
	if len(h) >= l.max {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, time.Now())
	return true
}

// --- sessions: signed cookie "uid|expiry|mac" ---

const sessionCookie = "lmcp_s"

func (a *app) mac(parts ...string) string {
	m := hmac.New(sha256.New, a.signKey)
	m.Write([]byte(strings.Join(parts, "|")))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *app) setCookie(w http.ResponseWriter, name, val string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: val, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: strings.HasPrefix(a.base, "https://"), SameSite: http.SameSiteLaxMode})
}

func (a *app) setSession(w http.ResponseWriter, uid string) {
	exp := strconv.FormatInt(time.Now().Add(30*24*time.Hour).Unix(), 10)
	id := base64.RawURLEncoding.EncodeToString([]byte(uid))
	a.setCookie(w, sessionCookie, id+"|"+exp+"|"+a.mac("s", id, exp), 30*24*time.Hour)
}

// session returns the signed-in user id, or "".
func (a *app) session(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	p := strings.Split(c.Value, "|")
	if len(p) != 3 || !hmac.Equal([]byte(p[2]), []byte(a.mac("s", p[0], p[1]))) {
		return ""
	}
	if exp, _ := strconv.ParseInt(p[1], 10, 64); time.Now().Unix() > exp {
		return ""
	}
	uid, _ := base64.RawURLEncoding.DecodeString(p[0])
	return string(uid)
}

func (a *app) csrf(r *http.Request) string {
	c, _ := r.Cookie(sessionCookie)
	if c == nil {
		return ""
	}
	return a.mac("csrf", c.Value)
}

// post wraps a state-changing handler: requires session + CSRF token + same origin.
func (a *app) post(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := a.session(r)
		if uid == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != a.base {
			http.Error(w, "bad origin", http.StatusForbidden)
			return
		}
		want := a.csrf(r)
		if want == "" || subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(want)) != 1 {
			http.Error(w, "bad csrf token, reload the page", http.StatusForbidden)
			return
		}
		h(w, r, uid)
	}
}

// safeNext only allows local paths, so login can't be used as an open redirect.
func safeNext(n string) string {
	if !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") || strings.Contains(n, "\\") {
		return "/dashboard"
	}
	return n
}

// --- LinkedIn sign-in ---

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	state := randToken("")[:32]
	next := safeNext(r.URL.Query().Get("next"))
	a.setCookie(w, "lmcp_st", state+"|"+base64.RawURLEncoding.EncodeToString([]byte(next)), 10*time.Minute)
	http.Redirect(w, r, "https://www.linkedin.com/oauth/v2/authorization?"+url.Values{
		"response_type": {"code"},
		"client_id":     {a.liID},
		"redirect_uri":  {a.base + "/callback"},
		"state":         {state},
		"scope":         {"openid profile email w_member_social"},
	}.Encode(), http.StatusFound)
}

func (a *app) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, err := r.Cookie("lmcp_st")
	if err != nil {
		a.msg(w, http.StatusBadRequest, "Sign-in expired", "Please try signing in again.")
		return
	}
	state, nextB64, _ := strings.Cut(c.Value, "|")
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
		a.msg(w, http.StatusBadRequest, "Sign-in failed", "State mismatch, please try again.")
		return
	}
	a.setCookie(w, "lmcp_st", "", -1)
	if e := q.Get("error"); e != "" {
		a.msg(w, http.StatusBadRequest, "LinkedIn sign-in cancelled", q.Get("error_description"))
		return
	}
	resp, err := http.PostForm("https://www.linkedin.com/oauth/v2/accessToken", url.Values{
		"grant_type": {"authorization_code"}, "code": {q.Get("code")},
		"redirect_uri": {a.base + "/callback"}, "client_id": {a.liID}, "client_secret": {a.liSec},
	})
	if err != nil {
		a.msg(w, http.StatusBadGateway, "LinkedIn unreachable", "Please try again.")
		return
	}
	defer resp.Body.Close()
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if json.NewDecoder(resp.Body).Decode(&tr) != nil || tr.AccessToken == "" {
		a.msg(w, http.StatusBadGateway, "Sign-in failed", "LinkedIn did not return a token.")
		return
	}
	cl := &Client{tok: &Token{AccessToken: tr.AccessToken}}
	var ui struct {
		Sub, Name, Email string
	}
	if _, err := cl.do("GET", "/v2/userinfo", nil, &ui); err != nil || ui.Sub == "" {
		a.msg(w, http.StatusBadGateway, "Sign-in failed", "Could not read your LinkedIn profile.")
		return
	}
	exp := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	if err := a.store.UpsertUser(ui.Sub, ui.Name, ui.Email, tr.AccessToken, exp); err != nil {
		log.Printf("upsert user: %v", err)
		a.msg(w, http.StatusInternalServerError, "Error", "Could not save your account.")
		return
	}
	log.Printf("sign-in user=%s", ui.Sub)
	a.setSession(w, ui.Sub)
	next, _ := base64.RawURLEncoding.DecodeString(nextB64)
	http.Redirect(w, r, safeNext(string(next)), http.StatusSeeOther)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request, _ string) {
	a.setCookie(w, sessionCookie, "", -1)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- pages ---

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	if a.session(r) != "" {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.render(w, "home", map[string]any{"Base": a.base})
}

func (a *app) dashboard(w http.ResponseWriter, r *http.Request) {
	a.showDashboard(w, r, a.session(r), "")
}

func (a *app) showDashboard(w http.ResponseWriter, r *http.Request, uid, newKey string) {
	if uid == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	u, err := a.store.User(uid)
	if err != nil {
		a.setCookie(w, sessionCookie, "", -1)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	a.render(w, "dashboard", map[string]any{
		"Base": a.base, "U": u, "Paid": a.paid(u), "Free": a.free[u.ID],
		"LiOK": time.Now().Before(u.LiExpires), "NewKey": newKey, "CSRF": a.csrf(r),
		"Billing": a.rzpID != "" && a.rzpPlan != "", "Paying": u.SubStatus == "active" || u.SubStatus == "authenticated",
	})
}

func (a *app) newKey(w http.ResponseWriter, r *http.Request, uid string) {
	key, err := a.store.NewAPIKey(uid)
	if err != nil {
		a.msg(w, http.StatusInternalServerError, "Error", "Could not create a key.")
		return
	}
	a.showDashboard(w, r, uid, key)
}

func (a *app) deleteAccount(w http.ResponseWriter, r *http.Request, uid string) {
	if u, err := a.store.User(uid); err == nil && u.SubID != "" && u.SubStatus != "cancelled" {
		if err := a.rzp("POST", "/v1/subscriptions/"+u.SubID+"/cancel", map[string]any{"cancel_at_cycle_end": 0}, nil); err != nil {
			log.Printf("cancel on delete user=%s: %v", uid, err)
			a.msg(w, http.StatusBadGateway, "Could not cancel billing", "Account not deleted. Please try again.")
			return
		}
	}
	if err := a.store.DeleteUser(uid); err != nil {
		a.msg(w, http.StatusInternalServerError, "Error", "Could not delete the account.")
		return
	}
	log.Printf("deleted user=%s", uid)
	a.setCookie(w, sessionCookie, "", -1)
	a.msg(w, http.StatusOK, "Account deleted", "Your LinkedIn token, keys and connections are gone. You can also remove the app at linkedin.com/psettings/permitted-services.")
}

// --- billing: Razorpay subscription, $1/month ---

func (a *app) rzp(method, path string, body, out any) error {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, "https://api.razorpay.com"+path, strings.NewReader(string(b)))
	req.SetBasicAuth(a.rzpID, a.rzpSec)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct{ Description string } `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("razorpay %s: %s", resp.Status, e.Error.Description)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (a *app) subscribe(w http.ResponseWriter, r *http.Request, uid string) {
	if a.rzpID == "" || a.rzpPlan == "" {
		a.msg(w, http.StatusServiceUnavailable, "Billing not open yet", "Subscriptions are not enabled yet.")
		return
	}
	u, err := a.store.User(uid)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	var sub struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		ShortURL string `json:"short_url"`
	}
	// reuse a pending checkout instead of creating duplicate subscriptions
	if u.SubID != "" && u.SubStatus == "created" {
		if a.rzp("GET", "/v1/subscriptions/"+u.SubID, nil, &sub) == nil && sub.Status == "created" && sub.ShortURL != "" {
			http.Redirect(w, r, sub.ShortURL, http.StatusSeeOther)
			return
		}
	}
	err = a.rzp("POST", "/v1/subscriptions", map[string]any{
		"plan_id": a.rzpPlan, "total_count": 120, "customer_notify": 1,
		"notes": map[string]string{"user_id": uid, "product": "linkedin-mcp"},
	}, &sub)
	if err != nil || sub.ShortURL == "" {
		log.Printf("create subscription user=%s: %v", uid, err)
		a.msg(w, http.StatusBadGateway, "Billing error", "Could not start checkout, please try again.")
		return
	}
	a.store.SetSub(uid, sub.ID, sub.Status, time.Time{})
	http.Redirect(w, r, sub.ShortURL, http.StatusSeeOther)
}

func (a *app) cancel(w http.ResponseWriter, r *http.Request, uid string) {
	u, err := a.store.User(uid)
	if err != nil || u.SubID == "" {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	if err := a.rzp("POST", "/v1/subscriptions/"+u.SubID+"/cancel", map[string]any{"cancel_at_cycle_end": 1}, nil); err != nil {
		log.Printf("cancel user=%s: %v", uid, err)
		a.msg(w, http.StatusBadGateway, "Billing error", "Could not cancel, please try again.")
		return
	}
	a.store.SetSub(uid, u.SubID, "cancel_at_cycle_end", time.Time{})
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *app) razorpayHook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil || a.rzpHook == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	m := hmac.New(sha256.New, []byte(a.rzpHook))
	m.Write(body)
	if !hmac.Equal([]byte(hex.EncodeToString(m.Sum(nil))), []byte(r.Header.Get("X-Razorpay-Signature"))) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	var ev struct {
		Event   string `json:"event"`
		Payload struct {
			Subscription struct {
				Entity struct {
					ID         string            `json:"id"`
					Status     string            `json:"status"`
					CurrentEnd int64             `json:"current_end"`
					Notes      map[string]string `json:"notes"`
				} `json:"entity"`
			} `json:"subscription"`
		} `json:"payload"`
	}
	if json.Unmarshal(body, &ev) != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s := ev.Payload.Subscription.Entity
	if s.ID == "" {
		w.WriteHeader(http.StatusOK) // not a subscription event
		return
	}
	uid, err := a.store.UserIDBySub(s.ID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		uid = s.Notes["user_id"] // subscription id not saved yet (race with redirect)
	case err != nil:
		http.Error(w, "store error", http.StatusInternalServerError) // Razorpay retries
		return
	}
	if uid == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var until time.Time
	if ev.Event == "subscription.activated" || ev.Event == "subscription.charged" {
		until = time.Unix(s.CurrentEnd, 0).Add(24 * time.Hour) // 1 day grace for renewal retries
	}
	if err := a.store.SetSub(uid, s.ID, s.Status, until); err != nil {
		http.Error(w, "store error", http.StatusInternalServerError) // Razorpay retries
		return
	}
	log.Printf("razorpay %s user=%s status=%s", ev.Event, uid, s.Status)
	w.WriteHeader(http.StatusOK)
}

// --- rendering ---

var tpl = template.Must(template.New("").Funcs(template.FuncMap{
	"date": func(t time.Time) string { return t.Format("2 Jan 2006") },
}).Parse(pagesHTML))

func (a *app) render(w http.ResponseWriter, name string, data any) {
	a.renderCode(w, http.StatusOK, name, data)
}

func (a *app) renderCode(w http.ResponseWriter, code int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := tpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (a *app) msg(w http.ResponseWriter, code int, title, body string) {
	a.renderCode(w, code, "msg", map[string]string{"Title": title, "Body": body})
}
