#!/usr/bin/env bash
# ==============================================================================
# Example: Scriptable alignment test driven by native bash
# ==============================================================================
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$DIR/tuialign.sh"

echo "==> [tuialign] Running shell script driven test..."

# 1. Start mock server
tuialign_start 58995

# 2. Program expected mock turn
tuialign_mock_turn_thought \
  "Examining openagy repository structure..." 140 \
  "Everything looks cleanly organized and modular!" \
  18000 20

# 3. Boot both agy and pigasus
tuialign_boot_both 100 30

# 4. Wait for prompt ready
tuialign_wait ">" 30

# 5. Send user input
tuialign_send "check repo" C-m

# 6. Wait for model response
tuialign_wait "Everything looks cleanly" 30

# 7. Take snapshot and compare
tuialign_snapshot "repo_check_collapsed"

# 8. Expand thought with ctrl+o
tuialign_send C-o
sleep 0.5
tuialign_snapshot "repo_check_expanded"

# 9. Cleanup
tuialign_cleanup

echo "==> [tuialign] Shell test finished successfully!"
