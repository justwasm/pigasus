#!/usr/bin/env bash
# ==============================================================================
# scripts/canary-pipeline.sh — Upstream Autopilot & Drift Defense Pipeline
# ==============================================================================
# Integrates:
#   1. agy-canary (SHA-256, version drift, RPC methods, embed assets, tools)
#   2. embedextract (Embedded Go asset tree and frontend ZIP verification)
#   3. wirealign (Wire-level parity & prompt alignment test suite)
#
# Designed for automated execution in daily CI / cron environments.
# ==============================================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# Defaults
AGY_BIN="${AGY_BIN:-${AGY_REF:-$HOME/.local/bin/agy}}"
CHECK_MODE=0
JSON_MODE=0
UPDATE_MODE=0
PASS=1

print_usage() {
  cat <<EOF
Usage: scripts/canary-pipeline.sh [options]

Options:
  --bin PATH       Path to upstream agy binary (default: $AGY_BIN)
  --check          Enforce zero drift: exit with code 1 if upstream drift is detected
  --json           Output agy-canary report in machine-readable JSON format
  --update         Auto-update reference.lock.json digests from current disk fixtures
  --help, -h       Show this help message

Environment variables:
  AGY_BIN, AGY_REF  Path to reference agy binary
EOF
}

CANARY_ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin)
      AGY_BIN="$2"
      CANARY_ARGS+=("-bin" "$2")
      shift 2
      ;;
    --bin=*)
      AGY_BIN="${1#*=}"
      CANARY_ARGS+=("-bin" "$AGY_BIN")
      shift
      ;;
    --check)
      CHECK_MODE=1
      CANARY_ARGS+=("-check")
      shift
      ;;
    --json)
      JSON_MODE=1
      CANARY_ARGS+=("-json")
      shift
      ;;
    --update)
      UPDATE_MODE=1
      CANARY_ARGS+=("-update")
      shift
      ;;
    --help|-h)
      print_usage
      exit 0
      ;;
    *)
      CANARY_ARGS+=("$1")
      shift
      ;;
  esac
done

echo "========================================================================"
echo "         OpenAGY Upstream Autopilot & Drift Defense Pipeline"
echo "========================================================================"
echo "Root Dir:    $ROOT_DIR"
echo "Target Bin:  $AGY_BIN"
echo "Check Mode:  $CHECK_MODE"
echo "JSON Mode:   $JSON_MODE"
echo "========================================================================"
echo ""

# ── Stage 1: agy-canary ───────────────────────────────────────────────────────
echo "==> [Stage 1/3] Running agy-canary (upstream binary & lock integrity)..."
set +e
go run "$ROOT_DIR/cmd/agy-canary" -repo "$ROOT_DIR" "${CANARY_ARGS[@]}"
CANARY_STATUS=$?
set -e

if [[ $CANARY_STATUS -ne 0 ]]; then
  echo "--> [Stage 1: FAILED] agy-canary reported drift or error (exit code $CANARY_STATUS)"
  if [[ $CHECK_MODE -eq 1 ]]; then
    PASS=0
  fi
else
  echo "--> [Stage 1: PASSED] agy-canary completed successfully."
fi
echo ""

# ── Stage 2: embedextract ─────────────────────────────────────────────────────
echo "==> [Stage 2/3] Running embedextract (static assets & embedfs check)..."
if [[ -f "$AGY_BIN" && -x "$AGY_BIN" ]]; then
  set +e
  go run "$ROOT_DIR/devutils/embedextract" -bin "$AGY_BIN" -list
  EMBED_STATUS=$?
  set -e
  if [[ $EMBED_STATUS -ne 0 ]]; then
    echo "--> [Stage 2: FAILED] embedextract encountered an error (exit code $EMBED_STATUS)"
    PASS=0
  else
    echo "--> [Stage 2: PASSED] embedextract successfully verified binary asset systems."
  fi
else
  echo "--> [Stage 2: SKIPPED] Reference binary not found at $AGY_BIN; skipping live embedextract scan."
fi
echo ""

# ── Stage 3: wirealign ────────────────────────────────────────────────────────
echo "==> [Stage 3/3] Running wirealign (wire-level parity & prompt alignment)..."
set +e
"$ROOT_DIR/scripts/wire-align.sh" test
WIRE_STATUS=$?
set -e

if [[ $WIRE_STATUS -ne 0 ]]; then
  echo "--> [Stage 3: FAILED] wirealign parity scenarios failed (exit code $WIRE_STATUS)"
  PASS=0
else
  echo "--> [Stage 3: PASSED] wirealign all scenarios verified."
fi
echo ""

# ── Pipeline Summary ──────────────────────────────────────────────────────────
echo "========================================================================"
if [[ $PASS -eq 1 ]]; then
  echo "  [PIPELINE SUCCESS] All defense stages completed successfully!"
  echo "========================================================================"
  exit 0
else
  echo "  [PIPELINE FAILURE] One or more defense stages detected drift or errors."
  echo "========================================================================"
  exit 1
fi
