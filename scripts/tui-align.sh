#!/usr/bin/env bash
# ==============================================================================
# scripts/tui-align.sh — Top-level wrapper for tuialign
# ==============================================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Ensure pigasus is built
if [ ! -f "/tmp/pigasus" ] || [ "${FORCE_BUILD:-0}" = "1" ]; then
  echo "==> Building /tmp/pigasus..."
  (cd "$ROOT_DIR" && go build -o /tmp/pigasus ./cmd/pigasus)
fi

exec go run "$ROOT_DIR/devutils/tuialign" "$@"
