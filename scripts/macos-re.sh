#!/usr/bin/env bash
#
# scripts/macos-re.sh — Antigravity CLI reverse-engineering launch script for macOS.
#
# What this script does (in order, all guarded by feature flags):
#   1. Sources `out/local-macos/env.sh` (if present) to set up JAVA_HOME,
#      GHIDRA_INSTALL_DIR, REA_GHIDRA_SEED_FILE, and PATH for Ghidra / JDK 21.
#   2. Verifies we are actually on macOS (darwin). Refuses to run otherwise.
#   3. Verifies the toolchain (`go`, `java`, `ghidraRun`, `rea`).
#   4. Optionally re-derives pclntab seeds via `rea-go-seeds/pclnseeds`.
#   5. Optionally runs Ghidra `analyzeHeadless` in two stages
#      (① import + seed  → ② decompile) — see AGENTS.md "Commands" section.
#   6. Optionally collects Antigravity OAuth / mTLS / cloudaicompanion tokens
#      from the live `agy` binary via `strings(1)` — local artifacts only,
#      no network calls.
#
# Usage:
#   ./scripts/macos-re.sh env            — only print resolved env (dry run)
#   ./scripts/macos-re.sh doctor         — verify toolchain (no side effects)
#   ./scripts/macos-re.sh seeds          — re-derive pclntab seeds
#   ./scripts/macos-re.sh decompile      — run Ghidra headless decompile
#   ./scripts/macos-re.sh tokens         — collect antigravity tokens
#   ./scripts/macos-re.sh all            — seeds → decompile → tokens
#
# Flags:
#   --dry-run     Print what would be run, but do not execute Ghidra / seeds / tokens.
#   --agy=PATH    Override the antigravity binary path (default: ~/.local/bin/agy).
#   --ghidra-mem  Heap for analyzeHeadless (default: 8G; use 2G-4G on 16 GB Macs).
#   --no-seeds    Skip seed regeneration.
#   --no-decompile Skip Ghidra decompile stage.
#   --no-tokens   Skip token collection.
#   -h | --help   Show this header.
#
# This script does NOT push, modify, or upload anything. All artifacts stay
# under `out/local-macos/` (which is gitignored). It does NOT make network
# requests. It does NOT contact cloudaicompanion / accounts.google.com.
#
# Refs:
#   AGENTS.md                  — full toolchain walkthrough + gotchas
#   out/local-macos/env.sh     — env vars this script sources
#   out/local-macos/FINDINGS.md — what the live agy build contains
#   rea-go-seeds/README.md     — pclnseeds tool documentation

set -euo pipefail

# ---------- Args ----------
DRY_RUN=0
AGY_BIN="${HOME}/.local/bin/agy"
GHIDRA_MAXMEM="8G"
DO_SEEDS=1
DO_DECOMPILE=1
DO_TOKENS=1

usage_short() {
    sed -n '2,40p' "$0"
    exit "${1:-0}"
}

while (( $# > 0 )); do
    case "$1" in
        --dry-run)    DRY_RUN=1 ;;
        --agy=*)      AGY_BIN="${1#*=}" ;;
        --ghidra-mem) GHIDRA_MAXMEM="$2"; shift ;;
        --no-seeds)    DO_SEEDS=0 ;;
        --no-decompile) DO_DECOMPILE=0 ;;
        --no-tokens)   DO_TOKENS=0 ;;
        -h|--help)     usage_short 0 ;;
        env|doctor|seeds|decompile|tokens|all) CMD="$1" ;;
        *) echo "macos-re.sh: unknown arg: $1" >&2; usage_short 2 ;;
    esac
    shift
done

CMD="${CMD:-env}"

# ---------- Locate repo root (this script's parent parent) ----------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# ---------- Source env ----------
ENV_LOCAL="$REPO_ROOT/out/local-macos/env.sh"
if [[ -f "$ENV_LOCAL" ]]; then
    # shellcheck disable=SC1090
    source "$ENV_LOCAL"
    echo "[env] sourced $ENV_LOCAL"
else
    echo "[env] WARNING: $ENV_LOCAL not found, falling back to defaults" >&2
fi

# ---------- Platform guard ----------
if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "macos-re.sh: this script targets macOS/arm64 only." >&2
    echo "Detected: $(uname -s) $(uname -m)" >&2
    echo "On Linux/WSL use the README + CI workflow instead." >&2
    exit 1
fi
if [[ "$(uname -m)" != "arm64" ]]; then
    echo "macos-re.sh: warning — not running on arm64 (detected $(uname -m))." >&2
    echo "macOS/x86_64 is untested. Expect layout diffs in __lrodata_gopcln." >&2
