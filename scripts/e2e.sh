#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

port="${E2E_PORT:-18080}"
base_url="http://localhost:${port}"
database_url="${E2E_DATABASE_URL:-}"

if [[ -z "$database_url" ]]; then
  docker compose -f deploy/compose.yaml up -d --wait
  database_url='postgres://commitpool:commitpool@localhost:55432/commitpool_test?sslmode=disable'
fi

if (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null; then
  echo "e2e: port ${port} is already in use; stop that server or set E2E_PORT" >&2
  exit 1
fi

pnpm -C web build

work="$(mktemp -d)"
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

go build -tags e2e -o "$work/commitpool" ./cmd/commitpool

PORT="$port" \
  COMMITPOOL_BASE_URL="$base_url" \
  DATABASE_URL="$database_url" \
  E2E_FAKE_PAYPAL=1 \
  COMMITPOOL_AI_PROVIDER=fake \
  COMMITPOOL_DEMO_MODE=1 \
  COMMITPOOL_SANDBOX_MOCKS=1 \
  COMMITPOOL_RATE_LIMIT=off \
  COMMITPOOL_SEAT_TTL_MIN=30 \
  COMMITPOOL_DEMO_PACE_MS=50 \
  COMMITPOOL_PAYOUTS=1 \
  COMMITPOOL_DEMO_PAYOUT_EMAIL=receiver@example.com \
  COMMITPOOL_VIDEO_URL=https://youtu.be/aB3_x-9Zk0Q \
  COMMITPOOL_DEVPOST_URL=https://devpost.com/software/commitpool-e2e \
  COMMITPOOL_JUDGE_BUYER_EMAIL=judge-buyer@example.com \
  COMMITPOOL_JUDGE_BUYER_PASSWORD=e2e-sandbox-password \
  "$work/commitpool" >"$work/server.log" 2>&1 &
server_pid=$!

deadline=$((SECONDS + 60))
until curl -fsS -o /dev/null "${base_url}/healthz" 2>/dev/null; do
  if ! kill -0 "$server_pid" 2>/dev/null || ((SECONDS >= deadline)); then
    echo "e2e: the server did not become healthy at ${base_url}; its log:" >&2
    cat "$work/server.log" >&2
    exit 1
  fi
  sleep 1
done

status=0
E2E_BASE_URL="$base_url" pnpm -C web exec playwright test "$@" || status=$?
if ((status != 0)); then
  echo "e2e: playwright failed; the last lines of the server log:" >&2
  tail -n 60 "$work/server.log" >&2
  mkdir -p web/test-results
  cp "$work/server.log" web/test-results/e2e-server.log || true
fi
exit "$status"
