# Cycle `command-output-truncation` — what `run_command` actually returns

**Status**: implemented, all green (`internal/tools`)
**Reference**: `~/.local/bin/agy` 1.4.4 (live turns, 2026-10-22)
**Evidence**: `captures.md` in this directory — the `functionResponse.response.output` of 13
real `run_command` calls, read out of the follow-up `streamGenerateContent` request bodies
captured by `cmd/agy`

## Why

`run_command` is the most-used tool in the surface and the only one whose result text had
never been captured. `internal/tools/testdata/result_texts_1.4.4.txt` has 32 call/result
pairs and **none of them is `run_command`**; the clone's text came from a 1.4.2-era
reconstruction and had drifted from the wire in five ways. The truncation behaviour was not
reconstructed at all: `internal/tools/output_budget.go` invented a `_truncated: true` /
`_preview: |` wrapper that the reference never emits.

Now that the Google path answers again, one `-p` turn per probe is enough: the model calls
`run_command`, and the reference sends the result back in the *next* request. That request
body is the ground truth — not the TUI, not a `strings` hit.

## The plain result (一手)

`printf 'a\nb\n'` →

```
Created At: 2026-10-22T11:14:02+08:00
Completed At: 2026-10-22T11:14:03+08:00
Command exited with code 0.

Command output:
a
b
```

`true` →

```
Created At: …
Completed At: …
Command exited with code 0.

No output from the command.
```

`sh -c 'echo out; echo err 1>&2; exit 3'` → `Command exited with code 3.` then
`Command output:\nout\nerr\n` (stderr merged, non-zero exit is data).

Five corrections against what the clone printed before this cycle:

| | clone (before) | reference 1.4.4 |
|---|---|---|
| blank line after the stamp | yes (`…\n\n`) | **no** |
| exit line | `The command exited with code N.` | `Command exited with code N.` |
| output header | `Output:` | `Command output:` |
| empty output | `Output:\n\n` | `No output from the command.` |
| trailing newline | one appended | none — the output's own bytes only |

## Line endings: CR is a line break, LF is left alone (一手)

| probe | result |
|---|---|
| `printf 'a\nb\n'` | `a\nb\n` — **no CRLF rewrite** |
| `printf 'a\r\nb\n'` | `a\n\nb\n` — the CR becomes an LF, so an existing CRLF turns into a blank line |
| `printf 'a\rb'` | `a\nb` — a bare CR is a line break |

The transform is `stripANSI` then `strings.ReplaceAll(s, "\r", "\n")`.

### Cycle 277's capture was the clone's own output

Cycle 277 recorded the opposite — `stripANSI` plus a naive `LF → CRLF` rewrite, with
`a\r\nb\n` coming out `a\r\r\nb\r\n` — and that claim reached `AGENTS.md` and four test
assertions. The string cycle 277 quotes as "reference (captured)" is

```
'Created At: …\nCompleted At: …\n\nThe command exited with code 0.\nOutput:\na\r\r\nb\r\nBOLD\r\n…'
```

which is **byte-for-byte the clone's `formatCommandResult` at that commit**, including the
leading `The` and `Output:` lines the reference does not print. A reference capture cannot
contain the clone's own wording, so that string never came from `agy`; it most likely came
from re-reading the clone's result. Every probe in this cycle contradicts it, on three
independent commands, and the print/TUI paths share one executor, so the clone was
corrupting every multi-line command output by doubling its LFs.

## Truncation (一手)

The result is echoed verbatim while the **body** (exit-code line + `Command output:` +
output) is at most **10240 bytes**: a 10195-byte output (10240-byte body) comes back whole,
10196 (10241) does not. Above that, the body is spilled to a file and replaced:

```
Created At: 2026-10-22T11:00:29+08:00
Completed At: 2026-10-22T11:00:29+08:00
The tool's output was truncated due to being too long. You can find the full output in a file at file:///Users/gear/.gemini/antigravity-cli/brain/<uuid>/.system_generated/steps/2/output.txt.

Here is a preview with the middle of the output truncated:
Command exited with code 0.

Command output:
<head>
<truncated 119072 of 128964 bytes from the middle of the output>
<tail>
```

Four things to note:

