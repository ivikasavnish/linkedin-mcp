package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// X (Twitter) posting via API v2 with an OAuth 2.0 user-context token
// (tweet.write; an app-only bearer token can't post). X_CLIENT_ACCESS_TOKEN is
// used as-is; with X_CLIENT_REFRESH_TOKEN + X_CLIENT_ID (and X_CLIENT_SECRET
// for confidential apps) it is refreshed automatically on expiry.

// xMaxChars is the per-post limit: 25000 for Premium accounts (long posts),
// set X_MAX_CHARS=280 for a free account. X still enforces the real limit.
func xMaxChars() int {
	if n, err := strconv.Atoi(os.Getenv("X_MAX_CHARS")); err == nil && n > 0 {
		return n
	}
	return 25000
}

type xPostInput struct {
	Texts   []string `json:"texts" jsonschema:"one entry per post; more than one entry is published as a thread, each replying to the previous"`
	ReplyTo string   `json:"reply_to,omitempty" jsonschema:"optional post ID to reply to with the first entry"`
}

var xHTTP = &http.Client{Timeout: 30 * time.Second}

// xTok is the cached OAuth 2.0 token pair. X user access tokens expire after
// ~2h and refresh tokens rotate on every use, so the latest pair is persisted
// to x_token.json next to token.json.
type xTok struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

var xMu sync.Mutex

func xTokenPath() string { return filepath.Join(filepath.Dir(tokenPath()), "x_token.json") }

func xLoad() xTok {
	var t xTok
	if b, err := os.ReadFile(xTokenPath()); err == nil {
		json.Unmarshal(b, &t)
	}
	if t.AccessToken == "" {
		t.AccessToken = os.Getenv("X_CLIENT_ACCESS_TOKEN")
	}
	if t.RefreshToken == "" {
		t.RefreshToken = os.Getenv("X_CLIENT_REFRESH_TOKEN")
	}
	return t
}

// xToken returns a usable access token, refreshing first when the cached one
// is known to be expired (or force is set after a 401).
func xToken(force bool) (string, error) {
	xMu.Lock()
	defer xMu.Unlock()
	t := xLoad()
	stale := !t.ExpiresAt.IsZero() && time.Until(t.ExpiresAt) < time.Minute
	if (force || stale || t.AccessToken == "") && t.RefreshToken != "" {
		nt, err := xRefresh(t.RefreshToken)
		if err != nil {
			return "", err
		}
		t = nt
	}
	if t.AccessToken == "" {
		return "", fmt.Errorf("X_CLIENT_ACCESS_TOKEN or X_CLIENT_REFRESH_TOKEN is required (env or ~/.config/linkedin-mcp/.env)")
	}
	return t.AccessToken, nil
}

// xRefresh trades a refresh token for a new pair and saves it. Confidential
// apps authenticate with X_CLIENT_SECRET; public apps send client_id only.
func xRefresh(refresh string) (xTok, error) {
	id := os.Getenv("X_CLIENT_ID")
	if id == "" {
		return xTok{}, fmt.Errorf("X_CLIENT_ID is required to refresh the X token")
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {id}}
	req, err := http.NewRequest("POST", "https://api.x.com/2/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return xTok{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if secret := os.Getenv("X_CLIENT_SECRET"); secret != "" {
		req.SetBasicAuth(id, secret)
	}
	resp, err := xHTTP.Do(req)
	if err != nil {
		return xTok{}, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return xTok{}, fmt.Errorf("x token refresh: %s: %s", resp.Status, strings.TrimSpace(string(rb)))
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(rb, &tr); err != nil || tr.AccessToken == "" {
		return xTok{}, fmt.Errorf("x token refresh: unexpected response")
	}
	t := xTok{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)}
	if t.RefreshToken == "" {
		t.RefreshToken = refresh
	}
	b, _ := json.MarshalIndent(t, "", "  ")
	if err := os.WriteFile(xTokenPath(), b, 0o600); err != nil {
		return xTok{}, fmt.Errorf("x token refresh: save: %w", err)
	}
	return t, nil
}

// xCheck validates every entry before anything is posted, so a thread never
// stops halfway on a length error.
func xCheck(texts []string) error {
	if len(texts) == 0 {
		return fmt.Errorf("texts is required")
	}
	for i, t := range texts {
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("texts[%d] is empty", i)
		}
		if n, max := utf8.RuneCountInString(t), xMaxChars(); n > max {
			return fmt.Errorf("texts[%d] is %d chars, X max is %d", i, n, max)
		}
	}
	return nil
}

var errXUnauthorized = errors.New("x: 401 Unauthorized (token expired or revoked)")

// xTweet posts one tweet and returns its ID.
func xTweet(token, text, replyTo string) (string, error) {
	body := map[string]any{"text": text}
	if replyTo != "" {
		body["reply"] = map[string]string{"in_reply_to_tweet_id": replyTo}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "https://api.x.com/2/tweets", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := xHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode == http.StatusUnauthorized {
		return "", errXUnauthorized
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("x: %s: %s", resp.Status, strings.TrimSpace(string(rb)))
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rb, &out); err != nil || out.Data.ID == "" {
		return "", fmt.Errorf("x: unexpected response: %s", rb)
	}
	return out.Data.ID, nil
}

// xPost publishes texts as a single post or a thread. On a mid-thread failure
// it returns the IDs already posted alongside the error.
func xPost(in xPostInput) ([]string, error) {
	for i := range in.Texts {
		in.Texts[i] = formatText(in.Texts[i])
	}
	if err := xCheck(in.Texts); err != nil {
		return nil, err
	}
	token, err := xToken(false)
	if err != nil {
		return nil, err
	}
	var ids []string
	prev := in.ReplyTo
	for i, t := range in.Texts {
		id, err := xTweet(token, t, prev)
		if errors.Is(err, errXUnauthorized) {
			// A 401 means nothing was posted, so refresh once and retry.
			if token, err = xToken(true); err == nil {
				id, err = xTweet(token, t, prev)
			}
		}
		if err != nil {
			return ids, fmt.Errorf("post %d of %d: %w", i+1, len(in.Texts), err)
		}
		ids = append(ids, id)
		prev = id
	}
	return ids, nil
}
