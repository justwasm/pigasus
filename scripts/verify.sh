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
    mkdir -p ./bin
    go build -o ./bin/pigasus ./cmd/pigasus
    echo "==> Quick verification passed!"
    ;;

  align)
    echo "==> Running tuialign scenario DSL tests..."
    go test ./devutils/tuialign/...
    echo "==> Running wirealign wire parity scenario tests..."
    go test ./devutils/wirealign/...

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

  race)
    echo "==> Running race detector on goroutine-owning packages..."
    go test -count=1 -race \
      ./internal/app/subagent/... \
      ./internal/app/subagenttools/... \
      ./internal/app/tools/... \
      ./internal/features/tools/... \
      ./internal/features/plugins/watch/... \
      ./internal/features/auth/... \
      ./internal/rchost/... \
      ./internal/tui/stream/...
    ;;

  all|test)
    echo "==> Running architecture layering guard..."
    go test ./internal/core/architecture/...

    echo "==> Running alignment suites (tuialign & wirealign)..."
    go test ./devutils/tuialign/... ./devutils/wirealign/...

    echo "==> Running cmd & public package suite..."
    go test -count=1 -timeout 10m \
      ./cmd/... ./endpoints/... ./configdir/... ./render/... ./diffsyntax/... \
      ./palette/... ./retry/... ./retrybudget/... ./procgroup/... ./browsercdp/... \
      ./celpolicy/... ./dns/... ./rcmesh/... ./remotetransport/... ./rcterminal/... \
      ./urlchunk/... ./geminicode/... ./accountpool/... ./agentlink/... \
      ./localharness/... ./herdr/...

    echo "==> Running internal tier suites (core, features, infra, app, ui)..."
    go test -count=1 -timeout 10m \
      ./internal/core/... ./internal/features/... ./internal/infra/... \
      ./internal/app/... ./internal/ui/...

    echo "==> Running entry-host & remote-control suites..."
    go test -count=1 -timeout 10m \
      ./internal/printmode/... ./internal/cli/... ./internal/clirunner/... \
      ./internal/modular/... ./internal/cortex/... ./internal/lsserver/... \
      ./internal/agyupdate/... ./internal/voice/... ./internal/language_server/... \
      ./internal/planrender/... ./internal/remotecontrol/... ./internal/rchost/... \
      ./internal/rcaudio/... ./internal/rcfrontend/... ./internal/rctunnel/...

    echo "==> Running TUI host & subpackage suites..."
    go test -count=1 -timeout 10m ./internal/tui -run "^Test[A-M]"
    go test -count=1 -timeout 10m ./internal/tui -run "^Test[N-R]"
    go test -count=1 -timeout 10m ./internal/tui -run "^TestS"
    go test -count=1 -timeout 10m ./internal/tui -run "^Test[T-Z]"
    go test -count=1 -timeout 10m ./internal/tui -run "^Test_"
    go test -count=1 -timeout 10m ./internal/tui/commands/... ./internal/tui/panels/... ./internal/tui/stream/...

    echo "==> Verifying main binary build..."
    mkdir -p ./bin
    go build -o ./bin/pigasus ./cmd/pigasus

    echo "==> All verification checks passed!"
    ;;

  *)
    echo "Unknown mode: $MODE"
    echo "Usage: $0 [arch|quick|all|align|race]"
    exit 1
    ;;
esac
