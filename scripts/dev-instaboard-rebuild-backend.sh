#!/usr/bin/env bash
# Rebuild Instaboard Go API (no cache) and restart on API_PORT.
# Stops any process already listening on the port first.
#
# Layout: monorepo — this script lives in instaboard/scripts/; backend is ../backend.
#
# Environment:
#   API_PORT        (default: 8788)
#   FRONTEND_PORT   (default: 5200 — used for CORS)
#   DEV_DIR         (default: <parent of instaboard>/.dev)
#
# Optional: backend/.env is loaded by the binary (godotenv). This script also
# exports PORT / CORS_ORIGIN so local ports stay consistent.

set -eo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMBINED_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

BACKEND_DIR="$REPO_ROOT/backend"
DEV_DIR="${DEV_DIR:-$COMBINED_ROOT/.dev}"
LOG_DIR="$DEV_DIR/logs"

API_PORT="${API_PORT:-8788}"
FRONTEND_PORT="${FRONTEND_PORT:-5200}"

BINARY_NAME="instaboard-backend"
TOTAL_STEPS=4

step() {
  local num=$1
  local title=$2
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  printf "  INSTABOARD BACKEND  STEP %s/%s — %s\n" "$num" "$TOTAL_STEPS" "$title"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

die() {
  echo "" >&2
  echo "ERROR: $*" >&2
  exit 1
}

wait_for_listen() {
  local port=$1
  local label=$2
  local log_file=$3
  local max_wait=${4:-120}
  local waited=0
  while (( waited < max_wait )); do
    if lsof -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    (( waited += 1 )) || true
    if (( waited % 10 == 0 )); then
      echo "  … still waiting for $label on :$port (${waited}s / ${max_wait}s)"
    fi
  done
  echo "Last lines from $log_file:" >&2
  tail -40 "$log_file" >&2 || true
  die "$label did not start listening on port $port within ${max_wait}s (see $log_file)"
}

wait_for_http() {
  local url=$1
  local label=$2
  local log_file=$3
  local max_wait=${4:-30}
  local waited=0
  while (( waited < max_wait )); do
    if curl -fsS -m 2 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    (( waited += 1 )) || true
  done
  echo "Last lines from $log_file:" >&2
  tail -40 "$log_file" >&2 || true
  die "$label did not respond at $url within ${max_wait}s (see $log_file)"
}

kill_listeners_on_port() {
  local port=$1
  local name=$2
  local pids
  pids="$(lsof -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
  if [[ -n "${pids// }" ]]; then
    echo "Stopping $name on port $port (PID(s): $pids)"
    # shellcheck disable=SC2086
    kill $pids 2>/dev/null || true
    sleep 1
    pids="$(lsof -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
    if [[ -n "${pids// }" ]]; then
      echo "Force killing remaining PID(s): $pids"
      # shellcheck disable=SC2086
      kill -9 $pids 2>/dev/null || true
      sleep 1
    fi
  else
    echo "Nothing listening on port $port ($name)."
  fi
}

kill_stale_pid_file() {
  local pid_file=$1
  local name=$2
  if [[ -f "$pid_file" ]]; then
    local old_pid
    old_pid="$(cat "$pid_file" 2>/dev/null || true)"
    if [[ -n "$old_pid" ]] && kill -0 "$old_pid" 2>/dev/null; then
      echo "Stopping stale $name (PID $old_pid from $pid_file)"
      kill "$old_pid" 2>/dev/null || true
      sleep 1
      if kill -0 "$old_pid" 2>/dev/null; then
        kill -9 "$old_pid" 2>/dev/null || true
      fi
    fi
  fi
}

# Start a process in a new session so it survives when the parent shell/agent exits.
# Usage: start_detached <pidfile> <logfile> <cwd> <cmd> [args...]
start_detached() {
  local pid_file=$1
  local log_file=$2
  local work_dir=$3
  shift 3
  python3 - "$pid_file" "$log_file" "$work_dir" "$@" <<'PY'
import os, subprocess, sys
pid_file, log_file, work_dir, cmd = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4:]
os.makedirs(os.path.dirname(pid_file) or ".", exist_ok=True)
os.makedirs(os.path.dirname(log_file) or ".", exist_ok=True)
log = open(log_file, "a", buffering=1)
proc = subprocess.Popen(
    cmd,
    cwd=work_dir,
    stdin=subprocess.DEVNULL,
    stdout=log,
    stderr=subprocess.STDOUT,
    start_new_session=True,
    env=os.environ.copy(),
)
with open(pid_file, "w", encoding="utf-8") as f:
    f.write(str(proc.pid))
PY
}

[[ -d "$BACKEND_DIR" ]] || die "Backend directory not found: $BACKEND_DIR"
[[ -d "$BACKEND_DIR/cmd/api" ]] || die "Expected Go module at $BACKEND_DIR (missing cmd/api)"
mkdir -p "$LOG_DIR"

echo ""
echo "Repo:            $REPO_ROOT"
echo "API port:        $API_PORT"
echo "Frontend port:   $FRONTEND_PORT (CORS)"
echo "Dev logs:        $LOG_DIR/instaboard-backend.log"

step 1 "Stop existing Instaboard API (if running)"
kill_stale_pid_file "$DEV_DIR/instaboard-backend.pid" "Instaboard API"
kill_listeners_on_port "$API_PORT" "Instaboard API"

step 2 "Go — clean build cache and compile (no incremental cache)"
(
  cd "$BACKEND_DIR"
  go clean -cache
  go build -a -o "$DEV_DIR/${BINARY_NAME}" ./cmd/api
)
echo "Go build OK → $DEV_DIR/${BINARY_NAME}"

step 3 "Export local port / CORS overrides"
export PORT="$API_PORT"
export HOST="${HOST:-127.0.0.1}"
export CORS_ORIGIN="http://localhost:$FRONTEND_PORT"
export BACKEND_PUBLIC_URL="http://localhost:$API_PORT"
if [[ -f "$BACKEND_DIR/.env" ]]; then
  echo "  Env:    $BACKEND_DIR/.env (binary also loads it via godotenv)"
else
  echo "  Env:    defaults (optional backend/.env not present)"
fi

step 4 "Start API in background (logs: $LOG_DIR/instaboard-backend.log)"
: >"$LOG_DIR/instaboard-backend.log"
start_detached "$DEV_DIR/instaboard-backend.pid" "$LOG_DIR/instaboard-backend.log" "$BACKEND_DIR" \
  "$DEV_DIR/${BINARY_NAME}"
wait_for_listen "$API_PORT" "Instaboard API" "$LOG_DIR/instaboard-backend.log" 30
wait_for_http "http://127.0.0.1:$API_PORT/health" "Instaboard API health" "$LOG_DIR/instaboard-backend.log" 30
echo "Instaboard API listening on :$API_PORT (PID $(cat "$DEV_DIR/instaboard-backend.pid"))"
echo "  Health:  curl -sS \"http://127.0.0.1:$API_PORT/health\""

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  INSTABOARD BACKEND DONE — $TOTAL_STEPS steps completed."
echo "  Log:  tail -f \"$LOG_DIR/instaboard-backend.log\""
echo "  Stop: kill \$(lsof -t -iTCP:$API_PORT -sTCP:LISTEN)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
