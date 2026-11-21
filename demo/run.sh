#!/usr/bin/env bash
# Run script for Pigasus Google OAuth & LiveChat Web Demo
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

PORT="${PORT:-8090}"
PROXY="${OPENAGY_VOICE_ALT_BACKEND:-http://127.0.0.1:8085}"

echo "=========================================================="
echo " Starting Pigasus Google OAuth & Multimodal LiveChat Demo"
echo " Repo root:   ${REPO_ROOT}"
echo " Web Port:    ${PORT}"
echo " Proxy Route: ${PROXY}"
echo "=========================================================="

cd "${REPO_ROOT}"
go run ./cmd/livechat --web -port "${PORT}" -proxy "${PROXY}"
