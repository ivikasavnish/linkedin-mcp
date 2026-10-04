package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// MCP authorization server (OAuth 2.1 + PKCE + dynamic client registration),
// so clients like claude.ai can connect with "Add connector" and no API key.
// The user signs in with LinkedIn, then approves the client on a consent page.

const (
	accessTTL  = time.Hour
	refreshTTL = 30 * 24 * time.Hour
	codeTTL    = 5 * time.Minute
)

func (a *app) oauthRoutes(mux *http.ServeMux) {
	prm := a.cors(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"resource":                 a.base + "/mcp",
			"authorization_servers":    []string{a.base},
			"bearer_methods_supported": []string{"header"},
			"resource_name":            "LinkedIn MCP",
		})
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", prm)
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", prm)
	asm := a.cors(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer":                                a.base,
			"authorization_endpoint":                a.base + "/authorize",
			"token_endpoint":                        a.base + "/token",
			"registration_endpoint":                 a.base + "/register",
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"none"},
			"scopes_supported":                      []string{"linkedin"},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", asm)
	mux.HandleFunc("/.well-known/openid-configuration", asm)
	mux.HandleFunc("/register", a.cors(a.register))
	mux.HandleFunc("GET /authorize", a.authorize)
	mux.HandleFunc("POST /authorize", a.post(a.approve))
	mux.HandleFunc("/token", a.cors(a.token))
	mux.HandleFunc("POST /revoke-apps", a.post(a.revokeApps))
}

// cors allows browser-based MCP clients to reach the public OAuth endpoints.
func (a *app) cors(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func oauthErr(w http.ResponseWriter, code int, e, desc string) {
	writeJSON(w, code, map[string]string{"error": e, "error_description": desc})
}

// clientIP trusts X-Real-IP only from the local nginx proxy; anyone else could
// rotate the header to dodge rate limits.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if real := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); real != nil {
			return real.String()
		}
	}
	return host
}

var schemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

// validRedirect allows https, http on loopback only, and native-app custom schemes.
func validRedirect(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Fragment != "" || !schemeRe.MatchString(u.Scheme) {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Host != ""
	case "http":
		h := u.Hostname()
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	case "javascript", "data", "file", "vbscript", "blob", "about":
		return false
	}
	return true
}

func (a *app) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		oauthErr(w, http.StatusMethodNotAllowed, "invalid_request", "POST only")
		return
	}
	if !a.limiter.allow("register:" + clientIP(r)) {
		oauthErr(w, http.StatusTooManyRequests, "invalid_request", "too many registrations")
		return
	}
	var in struct {
		RedirectURIs []string `json:"redirect_uris"`
		ClientName   string   `json:"client_name"`
	}
	body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if json.Unmarshal(body, &in) != nil || len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 10 {
		oauthErr(w, http.StatusBadRequest, "invalid_client_metadata", "redirect_uris required (1-10)")
		return
	}
	for _, u := range in.RedirectURIs {
		if len(u) > 2000 || !validRedirect(u) {
			oauthErr(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri must be https, http://localhost, or an app scheme")
			return
		}
	}
	name := strings.TrimSpace(in.ClientName)
	if name == "" {
		name = "Unnamed app"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	uris, _ := json.Marshal(in.RedirectURIs)
	c := OAuthClient{ID: randToken("mcpc_")[:37], Name: name, RedirectURIs: string(uris)}
	if err := a.store.AddClient(c); err != nil {
		oauthErr(w, http.StatusInternalServerError, "server_error", "could not register")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id": c.ID, "client_id_issued_at": time.Now().Unix(), "client_name": name,
		"redirect_uris": in.RedirectURIs, "token_endpoint_auth_method": "none",
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
	})
}

type authzReq struct {
	Client      *OAuthClient
	RedirectURI string
	State       string
	Challenge   string
}

// checkAuthz validates an authorize request. fatal=true means the client or
// redirect_uri is bad, so we must not redirect back (show an error page instead).
func (a *app) checkAuthz(q url.Values) (ar *authzReq, errCode, desc string, fatal bool) {
	c, err := a.store.Client(q.Get("client_id"))
	if err != nil {
		return nil, "invalid_client", "Unknown app. Remove and re-add the connector.", true
	}
	var uris []string
	json.Unmarshal([]byte(c.RedirectURIs), &uris)
	ru := q.Get("redirect_uri")
	if ru == "" && len(uris) == 1 {
		ru = uris[0]
	}
	if !slices.Contains(uris, ru) {
		return nil, "invalid_request", "redirect_uri does not match the registered app.", true
	}
	ar = &authzReq{Client: c, RedirectURI: ru, State: q.Get("state"), Challenge: q.Get("code_challenge")}
	switch {
	case q.Get("response_type") != "code":
		return ar, "unsupported_response_type", "response_type must be code", false
	case ar.Challenge == "" || q.Get("code_challenge_method") != "S256":
		return ar, "invalid_request", "PKCE with S256 is required", false
	}
	return ar, "", "", false
}

