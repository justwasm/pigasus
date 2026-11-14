#!/usr/bin/env bash
# scripts/verify.sh - Safe, standard test and verification runner for openagy
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

MODE="${1:-all}"

echo "==> [verify] Mode: $MODE"

case "$MODE" in
  arch)
    echo "==> Running architecture layering guard..."
    go test -v ./internal/core/architecture/...
    ;;

  quick)
    echo "==> Running architecture guard & core packages..."
    go test ./internal/core/architecture/...
    go test ./endpoints/... ./configdir/... ./render/... ./palette/...
    echo "==> Verifying main build..."
    go build -o /tmp/pigasus ./cmd/pigasus
    echo "==> Quick verification passed!"
    ;;

  align)
    echo "==> Running tuialign scenario DSL tests..."
    go test ./devutils/tuialign/...

    # The live half needs the reference binary and tmux; it is skipped
    # rather than failed on a machine that has neither, because the
    # scenario's textual gates (EXPECT_TEXT / ASSERT_NOT_TEXT) are the
    # point and they run against pigasus.
    agy_bin="${AGY_REAL_BIN:-$HOME/.local/bin/agy}"
    if [ "${RUN_LIVE_ALIGN:-1}" = "1" ] && command -v tmux >/dev/null 2>&1 && [ -x "$agy_bin" ]; then
      echo "==> Running the live alignment scenario against $agy_bin..."
      ./scripts/tui-align.sh run devutils/tuialign/scenarios/26_tool_output_preformatted.align
    else
      echo "==> Skipping the live scenario (no tmux or no reference agy; set AGY_REAL_BIN, or RUN_LIVE_ALIGN=0)"
    fi
    ;;

  all|test)
    echo "==> Running architecture layering guard..."
    go test ./internal/core/architecture/...

    echo "==> Running standard package suite (excluding scratch/ & uncompiled dev tools)..."
    go test ./cmd/... ./endpoints/... ./configdir/... ./render/... ./diffsyntax/... ./palette/... ./retry/...

    echo "==> Running internal core and feature tests..."
    go test ./internal/core/... ./internal/features/... ./internal/infra/...

    echo "==> Verifying main binary build..."
    go build -o /tmp/pigasus ./cmd/pigasus

    echo "==> All verification checks passed!"
    ;;

  *)
    echo "Unknown mode: $MODE"
    echo "Usage: $0 [arch|quick|all|align]"
    exit 1
    ;;
esac
