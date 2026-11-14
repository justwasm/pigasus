#!/usr/bin/env bash
# ==============================================================================
# tuialign.sh — Shell driver library for scriptable TUI alignment
#
# Source this file into your bash/zsh script to easily program mock LLM
# responses, drive tmux sessions, capture terminal screens, and assert parity.
#
# Example usage:
#
#   source devutils/tuialign/tuialign.sh
#
#   tuialign_start
#   tuialign_mock_turn_thought "Analyzing task..." 130 "Hello world!" 18200 18
#   tuialign_boot_both 100 30
#   tuialign_wait ">"
#   tuialign_send "hi" C-m
#   tuialign_wait "Hello world!"
#   tuialign_snapshot "greeting"
#   tuialign_diff
#   tuialign_cleanup
# ==============================================================================

TUIALIGN_PORT="${TUIALIGN_PORT:-58999}"
TUIALIGN_SOCKET="${TUIALIGN_SOCKET:-tuialign}"
TUIALIGN_SESSION="${TUIALIGN_SESSION:-align_$$}"
TUIALIGN_SERVER_PID=""
TUIALIGN_WS=""

# tuialign_start launches the background mock CloudCode proxy server.
tuialign_start() {
  local port="${1:-$TUIALIGN_PORT}"
  TUIALIGN_PORT="$port"
  echo "[tuialign.sh] Starting mock CloudCode server on 127.0.0.1:$port..."
  go run ./devutils/tuialign server -addr "127.0.0.1:$port" >/dev/null 2>&1 &
  TUIALIGN_SERVER_PID=$!

  # Wait for server ready
  for _ in {1..30}; do
    if curl -s "http://127.0.0.1:$port/_mock/health" >/dev/null 2>&1; then
      echo "[tuialign.sh] Mock server ready on http://127.0.0.1:$port"
      return 0
    fi
    sleep 0.1
  done
  echo "[tuialign.sh] Failed to start mock server" >&2
  return 1
}

# tuialign_mock_reset resets all programmed mock turns.
tuialign_mock_reset() {
  curl -s -X POST "http://127.0.0.1:$TUIALIGN_PORT/_mock/reset" >/dev/null
}

# tuialign_mock_turn sends raw JSON defining a mock turn.
tuialign_mock_turn() {
  local json_payload="$1"
  curl -s -X POST "http://127.0.0.1:$TUIALIGN_PORT/_mock/turn" \
    -H "Content-Type: application/json" \
    -d "$json_payload" >/dev/null
}

# tuialign_mock_turn_thought adds a turn with thought reasoning and text response.
tuialign_mock_turn_thought() {
  local thought="$1"
  local thought_tokens="${2:-130}"
  local text="$3"
  local prompt_tokens="${4:-18200}"
  local cand_tokens="${5:-18}"

  local payload=$(cat <<EOF
{
  "chunks": [
    {
      "thought": "$thought",
      "thought_tokens": $thought_tokens,
      "delay_ms": 100000000
    },
    {
      "text": "$text",
      "prompt_tokens": $prompt_tokens,
      "candidate_tokens": $cand_tokens,
      "thought_tokens": $thought_tokens,
      "finish_reason": "STOP",
      "delay_ms": 100000000
    }
  ]
}
EOF
)
  tuialign_mock_turn "$payload"
}

# tuialign_boot_both starts agy (pane 0) and pigasus (pane 1) in split window.
tuialign_boot_both() {
  local width="${1:-100}"
  local height="${2:-30}"
  local agy_bin="${AGY_REAL_BIN:-$HOME/.local/bin/agy}"
  local pigasus_bin="${PIGASUS_BIN:-/tmp/pigasus}"

  TUIALIGN_WS=$(mktemp -d /tmp/tuialign_ws_XXXXXX)
  (cd "$TUIALIGN_WS" && git init -q)

  tmux -L "$TUIALIGN_SOCKET" kill-session -t "$TUIALIGN_SESSION" 2>/dev/null || true

  local total_w=$((width * 2 + 2))
  local env_cmd="env CLOUD_CODE_URL=http://127.0.0.1:$TUIALIGN_PORT JETSKI_OAUTH_TOKEN=mock-token OPENAGY_TRUST=1"

  # Left pane: agy
  tmux -L "$TUIALIGN_SOCKET" -f /dev/null new-session -d -s "$TUIALIGN_SESSION" \
    -x "$total_w" -y "$height" -c "$TUIALIGN_WS" \
    "$env_cmd $agy_bin --model gemini-3.8-flash-high"

  # Right pane: pigasus
  tmux -L "$TUIALIGN_SOCKET" split-window -h -t "$TUIALIGN_SESSION.0" \
    -c "$TUIALIGN_WS" \
    "$env_cmd $pigasus_bin --model gemini-3.8-flash-high"

  tmux -L "$TUIALIGN_SOCKET" select-layout -t "$TUIALIGN_SESSION" even-horizontal
}

# tuialign_send sends keys to both panes.
tuialign_send() {
  tmux -L "$TUIALIGN_SOCKET" send-keys -t "$TUIALIGN_SESSION.0" "$@"
  tmux -L "$TUIALIGN_SOCKET" send-keys -t "$TUIALIGN_SESSION.1" "$@"
}

# tuialign_wait waits until target text appears in both panes.
tuialign_wait() {
  local text="$1"
  local max_tries="${2:-50}"
  for _ in $(seq 1 "$max_tries"); do
    local left=$(tmux -L "$TUIALIGN_SOCKET" capture-pane -p -t "$TUIALIGN_SESSION.0" 2>/dev/null || true)
    local right=$(tmux -L "$TUIALIGN_SOCKET" capture-pane -p -t "$TUIALIGN_SESSION.1" 2>/dev/null || true)
    if [[ "$left" == *"Do you trust"* || "$left" == *"Trust this folder"* ]]; then
      tmux -L "$TUIALIGN_SOCKET" send-keys -t "$TUIALIGN_SESSION.0" C-m
    fi
    if [[ "$right" == *"Do you trust"* || "$right" == *"Trust this folder"* ]]; then
      tmux -L "$TUIALIGN_SOCKET" send-keys -t "$TUIALIGN_SESSION.1" C-m
    fi
    if [[ "$left" == *"$text"* && "$right" == *"$text"* ]]; then
      return 0
    fi
    sleep 0.2
  done
  echo "[tuialign.sh] Timed out waiting for '$text' in both panes" >&2
  return 1
}

# tuialign_snapshot captures both panes and outputs diff.
tuialign_snapshot() {
  local label="${1:-snapshot}"
  local left_file="/tmp/tuialign_${label}_agy.txt"
  local right_file="/tmp/tuialign_${label}_pigasus.txt"

  tmux -L "$TUIALIGN_SOCKET" capture-pane -e -p -t "$TUIALIGN_SESSION.0" > "$left_file"
  tmux -L "$TUIALIGN_SOCKET" capture-pane -e -p -t "$TUIALIGN_SESSION.1" > "$right_file"

  go run ./devutils/tuialign diff "$left_file" "$right_file"
}

# tuialign_cleanup terminates tmux session and stops mock server.
tuialign_cleanup() {
  tmux -L "$TUIALIGN_SOCKET" kill-session -t "$TUIALIGN_SESSION" 2>/dev/null || true
  if [ -n "$TUIALIGN_SERVER_PID" ]; then
    kill "$TUIALIGN_SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "$TUIALIGN_WS" ]; then
    rm -rf "$TUIALIGN_WS"
  fi
}
