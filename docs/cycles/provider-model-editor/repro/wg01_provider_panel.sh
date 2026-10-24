#!/usr/bin/env bash
# wg01_provider_panel.sh — cycle provider-model-editor: the live capture of
# the /provider panel.
#
# The panel is the TUI's front-end onto ~/.config/openagy/providers.json.
# The capture drives the built binary in a tmux pane and records what the
# panel shows at each step:
#
#   1. the list (providers, their models, the built-in group);
#   2. the edit form (a provider);
#   3. the add-model form;
#   4. the delete confirmation;
#   5. the file after a write.
#
# Usage: wg01_provider_panel.sh [binary] [outdir]
#        defaults: /tmp/wagyu  ./captures
#
# The run is offline: the TUI is started with --backend shim, so no
# network call is made. The configuration is a fixture under $TMPDIR, and
# the pane's cwd is that fixture (so the workspace-trust prompt is
# answered once, at the start).

set -u

BIN=${1:-/tmp/wagyu}
OUT=${2:-./captures}
DIR=$(mktemp -d "${TMPDIR:-/tmp}/wg01-provider.XXXXXX")
SESSION=wg01provider

mkdir -p "$OUT"

cat > "$DIR/providers.json" <<'JSON'
{
  "providers": {
    "gateway": {
      "id": "gateway",
      "name": "Gateway",
      "type": "openai",
      "base_url": "https://gw.example/v1",
      "api_key": "$OPENAGY_GATEWAY_KEY",
      "models": [
        {
          "id": "minimax-m3",
          "name": "MiniMax M3",
          "model": "MiniMax-M3",
          "context_window": 1048576
        }
      ]
    },
    "local": {
      "id": "local",
      "name": "Ollama",
      "type": "openai",
      "api_url": "http://127.0.0.1:11434/v1/chat/completions",
      "api_key": "-"
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
  echo "captured $OUT/$name.txt"
}

tmux send-keys -t "$SESSION" "/provider"
sleep 1
tmux send-keys -t "$SESSION" Enter
sleep 2
capture 01-panel-list

tmux send-keys -t "$SESSION" Enter
sleep 1
capture 02-panel-edit-provider

tmux send-keys -t "$SESSION" Escape
sleep 1
tmux send-keys -t "$SESSION" "m"
sleep 1
capture 03-panel-add-model

tmux send-keys -t "$SESSION" Escape
sleep 1
tmux send-keys -t "$SESSION" Down
tmux send-keys -t "$SESSION" Down
sleep 1
tmux send-keys -t "$SESSION" "d"
sleep 1
capture 04-panel-delete-confirm

tmux send-keys -t "$SESSION" "d"
sleep 1
capture 05-panel-after-delete

cp "$DIR/providers.json" "$OUT/providers.json.after"

tmux kill-session -t "$SESSION" 2>/dev/null
echo "fixture: $DIR"
