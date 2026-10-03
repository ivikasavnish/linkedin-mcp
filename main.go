package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	loadEnv()
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "auth":
		if err := auth(); err != nil {
			log.Fatal(err)
		}
	case "http":
		addr := os.Getenv("LINKEDIN_MCP_ADDR")
		if addr == "" {
			addr = "127.0.0.1:8766"
		}
		h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return newServer() }, nil)
		log.Printf("linkedin-mcp listening on http://%s/mcp", addr)
		mux := http.NewServeMux()
		mux.Handle("/mcp", h)
		log.Fatal(http.ListenAndServe(addr, mux))
	default:
		if err := newServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
	}
}

func client() (*Client, error) {
	t, err := loadToken()
	if err != nil {
		return nil, err
	}
	return &Client{tok: t}, nil
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

type empty struct{}

type deleteInput struct {
	URN string `json:"urn" jsonschema:"post URN returned by create_post, e.g. urn:li:share:123"`
}

func newServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "linkedin", Version: "v0.1.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{Name: "whoami", Description: "Show the LinkedIn account this server posts as"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
			c, err := client()
			if err != nil {
				return nil, nil, err
			}
			return text(fmt.Sprintf("%s (%s), token expires %s", c.tok.Name, c.tok.PersonURN, c.tok.ExpiresAt.Format(time.DateOnly))), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "create_post", Description: "Publish a LinkedIn post (text, optional images or link card). Supports **bold** and *italic*. Posts are public by default — confirm content with the user first."},
		func(ctx context.Context, req *mcp.CallToolRequest, in PostInput) (*mcp.CallToolResult, any, error) {
			if strings.TrimSpace(in.Text) == "" {
				return nil, nil, fmt.Errorf("text is required")
			}
			in.Text = formatText(in.Text)
			if n := utf8.RuneCountInString(in.Text); n > 3000 {
				return nil, nil, fmt.Errorf("text is %d chars, LinkedIn max is 3000", n)
			}
			c, err := client()
			if err != nil {
				return nil, nil, err
			}
			urn, err := c.CreatePost(in)
			if err != nil {
				return nil, nil, err
			}
			if in.Draft {
				return text("draft saved: " + urn), nil, nil
			}
			return text(fmt.Sprintf("posted: %s\nhttps://www.linkedin.com/feed/update/%s/", urn, urn)), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "delete_post", Description: "Delete a LinkedIn post by URN"},
		func(ctx context.Context, req *mcp.CallToolRequest, in deleteInput) (*mcp.CallToolResult, any, error) {
			c, err := client()
			if err != nil {
				return nil, nil, err
			}
			if err := c.DeletePost(in.URN); err != nil {
				return nil, nil, err
			}
			return text("deleted " + in.URN), nil, nil
		})

	return s
}

// auth runs the 3-legged OAuth flow on localhost and saves the token.
func auth() error {
	id, secret := os.Getenv("LINKEDIN_CLIENT_ID"), os.Getenv("LINKEDIN_CLIENT_SECRET")
	if id == "" || secret == "" {
		return fmt.Errorf("set LINKEDIN_CLIENT_ID and LINKEDIN_CLIENT_SECRET")
	}
	scopes := os.Getenv("LINKEDIN_SCOPES")
	if scopes == "" {
		scopes = "openid profile w_member_social"
	}
	redirectURI := os.Getenv("LINKEDIN_REDIRECT_URI")
	if redirectURI == "" {
		redirectURI = "http://localhost:8779/auth/linkedin/callback"
	}
	ru, err := url.Parse(redirectURI)
	if err != nil {
		return fmt.Errorf("bad LINKEDIN_REDIRECT_URI: %w", err)
	}

	sb := make([]byte, 16)
	rand.Read(sb)
	state := hex.EncodeToString(sb)

	authURL := "https://www.linkedin.com/oauth/v2/authorization?" + url.Values{
		"response_type": {"code"},
		"client_id":     {id},
		"redirect_uri":  {redirectURI},
		"state":         {state},
		"scope":         {scopes},
	}.Encode()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	srv := &http.Server{Addr: ru.Host, Handler: mux}
	mux.HandleFunc(ru.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", 400)
			errCh <- fmt.Errorf("state mismatch")
			return
		}
		if e := q.Get("error"); e != "" {
			http.Error(w, e, 400)
			errCh <- fmt.Errorf("%s: %s", e, q.Get("error_description"))
			return
		}
		fmt.Fprintln(w, "LinkedIn connected. You can close this tab.")
		codeCh <- q.Get("code")
	})
	go srv.ListenAndServe()
	defer srv.Close()

	fmt.Println("Open in browser:\n" + authURL)
	exec.Command("xdg-open", authURL).Start()

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("timed out waiting for callback")
	}

	resp, err := http.PostForm("https://www.linkedin.com/oauth/v2/accessToken", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {id},
		"client_secret": {secret},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return err
	}
	if tr.AccessToken == "" {
		return fmt.Errorf("token exchange failed: %s", tr.Error)
	}

	c := &Client{tok: &Token{AccessToken: tr.AccessToken, ExpiresAt: time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)}}
	if err := c.fillProfile(); err != nil {
		return err
	}
	if err := saveToken(c.tok); err != nil {
		return err
	}
	fmt.Printf("Saved token for %s (%s) to %s\n", c.tok.Name, c.tok.PersonURN, tokenPath())
	return nil
}
