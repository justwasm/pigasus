# The headless surface

> Where the clone answers without a terminal. `--print` is the whole
> headless surface: the envelope, the NDJSON stream, the stdin pipeline, the
> slash commands the CLI answers itself, and the exit codes.

The headless path lives in `internal/printmode` (**an entry surface, above
tier 5** — the ui tier may not import it, invariant 119). It runs PiG's
`coding.Session`, the same engine the interactive TUI's `--print` path
uses; the two turn engines are described in
[`agent-loop.md`](agent-loop.md).

| concern | where |
|---|---|
| the flags | `internal/cli/parser_flags.go` — `standardFlagsCore` / `standardFlagsTail` |
| the envelope and the stream shapes | `internal/printmode/print_format.go` |
| the run | `internal/printmode/main_print.go`, `print_pig.go`, `main_print_tools.go` |
| the stdin pipeline | `internal/printmode/stream_json.go` |
| the slash commands the CLI answers itself | `internal/printmode/print_slash.go` and its siblings |
| the JSON schema flag | `internal/printmode/json_schema.go` |
| the interrupt guard | `internal/printmode/print_interrupt.go` |

---

## The three output formats

`--output-format` accepts exactly three values and defaults to `text`
(`internal/cli/parser_flags.go`, `output-format`):

| value | what stdout carries |
|---|---|
| `text` | the model's answer, as plain text |
| `json` | **one** JSON object — the envelope |
| `stream-json` | **NDJSON** — one JSON event per line |

`--input-format` mirrors it (`text` by default; `stream-json` reads one
NDJSON message per line from stdin and runs a turn for each). The two are
coupled: `--input-format stream-json` **requires** `--output-format
stream-json`, and the clone refuses the combination otherwise with

```
Error: --input-format stream-json requires --output-format stream-json
```

---

## The envelope

`--output-format json` prints exactly one object, and the `result` event of
a `stream-json` run carries the **same** object.

```json
{"conversation_id":"","status":"SUCCESS","response":"/agents\tList available…",
 "duration_seconds":0,"num_turns":0,
 "usage":{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0},
 "command":{"name":"help","data":{"commands":[{"name":"agents","description":"List available custom agents"},…]}}}
```

The member order is the reference's wire order
(`internal/printmode/print_format.go`, `jsonEnvelope`):

| member | notes |
|---|---|
| `conversation_id` | `""` for anything answered without a session; a UUID once a model turn starts |
| `status` | exactly two values — `SUCCESS` and `ERROR` |
| `response` | the answer, as text |
| `error` | **omitempty** — the failure text |
| `duration_seconds` | the turn's wall time |
| `num_turns` | `0` for a local command, `1` for a model turn — **even when that turn fails** |
| `usage` | the five counters: `input_tokens`, `output_tokens`, `thinking_tokens`, `cache_read_tokens`, `total_tokens` |
| `command` | **omitempty** — the structured form of a local slash command's answer |
| `finished_reason` | **omitempty** — see below |

### The `status` vocabulary is two, not seven

The documentation lists seven `status` values — `SUCCESS`, `ERROR`,
`CANCELED`, `INTERRUPTED`, `INVALID`, `WAITING`, `RUNNING`. That table is
the **step**-status enum, not the envelope's: the reference's own
`CORTEX_STEP_STATUS_*` enum has thirteen members and **no `SUCCESS`**
(【strings】). The envelope's two values live at
`printmode.printStatusSuccess` / `printStatusError`.

### `finished_reason` is the last member, and it is omitted on a normal finish

【一手】 agy 1.4.11 added the field: it says why a turn ended **early** —
`print_timeout` when `--print-timeout` expired and the response is partial,
`interrupted` when the run was cancelled (e.g. with `Ctrl+C`), and `error`
for any other failure. The field is **absent** when the turn finishes
normally.

`command` and `finished_reason` never travel together — a command result is
answered locally and finishes normally — so their relative order is
unobservable; `finished_reason` is last in the struct because the reference
*added* it to the 1.3.6 field order.

