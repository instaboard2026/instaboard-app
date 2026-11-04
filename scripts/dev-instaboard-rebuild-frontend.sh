#!/usr/bin/env bash
# Rebuild Angular app (instaboard/frontend) with no cache and restart ng serve.
# Stops any process already listening on FRONTEND_PORT first.
#
# IMPORTANT — most code edits do NOT need this script. The Angular dev
# server has HMR (Hot Module Replacement) enabled, so changes to
# .ts / .html / .css under src/ are pushed live to the browser without a
# rebuild. See the banner printed at startup for "when to actually run me".
#
# Layout: monorepo — this script lives in instaboard/scripts/; frontend is ../frontend.
# Override: FRONTEND_DIR=/path/to/frontend
#
# Environment:
#   FRONTEND_PORT  (default: 5200, matches angular.json)
#   FRONTEND_DIR
#   DEV_DIR        (default: <parent of instaboard>/.dev)
#   QUICK=1        Skip the node_modules / .angular wipe and npm ci. Just
#                  kill the running ng serve and start a fresh one. Use
#                  when HMR seems wedged but deps have not changed.
#                  Roughly 15s vs 3-5m for the full rebuild.

set -eo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMBINED_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

FRONTEND_DIR="${FRONTEND_DIR:-$REPO_ROOT/frontend}"
DEV_DIR="${DEV_DIR:-$COMBINED_ROOT/.dev}"
LOG_DIR="$DEV_DIR/logs"

FRONTEND_PORT="${FRONTEND_PORT:-5200}"
QUICK="${QUICK:-0}"

if [[ "$QUICK" == "1" ]]; then
  TOTAL_STEPS=2
else
  TOTAL_STEPS=5
fi

print_usage_banner() {
  cat <<BANNER

╔══════════════════════════════════════════════════════════════════════════╗
║  Instaboard frontend rebuild — do you actually need to run this?          ║
╚══════════════════════════════════════════════════════════════════════════╝

  Angular dev server has HMR enabled. For 95% of edits, your changes are
  ALREADY live in the browser — no rebuild needed. Watch the dev log
  ($LOG_DIR/instaboard-frontend.log); when you see one of:

      Component update sent to client(s).
      Page reload sent to client(s).

  the latest code is already running. You can stop here.

  Run the FULL rebuild (default mode, ~3-5 min) when:
    - package.json / package-lock.json changed (deps added or upgraded)
    - angular.json / tsconfig*.json / build config changed
    - environments/*.ts changed and HMR did not pick it up
    - You see "Cannot find module" / stale-chunk errors that will not clear
    - Dev server has been up for days and is acting weird
    - Node or npm version changed

  Run with QUICK=1 (kill + restart ng serve only, ~15s) when:
    - HMR seems wedged but deps have not changed
    - You just want a clean ng serve process

      QUICK=1 ./instaboard/scripts/dev-instaboard-rebuild-frontend.sh

  None of those? You probably do not need this script — Ctrl-C now.

  Mode for this run:  $(if [[ "$QUICK" == "1" ]]; then echo "QUICK (no wipe)"; else echo "FULL rebuild (wipe + npm ci + build)"; fi)

BANNER
}

print_usage_banner

step() {
  local num=$1
  local title=$2
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  printf "  INSTABOARD FRONTEND  STEP %s/%s — %s\n" "$num" "$TOTAL_STEPS" "$title"
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
  local max_wait=${4:-120}
  local waited=0
  while (( waited < max_wait )); do
    if curl -fsS -m 2 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    (( waited += 1 )) || true
    if (( waited % 10 == 0 )); then
      echo "  … still waiting for $label (${waited}s / ${max_wait}s)"
    fi
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

[[ -d "$FRONTEND_DIR" ]] || die "Frontend directory not found: $FRONTEND_DIR (set FRONTEND_DIR)"
[[ -f "$FRONTEND_DIR/angular.json" ]] || die "Not an Angular project: $FRONTEND_DIR"
mkdir -p "$LOG_DIR"

echo ""
echo "Frontend:  $FRONTEND_DIR"
echo "Dev port:  $FRONTEND_PORT"
echo "Logs:      $LOG_DIR/instaboard-frontend.log"

step 1 "Stop existing Angular dev server (if running)"
kill_stale_pid_file "$DEV_DIR/instaboard-frontend.pid" "Angular dev server"
kill_listeners_on_port "$FRONTEND_PORT" "Angular"

if [[ "$QUICK" != "1" ]]; then
  step 2 "Remove node_modules, .angular, dist"
  rm -rf "$FRONTEND_DIR/node_modules" "$FRONTEND_DIR/.angular" "$FRONTEND_DIR/dist"

  step 3 "npm — clean cache and fresh install (npm ci)"
  (
    cd "$FRONTEND_DIR"
    npm cache clean --force
    npm ci
  )

  step 4 "Compile check (ng build development)"
  (
    cd "$FRONTEND_DIR"
    npx ng build --configuration=development
  )
  echo "Angular build OK."

  step 5 "Start Angular dev server in background (logs: $LOG_DIR/instaboard-frontend.log)"
else
  step 2 "Start Angular dev server in background — QUICK mode, no wipe (logs: $LOG_DIR/instaboard-frontend.log)"
  echo "  Skipping wipe + npm ci + ng build (QUICK=1). If you hit stale-module"
  echo "  errors after this, re-run without QUICK=1 for the full rebuild."
fi

: >"$LOG_DIR/instaboard-frontend.log"
start_detached "$DEV_DIR/instaboard-frontend.pid" "$LOG_DIR/instaboard-frontend.log" "$FRONTEND_DIR" \
  npx ng serve --configuration=development --poll=2000 --port "$FRONTEND_PORT" --host 127.0.0.1
wait_for_listen "$FRONTEND_PORT" "Angular dev server" "$LOG_DIR/instaboard-frontend.log" 120
wait_for_http "http://127.0.0.1:$FRONTEND_PORT/" "Angular dev server" "$LOG_DIR/instaboard-frontend.log" 120
echo "Instaboard frontend dev server: http://localhost:$FRONTEND_PORT/ (PID $(cat "$DEV_DIR/instaboard-frontend.pid"))"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  INSTABOARD FRONTEND DONE — $TOTAL_STEPS steps completed."
echo "  Log:  tail -f \"$LOG_DIR/instaboard-frontend.log\""
echo "  Stop: kill \$(lsof -t -iTCP:$FRONTEND_PORT -sTCP:LISTEN)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
