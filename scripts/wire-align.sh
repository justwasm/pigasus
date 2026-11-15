#!/usr/bin/env bash
# ==============================================================================
# scripts/wire-align.sh — Top-level wrapper for wirealign
# ==============================================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

exec go run "$ROOT_DIR/devutils/wirealign" "$@"