**The interrupt has to be caught.** An uncaught `SIGINT` kills the process,
and a killed process writes no envelope — so the `interrupted` reason would
be unreachable. The guard (`internal/printmode/print_interrupt.go`) cancels
the turn's context on the **first** `Ctrl+C` and restores the default
disposition with `signal.Reset` **before** the cancel, so the second
`Ctrl+C` stays fatal.

---

## The stream

`--output-format stream-json` emits **one JSON object per line** (NDJSON).
The event names the clone emits:

| event | when |
|---|---|
| `init` | once, before the model is called |
| `step_update` | once per step |
| `partial_response` | once per text chunk — only with `--print-partial-response` |
| `assistant_message` | the assistant text body — only with `--verbose` |
| `command_result` | a local slash command's answer |
| `result` | the terminator — it carries the envelope |

The `init` payload carries the session's starting state:

| member | notes |
|---|---|
| `cwd` | the workspace |
| `tools` | the advertised tool names |
| `permission_mode` | `request-review` by default, `always-proceed` under `--dangerously-skip-permissions` |
| `model` | **only** when `--model` was given |
| `agent` | **only** when `--agent` was given |
| `json_schema` | **only** when `--json-schema` was given |

The omit rules matter: a payload that always carries the *resolved default*
model is a divergence of the same kind as a missing member.

The `step_update` payload carries the step's identity and state:

| member | notes |
|---|---|
| `conversation_id` | |
| `step_index` | **zero-based** |
| `state` | `DONE` for a finished step |
| `step_type` | |
| `tool_name` | omitted when the step is not a tool call |
| `text_delta` | omitted when the step carries no text |
| `duration_seconds` | omitted when the step has no duration |
| `usage` | omitted when the step has no counters |
| `tool_info` | the tool call's `name`, `parameters`, `output` and an `error` object with `type` and `message` |
| `subagent_info` | omitted when the step is not a subagent |

The reference's own struct tags are the authority for these names
(【strings】): `json:"tool_name,omitempty"`, `json:"tool_info,omitempty"`,
`json:"text_delta,omitempty"`, `json:"subagent_info,omitempty"`,
`json:"step_type,omitempty"`. **A member the clone has no fact for is
omitted** — a member that renders `null` or a zero is worse than an absent
one.

Every `assistant_message` event ends with a `\n`, so the shell prompt that
runs after the CLI does not glue to the last byte of stdout.

---

## The JSON schema

`--json-schema` takes a schema **string**, a path to a `.json` schema file,
or a primitive type name (`string`, `number`, `integer`, `boolean`).

Two rules are the reference's own and are reproduced byte for byte:

```
Error: --json-schema can only be used when --output-format is 'json' or 'stream-json'
Error: invalid --json-schema: %v
```

The **object-root** rule is the reference's too: the root must be a JSON
object whose `"type"` is exactly `"object"`. A primitive *name* is a
shorthand that expands to `{"type":"<name>"}` **before** that root check —
the root check itself is not widened.

With `--json-schema` set, the envelope gains two members, both
**omitempty**:

| member | notes |
|---|---|
| `structured_output` | the parsed schema output |
| `json_schema` | the schema that was enforced |

---

## The slash commands the CLI answers itself

In print mode the reference expands slash commands **client-side**. Three
outcomes:

| outcome | example | exit code |
|---|---|---|
| **handled locally** | `agy -p /help` — the command table, no model call | `0` |
| **rejected** | `agy -p /clear` — "not available in print mode" | `2` |
| **forwarded** | anything else (`/plan`, `/goal`, `/init`, `/xyz`) — sent to the model as literal text | the model's |

The rejection carries the reference's own line:

```
error: /clear is not available in print mode (<reason>); pass --disable-slash-commands to send /clear to the model as literal text
```

`--disable-slash-commands` suppresses the first two outcomes, so **every**
slash command is forwarded as literal text.

