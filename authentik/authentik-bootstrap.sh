#!/usr/bin/env bash
# Environment variables (optional overrides):
#   AUTHENTIK_URL                - Authentik base URL   (default: http://localhost:9000)
#   AUTHENTIK_TOKEN              - Bootstrap API token  (default: from AUTHENTIK_BOOTSTRAP_TOKEN or authentik-bootstrap-token)
#   CREDENTIALS_FILE             - Write credentials to this file path (optional)

set -euo pipefail

# Docker Compose `post_start` hooks run as a separate process whose output
# is NOT captured by `docker compose logs`.  Redirect everything to the main
# container process's stdout/stderr so the logs are visible.
if [ -e /proc/1/fd/1 ]; then
    exec >/proc/1/fd/1 2>/proc/1/fd/2
fi

AUTHENTIK_URL="${AUTHENTIK_URL:-http://localhost:9000}"
AUTHENTIK_TOKEN="${AUTHENTIK_TOKEN:-${AUTHENTIK_BOOTSTRAP_TOKEN:-authentik-bootstrap-token}}"
APP_SLUG="dead-mans-switch"
MAX_WAIT=180  # seconds

# Helper: parse JSON with python3 (works in Authentik container and on macOS/Linux)
json_get() {
    python3 -c "import sys,json; data=json.loads(sys.stdin.read()); print(data$1)"
}

# ── Wait for Authentik to become healthy ──────────────────────────────
echo "⏳ Waiting for Authentik to become ready at ${AUTHENTIK_URL}..."
elapsed=0
until curl -sf "${AUTHENTIK_URL}/-/health/live/" > /dev/null 2>&1; do
    if [ "$elapsed" -ge "$MAX_WAIT" ]; then
        echo "Timed out waiting for Authentik after ${MAX_WAIT}s"
        exit 1
    fi
    sleep 5
    elapsed=$((elapsed + 5))
    echo "   ...still waiting (${elapsed}s)"
done
echo "Authentik is ready"

# Wait for the blueprint to be applied (provider must exist)
echo "⏳ Waiting for blueprint to create the OAuth2 provider..."
elapsed=0
until curl -sf \
    -H "Authorization: Bearer ${AUTHENTIK_TOKEN}" \
    "${AUTHENTIK_URL}/api/v3/providers/oauth2/?search=Dead+Man" 2>/dev/null \
    | python3 -c "import sys,json; r=json.loads(sys.stdin.read()); exit(0 if len(r.get('results',[])) > 0 else 1)" 2>/dev/null; do
    if [ "$elapsed" -ge "$MAX_WAIT" ]; then
        echo "Timed out waiting for OAuth2 provider to be created by blueprint"
        echo "Check Authentik logs: docker compose -f docker-compose.authentik.yaml logs authentik-worker"
        exit 1
    fi
    sleep 5
    elapsed=$((elapsed + 5))
    echo "   ...still waiting (${elapsed}s)"
done
echo "OAuth2 provider found"

# Fetch provider details
PROVIDER_JSON=$(curl -sf \
    -H "Authorization: Bearer ${AUTHENTIK_TOKEN}" \
    "${AUTHENTIK_URL}/api/v3/providers/oauth2/?search=Dead+Man")

CLIENT_ID=$(echo "$PROVIDER_JSON" | json_get "['results'][0]['client_id']")
CLIENT_SECRET=$(echo "$PROVIDER_JSON" | json_get "['results'][0]['client_secret']")

if [ -z "$CLIENT_ID" ] || [ "$CLIENT_ID" = "None" ]; then
    echo "Failed to retrieve client_id from provider"
    exit 1
fi

ISSUER_URL="${AUTHENTIK_URL}/application/o/${APP_SLUG}/"

# Print results
cat <<EOF

═══════════════════════════════════════════════════════════════
  Authentik OAuth2 Configuration
═══════════════════════════════════════════════════════════════
  Client ID:     ${CLIENT_ID}
  Client Secret: ${CLIENT_SECRET}
  Issuer URL:    ${ISSUER_URL}
═══════════════════════════════════════════════════════════════

Start dead-mans-switch with:

  go run . server \\
    --auth-enabled \\
    --auth-issuer-url "${ISSUER_URL}"

And get a token with:

  curl -X POST ${AUTHENTIK_URL}/application/o/token/ \\
    -d grant_type=client_credentials \\
    -d client_id=${CLIENT_ID} \\
    -d client_secret=${CLIENT_SECRET} | jq -r .access_token

EOF

# Save creds
if [ -n "${CREDENTIALS_FILE:-}" ]; then
    cat > "$CREDENTIALS_FILE" <<EOF
CLIENT_ID=${CLIENT_ID}
CLIENT_SECRET=${CLIENT_SECRET}
ISSUER_URL=${ISSUER_URL}
EOF
    echo "Credentials written to ${CREDENTIALS_FILE}"
fi
