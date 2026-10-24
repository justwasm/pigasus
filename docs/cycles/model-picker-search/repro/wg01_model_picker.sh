#!/usr/bin/env bash
# wg01_model_picker.sh — cycle model-picker-search: the live capture of
# the /model picker and its search field.
#
# The picker is the TUI's front-end onto the model catalogue. The capture
# drives the built binary in a tmux pane and records what the panel shows:
#
#   1. the picker with the reference's rows plus two configured models
#      (seven rows — the seventh used to be unreachable);
#   2. the cursor on the last row, reached with ↓;
#   3. the search field filtering live ("flash");
#   4. the empty-result line ("zz");
#   5. Esc clearing the search and keeping the panel open;
#   6. a row selected through the filter.
#
# Usage: wg01_model_picker.sh [binary] [outdir]
#        defaults: /tmp/wagyu  ./captures
#
# The run is offline: the TUI is started with --backend shim, so no
# network call is made. The configuration is a fixture under $TMPDIR.

set -u

BIN=${1:-/tmp/wagyu}
OUT=${2:-./captures}
DIR=$(mktemp -d "${TMPDIR:-/tmp}/wg01-model.XXXXXX")
SESSION=wg01model

mkdir -p "$OUT"

cat > "$DIR/providers.json" <<'JSON'
{
  "providers": {
    "gateway": {
      "id": "gateway",
      "name": "Gateway",
      "type": "openai",
      "base_url": "https://gw.example/v1",
      "api_key": "-",
      "models": [
        {
          "id": "minimax-m3",
          "name": "MiniMax M3",
          "model": "MiniMax-M3"
        },
        {
          "id": "minimax-m3.1-flash",
          "name": "MiniMax M3.1 Flash",
          "model": "MiniMax-M3.1-Flash"
        }
      ]
    }
  }
}
JSON

tmux kill-session -t "$SESSION" 2>/dev/null
tmux new-session -d -s "$SESSION" -x 120 -y 40 -c "$DIR" \
  "OPENAGY_PROVIDERS_CONFIG=$DIR/providers.json OPENAGY_MODELS_CONFIG=$DIR/models.json $BIN --backend shim 2>$DIR/stderr.log"

# The first run asks whether the folder can be trusted.
sleep 4
tmux send-keys -t "$SESSION" Enter
sleep 4

capture() {
  local name=$1
  tmux capture-pane -p -t "$SESSION" > "$OUT/$name.txt"
  tmux capture-pane -e -p -t "$SESSION" > "$OUT/$name.raw.txt"
  echo "captured $OUT/$name.txt"
}

tmux send-keys -t "$SESSION" "/model"
sleep 1
tmux send-keys -t "$SESSION" Enter
sleep 2
capture 01-picker

# Walk down to the last row: the seventh row (MiniMax M3.1 Flash) used to
# be unreachable, the cursor stopped on the sixth.
for _ in 1 2 3 4 5 6 7 8; do
  tmux send-keys -t "$SESSION" Down
  sleep 0.3
done
sleep 1
capture 02-picker-last-row

# The search field filters the rows live.
tmux send-keys -t "$SESSION" "flash"
sleep 2
capture 03-picker-search

# A query that matches nothing.
tmux send-keys -t "$SESSION" "zz"
sleep 2
capture 04-picker-no-match

# Esc clears the search and keeps the panel open.
tmux send-keys -t "$SESSION" Escape
sleep 2
capture 05-picker-search-cleared

# Select a row through the filter.
tmux send-keys -t "$SESSION" "m3.1"
sleep 2
tmux send-keys -t "$SESSION" Enter
sleep 2
capture 06-picker-after-select

tmux kill-session -t "$SESSION" 2>/dev/null
echo "fixture: $DIR"
