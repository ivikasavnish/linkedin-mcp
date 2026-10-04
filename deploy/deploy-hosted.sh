#!/bin/bash
# Deploy hosted linkedin-mcp to servloci.in as https://linkedin.servloci.in
#
# First run: creates DNS record, system user, env file (master key generated
# on the server, LinkedIn app creds copied from ~/.config/linkedin-mcp/.env),
# systemd unit, nginx vhost + Let's Encrypt cert.
# Later runs: rebuild + replace binary + restart. Env file is never overwritten.
#
# Needs: CLOUDFLARE_API_TOKEN in env, ssh root@servloci.in.
set -euo pipefail

HOST=root@servloci.in
DOMAIN=linkedin.servloci.in
IP=31.97.60.210
cd "$(dirname "$0")/.."

echo "== test + build"
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/linkedin-mcp-hosted .

echo "== DNS $DOMAIN -> $IP"
if [ -z "$(dig +short "$DOMAIN")" ]; then
  : "${CLOUDFLARE_API_TOKEN:?set CLOUDFLARE_API_TOKEN}"
  ZONE=$(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
    "https://api.cloudflare.com/client/v4/zones?name=servloci.in" | python3 -c 'import json,sys;print(json.load(sys.stdin)["result"][0]["id"])')
  curl -s -X POST -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" -H "Content-Type: application/json" \
    "https://api.cloudflare.com/client/v4/zones/$ZONE/dns_records" \
    -d "{\"type\":\"A\",\"name\":\"$DOMAIN\",\"content\":\"$IP\",\"ttl\":300,\"proxied\":false}" \
    | python3 -c 'import json,sys;r=json.load(sys.stdin);print("dns ok" if r["success"] else r["errors"])'
  echo "waiting for DNS..."; for i in $(seq 30); do [ -n "$(dig +short "$DOMAIN" @1.1.1.1)" ] && break; sleep 5; done
else
  echo "exists: $(dig +short "$DOMAIN")"
fi

echo "== upload"
scp -q /tmp/linkedin-mcp-hosted deploy/linkedin-mcp-hosted.service deploy/nginx-linkedin.servloci.in.conf "$HOST":/tmp/
rm -f /tmp/linkedin-mcp-hosted

# first-run env: LinkedIn creds + owner (free access) from the local install; sent over ssh stdin
ENV_SEED=$(python3 - <<'EOF'
import json, re, os
cfg = os.path.expanduser("~/.config/linkedin-mcp")
env = dict(re.findall(r'^\s*(?:export\s+)?(\w+)=["\']?([^"\'\n]*)', open(cfg + "/.env").read(), re.M))
sub = json.load(open(cfg + "/token.json"))["person_urn"].split(":")[-1]
print(f"""PUBLIC_URL=https://linkedin.servloci.in
LINKEDIN_CLIENT_ID={env['LINKEDIN_CLIENT_ID']}
LINKEDIN_CLIENT_SECRET={env['LINKEDIN_CLIENT_SECRET']}
LMCP_FREE_USERS={sub}
# Razorpay: fill all four to open billing ($1/month USD plan), then: systemctl restart linkedin-mcp
RAZORPAY_KEY_ID=
RAZORPAY_KEY_SECRET=
RAZORPAY_WEBHOOK_SECRET=
RAZORPAY_PLAN_ID=""")
EOF
)

echo "== install"
printf '%s\n' "$ENV_SEED" | ssh "$HOST" "DOMAIN=$DOMAIN bash -s" <<'REMOTE'
set -euo pipefail
SEED=$(cat)
id lmcp >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin lmcp
install -d -m 755 /opt/linkedin-mcp
install -m 755 /tmp/linkedin-mcp-hosted /opt/linkedin-mcp/linkedin-mcp && rm /tmp/linkedin-mcp-hosted
install -d -m 750 -o root -g lmcp /etc/linkedin-mcp
if [ ! -f /etc/linkedin-mcp/env ]; then
  umask 027
  { printf '%s\n' "$SEED"; echo "LMCP_MASTER_KEY=$(openssl rand -hex 32)"; } > /etc/linkedin-mcp/env
  echo "created /etc/linkedin-mcp/env (back up LMCP_MASTER_KEY: losing it loses all stored LinkedIn tokens)"
fi
chown root:lmcp /etc/linkedin-mcp/env && chmod 640 /etc/linkedin-mcp/env
mv /tmp/linkedin-mcp-hosted.service /etc/systemd/system/linkedin-mcp.service
systemctl daemon-reload && systemctl enable linkedin-mcp >/dev/null 2>&1 && systemctl restart linkedin-mcp

CONF=/etc/nginx/sites-enabled/$DOMAIN.conf
if [ ! -f /etc/letsencrypt/live/$DOMAIN/fullchain.pem ]; then
  mkdir -p /var/www/certbot
  printf 'server { listen 80; listen [::]:80; server_name %s; location /.well-known/acme-challenge/ { root /var/www/certbot; } location / { return 404; } }\n' "$DOMAIN" > "$CONF"
  nginx -t -q && systemctl reload nginx
  certbot certonly --webroot -w /var/www/certbot -d "$DOMAIN" --non-interactive --agree-tos --register-unsafely-without-email --keep-until-expiring
fi
mv /tmp/nginx-linkedin.servloci.in.conf "$CONF"
nginx -t -q && systemctl reload nginx
sleep 1
systemctl is-active linkedin-mcp
REMOTE

echo "== check"
curl -fsS "https://$DOMAIN/healthz" && echo
curl -fsS "https://$DOMAIN/.well-known/oauth-authorization-server" | head -c 200 && echo
echo "Live: https://$DOMAIN"
