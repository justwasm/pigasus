#!/usr/bin/env bash
# ==============================================================================
# scripts/tui-align.sh — Top-level wrapper for tuialign
# ==============================================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Ensure pigasus is built
pigasus_bin="${PIGASUS_BIN:-$ROOT_DIR/bin/pigasus}"
if [ ! -f "$pigasus_bin" ] || [ "${FORCE_BUILD:-0}" = "1" ]; then
  echo "==> Building $pigasus_bin..."
  (cd "$ROOT_DIR" && mkdir -p "$(dirname "$pigasus_bin")" && go build -o "$pigasus_bin" ./cmd/pigasus)
fi

export PIGASUS_BIN="$pigasus_bin"
exec go run "$ROOT_DIR/devutils/tuialign" "$@"
