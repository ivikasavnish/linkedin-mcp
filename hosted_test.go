package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) (*app, *httptest.Server) {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"), bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	a := &app{store: st, signKey: []byte("k"), free: map[string]bool{}, rzpHook: "whsec",
		limiter: &limiter{max: 60, per: time.Hour, hits: map[string][]time.Time{}}}
	srv := httptest.NewServer(nil)
	a.base = srv.URL
	srv.Config.Handler = a.routes()
	if err := st.UpsertUser("u1", "Ada", "ada@example.com", "li-secret", time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	return a, srv
}

// sessionClient returns an http client signed in as uid that doesn't follow redirects.
func sessionClient(a *app, uid string) *http.Client {
	rec := httptest.NewRecorder()
	a.setSession(rec, uid)
	jar := &staticJar{c: rec.Result().Cookies()}
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type staticJar struct{ c []*http.Cookie }

func (j *staticJar) SetCookies(*url.URL, []*http.Cookie) {}
func (j *staticJar) Cookies(*url.URL) []*http.Cookie     { return j.c }

func mcpCall(t *testing.T, base, bearer string, body string, session string) (int, string, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, string(b), resp.Header.Get("Mcp-Session-Id")
}

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

// callTool runs initialize + tools/call and returns the raw response of the call.
func callTool(t *testing.T, base, bearer, tool, args string) string {
	t.Helper()
	code, body, sid := mcpCall(t, base, bearer, initBody, "")
	if code != 200 {
		t.Fatalf("initialize: %d %s", code, body)
	}
	mcpCall(t, base, bearer, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, sid)
	_, body, _ = mcpCall(t, base, bearer, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"`+tool+`","arguments":`+args+`}}`, sid)
	return body
}

func TestHostedFlow(t *testing.T) {
	a, srv := testApp(t)
	defer srv.Close()
	base := srv.URL

	// unauthenticated MCP -> 401 pointing at resource metadata
	resp, _ := http.Post(base+"/mcp", "application/json", strings.NewReader(initBody))
	if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "oauth-protected-resource") {
		t.Fatalf("want 401 + resource_metadata, got %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}

	// --- OAuth: register, authorize, approve, token ---
	resp, _ = http.Post(base+"/register", "application/json", strings.NewReader(`{"redirect_uris":["javascript:alert(1)"]}`))
	if resp.StatusCode != 400 {
		t.Fatalf("javascript redirect accepted: %d", resp.StatusCode)
	}
	resp, _ = http.Post(base+"/register", "application/json", strings.NewReader(`{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"]}`))
	var reg struct {
		ClientID string `json:"client_id"`
	}
	json.NewDecoder(resp.Body).Decode(&reg)
	if resp.StatusCode != 201 || reg.ClientID == "" {
		t.Fatalf("register: %d", resp.StatusCode)
	}

	verifier := "verifier-verifier-verifier-verifier-verifier-1234"
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {reg.ClientID}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"},
		"response_type": {"code"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"}, "state": {"xyz"}}

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, _ = noRedirect.Get(base + "/authorize?" + q.Encode())
	if resp.StatusCode != 302 || !strings.HasPrefix(resp.Header.Get("Location"), "/login?next=") {
		t.Fatalf("anon authorize should go to login: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	bad := url.Values{}
	for k, v := range q {
		bad[k] = v
	}
	bad.Set("redirect_uri", "https://evil.example/cb")
	resp, _ = noRedirect.Get(base + "/authorize?" + bad.Encode())
	if resp.StatusCode != 400 {
		t.Fatalf("unregistered redirect_uri must not redirect: %d", resp.StatusCode)
	}

	c := sessionClient(a, "u1")
	resp, _ = c.Get(base + "/authorize?" + q.Encode())
	page, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(page), "Allow <b>Claude</b>") {
		t.Fatalf("consent page: %d", resp.StatusCode)
	}
	csrf := a.csrf(&http.Request{Header: http.Header{"Cookie": {c.Jar.Cookies(nil)[0].String()}}})

	// missing csrf -> 403
	resp, _ = c.PostForm(base+"/authorize", url.Values{"q": {q.Encode()}, "action": {"allow"}})
	if resp.StatusCode != 403 {
		t.Fatalf("approve without csrf: %d", resp.StatusCode)
	}
	resp, _ = c.PostForm(base+"/authorize", url.Values{"q": {q.Encode()}, "action": {"allow"}, "csrf": {csrf}})
	loc, _ := url.Parse(resp.Header.Get("Location"))
	code := loc.Query().Get("code")
	if resp.StatusCode != 302 || code == "" || loc.Query().Get("state") != "xyz" || loc.Host != "claude.ai" {
		t.Fatalf("approve: %d %s", resp.StatusCode, loc)
	}

	tokenReq := func(f url.Values) (int, map[string]any) {
		resp, _ := http.PostForm(base+"/token", f)
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	exch := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {reg.ClientID},
		"redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "code_verifier": {"wrong"}}
	if st, _ := tokenReq(exch); st != 400 {
		t.Fatalf("bad PKCE accepted: %d", st)
	}
	// code was consumed by the failed attempt: single use
	exch.Set("code_verifier", verifier)
	if st, _ := tokenReq(exch); st != 400 {
		t.Fatalf("code reused: %d", st)
	}
	// fresh code
	resp, _ = c.PostForm(base+"/authorize", url.Values{"q": {q.Encode()}, "action": {"allow"}, "csrf": {csrf}})
	loc, _ = url.Parse(resp.Header.Get("Location"))
	exch.Set("code", loc.Query().Get("code"))
	st, tok := tokenReq(exch)
	if st != 200 || tok["access_token"] == nil {
		t.Fatalf("token: %d %v", st, tok)
	}
	access, refresh := tok["access_token"].(string), tok["refresh_token"].(string)

	// refresh rotation: old refresh token dies
	st, tok2 := tokenReq(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {reg.ClientID}})
	if st != 200 || tok2["access_token"] == access {
		t.Fatalf("refresh: %d", st)
	}
	if st, _ := tokenReq(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}); st != 400 {
		t.Fatalf("old refresh token still works: %d", st)
	}

	// --- billing gate: unpaid user gets a subscribe message ---
	if body := callTool(t, base, access, "whoami", `{}`); !strings.Contains(body, "no active subscription") {
		t.Fatalf("unpaid whoami: %s", body)
	}

	// webhook: bad signature rejected, good one activates
	ev := `{"event":"subscription.charged","payload":{"subscription":{"entity":{"id":"sub_1","status":"active","current_end":` +
		strconv.FormatInt(time.Now().Add(30*24*time.Hour).Unix(), 10) + `,"notes":{"user_id":"u1"}}}}}`
	req, _ := http.NewRequest("POST", base+"/webhooks/razorpay", strings.NewReader(ev))
	req.Header.Set("X-Razorpay-Signature", "deadbeef")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 401 {
		t.Fatalf("bad signature accepted: %d", resp.StatusCode)
	}
	m := hmac.New(sha256.New, []byte("whsec"))
	m.Write([]byte(ev))
	req, _ = http.NewRequest("POST", base+"/webhooks/razorpay", strings.NewReader(ev))
	req.Header.Set("X-Razorpay-Signature", hex.EncodeToString(m.Sum(nil)))
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 200 {
		t.Fatalf("webhook: %d", resp.StatusCode)
	}

	if body := callTool(t, base, access, "whoami", `{}`); !strings.Contains(body, "Ada (urn:li:person:u1)") {
		t.Fatalf("paid whoami: %s", body)
	}
	// hosted mode must never read server files
	if body := callTool(t, base, access, "create_post", `{"text":"hi","image_paths":["/etc/passwd"]}`); !strings.Contains(body, "not available on the hosted server") {
		t.Fatalf("image_paths not blocked: %s", body)
	}

	// --- API key ---
	key, _ := a.store.NewAPIKey("u1")
	if body := callTool(t, base, key, "whoami", `{}`); !strings.Contains(body, "Ada") {
		t.Fatalf("api key whoami: %s", body)
	}
	key2, _ := a.store.NewAPIKey("u1")
	if code, _, _ := mcpCall(t, base, key, initBody, ""); code != 401 {
		t.Fatalf("rotated key still valid: %d", code)
	}
	if body := callTool(t, base, key2, "whoami", `{}`); !strings.Contains(body, "Ada") {
		t.Fatalf("new key: %s", body)
	}

	// revoke apps kills OAuth tokens
	resp, _ = c.PostForm(base+"/revoke-apps", url.Values{"csrf": {csrf}})
	if code, _, _ := mcpCall(t, base, tok2["access_token"].(string), initBody, ""); code != 401 {
		t.Fatalf("revoked token still valid: %d", code)
	}

	// LinkedIn token is encrypted at rest
	var raw []byte
	a.store.db.QueryRow(`SELECT li_token FROM users WHERE id='u1'`).Scan(&raw)
	if bytes.Contains(raw, []byte("li-secret")) {
		t.Fatal("LinkedIn token stored in plaintext")
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{"/authorize?x=1": "/authorize?x=1", "//evil.com": "/dashboard",
		"https://evil.com": "/dashboard", "/\\evil.com": "/dashboard", "": "/dashboard"} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClientIP(t *testing.T) {
	for _, c := range []struct{ remote, hdr, want string }{
		{"127.0.0.1:5000", "203.0.113.9", "203.0.113.9"},     // via nginx
		{"198.51.100.7:5000", "203.0.113.9", "198.51.100.7"}, // direct: header ignored
		{"[2001:db8::1]:443", "", "2001:db8::1"},
		{"127.0.0.1:5000", "not-an-ip", "127.0.0.1"},
	} {
		r := &http.Request{RemoteAddr: c.remote, Header: http.Header{}}
		if c.hdr != "" {
			r.Header.Set("X-Real-IP", c.hdr)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("clientIP(%s, %q) = %q, want %q", c.remote, c.hdr, got, c.want)
		}
	}
}

func TestFreeByIDOnly(t *testing.T) {
	a := &app{free: map[string]bool{"AbC": true}}
	if a.paid(&User{ID: "abc", Email: "AbC"}) {
		t.Fatal("free access matched on case-folded id or email")
	}
	if !a.paid(&User{ID: "AbC"}) {
		t.Fatal("free id not honored")
	}
}

func TestLimiterGC(t *testing.T) {
	l := &limiter{max: 1, per: time.Millisecond, hits: map[string][]time.Time{}}
	l.allow("a")
	time.Sleep(3 * time.Millisecond)
	l.gc()
	if len(l.hits) != 0 {
		t.Fatal("stale limiter keys kept")
	}
}
