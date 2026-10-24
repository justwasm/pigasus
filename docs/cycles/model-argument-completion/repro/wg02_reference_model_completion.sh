#!/usr/bin/env bash
# wg02_reference_model_completion.sh — cycle model-argument-completion: the
# live capture of the *reference's* "/model <query>" completion popup, the
# ground truth for the clone.
#
# The reference completes the /model argument with a popup of models (the
# display label in a 30-column field and the catalogue id two columns
# further right). This script drives the installed reference binary
# (~/.local/bin/agy) in a tmux pane and records:
#
#   1. "/model " — the whole catalogue (five rows + "↓ 1 more");
#   2. "/model claude" — the two Claude models;
#   3. "/model zzz" — the "No matches" row;
#   4. "/model claude" + Tab — the composer holds the label, the popup is
#      closed;
#   5. "/model Claude Sonnet 4.6 (Thinking)" + Enter — "Model set to …";
#   6. "/model zzz" + Enter — 'Invalid model "zzz" (available: …)';
#   7. "/model" — the *command* popup, not the model rows;
#   8. "/model " + Esc — the popup closes, the "[model]" hint stays.
#
# Usage: wg02_reference_model_completion.sh [binary] [outdir]
#        defaults: ~/.local/bin/agy  ./captures
#
# The run is live: the reference talks to Google's servers to fetch the
# model catalogue, so it needs the machine's credentials.

set -u

BIN=${1:-$HOME/.local/bin/agy}
OUT=${2:-./captures}
DIR=$(mktemp -d "${TMPDIR:-/tmp}/wg02-reference.XXXXXX")
SESSION=wg02reference

if [ ! -x "$BIN" ]; then
  echo "no reference binary at $BIN" >&2
  exit 1
fi

mkdir -p "$OUT"

tmux kill-session -t "$SESSION" 2>/dev/null
tmux new-session -d -s "$SESSION" -x 120 -y 40 -c "$DIR" "$BIN"

capture() {
  local name=$1
  tmux capture-pane -p -t "$SESSION" > "$OUT/$name.txt"
  tmux capture-pane -e -p -t "$SESSION" > "$OUT/$name.raw.txt"
  echo "captured $OUT/$name.txt"
}

wait_for() {
  local text=$1 attempts=${2:-40}
  for _ in $(seq 1 "$attempts"); do
    if tmux capture-pane -p -t "$SESSION" | grep -q -- "$text"; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for: $text" >&2
  return 1
}

wait_for "for shortcuts" || wait_for "Do you trust" || exit 1
sleep 3

# A fresh working directory is not trusted yet: the reference asks first.
if tmux capture-pane -p -t "$SESSION" | grep -q "Do you trust"; then
  tmux send-keys -t "$SESSION" Enter
  sleep 6
fi
wait_for "for shortcuts" || exit 1
sleep 3

clear_line() {
  tmux send-keys -t "$SESSION" C-u
  sleep 1
}

clear_line
tmux send-keys -t "$SESSION" "/model "
sleep 2
capture 01-reference-model-empty

clear_line
tmux send-keys -t "$SESSION" "/model claude"
sleep 2
capture 02-reference-model-claude

clear_line
tmux send-keys -t "$SESSION" "/model zzz"
sleep 2
capture 03-reference-model-no-match

clear_line
tmux send-keys -t "$SESSION" "/model claude"
sleep 2
tmux send-keys -t "$SESSION" Tab
sleep 2
capture 04-reference-model-tab

clear_line
tmux send-keys -t "$SESSION" "/model Claude Sonnet 4.6 (Thinking)"
sleep 2
tmux send-keys -t "$SESSION" Enter
sleep 3
capture 05-reference-model-enter-set

clear_line
tmux send-keys -t "$SESSION" "/model zzz"
sleep 2
tmux send-keys -t "$SESSION" Enter
sleep 3
capture 06-reference-model-enter-invalid

clear_line
tmux send-keys -t "$SESSION" "/model"
sleep 2
capture 07-reference-model-command-popup

clear_line
tmux send-keys -t "$SESSION" "/model "
sleep 2
tmux send-keys -t "$SESSION" Escape
sleep 2
capture 08-reference-model-esc

tmux kill-session -t "$SESSION" 2>/dev/null
echo "fixture: $DIR"
