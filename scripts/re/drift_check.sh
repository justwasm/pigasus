#!/usr/bin/env bash
# ==============================================================================
# drift_check.sh — Automated Upstream Reference Drift & Parity Inspection SOP
#
# Usage:
#   ./scripts/re/drift_check.sh [OPTIONS]
#
# Options:
#   --binary <PATH>            Path to upstream candidate agy binary
#   --baseline-dir <PATH>      Baseline extracted directory (default: out/extracted-1.5.14)
#   --baseline-version <VER>   Baseline reference version (default: 1.5.14)
#   --simulate-canary          Simulate upstream 1.5.15+ canary drift for verification
#   --update-docs              Update docs/guide/reference-drift.md (default: true)
#   --no-update-docs           Skip updating documentation
#   --report-json <PATH>       Path to export JSON report
#   --report-md <PATH>         Path to export Markdown report
#   --fail-on-drift            Exit with code 1 if critical or warning drift is detected
#   -v, --verbose              Enable verbose output
#   -h, --help                 Show this help message
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Ensure python3 is available
if ! command -v python3 &>/dev/null; then
    echo "[-] Error: python3 is required for drift_check.sh" >&2
    exit 1
fi

PYTHON_ANALYZER="${SCRIPT_DIR}/drift_analyzer.py"
if [[ ! -f "${PYTHON_ANALYZER}" ]]; then
    echo "[-] Error: ${PYTHON_ANALYZER} not found" >&2
    exit 1
fi

# Print banner
echo "========================================================================"
echo "  OpenAGY / Pigasus Reference Drift Inspector (Reverse-Engineering SOP) "
echo "========================================================================"

# Forward all arguments directly to drift_analyzer.py
python3 "${PYTHON_ANALYZER}" "$@"
