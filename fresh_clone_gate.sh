#!/usr/bin/env bash
# Pre-completion fresh-clone gate for Sift deployment.
# Clones cascade-rerun3-search-deploy origin/main into a temp dir (which includes
# the pre-committed ./db ./api ./web sibling contents), builds the stack, and runs
# journey + search acceptance scripts.
set -euo pipefail

DEPLOY_REPO="https://duganbrettc:${GITHUB_TOKEN:-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}@github.com/duganbrettc/cascade-rerun3-search-deploy.git"
GATE_PORT="${GATE_PORT:-9410}"
PROJECT_NAME="freshgate$$"
TMPDIR=$(mktemp -d)

cleanup() {
  echo "--- Cleanup: stopping containers and removing temp dir ---"
  cd "$TMPDIR/repo" 2>/dev/null && \
    HOST_PORT=$GATE_PORT docker compose -p "$PROJECT_NAME" down -v 2>/dev/null || true
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

echo "=== Fresh-Clone Gate (port $GATE_PORT) ==="
echo "Working in: $TMPDIR"

# Clone fresh from origin/main
echo "[clone] cascade-rerun3-search-deploy"
git clone "$DEPLOY_REPO" "$TMPDIR/repo"

cd "$TMPDIR/repo"

echo "[build] docker compose build --no-cache web"
HOST_PORT=$GATE_PORT docker compose -p "$PROJECT_NAME" build --no-cache web

echo "[up] docker compose up --wait"
HOST_PORT=$GATE_PORT docker compose -p "$PROJECT_NAME" up --wait -d

echo "[probe] GET /healthz"
HEALTH=$(curl -sf "http://host.docker.internal:$GATE_PORT/healthz")
if [ "$HEALTH" != "ok" ]; then
  echo "FAIL: /healthz returned '$HEALTH'"
  exit 1
fi
echo "  PASS: /healthz -> ok"

echo "[run] journey.sh"
BASE_URL="http://host.docker.internal:$GATE_PORT" bash journey.sh

echo "[run] search_verify.sh"
BASE_URL="http://host.docker.internal:$GATE_PORT" bash search_verify.sh

echo ""
echo "=== FRESH-CLONE GATE PASSED ==="
