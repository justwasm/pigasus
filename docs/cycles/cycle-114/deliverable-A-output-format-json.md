# Cycle 114 — `--output-format json` in print mode

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** the clone parsed `--output-format` but ignored it — `-p='…'
--output-format json` printed plain text. This cycle implements the `json`
format: the same answer, wrapped in the reference's envelope, byte-for-byte.

---

## 1. How the shape was recovered 【一手】

The account quota is exhausted, so a model turn cannot succeed — but the
**local** print-mode slash commands answer without a model call, and they emit
the full success envelope. That is what made this cycle verifiable rather than
guessed:

```
$ agy -p=/help --output-format json
{"conversation_id":"","status":"SUCCESS","response":"/agents\tList available custom agents\n…",
 "duration_seconds":0,"num_turns":0,
 "usage":{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0},
 "command":{"name":"help","data":{"commands":[{"name":"agents","description":"List available custom agents"},…]}}}
```

The failure shapes came from the same place:

```
$ agy -p=/clear  --output-format json    # rc 2
{"conversation_id":"","status":"ERROR","response":"","error":"/clear is not available in print mode (…)",…}

$ agy -p=/credits --output-format json   # rc 1
{"conversation_id":"","status":"ERROR","response":"","error":"/credits failed: retrieving credits: no credits info found",…}

$ agy -p='say hi' --output-format json   # rc 3, quota exhausted
{"conversation_id":"cfebbff3-…","status":"ERROR","response":"","error":"Individual quota reached. …","num_turns":1,…}
```

Four things those captures pin down:

1. **Field order** is `conversation_id`, `status`, `response`, `error`,
   `duration_seconds`, `num_turns`, `usage`, `command`.
2. **`error` and `command` are omitempty** — each is absent on the other path.
3. **`conversation_id` is `""`** for anything answered without a session (every
   local command, including the rc 1 and rc 2 failures) and a UUID once a model
   turn starts.
4. **`num_turns` is 1** once a turn has started, *even when that turn fails*,
   and 0 for a local command.

The envelope is written to stdout on **every** path, including the rc 2
rejections — whose stderr line is unchanged.

## 2. The `command` payloads

`command.data` is the structured form of the same answer `response` carries as
text. All of it is captured; the field orders prove each payload is a **struct**,
not a map (a map would marshal alphabetically):

| command | `data` | note |
|---|---|---|
| `/help` | `{commands:[{name, aliases?, description}]}` | `aliases` omitempty |
| `/changelog` | `{changelog: "<markdown>"}` | |
| `/permissions` | `{permissions:[{scope, allow?, deny?}]}` | always the three scopes project, shared, global |
| `/config` | `{config: {…34 typed keys…}}` | see below |
| `/skills` | `{skills:[{name, description, path, plugin?, builtin, model_invocable}]}` | `plugin` omitempty |
| `/usage` | — | reference succeeds; see §5 |
| `/agents` | `{agents: []}` | empty on this machine |
| `/model` | `{id, label, effort, is_default}` | |
| `/hooks` | `{hooks: []}` | empty on this machine |
| `/credits` | — | always an error |
| `/effort` | `{adjustable, current, available}` | |

### `/config`'s key types

The payload revealed something the text answer cannot: the reference models
**five keys as nullable** — they come back as `null` when never set
(`statusLine`, `title`, `gcp`, `sandboxProxy`, `customModelsConfig`) — while the
other 29 fall back to a zero value of their kind (`""`, `false`, `0`,
`2000` for `historySize`, `"medium"` for `verbosity`, …). The clone reproduces
that split in `printSlashConfigTypes` / `configValue`, and the values themselves
are the raw parsed JSON whenever settings.json has the key.

### `/model` and `/effort` also validate `--effort`

Captured, and a real behaviour change:

```
$ agy -p=/model --effort=low    -> {"id":"gemini-3.8-flash-low",…,"effort":"low"}          rc 0
$ agy -p=/effort --effort=low   -> {"adjustable":true,"current":"low","available":["low","medium","high"]}
$ agy -p=/model --effort=xhigh  -> rc 1, ERROR envelope,
                                   "gemini-3.8-flash has no \"xhigh\" effort (available: low, medium, high)"
```

So `--effort` alone selects a model on the *slash* path, and an invalid pair
becomes an ERROR envelope with rc 1 — the same message `resolveModelSelection`
already produces. `--model` is no longer required for the model to change.

### `/skills` carries a `plugin` member

Diffing our first attempt against the capture found a field the earlier cycles
had missed: plugin-discovered skills carry `"plugin":"<name>"` between `path`
and `builtin` (46 of the 57 entries on this machine). Added.

## 3. Implementation

* `cmd/antigravity/print_format.go` (new) — `printOutputFormat`, the
  `jsonEnvelope` / `jsonUsage` / `jsonCommandPayload` types, `writeJSONLine`,
  the `/help` command table, the settings-key type table, `configValues`,
  `printErrorMessage`, `newConversationID`.