The commands the clone answers itself: `help`, `changelog`, `feature-diff`
(aliases `featdiff`, `diff-agy`), `model`, `config`, `effort`, `hooks`,
`agents`, `agents-dirs`, `permissions`, `skills`, `usage`, `credits`,
`accounts`, `pool`.

`/plugin` is **rejected** in print mode — it is a panel, and the print mode
has no panel.

`accounts` and `pool` are the clone's own **superset**: the reference has
one credential and no account command, and the print mode is one-shot, so
`/pool` *shows* the routing policy rather than opening a panel.

### The print mode's slash grammar is not the host's

The prompt is parsed with `slashparse.SplitFields` — the print mode's rule,
whose separator set is **every Unicode whitespace rune**. The interactive
host parses with `slashparse.Split`, whose separator set is **space, tab and
newline**. The two rules are neighbours in the same tier-1 package and are
**not** the same rule: they differ observably at `\v`, `\f`, `\r` and at
every Unicode whitespace rune, and the difference is reachable from a real
dispatcher (`agy -p $'/help\vfoo'` dispatches `/help` under the print rule
and is an unknown command under the host's).

Both live in `internal/core/slashparse` (tier 1) so a future unification is
a one-place change — but unifying them is a **behaviour change** and needs
its own evidence, not a refactor's.

---

## The exit codes

| code | when |
|---|---|
| `0` | the turn finished, or a local command answered |
| `1` | a local command failed (`agy -p /credits` with no credits info) |
| `2` | a print-mode slash command was rejected |
| `3` | the turn failed at the API (`apierror.HeadlessAPIExitCode`) |
| `130` | the run was interrupted (`Ctrl+C`) |

**The envelope is written to stdout on every path**, including the rc-2
rejections — their stderr line is unchanged.

**The headless soft-deny** is not a failure: a tool that needs an approval
it cannot obtain is **denied**, the run **continues**, and the exit code
stays `0`.

---

## The stdin pipeline

`--input-format stream-json` reads **one NDJSON message per line** from
stdin and runs a turn for each. The message's `message.content` is either a
string or an array of text blocks.

Two of the reference's own strings govern the edges:

```
warning: ignoring unsupported stream input message event %q
Error: --input-format %s reads prompts from stdin, so a prompt given on the command line would be ignored
```

and a slash command the CLI answers itself is refused:

```
/%s is answered by the CLI itself and is unavailable with --input-format stream-json; run it as its own --print /%s invocation
```

---

## The permissions in a headless run

`--dangerously-skip-permissions` reaches the **policy**: the session's
permission policy and its terminal sandbox are built from the flag
(`internal/app/session/setup/setup.go` — `PolicyFrom` / `SandboxFrom`).

The **reported** `permission_mode` follows it too: `always-proceed` under
the flag, `request-review` otherwise — and a `settings.json` value, when
present, still wins over the default.

---

## The known gaps

These are recorded in the cycle-383 ledger
(`docs/cycles/cycle-383-docs-alignment/slice-d-headless-sessions-sdk.md`)
and are **not** fixed:

| finding | what |
|---|---|
| D-03 | `--json-schema` is **validated** but never **enforced** — the bytes never reach the backend request, so the model is not constrained |
| D-04 | the envelope's `structured_output` and `json_schema` members depend on D-03 |
| D-05 | the primitive type names are a shorthand; the ledger flags the ambiguity and **does not** widen the object-root rule |
| D-10 | an unknown `--model` in headless mode produces **no** JSON envelope |
| D-11 | the documented headless `--effort` vocabulary is three levels; the clone's is five |
| D-13 | the SDK's `session_continuation_mode` / `initial_trajectory` are read from the wire but never acted on — the clone starts every session fresh |
| D-16 | the documented `--print-timeout` default of `5m` is **stale**; the clone's is `0` (no deadline) |

Report: `docs/cycles/cycle-383-docs-alignment/deliverable.md`.
