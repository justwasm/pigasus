# The status-line and window-title scripts — deliverable

**Cycle:** `cycle-301-statusline-title-scripts`
**Subject:** the clone's custom status line and custom window title were a
four-field invention; the reference pipes a **27-member JSON document** to
the script's stdin, and the upstream example scripts
(`examples/statusline/statusline.sh`, `examples/title/title.sh` from
<https://github.com/google-antigravity/antigravity-cli>) read members the
clone never wrote.
**Evidence:** 【一手】 agy 1.4.7 driven live — a capture script bound to
`statusLine.command` recorded every payload; the reference's own
`/statusline` and `/title` handlers were driven through every verb and the
resulting `settings.json` was read after each one; the two upstream
example scripts were downloaded and run against the clone.
【反编译】 the JSON tags were read out of the binary's string table
(`types.StatusLine*`).

---

## 0. Summary

The clone shipped a status line of its own invention:

```json
{"model": "…", "cost": 1.23, "current_dir": "…"}
```

and a window title that was the conversation title. The reference does
neither. It writes a **document** — `types.StatusLineData` — to the
script's stdin, and the same document to the window-title script. The
upstream examples read `agent_state`, `model.display_name`,
`context_window.used_percentage`, `vcs.type`, `sandbox.enabled`,
`artifact_count`, `subagents`, `task_count` and `terminal_width`; with the
clone's old payload every one of them was missing and the example scripts
rendered an empty row.

What this cycle delivers:

| | before | after |
|---|---|---|
| payload | 4 invented members | the reference's 27, same names, same order, same omit rules |
| script invocation | argv split | `sh -c "<command>"`, payload on stdin |
| settings block | `statusLine: "on"` (a string) | `{"type","command","padding","enabled","stack_with_default"}` |
| `/statusline` | a two-state toggle | the reference's subcommand set |
| `/title` | `title: "on"` | the reference's subcommand set, and a bare `/title` toggles |
| window title | the conversation title | the title script's output, sanitised |
| failure | a silent empty row | the reference's error card, then `⚠ Statusline disabled after 30 consecutive failures.` |

The reference's own documentation agrees with the measurements, and is
quoted where it settles a question the binary cannot:
<https://antigravity.google/docs/cli/statusline> and
<https://antigravity.google/docs/cli/title>.

---

## 1. The payload

`internal/statusline/payload.go` holds the document. The member order is
the reference's declaration order (`types.StatusLineData` is a Go struct,
and `encoding/json` writes the members in declaration order):

```
cwd, session_id, conversation_id, conversation_title, transcript_path,
model, workspace, version, context_window, exceeds_200k_tokens, agent,
product, quota, agent_state, vcs, cycle_mode, sandbox, subagents,
artifact_count, plan_tier, email, pending_input_count,
tool_confirmation_pending, task_count, terminal_width, vim, cost
```

`internal/statusline/payload_test.go` pins the order against a payload
agy 1.4.7 actually produced, and pins the **bytes** — the clone's document
for the reference's values is byte-identical to the reference's.

### The omit rules

The reference's members are not all written. Three of them are written
even when empty, and the live captures show which:

| member | when it is absent | evidence |
|---|---|---|
| `model` | written as JSON **null** | 【一手】 one payload carried `"model":null` while the model was still being resolved |
| `product` | absent while the CLI is starting up | 【一手】 the same payload had no `product` member; later payloads carried `"antigravity"` |
| `version` | written as `""` | 【一手】 `"version":""` |
| `exceeds_200k_tokens` | written as **null** | 【一手】 `"exceeds_200k_tokens":null` before the first response |
| `context_window.current_usage` | written as **null** | 【一手】 `"current_usage":null` |
| everything else | absent when empty | 【一手】 14 captured payloads |

`internal/statusline/payload_test.go` has a test for the empty document:
it is exactly

```json
{"cwd":"","session_id":"","conversation_id":"","model":null,
 "workspace":{"current_dir":"","project_dir":""},"version":"",
 "context_window":{"total_input_tokens":0,"total_output_tokens":0,
 "context_window_size":0,"used_percentage":0,"remaining_percentage":0,
 "current_usage":null},"exceeds_200k_tokens":null}
```

### `vcs` carries the type only

The upstream example reads `.vcs.branch` and `.vcs.dirty` — and the
reference **never fills them**. agy 1.4.7 was run twice with a capture
script bound to `statusLine.command`: once in a clean repository, once in a
dirty one on `main`. All **14** payloads carried

```json
"vcs": {"type": "git"}
```

and neither `branch` nor `dirty` ever appeared. The example script's
`// ""` and `// false` fallbacks absorb this, so the upstream script
renders the branch segment only on a build that does fill it.

### The values

