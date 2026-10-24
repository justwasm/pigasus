#!/usr/bin/env bash
# wg02_provider_verify.sh — cycle provider-model-editor: the live capture of
# the /provider panel's verification probe.
#
# The probe is the panel's answer to "does this endpoint accept this
# credential?". The capture drives the built binary in a tmux pane against a
# local mock endpoint and records what the panel shows:
#
#   1. the probe in flight ("verifying gateway…");
#   2. the probe's verdict (a ✓ for the good key, a ✗ for the bad one);
#   3. the form's probe — the credential the user has typed, before it is
#      committed;
#   4. the probe a save starts.
#
# Usage: wg02_provider_verify.sh [binary] [outdir]
#        defaults: /tmp/wagyu  ./captures
#
# The run is offline: the endpoint is a local mock (a python http server),
# and the TUI itself is started with --backend shim, so no model call is
# made. The configuration is a fixture under $TMPDIR, and the pane's cwd is
# that fixture (so the workspace-trust prompt is answered once, at the
# start).

set -u

BIN=${1:-/tmp/wagyu}
OUT=${2:-./captures}
DIR=$(mktemp -d "${TMPDIR:-/tmp}/wg02-verify.XXXXXX")
SESSION=wg02verify
PORT=${WG02_PORT:-8099}

mkdir -p "$OUT"

# ---- the mock endpoint ---------------------------------------------------

cat > "$DIR/mock.py" <<'PY'
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

KEY = "sk-live-test"
# The mock answers slowly on purpose: the probe's in-flight state
# ("verifying gateway…") is only on screen for the length of the request.
DELAY = 1.5


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def _send(self, code, body):
        raw = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path.startswith("/v1/models"):
            if self.headers.get("Authorization") != "Bearer " + KEY:
                self._send(401, {"error": {"message": "invalid api key"}})
                return
            self._send(200, {"object": "list", "data": [{"id": "MiniMax-M3"}]})
            return
        self._send(404, {"error": {"message": "no such path"}})

    def do_POST(self):
        length = int(self.headers.get("content-length") or 0)
        body = self.rfile.read(length) if length else b"{}"
        if self.path.startswith("/v1/chat/completions"):
            if self.headers.get("Authorization") != "Bearer " + KEY:
                self._send(401, {"error": {"message": "invalid api key"}})
                return
            try:
                model = json.loads(body).get("model", "")
            except Exception:
                model = ""
            time.sleep(DELAY)
            self._send(200, {
                "id": "chatcmpl-live",
                "model": model,
                "choices": [{"index": 0, "message": {"role": "assistant",
                              "content": "pong"}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 3, "completion_tokens": 1,
                          "total_tokens": 4},
            })
            return
        self._send(404, {"error": {"message": "no such path"}})


HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY

python3 "$DIR/mock.py" "$PORT" > "$DIR/mock.log" 2>&1 &
MOCK_PID=$!
trap 'kill "$MOCK_PID" 2>/dev/null; tmux kill-session -t "$SESSION" 2>/dev/null' EXIT
sleep 1

# ---- the fixture ---------------------------------------------------------

cat > "$DIR/providers.json" <<JSON
{
  "providers": {
    "gateway": {
      "id": "gateway",
      "name": "Local gateway",
      "type": "openai",
      "base_url": "http://127.0.0.1:$PORT/v1",
      "api_key": "sk-live-test",
      "models": [
        {
          "id": "minimax-m3",
          "name": "MiniMax M3",
          "model": "MiniMax-M3"
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
  echo "captured $OUT/$name.txt"
}

# ---- 1. the list, then the probe ----------------------------------------

tmux send-keys -t "$SESSION" "/provider"
sleep 1
tmux send-keys -t "$SESSION" Enter
sleep 2
capture 01-panel-list

tmux send-keys -t "$SESSION" "v"
sleep 0.4
capture 02-panel-verifying
sleep 3
capture 03-panel-verified

# ---- 2. the form's probe, with a wrong key -------------------------------

tmux send-keys -t "$SESSION" Enter
sleep 1
# Walk to the credential field: the form opens on the name field.
tmux send-keys -t "$SESSION" Down
tmux send-keys -t "$SESSION" Down
tmux send-keys -t "$SESSION" Down
tmux send-keys -t "$SESSION" Down
sleep 0.5
capture 04-panel-form
tmux send-keys -t "$SESSION" "sk-wrong"
sleep 0.5
tmux send-keys -t "$SESSION" "v"
sleep 2
capture 05-panel-form-rejected
tmux send-keys -t "$SESSION" Escape
sleep 1

# ---- 3. the probe a save starts ------------------------------------------

tmux send-keys -t "$SESSION" Enter
sleep 1
tmux send-keys -t "$SESSION" Enter
sleep 0.4
capture 06-panel-save-verifying
sleep 3
capture 07-panel-save-verified

cp "$DIR/providers.json" "$OUT/providers.json.after"

tmux kill-session -t "$SESSION" 2>/dev/null
echo "fixture: $DIR"
