package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const apiBase = "https://api.linkedin.com"

type Token struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	PersonURN   string    `json:"person_urn"`
	Name        string    `json:"name"`
}

func tokenPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "linkedin-mcp", "token.json")
}

func loadToken() (*Token, error) {
	if t := os.Getenv("LINKEDIN_ACCESS_TOKEN"); t != "" {
		c := &Client{tok: &Token{AccessToken: t}}
		if err := c.fillProfile(); err != nil {
			return nil, err
		}
		return c.tok, nil
	}
	b, err := os.ReadFile(tokenPath())
	if err != nil {
		return nil, fmt.Errorf("no token, run `linkedin-mcp auth` first: %w", err)
	}
	var t Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	if !t.ExpiresAt.IsZero() && time.Now().After(t.ExpiresAt) {
		return nil, fmt.Errorf("token expired %s, run `linkedin-mcp auth` again", t.ExpiresAt.Format(time.DateOnly))
	}
	return &t, nil
}

func saveToken(t *Token) error {
	p := tokenPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(t, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

type Client struct {
	tok *Token
}

func apiVersion() string {
	if v := os.Getenv("LINKEDIN_API_VERSION"); v != "" {
		return v
	}
	return "202607"
}

// do sends a request and decodes JSON response into out (if non-nil). Returns response headers.
func (c *Client) do(method, path string, body, out any) (http.Header, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, apiBase+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.tok.AccessToken)
	req.Header.Set("LinkedIn-Version", apiVersion())
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return nil, err
		}
	}
	return resp.Header, nil
}

func (c *Client) fillProfile() error {
	var u struct {
		Sub  string `json:"sub"`
		Name string `json:"name"`
	}
	if _, err := c.do("GET", "/v2/userinfo", nil, &u); err != nil {
		return err
	}
	c.tok.PersonURN = "urn:li:person:" + u.Sub
	c.tok.Name = u.Name
	return nil
}

func (c *Client) uploadImage(owner, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// check content, not extension, so a non-image file can't be posted publicly
	switch ct := http.DetectContentType(data); ct {
	case "image/jpeg", "image/png", "image/gif":
	default:
		return "", fmt.Errorf("%s is %s, only jpg/png/gif allowed", path, ct)
	}
	var init struct {
		Value struct {
			UploadURL string `json:"uploadUrl"`
			Image     string `json:"image"`
		} `json:"value"`
	}
	req := map[string]any{"initializeUploadRequest": map[string]any{"owner": owner}}
	if _, err := c.do("POST", "/rest/images?action=initializeUpload", req, &init); err != nil {
		return "", err
	}
	put, err := http.NewRequest("PUT", init.Value.UploadURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	put.Header.Set("Authorization", "Bearer "+c.tok.AccessToken)
	resp, err := http.DefaultClient.Do(put)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("image upload: %s: %s", resp.Status, b)
	}
	return init.Value.Image, nil
}

type PostInput struct {
	Text       string   `json:"text" jsonschema:"post body; #hashtags become real hashtags, **bold** and *italic* become Unicode bold/italic"`
	Visibility string   `json:"visibility,omitempty" jsonschema:"PUBLIC (default) or CONNECTIONS"`
	ImagePaths []string `json:"image_paths,omitempty" jsonschema:"local image files to attach (jpg/png/gif)"`
	ImageAlt   string   `json:"image_alt,omitempty" jsonschema:"alt text for single image"`
	LinkURL    string   `json:"link_url,omitempty" jsonschema:"article link to attach (ignored if images given)"`
	LinkTitle  string   `json:"link_title,omitempty" jsonschema:"title for the link card"`
	LinkDesc   string   `json:"link_description,omitempty" jsonschema:"description for the link card"`
	Author     string   `json:"author,omitempty" jsonschema:"author URN, default is you; use urn:li:organization:ID for a company page"`
	Draft      bool     `json:"draft,omitempty" jsonschema:"save as draft instead of publishing"`
}

func (c *Client) CreatePost(in PostInput) (string, error) {
	author := in.Author
	if author == "" {
		author = c.tok.PersonURN
	}
	vis := strings.ToUpper(in.Visibility)
	if vis == "" {
		vis = "PUBLIC"
	}
	state := "PUBLISHED"
	if in.Draft {
		state = "DRAFT"
	}
	post := map[string]any{
		"author":     author,
		"commentary": littleText(in.Text),
		"visibility": vis,
		"distribution": map[string]any{
			"feedDistribution":               "MAIN_FEED",
			"targetEntities":                 []any{},
			"thirdPartyDistributionChannels": []any{},
		},
		"lifecycleState":            state,
		"isReshareDisabledByAuthor": false,
	}

	switch {
	case len(in.ImagePaths) == 1:
		id, err := c.uploadImage(author, in.ImagePaths[0])
		if err != nil {
			return "", err
		}
		post["content"] = map[string]any{"media": map[string]any{"id": id, "altText": in.ImageAlt}}
	case len(in.ImagePaths) > 1:
		var imgs []map[string]any
		for _, p := range in.ImagePaths {
			id, err := c.uploadImage(author, p)
			if err != nil {
				return "", err
			}
			imgs = append(imgs, map[string]any{"id": id})
		}
		post["content"] = map[string]any{"multiImage": map[string]any{"images": imgs}}
	case in.LinkURL != "":
		art := map[string]any{"source": in.LinkURL, "title": in.LinkTitle}
		if art["title"] == "" {
			art["title"] = in.LinkURL
		}
		if in.LinkDesc != "" {
			art["description"] = in.LinkDesc
		}
		post["content"] = map[string]any{"article": art}
	}

	h, err := c.do("POST", "/rest/posts", post, nil)
	if err != nil {
		return "", err
	}
	return h.Get("X-Restli-Id"), nil
}

func (c *Client) DeletePost(urn string) error {
	_, err := c.do("DELETE", "/rest/posts/"+url.QueryEscape(urn), nil, nil)
	return err
}

var (
	reserved = regexp.MustCompile(`[\\|{}@\[\]()<>#*_~]`)
	hashtag  = regexp.MustCompile(`#(\w+)`)
)

// littleText escapes LinkedIn "little text" reserved chars (unescaped ones truncate the post)
// and turns #word into a real hashtag.
func littleText(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range hashtag.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(reserved.ReplaceAllString(s[last:m[0]], `\$0`))
		fmt.Fprintf(&b, `{hashtag|\#|%s}`, reserved.ReplaceAllString(s[m[2]:m[3]], `\$0`))
		last = m[1]
	}
	b.WriteString(reserved.ReplaceAllString(s[last:], `\$0`))
	return b.String()
}

var (
	boldMd   = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	italicMd = regexp.MustCompile(`\*([^*\s][^*\n]*?)\*`)
)

// formatText turns **bold** and *italic* into Unicode math sans letters,
// since LinkedIn has no rich text.
func formatText(s string) string {
	s = boldMd.ReplaceAllStringFunc(s, func(m string) string { return styled(m[2:len(m)-2], 0x1D5D4, 0x1D5EE, 0x1D7EC) })
	return italicMd.ReplaceAllStringFunc(s, func(m string) string { return styled(m[1:len(m)-1], 0x1D608, 0x1D622, 0) })
}

func styled(s string, upper, lower, digit rune) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			r = upper + r - 'A'
		case r >= 'a' && r <= 'z':
			r = lower + r - 'a'
		case digit != 0 && r >= '0' && r <= '9':
			r = digit + r - '0'
		}
		b.WriteRune(r)
	}
	return b.String()
}
