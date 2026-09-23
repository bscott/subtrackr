#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SUFFIX="${$}"
NETWORK="subtrackr-oidc-e2e-${SUFFIX}"
POCKET_CONTAINER="pocket-id-e2e-${SUFFIX}"
PROXY_CONTAINER="pocket-id-proxy-e2e-${SUFFIX}"
SUBTRACKR_CONTAINER="subtrackr-e2e-${SUFFIX}"
TEMP_DIR=$(mktemp -d)
TLS_DIR="$TEMP_DIR/tls"

POCKET_IMAGE="ghcr.io/pocket-id/pocket-id:v2.14.0"
PROXY_IMAGE="nginx:1.29-alpine"
CURL_IMAGE="curlimages/curl:8.16.0"
PLAYWRIGHT_IMAGE="mcr.microsoft.com/playwright:v1.56.0-noble"
SUBTRACKR_IMAGE="subtrackr-oidc-e2e:local"
POCKET_EXTERNAL_URL="https://pocket-id.test:8443"
SUBTRACKR_URL="http://subtrackr.test:8080"

cleanup() {
  docker rm -f "$SUBTRACKR_CONTAINER" "$PROXY_CONTAINER" "$POCKET_CONTAINER" >/dev/null 2>&1 || true
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
  rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

mkdir -p "$TLS_DIR"
docker network create "$NETWORK" >/dev/null

docker run --rm -v "$TLS_DIR:/tls" alpine:3.22 sh -c '
  apk add --no-cache openssl >/dev/null &&
  openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
    -keyout /tls/server.key -out /tls/server.crt \
    -subj /CN=pocket-id.test \
    -addext subjectAltName=DNS:pocket-id.test \
    -addext basicConstraints=critical,CA:TRUE >/dev/null 2>&1
'

if [[ "${SKIP_BUILD:-false}" != "true" ]]; then
  docker build -t "$SUBTRACKR_IMAGE" "$ROOT" >/dev/null
fi

docker run -d --name "$POCKET_CONTAINER" --network "$NETWORK" \
  --network-alias pocket-id-internal \
  -e APP_URL="$POCKET_EXTERNAL_URL" \
  -e ENCRYPTION_KEY=pocket-id-e2e-encryption-key-32b \
  -e STATIC_API_KEY=pocket-id-e2e-admin-api-key \
  -e ALLOW_INSECURE_CALLBACK_URLS=true \
  -e ANALYTICS_DISABLED=true \
  -e VERSION_CHECK_DISABLED=true \
  "$POCKET_IMAGE" >/dev/null

for _ in $(seq 1 60); do
  if docker exec "$POCKET_CONTAINER" /app/pocket-id healthcheck >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$POCKET_CONTAINER" /app/pocket-id healthcheck >/dev/null 2>&1 || {
  docker logs "$POCKET_CONTAINER"
  exit 1
}

docker run -d --name "$PROXY_CONTAINER" --network "$NETWORK" \
  --network-alias pocket-id.test \
  -v "$ROOT/tests/pocketid-nginx.conf:/etc/nginx/nginx.conf:ro" \
  -v "$TLS_DIR:/etc/nginx/test-tls:ro" \
  "$PROXY_IMAGE" >/dev/null

for _ in $(seq 1 30); do
  if docker run --rm --network "$NETWORK" "$CURL_IMAGE" -kfsS "$POCKET_EXTERNAL_URL/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker run --rm --network "$NETWORK" "$CURL_IMAGE" -kfsS "$POCKET_EXTERNAL_URL/healthz" >/dev/null

docker run --rm --network "$NETWORK" "$CURL_IMAGE" -fsS \
  -X POST http://pocket-id-internal:1411/api/oidc/clients \
  -H 'Content-Type: application/json' \
  -H 'X-API-KEY: pocket-id-e2e-admin-api-key' \
  -d "{\"id\":\"subtrackr-e2e\",\"name\":\"SubTrackr E2E\",\"callbackURLs\":[\"$SUBTRACKR_URL/auth/oidc/callback\"],\"pkceEnabled\":false,\"isPublic\":false}" >/dev/null

docker run --rm --network "$NETWORK" "$CURL_IMAGE" -fsS \
  -X POST http://pocket-id-internal:1411/api/oidc/clients/subtrackr-e2e/secrets \
  -H 'Content-Type: application/json' \
  -H 'X-API-KEY: pocket-id-e2e-admin-api-key' \
  -d '{"name":"SubTrackr E2E","secret":"subtrackr-e2e-client-secret"}' >/dev/null

docker run -d --name "$SUBTRACKR_CONTAINER" --network "$NETWORK" \
  --network-alias subtrackr.test \
  -e PORT=8080 \
  -e DATABASE_PATH=/tmp/subtrackr-e2e.db \
  -e SSL_CERT_FILE=/certs/pocket-id.crt \
  -v "$TLS_DIR/server.crt:/certs/pocket-id.crt:ro" \
  "$SUBTRACKR_IMAGE" >/dev/null

for _ in $(seq 1 60); do
  if docker run --rm --network "$NETWORK" "$CURL_IMAGE" -fsS "$SUBTRACKR_URL/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker run --rm --network "$NETWORK" "$CURL_IMAGE" -fsS "$SUBTRACKR_URL/healthz" >/dev/null

docker run --rm --network "$NETWORK" "$CURL_IMAGE" -fsS \
  -X POST "$SUBTRACKR_URL/api/settings/base-url" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "base_url=$SUBTRACKR_URL" >/dev/null

docker run --rm --network "$NETWORK" "$CURL_IMAGE" -fsS \
  -X POST "$SUBTRACKR_URL/api/settings/auth/oidc" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode 'enabled=true' \
  --data-urlencode 'display_name=Pocket ID' \
  --data-urlencode "issuer_url=$POCKET_EXTERNAL_URL" \
  --data-urlencode 'client_id=subtrackr-e2e' \
  --data-urlencode 'client_secret=subtrackr-e2e-client-secret' \
  --data-urlencode 'scopes=openid profile email' >/dev/null

if ! docker run --rm --network "$NETWORK" \
  -v "$ROOT:/work" -v /work/node_modules -w /work \
  "$PLAYWRIGHT_IMAGE" sh -c \
  'npm ci --ignore-scripts --no-audit --no-fund >/dev/null && node tests/pocketid.e2e.js'; then
  echo '--- Pocket ID logs ---' >&2
  docker logs "$POCKET_CONTAINER" >&2
  echo '--- TLS proxy logs ---' >&2
  docker logs "$PROXY_CONTAINER" >&2
  echo '--- SubTrackr logs ---' >&2
  docker logs "$SUBTRACKR_CONTAINER" >&2
  exit 1
fi
