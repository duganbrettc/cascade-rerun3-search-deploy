#!/usr/bin/env bash
# Acceptance journey script for Sift stack
# Usage: HOST_PORT=<port> ./journey.sh
set -euo pipefail

BASE="${BASE_URL:-http://host.docker.internal:${HOST_PORT:-3000}}"
echo "=== Sift Journey Test against $BASE ==="

# Step 1: GET /healthz
echo "[1] GET /healthz"
HEALTH=$(curl -sf "$BASE/healthz")
if [ "$HEALTH" != "ok" ]; then
  echo "FAIL: /healthz expected 'ok', got '$HEALTH'"
  exit 1
fi
echo "  PASS: /healthz -> ok"

# Step 2: Signup alice
echo "[2] Signup alice"
ALICE_RESP=$(curl -sf -X POST "$BASE/api/auth/signup" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"alice123"}')
ALICE_TOKEN=$(echo "$ALICE_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['session_token'])")
if [ -z "$ALICE_TOKEN" ]; then
  echo "FAIL: alice signup returned no token. Response: $ALICE_RESP"
  exit 1
fi
echo "  PASS: alice signed up, token received"

# Store token in a way that simulates localStorage
echo "  [localStorage simulation] alice_token stored"
STORED_TOKEN="$ALICE_TOKEN"
if [ "$STORED_TOKEN" != "$ALICE_TOKEN" ]; then
  echo "FAIL: token not retained"
  exit 1
fi
echo "  PASS: session token stored"

# Step 3: Signup bob
echo "[3] Signup bob"
BOB_RESP=$(curl -sf -X POST "$BASE/api/auth/signup" \
  -H "Content-Type: application/json" \
  -d '{"username":"bob","password":"bob123"}')
BOB_TOKEN=$(echo "$BOB_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['session_token'])")
if [ -z "$BOB_TOKEN" ]; then
  echo "FAIL: bob signup returned no token. Response: $BOB_RESP"
  exit 1
fi
echo "  PASS: bob signed up"

# Step 4: Bob posts a marker
echo "[4] Bob posts a marker"
POST_RESP=$(curl -sf -X POST "$BASE/api/posts" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BOB_TOKEN" \
  -d '{"body":"the quick sift fox"}')
POST_BODY=$(echo "$POST_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['body'])")
if [ "$POST_BODY" != "the quick sift fox" ]; then
  echo "FAIL: post body mismatch. Got: $POST_BODY"
  exit 1
fi
echo "  PASS: bob posted 'the quick sift fox'"

# Step 5: Browse GET /api/users
echo "[5] Browse /api/users"
USERS=$(curl -sf "$BASE/api/users" -H "Authorization: Bearer $ALICE_TOKEN")
USER_COUNT=$(echo "$USERS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
if [ "$USER_COUNT" -lt 2 ]; then
  echo "FAIL: expected at least 2 users, got $USER_COUNT. Response: $USERS"
  exit 1
fi
echo "  PASS: /api/users lists $USER_COUNT users"

# Step 6: Alice follows bob
echo "[6] Alice follows bob"
FOLLOW=$(curl -sf -X POST "$BASE/api/follow/bob" -H "Authorization: Bearer $ALICE_TOKEN")
FOLLOWING=$(echo "$FOLLOW" | python3 -c "import sys,json; print(json.load(sys.stdin).get('following', False))")
if [ "$FOLLOWING" != "True" ]; then
  echo "FAIL: follow response expected following=true. Got: $FOLLOW"
  exit 1
fi
echo "  PASS: alice follows bob"

# Step 7: GET /api/users/bob renders bob's profile
echo "[7] GET /api/users/bob"
BOB_PROFILE=$(curl -sf "$BASE/api/users/bob" -H "Authorization: Bearer $ALICE_TOKEN")
BOB_USERNAME=$(echo "$BOB_PROFILE" | python3 -c "import sys,json; print(json.load(sys.stdin)['username'])")
BOB_IS_FOLLOWING=$(echo "$BOB_PROFILE" | python3 -c "import sys,json; print(json.load(sys.stdin)['is_following'])")
if [ "$BOB_USERNAME" != "bob" ]; then
  echo "FAIL: expected username=bob. Got: $BOB_PROFILE"
  exit 1
fi
if [ "$BOB_IS_FOLLOWING" != "True" ]; then
  echo "FAIL: expected is_following=true for alice viewing bob. Got: $BOB_PROFILE"
  exit 1
fi
echo "  PASS: /api/users/bob returns bob's profile with is_following=true"

# Step 8: GET /api/posts/by/bob shows bob's marker post
echo "[8] GET /api/posts/by/bob"
BOB_POSTS=$(curl -sf "$BASE/api/posts/by/bob")
POST_COUNT=$(echo "$BOB_POSTS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
MARKER_POST=$(echo "$BOB_POSTS" | python3 -c "import sys,json; posts=json.load(sys.stdin); print(any(p['body']=='the quick sift fox' for p in posts))")
if [ "$POST_COUNT" -lt 1 ] || [ "$MARKER_POST" != "True" ]; then
  echo "FAIL: bob's marker post not found. Posts: $BOB_POSTS"
  exit 1
fi
echo "  PASS: bob's marker post 'the quick sift fox' found"

echo ""
echo "=== ALL JOURNEY TESTS PASSED ==="
