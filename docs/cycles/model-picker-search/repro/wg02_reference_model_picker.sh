#!/usr/bin/env bash
# wg02_reference_model_picker.sh — cycle model-picker-search: the live
# capture of the *reference's* /model picker, the ground truth for the
# clone's search field.
#
# The reference draws a "Search:" box above the model rows and filters
# them as the user types. This script drives the installed reference
# binary (~/.local/bin/agy) in a tmux pane and records:
#
#   1. the picker with no query;
#   2. the query "claude" (two rows, in the reference's order);
#   3. the query "s" (four rows — the relevance order);
#   4. the query "clauson" (a subsequence, not a substring);
#   5. the query "cl-opus" (a subsequence of the catalogue id, not of
#      the label: nothing matches);
#   6. the query "zzz" (the empty-result line);
#   7. the query "j" (a printable key filters, it does not navigate);
#   8. Esc clearing the search and keeping the panel open.
#
# Usage: wg02_reference_model_picker.sh [binary] [outdir]
#        defaults: ~/.local/bin/agy  ./captures
#
# The run is live: the reference talks to Google's servers to fetch the
# model catalogue, so it needs the machine's credentials. Every step waits
# for the panel to show up rather than for a fixed delay — the reference
# fetches the catalogue over the network first, and a fixed delay races
# with it.

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

# wait_for <text> [attempts]: blocks until the pane shows <text>.
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

# Open the picker and wait for it.
tmux send-keys -t "$SESSION" "/model"
sleep 1
tmux send-keys -t "$SESSION" Enter
if ! wait_for "Switch Model"; then
  echo "the /model panel did not open" >&2
  capture 00-reference-picker-failed
  tmux kill-session -t "$SESSION" 2>/dev/null
  exit 1
fi
sleep 1
capture 01-reference-picker

# query <name> <text>: clear any previous query, type <text>, capture.
#
# Esc clears a *live* query and keeps the panel open — but with an empty
# search box Esc means "Go Back" and closes the panel. So the first query
# types straight away and every later one starts with an Esc.
PENDING=0
query() {
  local name=$1 text=$2
  if [ "$PENDING" = "1" ]; then
    tmux send-keys -t "$SESSION" Escape   # clear the previous query
    sleep 1
  fi
  tmux send-keys -t "$SESSION" "$text"
  sleep 2
  capture "$name"
  PENDING=1
}

query 02-reference-claude "claude"
query 03-reference-s "s"
query 04-reference-clauson "clauson"
query 05-reference-cl-opus "cl-opus"
query 06-reference-no-match "zzz"
query 07-reference-j "j"

# Esc clears the search and keeps the panel open.
tmux send-keys -t "$SESSION" Escape
sleep 2
capture 08-reference-search-cleared

tmux kill-session -t "$SESSION" 2>/dev/null
echo "fixture: $DIR"