fi

# ---------- Toolchain resolution ----------
resolve_toolchain() {
    : "${JAVA_HOME:=/opt/homebrew/opt/openjdk@21}"
    : "${GHIDRA_INSTALL_DIR:=$HOME/tools/ghidra_12.1.4_PUBLIC}"
    : "${REA_GHIDRA_SEED_FILE:=$REPO_ROOT/out/local-macos/seeds/seeds.txt}"
    export JAVA_HOME GHIDRA_INSTALL_DIR REA_GHIDRA_SEED_FILE
    PATH="$JAVA_HOME/bin:$PATH"
}
resolve_toolchain

# Common paths
PROJ="$REPO_ROOT/out/local-macos/proj"
SCRIPTS="$REPO_ROOT/out/local-macos/ghidra-scripts"
SEEDS_DIR="$REPO_ROOT/out/local-macos/seeds"
TOKENS_DIR="$REPO_ROOT/out/local-macos/tokens"
PCLNSEEDS_BIN="/tmp/pclnseeds"

GHIDRA_HEADLESS="$GHIDRA_INSTALL_DIR/support/analyzeHeadless"

doctor() {
    local fail=0
    echo "== Toolchain doctor =="
    for pair in \
        "go:$(command -v go 2>/dev/null || echo MISSING)" \
        "java:$JAVA_HOME/bin/java" \
        "ghidra:$GHIDRA_HEADLESS" \
        "rea:$(command -v rea 2>/dev/null || echo MISSING)" \
        "strings:$(command -v strings 2>/dev/null || echo MISSING)" \
        "agy:$AGY_BIN"
    do
        name="${pair%%:*}"; path="${pair#*:}"
        if [[ -x "$path" ]]; then
            echo "  OK   $name → $path"
        else
            echo "  FAIL $name → $path"
        fi
    done

    if [[ ! -x "$JAVA_HOME/bin/java" ]]; then
        echo "  hint: brew install openjdk@21" >&2; fail=1
    fi
    if [[ ! -x "$GHIDRA_HEADLESS" ]]; then
        echo "  hint: build Ghidra native decompiler:" >&2; fail=1
        echo "        cd \$GHIDRA/support/gradle && ./gradlew buildNatives" >&2
    fi
    if [[ ! -x "$AGY_BIN" ]]; then
        echo "  hint: install Antigravity CLI to $AGY_BIN" >&2; fail=1
    fi
    return "$fail"
}

cmd_env() {
    echo "== Resolved environment =="
    echo "REPO_ROOT         = $REPO_ROOT"
    echo "AGY_BIN           = $AGY_BIN"
    echo "JAVA_HOME         = $JAVA_HOME"
    echo "GHIDRA_INSTALL_DIR= $GHIDRA_INSTALL_DIR"
    echo "REA_GHIDRA_SEED   = $REA_GHIDRA_SEED_FILE"
    echo "PROJ              = $PROJ"
    echo "SCRIPTS           = $SCRIPTS"
    echo "GHIDRA_MAXMEM     = $GHIDRA_MAXMEM"
    echo "DRY_RUN           = $DRY_RUN"
    echo "CMD               = $CMD"
}

cmd_doctor() {
    doctor
}

run_pclnseeds() {
    if [[ ! -x "$PCLNSEEDS_BIN" ]]; then
        echo "[seeds] building pclnseeds from rea-go-seeds/"
        if [[ "$DRY_RUN" -eq 1 ]]; then
            echo "[seeds] DRY-RUN: would run: (cd $REPO_ROOT/rea-go-seeds && go build -o $PCLNSEEDS_BIN .)"
            return 0
        fi
        (cd "$REPO_ROOT/rea-go-seeds" && go build -o "$PCLNSEEDS_BIN" .)
    fi

    mkdir -p "$SEEDS_DIR"
    if [[ ! -f "$AGY_BIN" ]]; then
        echo "[seeds] $AGY_BIN missing — cannot derive" >&2
        return 1
    fi

    echo "[seeds] running: $PCLNSEEDS_BIN $AGY_BIN $SEEDS_DIR"
    if [[ "$DRY_RUN" -eq 1 ]]; then
        echo "[seeds] DRY-RUN: skipped pclnseeds exec"
        return 0
    fi
    "$PCLNSEEDS_BIN" "$AGY_BIN" "$SEEDS_DIR"
    echo "[seeds] wrote $SEEDS_DIR/seeds.txt + funcs.txt"
}

cmd_seeds() {
    run_pclnseeds
}

