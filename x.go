package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// X (Twitter) posting via API v2. Uses X_CLIENT_ACCESS_TOKEN, an OAuth 2.0
// user-context token with tweet.write (an app-only bearer token can't post).

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

func xToken() (string, error) {
	t := os.Getenv("X_CLIENT_ACCESS_TOKEN")
	if t == "" {
		return "", fmt.Errorf("X_CLIENT_ACCESS_TOKEN is not set (env or ~/.config/linkedin-mcp/.env)")
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
	token, err := xToken()
	if err != nil {
		return nil, err
	}
	var ids []string
	prev := in.ReplyTo
	for i, t := range in.Texts {
		id, err := xTweet(token, t, prev)
		if err != nil {
			return ids, fmt.Errorf("post %d of %d: %w", i+1, len(in.Texts), err)
		}
		ids = append(ids, id)
		prev = id
	}
	return ids, nil
}