* `cmd/antigravity/print_slash.go` — `printSlashResult` gained `Name`, `Data`
  and `ErrorText`; a `rejected(code, msg)` helper keeps the `error: `-prefixed
  stderr text and the unprefixed JSON `error` member in step from one string.
  `printSlashModels` was split into `printSlashModelID` (resolution + error) and
  `printSlashModelData`; `printSlashSkills` was split into
  `printSlashSkillInfos` + the text and data renderers.
* `cmd/antigravity/main.go` — `runPrintMode` resolves the format once, wraps
  the local-command result, buffers the model turn's answer (the reference's
  `response` is the answer text, without the trailing newline `text` appends),
  collects `usage` from the terminating chunk's `ChunkMeta`, and emits the
  envelope while keeping the existing stderr lines and exit code.

`text` remains the default and is byte-identical to before.

## 4. Verification

* **Byte-level comparison against the reference, 9 local commands.** Every one
  matches exactly, stdout and stderr:

  ```
  help: BYTE-IDENTICAL          permissions: BYTE-IDENTICAL    agents: BYTE-IDENTICAL
  model: BYTE-IDENTICAL         hooks: BYTE-IDENTICAL          effort: BYTE-IDENTICAL
  config: BYTE-IDENTICAL        changelog: BYTE-IDENTICAL      skills: BYTE-IDENTICAL
  ```

  (`/skills` needed the `plugin` member added to get there.)
* **Failure paths.** `/credits` (rc 1) and `/clear` (rc 2) are byte-identical on
  both streams.
* **Model-turn failure.** Against a local 429 server, the envelope is identical
  to the reference capture apart from the random UUID and `error_id`:

  ```
  {"conversation_id":"5aeda467-…","status":"ERROR","response":"","error":"Individual quota reached. …","duration_seconds":0,"num_turns":1,"usage":{…all zeros…}}
  error: Individual quota reached. …
  AGY_ERROR: {"short_error":"RESOURCE_EXHAUSTED (code 429): …",…}
  ```

  rc 3, stderr unchanged.
* **Model-turn success.** A real turn through the gateway returns
  `status:"SUCCESS"`, `num_turns:1`, the real `duration_seconds`, and a `usage`
  block whose `total_tokens` equals input + output (3370 + 27 = 3397).
* `text` is unchanged: `agy -p=/help` is byte-identical to the reference.
* `go build`, `go vet`, `go test ./cmd/... ./internal/...` — clean; `gofmt`
  clean on every file touched. Nine new tests in `print_format_test.go`.
* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** (both documented diffs). One earlier run reported
  14/25 with a dozen `124/…` rows; the reference is quota-degraded and
  intermittently hangs past the 25 s timeout, and the immediate re-run returned
  the baseline. Treat `124/…` rows as reference noise.

## 5. Divergences and limitations

* **`--output-format stream-json` is not implemented.** The flag parses, but the
  format currently falls through to `text`. The shape is already captured —
  `{"event":"init","conversation_id":…,"init":{"cwd","tools","permission_mode"}}`,
  `{"event":"step_update","step_update":{…}}`, `{"event":"command_result","command":{…}}`,
  `{"event":"result","result":{…the envelope above…}}` — and is the next cycle.
* **The model-turn success *sequence* is unrecovered.** The `result` envelope is
  pinned, but the `step_update` sequence for a successful turn (which step types
  appear, in what order) cannot be captured while the account is exhausted. Only
  the failure sequence (`user_input`, then `error_message`) is 【一手】.
* **`usage` comes from the agw transport, not from Cortex.** `input_tokens`,
  `output_tokens`, `thinking_tokens` and `total_tokens` map onto
  `ChunkMeta.PromptTokens` / `CompletionTokens` / `ReasoningTokens` /
  `TotalTokens`. `cache_read_tokens` has no counterpart there and stays 0, and
  the reference's own arithmetic for `total_tokens` is unverified (ours is
  whatever the gateway reports, which happened to equal input + output).
* **`duration_seconds` and `conversation_id` are 【推测】 on the success path.**
  Only the failure capture exists, where the reference reported `0` and a UUID.
  The clone measures real elapsed seconds and generates a fresh UUID per run.
* **`/usage` still fails.** The reference's `/usage --output-format json`
  *succeeds* (`status:"SUCCESS"` with a `groups[].buckets[]` payload), but it
  reads a live quota service the clone cannot reach, so the clone keeps the
  recovered failure text — now emitted as an ERROR envelope with rc 1. This is
  the pre-existing divergence, unchanged in substance.
* **`is_default` is emitted as `false`.** It was `false` in all four `/model`
  captures (with and without `--model`, three different models), so the clone
  matches every observation, but the flag's real meaning is unrecovered.
* **`model_invocable` is always `true`.** True for all 57 skills on this
  machine; the frontmatter marker that would clear it is unrecovered.
* **`/permissions` always reports project and shared scopes as empty.** The
  reference does too *on this machine*; the clone has no project- or
  shared-scope config reader, so it emits them empty unconditionally.
* **`printSlashConfig` (the text answer) was left untouched.** The JSON payload
  uses the new typed reader; the text renderer keeps its own path so the
  byte-verified text output cannot drift. The two could disagree for an unusual
  settings.json (e.g. a key present but explicitly `null`).
* **`configValues` re-reads settings.json** for the payload while the text
  renderer reads it again. Harmless, but it means the two are not atomic.