run_decompile() {
    if [[ ! -x "$GHIDRA_HEADLESS" ]]; then
        echo "[decompile] Ghidra headless not found at $GHIDRA_HEADLESS" >&2
        echo "[decompile] build it first: cd \$GHIDRA/support/gradle && ./gradlew buildNatives" >&2
        return 1
    fi
    if [[ ! -f "$REA_GHIDRA_SEED_FILE" ]]; then
        echo "[decompile] seed file missing: $REA_GHIDRA_SEED_FILE" >&2
        echo "[decompile] run: $0 seeds" >&2
        return 1
    fi
    if [[ ! -f "$AGY_BIN" ]]; then
        echo "[decompile] $AGY_BIN missing" >&2
        return 1
    fi
    mkdir -p "$PROJ"

    local VMARGS="-XX:ParallelGCThreads=4 -XX:CICompilerCount=4"
    local SEED_PATH="$REA_GHIDRA_SEED_FILE"

    echo "[decompile] ① import + seed (~11 min, see AGENTS.md)"
    echo "  CWD: /tmp  (neutral cwd, avoids DecompilePriority.java collision)"
    echo "  MAXMEM=$GHIDRA_MAXMEM  $AGY_BIN → $PROJ"

    if [[ "$DRY_RUN" -eq 1 ]]; then
        echo "[decompile] DRY-RUN: skipped analyzeHeadless"
        return 0
    fi

    ( cd /tmp && \
      MAXMEM="$GHIDRA_MAXMEM" VMARGS="$VMARGS" \
      "$GHIDRA_HEADLESS" "$PROJ" agy \
        -import "$AGY_BIN" -noanalysis -max-cpu 4 \
        -scriptPath /opt/homebrew/lib/node_modules/rea-agents/bridge/ghidra \
        -preScript  ReaGhidraApplySeeds.java "$SEED_PATH" \
        -postScript ReaGhidraNoReturnFix.java )

    echo "[decompile] ② decompile priority funcs (incremental, ~13s)"
    ( cd /tmp && \
      MAXMEM="$GHIDRA_MAXMEM" VMARGS="$VMARGS" \
      "$GHIDRA_HEADLESS" "$PROJ" agy \
        -process -noanalysis -max-cpu 4 \
        -scriptPath "$SCRIPTS" \
        -postScript DecompilePriority.java )
}

cmd_decompile() {
    run_decompile
}

collect_tokens() {
    if [[ ! -f "$AGY_BIN" ]]; then
        echo "[tokens] $AGY_BIN missing" >&2
        return 1
    fi
    mkdir -p "$TOKENS_DIR"

    local out="$TOKENS_DIR/tokens-$(basename "$AGY_BIN").txt"

    echo "[tokens] dumping strings (≥ 8 chars) → $out"
    if [[ "$DRY_RUN" -eq 1 ]]; then
        echo "[tokens] DRY-RUN: skipped strings(1)"
        return 0
    fi

    strings -n 8 "$AGY_BIN" > "$out"

    echo "[tokens] extracting antigravity-specific tokens:"
    grep -E 'antigravity|cloudaicompanion|jetski|accounts\.google\.com|oauth2|googleapis\.com|securetoken|gemini-[0-9]|claude-(sonnet|opus|haiku)' "$out" \
        > "$TOKENS_DIR/antigravity-hits.txt" || true

    echo "[tokens] OAuth client IDs:"
    grep -E '[0-9]+-[a-z0-9]+\.apps\.googleusercontent\.com' "$out" \
        > "$TOKENS_DIR/oauth-clients.txt" || true

    echo "[tokens] endpoint URLs:"
    grep -E 'https?://[a-z0-9.-]+\.googleapis\.com' "$out" \
        > "$TOKENS_DIR/endpoints.txt" || true

    echo "[tokens] wrote:"
    echo "  $out"
    echo "  $TOKENS_DIR/antigravity-hits.txt"
    echo "  $TOKENS_DIR/oauth-clients.txt"
    echo "  $TOKENS_DIR/endpoints.txt"
}

cmd_tokens() {
    collect_tokens
}

cmd_all() {
    [[ "$DO_SEEDS" -eq 1 ]] && run_pclnseeds
    [[ "$DO_DECOMPILE" -eq 1 ]] && run_decompile
    [[ "$DO_TOKENS" -eq 1 ]] && collect_tokens
}

# ---------- Dispatch ----------
case "$CMD" in
    env)       cmd_env ;;
    doctor)    cmd_doctor ;;
    seeds)     cmd_seeds ;;
    decompile) cmd_decompile ;;
    tokens)    cmd_tokens ;;
    all)       cmd_all ;;
    *) echo "macos-re.sh: unknown command: $CMD" >&2; usage_short 2 ;;
esac