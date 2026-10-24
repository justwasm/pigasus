#!/usr/bin/env bash
# wg01_clone_model_completion.sh — cycle model-argument-completion: the live
# capture of the *clone's* "/model <query>" completion popup.
#
# The reference completes the /model argument with a popup of models
# (label + catalogue id). This script drives the clone in a tmux pane and
# records the same states the reference capture records, so the two can be
# diffed byte for byte:
#
#   1. "/model " — the whole catalogue (five rows + "↓ N more");
#   2. "/model claude" — the two Claude models;
#   3. "/model zzz" — the "No matches" row;
#   4. "/model claude" + Tab — the composer holds the label, the popup is
#      closed;
#   5. "/model Claude Sonnet 4.6 (Thinking)" + Enter — "Model set to …";
#   6. "/model zzz" + Enter — 'Invalid model "zzz" (available: …)';
#   7. "/model" — the *command* popup, not the model rows.
#
# Usage: wg01_clone_model_completion.sh [binary] [outdir]
#        defaults: /tmp/wagyu  ./captures
#
# Build the binary first:
#   go build -o /tmp/wagyu ./cmd/wagyu

set -u

BIN=${1:-/tmp/wagyu}
OUT=${2:-./captures}
DIR=$(mktemp -d "${TMPDIR:-/tmp}/wg01-clone.XXXXXX")
SESSION=wg01clone

if [ ! -x "$BIN" ]; then
  echo "no clone binary at $BIN" >&2
  exit 1
fi

mkdir -p "$OUT"

tmux kill-session -t "$SESSION" 2>/dev/null
tmux new-session -d -s "$SESSION" -x 120 -y 40 -c "$DIR" "$BIN" --backend shim

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
    sleep 1
  done
  echo "timed out waiting for: $text" >&2
  return 1
}

sleep 3

# A fresh working directory is not trusted yet: the clone asks first.
if tmux capture-pane -p -t "$SESSION" | grep -q "Trust this folder"; then
  tmux send-keys -t "$SESSION" Enter
  sleep 4
fi
wait_for "for shortcuts" || exit 1
sleep 2

# The composer starts empty; C-u clears whatever is left.
clear_line() {
  tmux send-keys -t "$SESSION" C-u
  sleep 1
}

clear_line
tmux send-keys -t "$SESSION" "/model "
sleep 2
capture 01-clone-model-empty

clear_line
tmux send-keys -t "$SESSION" "/model claude"
sleep 2
capture 02-clone-model-claude

clear_line
tmux send-keys -t "$SESSION" "/model zzz"
sleep 2
capture 03-clone-model-no-match

clear_line
tmux send-keys -t "$SESSION" "/model claude"
sleep 2
tmux send-keys -t "$SESSION" Tab
sleep 2
capture 04-clone-model-tab

clear_line
tmux send-keys -t "$SESSION" "/model Claude Sonnet 4.6 (Thinking)"
sleep 2
tmux send-keys -t "$SESSION" Enter
sleep 2
capture 05-clone-model-enter-set

clear_line
tmux send-keys -t "$SESSION" "/model zzz"
sleep 2
tmux send-keys -t "$SESSION" Enter
sleep 2
capture 06-clone-model-enter-invalid

clear_line
tmux send-keys -t "$SESSION" "/model"
sleep 2
capture 07-clone-model-command-popup

tmux kill-session -t "$SESSION" 2>/dev/null
echo "fixture: $DIR"