| member | the reference's value | the clone's |
|---|---|---|
| `used_percentage` | `total_input_tokens / context_window_size * 100` — 19136/1048576 = `1.824951171875` | same formula |
| `model.id`, `model.display_name` | the label *with* the tier suffix (`Gemini 3.8 Flash (High)`) | same |
| `model.effort` | `high` | same |
| `transcript_path` | `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl` | same shape |
| `quota` | bucket ids `3p-5h`, `3p-weekly`, `gemini-5h`, `gemini-weekly` | same ids |
| `agent_state` | `initializing`, `authenticating`, `idle`, `working` | same |

---

## 2. The settings block

### The shape

Both blocks are objects. The reference re-encodes them from its own
struct, so the member order on disk is the declaration order:

```json
"statusLine": {
  "type": "command",
  "command": "~/bin/statusline.sh",
  "padding": 1,
  "enabled": true,
  "stack_with_default": true
}
```

`padding` and `stack_with_default` are omitted when they hold their zero
value; `enabled` is always written when the reference writes it.

**`enabled` defaults to true.** 【一手】 agy 1.4.7 ran a title script whose
block read `{"type":"command","command":"…"}` — no `enabled` member — and
the script ran. The reference's documentation says the same: *"`enabled`
(boolean, default `true`)"*.

**A boolean block is an object without a command.** The reference's
documentation: *"Set `statusLine` to an object, not to `true` or `false`.
The CLI reads a boolean as an object without a `command`, so no script
runs."* The clone keeps reading the legacy shape that way.

### What each command writes

Measured by driving agy 1.4.7 and reading `settings.json` after every
verb:

| command | the block afterwards |
|---|---|
| `/statusline <cmd>` | `{"type":"command","command":"<cmd>"}` — **no `enabled` member**, `padding` and `stack_with_default` survive |
| `/statusline on` | the block plus `"enabled": true` |
| `/statusline off` | the block plus `"enabled": false` |
| `/statusline delete` | **the `statusLine` key is removed from the file** |
| `/title on` | the block plus `"enabled": true` |
| `/title off` | the block plus `"enabled": false` |

Two of these are counter-intuitive and are worth writing down:

- **`/statusline <cmd>` drops the `enabled` member.** The member's default
  is true, so the script starts running immediately — which is what the
  reference's documentation promises ("This immediately updates your
  settings and starts running the script to render the status line").
  A block that was explicitly `enabled: false` comes back *without* the
  member, so it is on again.
- **`/statusline delete` removes the key.** It does not leave an empty
  `{"type":"","command":"","enabled":false}` block behind.

---

## 3. The commands

### `/statusline`

```
/statusline                 → the usage block
/statusline <command>       → "Statusline set to: <command>"
/statusline on | enable     → "Statusline on."  (" (<command>)" when one is configured)
/statusline off | disable   → "Statusline off."
/statusline delete | reset  → "Custom statusline command cleared. Reverted to built-in default."
/statusline help            → the usage block
```

The subcommand names are matched case-insensitively (the upstream
changelog for 1.0.1 records that fix). The usage block's row order is the
reference's:

```
Usage:
     /statusline                 Toggle statusline on/off.
     /statusline <command>       Configure a custom shell command to render the statusline.
     /statusline delete          Delete the custom command and revert to the built-in default.
     /statusline on | enable     Enable the statusline.
     /statusline off | disable   Disable the statusline.
     /statusline help            Show this help message.
```

### `/title`

```
/title           → toggles the feature
/title on        → "Custom title enabled. (<command>)"
/title off       → "Custom title disabled."
/title <other>   → "Usage: /title [on|off]"
```

with one precondition: **every** verb needs a configured title command.
Without one the reference answers

```
Error: No title command configured in settings.json. Please add a "title" block.
```

The argument is validated *before* the precondition, so `/title bogus`
prints the usage line even when no command is configured. The `%s` in
`Custom title enabled. (%s)` is the **command**, not the conversation
title — 【一手】 the reference answered `Custom title enabled.
(/tmp/agyverify/title-probe.sh)`.

---

## 4. Running the script

`internal/statusline/script.go`:

- the command runs through a shell — `sh -c "<command>"` (`cmd /c` on
  Windows) — because the reference rendered a command of
  `~/bin/probe.sh | cat`, which only works through a shell;
- a leading `~` expands to the home directory;
- the payload is written to the script's **stdin** and stdin is closed;
- a run is given up after **5 s**, and the CLI runs the script at most
  once every **300 ms**;
- after **30 failed runs in a row** the script stops running for the rest
  of the session and the status area shows
  `⚠ Statusline disabled after 30 consecutive failures.`

### The timeout needed a fence

`exec.CommandContext` kills the *shell*, not the shell's children, and a
child that holds the stdout pipe open keeps `Wait` blocked long past the
deadline. `cmd.WaitDelay` bounds that: the wait ends shortly after the
context is cancelled even when the pipes are still open. Without it a
`sleep 30` inside the script blocked the runner for 30 s — the test
`TestRunScript_Timeout` caught it.

### The title's sanitising

The reference's title script printed

```
TITLE-A\nTITLE-B\t\x1b[31mCOLOR\x07\n
```

