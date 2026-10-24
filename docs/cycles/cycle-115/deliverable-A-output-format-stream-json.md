# Cycle 115 — `--output-format stream-json` in print mode

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** cycle 114 implemented `--output-format json`. `stream-json` still fell
through to plain text. This cycle implements it: one JSON event per line, matching
the reference byte-for-byte on every path that can be captured.

---

## 1. The four events 【一手】

Captured from 1.3.6. There are exactly four, and each carries a single payload
member alongside `event`:

```
$ agy -p=/hooks --output-format stream-json
{"event":"command_result","command":{"name":"hooks","data":{"hooks":[]}}}
{"event":"result","result":{"conversation_id":"","status":"SUCCESS","response":"","duration_seconds":0,"num_turns":0,"usage":{…},"command":{"name":"hooks","data":{"hooks":[]}}}}

$ agy -p='say hi' --output-format stream-json      # rc 3, quota exhausted
{"event":"init","conversation_id":"bebd394e-…","init":{"cwd":"/tmp","tools":[…60 names…],"permission_mode":"always-proceed"}}
{"event":"step_update","step_update":{"conversation_id":"bebd394e-…","step_index":0,"state":"DONE","step_type":"user_input"}}
{"event":"step_update","step_update":{"conversation_id":"bebd394e-…","step_index":1,"state":"DONE","step_type":"error_message"}}
{"event":"result","result":{"conversation_id":"bebd394e-…","status":"ERROR","response":"","error":"Individual quota reached. …","duration_seconds":0,"num_turns":1,"usage":{…}}}
```

What that pins down:

* **`conversation_id` is a top-level member of `init` only.** The `step_update`
  and `result` events carry theirs *inside* the payload. A UUID is minted once a
  model turn starts; a local command keeps `""` everywhere.
* **`result.result` is exactly the cycle-114 envelope** — same members, same
  order, `command` included when the answer came from a slash command. So the
  `json` and `stream-json` formats share one serialiser.
* **`command_result` precedes `result`** for a local command and is emitted even
  when the text answer is empty (`/hooks`, `/agents`).
* **A rejected command emits only `result`.** `/clear` (rc 2) and `/credits`
  (rc 1) both produce a single ERROR envelope with no `command` and no
  `command_result`.
* **stderr and the exit code are unchanged** from `text`/`json` — the stream is
  purely additive on stdout.
* `init.tools` is the sorted 60-name advertised list (cycle 113), `init.cwd` is
  the working directory, and `init.permission_mode` is the `toolPermission`
  setting, captured as `always-proceed`.

## 2. Implementation

* `cmd/antigravity/print_format.go` — the event-name constants,
  `jsonInitPayload`, `jsonStepUpdate`, `jsonStreamEvent`, `emitStep`,
  `printPermissionMode`. `jsonStreamEvent`'s members are ordered
  `event`, `conversation_id`, `init`, `step_update`, `command`, `result`, with
  everything but `event` omitempty, which reproduces all four shapes from one
  type.
* `cmd/antigravity/main.go` — `runPrintMode` now branches on all three formats.
  The slash path emits `command_result` (when there is a command payload) then
  `result`. The model path buffers the answer for both non-text formats, emits
  `init` and the `user_input` step once the tool catalogue is built, and emits the
  closing step plus `result` at the end.

## 3. Verification

* **Byte-level comparison, 10 local commands** — stdout, stderr and exit code all
  identical to the reference:

  ```
  /help /hooks /permissions /config /model /effort /agents /changelog /skills   BYTE-IDENTICAL (rc 0)
  /credits                                                                     BYTE-IDENTICAL (rc 1)
  ```

* **Rejection** — `/clear --output-format stream-json` is byte-identical
  (rc 2, single `result` event).
* **Model-turn failure** — against a local 429 server, all four lines match the
  reference capture once the random UUID and the quota reset clock are
  normalised:

  ```
  ref events : ['init', 'step_update', 'step_update', 'result']
  ours events: ['init', 'step_update', 'step_update', 'result']
  line 0: MATCH   line 1: MATCH   line 2: MATCH   line 3: MATCH
  ```

  rc 3, and the stderr pair is unchanged.
* **Model-turn success** — a real turn through the gateway produces
  `init` → `step_update(user_input)` → `step_update(agent_response)` →
  `result` with `status:"SUCCESS"`, the answer text in `response`, and real
  `usage`. stderr empty, rc 0.
* `text` is still the default and is byte-identical to the reference.
* `go build`, `go vet`, `go test ./cmd/... ./internal/...` — clean; `gofmt` clean
  on every file touched. Three new tests.
* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** (both documented diffs).

## 4. Divergences and limitations

* **The successful turn's step sequence is extrapolated, not captured.** The
  failure sequence (`user_input`, then `error_message`) is 【一手】; the success
  sequence is inferred to be `user_input`, then `agent_response`, because
  `agent_response` is a recovered `step_type` literal and a successful turn must
  close with something. It cannot be confirmed while the account is exhausted.
* **Only the closing step is reported for a turn.** The clone does not emit
  `tool_calls`, `thinking`, `skill_used`, `file_diffs` or `checkpoint` steps even
  though those literals exist in the binary, because their index/state semantics
  are unrecovered. A consumer that wants tool activity has to read the final
  `result`.
* **Every step is reported `DONE`.** The reference also has `RUNNING` and
  `PENDING` states (recovered literals); the clone emits no interim state at all,
  so a consumer never sees a step in progress.
* **`step_index` is positional, not semantic.** The clone assigns 0 to
  `user_input` and 1 to the closing step, matching the captured failure. If the
  reference counts differently once tool calls interleave, the indices will
  differ.
* **`init.permission_mode` is read from the `toolPermission` setting**, which is
  the only correlation available; the reference may derive it from the resolved
  permission policy rather than the raw setting. Both agree at the default
  (`always-proceed`).
* **The `result` envelope inherits every limitation cycle 114 listed** —
  `usage` comes from the agw transport (`cache_read_tokens` always 0),
  `duration_seconds` and `conversation_id` are real rather than captured, and
  `is_default` / `model_invocable` are fixed values that match every observation.
* **`/usage` still fails** and therefore emits a single ERROR `result` event,
  where the reference succeeds.