- **The full output survives** at
  `brain/<conversation>/.system_generated/steps/<n>/output.txt` — the same per-conversation
  `steps/<n>/` tree `read_url_content` uses for `content.md`. `cmd/agy`'s capture shows
  `steps/2` for the second tool step of that turn, so the counter is shared across tools
  (`nextStep`, renamed from `nextURLStep`).
- **The marker is an arithmetic summary of the body**, not of the head/tail that survive:
  `kept = 9894` (header included) in every capture, so `dropped = body − 9894`. For
  newline-free output the surviving content really is 4902 + 4947 bytes, which is where
  `commandPreviewHead` / `commandPreviewTail` come from.
- **The preview is CRLF-terminated.** This is the only place the reference emits CRLF — the
  plain path does not. A terminal-render of the spilled file, not a rewrite of the command
  output.
- **The note is only honest if the file exists.** `spillOutput` returns an error when the
  brain directory is unwritable, and `renderResult` then omits the note (the marker still
  says what was dropped).

## What changed

| File | Change |
|---|---|
| `internal/tools/command.go` | `RunCommand` gained `ConversationID` / `Home`; `formatCommandResult` → `renderResult` + `commandBody` + `commandStamp` + `spillOutput`; `maxCommandBodyBytes`, `commandPreviewKept`, `commandPreviewHead/Tail` |
| `internal/tools/ansi.go` | `normalizeCommandOutput` is now `CR → LF`, not `LF → CRLF` |
| `internal/tools/stubs.go` | `nextURLStep` → `nextStep` (one counter per conversation, as the reference) |
| `internal/tools/command_output_truncation_test.go` | replays the captured plain/empty/threshold/truncated strings; asserts the spill file, the marker arithmetic, the CRLF preview and the no-note-when-unwritable fallback |
| `internal/tools/{ansi,tools,git_editor_disable}_test.go` | assertions moved from the clone's wording to the captured one |

`ApplyOutputBudget` no longer touches `run_command`: the tool caps its own body at ~10 KB,
well under the 32 KB budget, so the invented wrapper can only appear for other tools.

## 硬约束

- The plain path, the threshold and the note/marker wording are **byte-exact** against the
  captures; the tests spell the strings out rather than deriving them.
- The spill path is reproduced only in shape. The clone cannot know the reference's brain
  uuid or step number; it mints its own from `OPENAGY_CONVERSATION_ID` (or the process id)
  and the shared step counter. The `file://` URI therefore differs from the reference's
  while pointing at a real file.
- `output.txt` receives the normalised text (`stripANSI`, `CR → LF`) — i.e. what the model
  would have seen without truncation, re-indented by nothing. The capture shows the file's
  path, not its bytes, so this is the only defensible choice.

## 存疑

- **The head/tail split is tokenizer-driven, not byte-driven.** Measured on the same
  total: newline-free output keeps 4902 + 4947; 101-byte lines keep 4854 + 4898; `"a\n"`
  lines keep 3269 + 3300. The clone uses the newline-free split for every shape, so for
  newline-rich output its preview shows ~3 KB more content than the reference's. The
  *marker* is unaffected (it reports the fixed 9894).
- **The declared `total` is not exactly `header + LF→CRLF(output)` for every shape.** It
  matches exactly for newline-free output (`20045`) and for 101-byte lines (`20245`), but
  the `"a\n"×10000` capture reports `30073` where the formula gives `30045`, and `seq 1
  20000` reports `128964` where it gives `128939`. 25–28 bytes of content-dependent
  overhead are unaccounted for; the clone emits the formula.
- **The threshold is measured on the body, not on the converted text.** The 10195/10196
  probe uses newline-free output, so whether a newline-rich output crosses the boundary by
  its raw or its rendered length is untested.
- Cycle 277's report is left as written; this cycle contradicts it. Anyone citing the
  `a\r\r\n` string should cite `captures.md` instead.

## limitations

- 13 probes, one model, one turn each. Truncation was only exercised through
  `run_command`; whether `manage_task` / background output follows the same path is
  untested (the background tool has its own `bgLogTailBytes` marker, which is a *different*
  string).
- The TUI's transcript rendering of a truncated result (cycle 285's reduced detail body)
  was not captured — only the model-visible text.
- No end-to-end assertion that a live turn's tool result now equals the reference's; the
  tests pin `renderResult`, and `internal/resultparity` does not yet read a `run_command`
  section because the fixture has none. Adding `run_command` sections to
  `result_texts_1.4.4.txt` (with the brain path normalised) would close that gap.
