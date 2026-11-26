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
    echo "==> Running fast go vet on core/public & live packages..."
    go vet ./endpoints/... ./configdir/... ./render/... ./palette/... \
      ./internal/core/architecture/... ./internal/voice/... \
      ./internal/infra/livevoice/... ./internal/ui/livechat/... ./internal/app/tools/web/...
    echo "==> Verifying main build..."
    mkdir -p ./bin
    go build -o ./bin/pigasus ./cmd/pigasus
    echo "==> Quick verification passed!"
    ;;

  hygiene)
    echo "==> Running go vet across entire repository..."
    go vet ./...
    echo "==> Checking for oversized non-generated production Go files (> 1500 lines, non-blocking)..."
    oversized_prod="$(find . -name '*.go' \
      -not -path './out/*' -not -path './scratch/*' -not -path './.git/*' -not -path './node_modules/*' \
      -not -name '*.pb.go' -not -name '*_test.go' \
      -exec wc -l {} + | awk '$1 > 1500 && $2 != "total" {print $1, $2}' | sort -rn || true)"
    if [ -n "$oversized_prod" ]; then
      echo "WARNING: non-generated production .go files exceeding 1500 lines:"
      echo "$oversized_prod" | sed 's/^/  /'
    else
      echo "==> No oversized non-generated production .go files found."
    fi
    echo "==> Checking for oversized non-generated test Go files (> 1500 lines, non-blocking)..."
    oversized_test="$(find . -name '*_test.go' \
      -not -path './out/*' -not -path './scratch/*' -not -path './.git/*' -not -path './node_modules/*' \
      -exec wc -l {} + | awk '$1 > 1500 && $2 != "total" {print $1, $2}' | sort -rn || true)"
    if [ -n "$oversized_test" ]; then
      echo "INFO: non-generated *_test.go files exceeding 1500 lines:"
      echo "$oversized_test" | sed 's/^/  /'
    else
      echo "==> No oversized non-generated *_test.go files found."
    fi
    echo "==> Running Go AST hygiene analyzer and reviewed modularity ratchet..."
    go run ./devutils/hygiene -ratchet devutils/hygiene/modularity-baseline.json
    echo "==> Hygiene check completed!"
    ;;

  scenarios)
    # Independent domain/state-machine scenarios first; no server, PTY,
    # credentials, or real network. Do not run the whole Storybook catalog:
    # legacy workspace stories still start live shells and belong to integration.
    echo "==> Running independently constructible module scenarios..."
    go test -count=1 ./devutils/hygiene/... ./internal/core/architecture/... \
      ./internal/features/plugins/validate/... ./internal/infra/agyproxy/codec/... \
      ./internal/ui/editor/selection/... ./internal/remotecontrol/files/...
    echo "==> Running real-module Storybook selection interaction..."
    go test -count=1 ./internal/ui/storybook -run '^TestInputSelectionStoryScenarios$'
    echo "==> Enforcing reviewed legacy-package ceilings..."
    go run ./devutils/hygiene -ratchet devutils/hygiene/modularity-baseline.json
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
    echo "Usage: $0 [arch|quick|hygiene|scenarios|all|align|race]"
    exit 1
    ;;
esac
