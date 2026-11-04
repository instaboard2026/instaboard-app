#!/usr/bin/env bash
# Full stack: rebuild Instaboard backend, then frontend (no cache), restart both dev servers.
# Runs scripts next to this file: dev-instaboard-rebuild-backend.sh, dev-instaboard-rebuild-frontend.sh
#
# Environment (same as those scripts):
#   API_PORT        default 8788
#   FRONTEND_PORT   default 5200
#   FRONTEND_DIR    optional path to frontend
#   DEV_DIR         default: parent-of-instaboard/.dev
#   QUICK=1         passed through to frontend script (skip npm ci / wipe)

set -eo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMBINED_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DEV_DIR="${DEV_DIR:-$COMBINED_ROOT/.dev}"
LOG_DIR="$DEV_DIR/logs"

export API_PORT="${API_PORT:-8788}"
export FRONTEND_PORT="${FRONTEND_PORT:-5200}"
export QUICK="${QUICK:-0}"

mkdir -p "$LOG_DIR"

echo ""
echo "╔══════════════════════════════════════════════════════════════════════════╗"
echo "║  Instaboard full rebuild: backend (cmd/api) → frontend (Angular)        ║"
echo "╚══════════════════════════════════════════════════════════════════════════╝"
echo ""
echo "API port:        $API_PORT"
echo "Frontend port:   $FRONTEND_PORT"
echo "Dev logs:        $LOG_DIR/"

"$SCRIPT_DIR/dev-instaboard-rebuild-backend.sh"
"$SCRIPT_DIR/dev-instaboard-rebuild-frontend.sh"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  ALL DONE — Instaboard backend and frontend dev servers"
echo "  API:      http://127.0.0.1:$API_PORT/  (GET /health)"
echo "  Angular:  http://localhost:$FRONTEND_PORT/"
echo "  Verify:   curl -fsS \"http://127.0.0.1:$API_PORT/health\""
echo "            curl -fsS -o /dev/null -w '%{http_code}' \"http://127.0.0.1:$FRONTEND_PORT/\""
echo "  Logs:     tail -f \"$LOG_DIR/instaboard-backend.log\""
echo "            tail -f \"$LOG_DIR/instaboard-frontend.log\""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
