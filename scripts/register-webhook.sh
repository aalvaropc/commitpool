#!/usr/bin/env bash
set -euo pipefail

events=(
  CHECKOUT.ORDER.APPROVED
  PAYMENT.AUTHORIZATION.CREATED
  PAYMENT.AUTHORIZATION.VOIDED
  PAYMENT.CAPTURE.COMPLETED
  PAYMENT.CAPTURE.DENIED
  PAYMENT.CAPTURE.PENDING
  PAYMENT.CAPTURE.REFUNDED
  PAYMENT.PAYOUTSBATCH.DENIED
  PAYMENT.PAYOUTSBATCH.PROCESSING
  PAYMENT.PAYOUTSBATCH.SUCCESS
  PAYMENT.PAYOUTS-ITEM.BLOCKED
  PAYMENT.PAYOUTS-ITEM.CANCELED
  PAYMENT.PAYOUTS-ITEM.DENIED
  PAYMENT.PAYOUTS-ITEM.FAILED
  PAYMENT.PAYOUTS-ITEM.HELD
  PAYMENT.PAYOUTS-ITEM.REFUNDED
  PAYMENT.PAYOUTS-ITEM.RETURNED
  PAYMENT.PAYOUTS-ITEM.SUCCEEDED
  PAYMENT.PAYOUTS-ITEM.UNCLAIMED
)

log() { echo "register-webhook: $*" >&2; }
die() {
  log "$*"
  exit 1
}

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <base-url>" >&2
  exit 2
fi
base_url="${1%/}"
if [[ "$base_url" != https://* ]]; then
  log "the base URL must be the public https:// origin, got: $base_url"
  exit 2
fi
webhook_url="${base_url}/paypal/webhook"

for tool in curl jq; do
  command -v "$tool" >/dev/null || die "$tool is required"
done
[[ -n "${PAYPAL_CLIENT_ID:-}" ]] || die "PAYPAL_CLIENT_ID is not set"
[[ -n "${PAYPAL_CLIENT_SECRET:-}" ]] || die "PAYPAL_CLIENT_SECRET is not set"
paypal="${PAYPAL_BASE_URL:-https://api-m.sandbox.paypal.com}"
paypal="${paypal%/}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
body="$tmp/body"

quote() {
  local v="${1//\\/\\\\}"
  printf '"%s"' "${v//\"/\\\"}"
}

call() {
  local method="$1" path="$2" auth="$3" data="${4:-}" type="${5:-}"
  local args=(--silent --show-error --request "$method" --output "$body" --write-out '%{http_code}'
    --header 'Accept: application/json' --config -)
  if [[ -n "$data" ]]; then
    args+=(--header "Content-Type: $type" --data "$data")
  fi
  printf '%s\n' "$auth" | curl "${args[@]}" "${paypal}${path}"
}

fail() {
  local name debug_id
  name="$(jq -r '.name // .error // "unknown"' "$body" 2>/dev/null || echo unknown)"
  debug_id="$(jq -r '.debug_id // "none"' "$body" 2>/dev/null || echo none)"
  die "$1 failed: HTTP $2, name=$name, debug_id=$debug_id"
}

log "getting an access token from $paypal"
status="$(call POST /v1/oauth2/token "user = $(quote "${PAYPAL_CLIENT_ID}:${PAYPAL_CLIENT_SECRET}")" \
  grant_type=client_credentials application/x-www-form-urlencoded)" || die "could not reach $paypal"
[[ "$status" == 200 ]] || fail "access token" "$status"
token="$(jq -r '.access_token // empty' "$body")"
[[ -n "$token" ]] || die "access token failed: PayPal answered without an access_token"
bearer="header = $(quote "Authorization: Bearer $token")"

log "looking for a webhook for $webhook_url"
status="$(call GET /v1/notifications/webhooks "$bearer")" || die "could not reach $paypal"
[[ "$status" == 200 ]] || fail "listing webhooks" "$status"
existing="$(jq -r --arg url "$webhook_url" '[.webhooks[]? | select(.url == $url)][0] // empty' "$body")"

if [[ -n "$existing" ]]; then
  id="$(jq -r '.id' <<<"$existing")"
  missing="$(jq -r --args '[.event_types[]?.name] as $have | $ARGS.positional - $have | join(" ")' \
    "${events[@]}" <<<"$existing")"
  log "reusing webhook $id"
  if [[ -n "$missing" ]]; then
    log "webhook $id does not subscribe to: $missing; adding them"
    patch="$(jq -c --args '[{op: "replace", path: "/event_types",
      value: ([.event_types[]?.name] + $ARGS.positional | unique | map({name: .}))}]' "${events[@]}" <<<"$existing")"
    status="$(call PATCH "/v1/notifications/webhooks/$id" "$bearer" "$patch" application/json)" || die "could not reach $paypal"
    [[ "$status" == 200 ]] || fail "updating the webhook's events" "$status"
  fi
  echo "$id"
  exit 0
fi

payload="$(jq -n --arg url "$webhook_url" --args '{url: $url, event_types: [$ARGS.positional[] | {name: .}]}' "${events[@]}")"
log "creating a webhook for $webhook_url"
status="$(call POST /v1/notifications/webhooks "$bearer" "$payload" application/json)" || die "could not reach $paypal"
[[ "$status" == 201 ]] || fail "creating the webhook" "$status"
id="$(jq -r '.id // empty' "$body")"
[[ -n "$id" ]] || die "creating the webhook failed: PayPal answered without an id"
log "created webhook $id"
echo "$id"