func redirectErr(w http.ResponseWriter, r *http.Request, ar *authzReq, code, desc, iss string) {
	u, _ := url.Parse(ar.RedirectURI)
	q := u.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if ar.State != "" {
		q.Set("state", ar.State)
	}
	q.Set("iss", iss)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (a *app) authorize(w http.ResponseWriter, r *http.Request) {
	ar, code, desc, fatal := a.checkAuthz(r.URL.Query())
	if fatal {
		a.msg(w, http.StatusBadRequest, "Cannot connect", desc)
		return
	}
	if code != "" {
		redirectErr(w, r, ar, code, desc, a.base)
		return
	}
	uid := a.session(r)
	if uid == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	u, err := a.store.User(uid)
	if err != nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	rhost := ar.RedirectURI
	if pu, err := url.Parse(ar.RedirectURI); err == nil && pu.Host != "" {
		rhost = pu.Scheme + "://" + pu.Host
	}
	a.render(w, "consent", map[string]any{
		"U": u, "Client": ar.Client.Name, "RedirectHost": rhost, "Query": r.URL.RawQuery, "CSRF": a.csrf(r),
	})
}

func (a *app) approve(w http.ResponseWriter, r *http.Request, uid string) {
	q, err := url.ParseQuery(r.PostFormValue("q"))
	if err != nil {
		a.msg(w, http.StatusBadRequest, "Cannot connect", "Bad request.")
		return
	}
	ar, code, desc, fatal := a.checkAuthz(q)
	if fatal {
		a.msg(w, http.StatusBadRequest, "Cannot connect", desc)
		return
	}
	if code != "" {
		redirectErr(w, r, ar, code, desc, a.base)
		return
	}
	if r.PostFormValue("action") != "allow" {
		redirectErr(w, r, ar, "access_denied", "user denied access", a.base)
		return
	}
	c := randToken("")
	if err := a.store.PutCode(c, authCode{ClientID: ar.Client.ID, UserID: uid, RedirectURI: ar.RedirectURI, Challenge: ar.Challenge}, codeTTL); err != nil {
		redirectErr(w, r, ar, "server_error", "could not issue code", a.base)
		return
	}
	log.Printf("oauth approve user=%s client=%s", uid, ar.Client.ID)
	u, _ := url.Parse(ar.RedirectURI)
	uq := u.Query()
	uq.Set("code", c)
	if ar.State != "" {
		uq.Set("state", ar.State)
	}
	uq.Set("iss", a.base)
	u.RawQuery = uq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (a *app) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		oauthErr(w, http.StatusMethodNotAllowed, "invalid_request", "POST only")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.ParseForm() != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "bad form")
		return
	}
	f := r.PostForm
	clientID := f.Get("client_id")
	var uid string
	switch f.Get("grant_type") {
	case "authorization_code":
		c, err := a.store.TakeCode(f.Get("code"))
		if err != nil || c.ClientID != clientID || c.RedirectURI != f.Get("redirect_uri") {
			oauthErr(w, http.StatusBadRequest, "invalid_grant", "invalid or expired code")
			return
		}
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(c.Challenge)) != 1 {
			oauthErr(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
			return
		}
		uid = c.UserID
	case "refresh_token":
		cid, u, _, err := a.store.Token(f.Get("refresh_token"), "refresh", true) // rotate: old one is gone
		if err != nil || (clientID != "" && cid != clientID) {
			oauthErr(w, http.StatusBadRequest, "invalid_grant", "invalid or expired refresh token")
			return
		}
		uid, clientID = u, cid
	default:
		oauthErr(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
		return
	}
	at, rt := randToken("lmat_"), randToken("lmrt_")
	if a.store.PutToken(at, "access", clientID, uid, accessTTL) != nil ||
		a.store.PutToken(rt, "refresh", clientID, uid, refreshTTL) != nil {
		oauthErr(w, http.StatusInternalServerError, "server_error", "could not issue token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": at, "token_type": "Bearer", "expires_in": int(accessTTL.Seconds()),
		"refresh_token": rt, "scope": "linkedin",
	})
}

func (a *app) revokeApps(w http.ResponseWriter, r *http.Request, uid string) {
	a.store.RevokeTokens(uid)
	log.Printf("revoked oauth tokens user=%s", uid)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
