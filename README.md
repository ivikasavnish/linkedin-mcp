# linkedin-mcp

MCP server (stdio) that posts to LinkedIn.

Tools: `whoami`, `create_post` (text, #hashtags, images, link card, draft, company page author), `comment_post`, `delete_post`.

- `**bold**` and `*italic*` become Unicode bold/italic (LinkedIn's editor has neither)
- Reserved characters escaped, so posts with `()`, `@`, `_` aren't cut short
- `#word` becomes a real clickable hashtag
- HTTP mode requires a bearer key and a local Host header
- Images must actually be jpg/png/gif

Example posts made with it: [posts/](posts/).

## Setup

1. https://www.linkedin.com/developers/apps → create app.
   Products: add **Sign In with LinkedIn using OpenID Connect** + **Share on LinkedIn**.
   Auth tab: add redirect URL `http://localhost:8779/auth/linkedin/callback`.
2. Build + login:
   ```sh
   go build -o ~/.local/bin/linkedin-mcp .
   # put LINKEDIN_CLIENT_ID / LINKEDIN_CLIENT_SECRET in ~/.config/linkedin-mcp/.env
   linkedin-mcp auth
   ```
   Token saved to `~/.config/linkedin-mcp/token.json` (valid ~60 days, rerun `auth` after).
3. Run as service (auto-starts on boot, HTTP on 127.0.0.1:8766):
   ```sh
   cp linkedin-mcp.service ~/.config/systemd/user/
   systemctl --user enable --now linkedin-mcp
   # set LINKEDIN_MCP_KEY in ~/.config/linkedin-mcp/.env first (e.g. openssl rand -hex 32)
   claude mcp add -s user --transport http linkedin http://127.0.0.1:8766/mcp \
     --header "Authorization: Bearer $KEY"
   ```
   Or stdio instead: `claude mcp add -s user linkedin -- ~/.local/bin/linkedin-mcp`

## Config

Reads `~/.config/linkedin-mcp/.env` only (real env vars win). See `.env.example`.

## Env

- `LINKEDIN_ACCESS_TOKEN` — use this token instead of token.json
- `LINKEDIN_API_VERSION` — `LinkedIn-Version` header (default `202607`)
- `LINKEDIN_MCP_KEY` — required in http mode, clients must send `Authorization: Bearer <key>`
- `LINKEDIN_MCP_ADDR` — HTTP listen addr (default `127.0.0.1:8766`, keep it on localhost)
- `LINKEDIN_REDIRECT_URI` — OAuth callback (default `http://localhost:8779/auth/linkedin/callback`, must match app settings)
- `LINKEDIN_SCOPES` — auth scopes (default `openid profile w_member_social`; add `w_organization_social` for company pages, needs Community Management API)

## License

MIT
