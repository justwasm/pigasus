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
    echo "Usage: $0 [arch|quick|all]"
    exit 1
    ;;
esac
