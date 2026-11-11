#!/usr/bin/env bash
set -euo pipefail

dir=/opt/commitpool
# shellcheck source=/dev/null
. "$dir/config"
tag="${1:?usage: deploy.sh <image-tag>}"

param() {
  aws ssm get-parameter --region "$REGION" --with-decryption --name "$PARAMETER_PREFIX/$1" --query Parameter.Value --output text
}
optional_param() { param "$1" 2>/dev/null || true; }

pg_password="$(param postgres_password)"
umask 077
{
  echo "PORT=8080"
  echo "COMMITPOOL_BASE_URL=https://$DOMAIN"
  echo "DATABASE_URL=postgres://commitpool:${pg_password}@postgres:5432/commitpool?sslmode=disable"
  echo "PAYPAL_BASE_URL=https://api-m.sandbox.paypal.com"
  echo "PAYPAL_CLIENT_ID=$(param paypal_client_id)"
  echo "PAYPAL_CLIENT_SECRET=$(param paypal_client_secret)"
  echo "PAYPAL_WEBHOOK_ID=$(optional_param paypal_webhook_id)"
  echo "AWS_REGION=$REGION"
  echo "COMMITPOOL_AI_PROVIDER=bedrock"
  echo "COMMITPOOL_DEMO_MODE=1"
  echo "COMMITPOOL_SANDBOX_MOCKS=1"
  echo "COMMITPOOL_PAYOUTS=1"
  demo_payout_email="$(optional_param demo_payout_email)"
  if [[ -n "$demo_payout_email" ]]; then
    echo "COMMITPOOL_DEMO_PAYOUT_EMAIL=$demo_payout_email"
  fi
  mcp_secret="$(optional_param mcp_secret)"
  if [[ -n "$mcp_secret" ]]; then
    echo "COMMITPOOL_MCP_SECRET=$mcp_secret"
  fi
  video_url="$(optional_param video_url)"
  if [[ -n "$video_url" ]]; then
    echo "COMMITPOOL_VIDEO_URL=$video_url"
  fi
  devpost_url="$(optional_param devpost_url)"
  if [[ -n "$devpost_url" ]]; then
    echo "COMMITPOOL_DEVPOST_URL=$devpost_url"
  fi
  judge_buyer_email="$(optional_param judge_buyer_email)"
  judge_buyer_password="$(optional_param judge_buyer_password)"
  if [[ -n "$judge_buyer_email" && -n "$judge_buyer_password" ]]; then
    echo "COMMITPOOL_JUDGE_BUYER_EMAIL=$judge_buyer_email"
    echo "COMMITPOOL_JUDGE_BUYER_PASSWORD=$judge_buyer_password"
  fi
  mail_from="$(optional_param mail_from)"
  if [[ -n "$mail_from" ]]; then
    echo "COMMITPOOL_MAILER=ses"
    echo "COMMITPOOL_MAIL_FROM=$mail_from"
  fi
} >"$dir/app.env"
printf 'POSTGRES_USER=commitpool\nPOSTGRES_DB=commitpool\nPOSTGRES_PASSWORD=%s\n' "$pg_password" >"$dir/postgres.env"

aws ecr get-login-password --region "$REGION" | docker login --username AWS --password-stdin "${REPOSITORY%%/*}" >/dev/null

docker network inspect commitpool >/dev/null 2>&1 || docker network create commitpool >/dev/null

if ! docker container inspect postgres >/dev/null 2>&1; then
  docker run -d --name postgres --network commitpool --restart unless-stopped \
    --env-file "$dir/postgres.env" -v pgdata:/var/lib/postgresql/data postgres:16 >/dev/null
fi
for _ in $(seq 1 60); do
  docker exec postgres pg_isready -U commitpool -d commitpool >/dev/null 2>&1 && break
  sleep 1
done

docker pull --quiet "$REPOSITORY:$tag" >/dev/null
if ! docker run --rm --env-file "$dir/app.env" "$REPOSITORY:$tag" check-config >/dev/null; then
  echo "deploy: $tag refuses this configuration (see above); the running app is left as it is" >&2
  exit 1
fi
docker stop -t 35 app >/dev/null 2>&1 || true
docker rm app >/dev/null 2>&1 || true
docker run -d --name app --network commitpool --restart unless-stopped \
  --env-file "$dir/app.env" "$REPOSITORY:$tag" >/dev/null

if ! docker container inspect caddy >/dev/null 2>&1; then
  docker run -d --name caddy --network commitpool --restart unless-stopped \
    -p 80:80 -p 443:443 -v caddy_data:/data -v caddy_config:/config \
    -v "$dir/Caddyfile:/etc/caddy/Caddyfile:ro" caddy:2.10-alpine >/dev/null
fi

for _ in $(seq 1 60); do
  if docker exec caddy wget -qO- http://app:8080/healthz >/dev/null 2>&1; then
    echo "deploy: $tag is up at https://$DOMAIN"
    exit 0
  fi
  sleep 2
done
echo "deploy: the app did not become healthy; its last log lines:" >&2
docker logs --tail 40 app >&2
exit 1