and the window title became `TITLE-A TITLE-B[31mCOLOR`: the tab, the
escape and the bell characters are gone, the `[31m` survives as literal
text (ANSI is **not** interpreted), and the two lines are joined by one
space. `statusline.SanitizeTitle` implements exactly that.

---

## 5. The rendering

- a custom row **replaces** the built-in row; `stack_with_default: true`
  puts it *below* the built-in one instead;
- `padding: N` puts N blank lines above the custom row;
- a failing script shows the reference's error card:

```
⚠ Statusline Error
  ⎿  Statusline error:
     command failed: exit status 3 (stderr: boom)
     Full logs at: <config-home>/log/cli-<timestamp>.log
```

All four of these were confirmed live against the clone with the upstream
example scripts and with a deliberately failing one.

---

## 6. Two bugs the live run found

### The window title was an unsynchronised string

The title script runs on the tick goroutine and the renderer reads the
title on the model's goroutine. A Go string is two words, and the writer's
store of the length can be observed before its store of the pointer — on
arm64 the reader saw an **empty** title, so the window title fell back to
the program name and the title script's output never reached the
terminal.

The fix has two halves:

1. the payload is built on the **model's** goroutine, so the session's
   state (the quota cache, the token counters) is never read from another
   one — the quota cache is an unguarded map, and a concurrent read during
   a write is a fatal error;
2. the title itself moved behind a mutex (`internal/cliapp/windowtitle.go`).

### `/statusline <command>` dropped the layout members

The in-memory copy of the block was replaced wholesale by
`Config{Type: "command", Command: command, Enabled: true}`, which lost
`padding` and `stack_with_default`. The file on disk was correct; only
the running session lost the layout. The handler now mutates the cached
block instead of replacing it.

---

## 7. 硬约束 (hard constraints)

- **The payload is the reference's `types.StatusLineData`.** Its member
  names, member order and omit rules are a wire contract: the upstream
  example scripts read them by name, and `payload_test.go` pins all three
  against a captured reference payload.
- **`model` is written as JSON `null`, not omitted.** The upstream script
  reads `.model.display_name`; a missing member and a null member are not
  the same to `jq`.
- **`enabled` defaults to true.** A block without the member is on. This
  is the reference's documented default and its observed behaviour.
- **`/statusline delete` removes the `statusLine` key.** Do not leave an
  empty block behind.
- **The command runs through a shell.** `run_command` is a shell for the
  same reason `run_command` is a shell in the turn loop (§"Transports and
  tools" in `AGENTS.md`): a pipe or a redirection in the user's command
  has to work.
- **The script's output is never interpreted as ANSI.** The status line
  keeps the escape sequences (the upstream script uses them); the window
  title drops the control characters and keeps the rest as literal text.

## 8. 存疑 (open questions)

- **The reference's status-line and title blocks may not be the same Go
  type.** The status-line block carries `padding` and `stack_with_default`
  and the title block did not gain them, but both may be one struct with
  the two members omitted when zero. The clone writes one struct for both.
- **`/statusline` with no argument shows the usage block in agy 1.4.7**,
  while the reference's documentation says the bare command *toggles* the
  status line. The clone follows the binary (1.4.7 is the version it
  clones), but a later reference release may have changed this.
- **The reference's `quota` buckets and their reset cadence** were taken
  from the captured payloads; the bucket ids are stable, but the payload
  does not say when a bucket is fetched.
- **`vcs.branch` and `vcs.dirty` exist in the reference's struct** — the
  binary's string table carries `json:"branch,omitempty"` and
  `json:"dirty,omitempty"` — but 1.4.7 never filled them. Whether a later
  release does is unknown.

## 9. Limitations

- **The member order *inside* the settings block differs.** The reference
  writes `type, command, padding, enabled, stack_with_default`; the clone
  writes them alphabetically, because the shared settings formatter
  (`internal/auth`'s `formatSettingsJSON`) sorts every object it
  re-prints. The reference sorts only the *top level* of the file and
  keeps nested objects in their own order. Fixing this means changing
  that formatter, which touches every settings write in the clone — it
  was left alone here. The JSON is semantically identical either way.
- **The clone's `context_window_size` is 200 000 where the reference
  reported 1 048 576** for the same model. That is a separate, pre-existing
  divergence in the model table; it changes `used_percentage` by a factor
  of five but not the shape of the payload.
- **The payload was captured on one machine, one model and one
  repository.** The values that depend on the account (quota buckets, the
  plan tier, the e-mail) were seen in one state each.
- **The 30-failure budget was verified end to end, but the 5 s timeout
  was not measured against the reference's own clock** — it comes from the
  reference's documentation, not from a stopwatch on the binary.
- **The window title was captured through a pty**, not through the
  reference's own screen: the OSC 2 sequence the clone emits was compared
  byte for byte, but the reference's title handling was verified through
  its settings writes and its messages, not through its terminal output.
