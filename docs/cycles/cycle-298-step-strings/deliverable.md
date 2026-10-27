# The reference's step strings — deliverable

**Cycle:** `cycle-298-step-strings`
**Subject:** the reference renders a command step with its own template
(`step_strings/run_command.tmpl`), and the clone now renders it too — plus
the reader that makes both of the reference's result shapes parse
**Evidence:** 【一手】 the templates and the reference's own artifacts — the
step-string record in the reference's `transcript.jsonl` (2026-10-20), the
1,023 command results in the reference's 1.4.7 conversation store, the 587
background-task results in the same store, and the string table of **agy
1.4.7** (`~/.local/bin/agy`, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`) and of
the IDE's `language_server`

---

## 0. Summary

Cycle 297 recovered the reference's seventeen prompt templates and left the
two **step strings** on the table, with a note that the template and the
clone's own `run_command` result text disagree. This cycle settles that
disagreement and wires the template in.

The answer is not "the template is the reference's result text". It is:

> **The reference has two command-result shapes, both live, one per era.**
> The template renders the older one; the CLI grew its own formatter for the
> newer one.

| | Shape | Era | Evidence |
|---|---|---|---|
| **step string** | `The command exited with code N.` + `Output:` | 1.4.2 / 1.4.3 | the reference's own `transcript.jsonl` (11 records, 2026-10-19/20) |
| **current** | `Command exited with code N.` + `Command output:` | 1.4.4 → 1.4.7 | the live 1.4.4 functionResponse capture + 1,023 records in the reference's 1.4.7 store |

The clone's own `run_command` result is the **current** shape — correct, and
pinned by `internal/tools/command_output_truncation_test.go`. But the clone's
**consumers** of that result were written against the **step-string** shape:

- `internal/tuitools.CommandExitCode` matched `The command exited with code
  (-?\d+)\.` and nothing else, so **the clone's own results never parsed**.
- `internal/convstore.cleanToolResult` stripped `The command exited with
  code…\nOutput:\n` and nothing else, so the resumed-conversation view
  showed the whole boilerplate.

That is a live bug, not a cosmetic one: with `CommandExitCode` returning
`ok == false`, `internal/cliapp/tui_tools.go` never marked a failed command
as failed, and the `Created At` / `Completed At` stamp reached the screen —
which cycle 285 measured does not happen in the reference.

**What this cycle ships**

| | |
|---|---|
| `internal/agytemplates` | the recovered bundle, embedded **once**, with the two consumers below |
| `internal/agystep` | the step-string layer: the data model the templates read, the renderer, and the reader for both result shapes |
| `internal/tuitools` | the reader wired in → the failed-command badge and the stamp-free detail are back |
| `internal/convstore` | the reader wired in → a resumed conversation shows the output, not the boilerplate |
| `internal/tools` | `send_command_input` and the background branch of `run_command` now speak the reference's own wording, rendered **through the recovered template** |

---

## 1. The two shapes, side by side

**The step string** — what `step_strings/run_command.tmpl` renders, and what
the reference produced at 1.4.2/1.4.3. The template's own leading newline
included, so the record reads:

```
Created At: 2026-10-20T02:27:51+08:00
Completed At: 2026-10-20T02:27:51+08:00

The command exited with code 0.
Output:
PROBE-0=zsh
PROBE-SHELL=/opt/homebrew/bin/bash
zsh
PROBE-DONE

```

【一手】 — the record is in the reference's own
`~/.gemini/antigravity-cli/brain/<conv>/.system_generated/logs/transcript.jsonl`
(2026-10-20). `internal/agystep/step_test.go` renders the template, prefixes
the stamp and compares the whole thing byte for byte.

**The current shape** — the CLI's own formatter:

```
Created At: 2026-10-22T11:14:02+08:00
Completed At: 2026-10-22T11:14:03+08:00
Command exited with code 0.

Command output:
a
b
```

【一手】 — the live 1.4.4 functionResponse capture in
`docs/cycles/command-output-truncation/captures.md`, and 1,023 records in
the reference's 1.4.7 conversation store under
`~/.gemini/antigravity-cli/conversations/*.db`.

### Why the two coexist

The string table answers it. In `strings -a ~/.local/bin/agy` (and in the
IDE's `language_server`, identically):

| String | Occurrences | Where |
|---|---|---|
| `Command exited with code %d.` | 1 | the CLI's own formatter |
| `No output from the command.` | 1 | the CLI's own formatter |
| `The command exited with code` | 2 | **both** inside the two step-string templates |
| `Background command ID:` | 1 | `step_strings/run_command.tmpl` |
| `Output snapshot:` | 1 | `step_strings/run_command.tmpl` |
| `Input sent successfully.` | 1 | `step_strings/send_command_input.tmpl` |
| `The command is still running.` | 1 | `step_strings/send_command_input.tmpl` |
| `Terminal ID:` | 1 | `step_strings/run_command.tmpl` |

So: the CLI has its **own** formatter for the plain command path, and
**every other** command-step wording exists in the binary *only* inside the
step-string templates. There is no third source.

The templates' converters live in
`google3/third_party/jetski/cortex/chatconverters/code/code`
(`RunCommandStringConverter.ToOutputString`,
`SendCommandInputStringConverter.ToOutputString` — 6 symbol hits each), so
the template is the **Cortex** path's renderer, and the CLI's formatter is
the CLI path's. The clone is a CLI clone, which is why the *plain* result
keeps the CLI formatter's bytes.

---

## 2. `internal/agytemplates` — the bundle, embedded once

The bundle moved out of `internal/agyprompt/templates/` (a `git mv`, so the
history follows) into `internal/agytemplates/templates/`, because it now has
**two** consumers:

- `internal/agyprompt` renders the `system_prompts/*.tmpl` sections;
- `internal/agystep` renders the `step_strings/*.tmpl` step strings.

`agytemplates.NewSet(funcs)` parses the whole bundle into one template set.
It has to be the whole bundle: `helpers/errors.tmpl` defines
`templateError` and `step_strings/helpers/default_status_output_string.tmpl`
defines `defaultStatusOutputString`, and the templates call both by name —
a set that carried only the step strings would fail at *render* time, not at
parse time.

The four bare identifiers the prompt templates call (`ArtifactDirectoryPath`,
`SystemGeneratedLogsPath`, `KnowledgeDirectoryPath`, `MetadataFileName`) are
registered as functions by the bundle itself, answering `""` until a caller
binds them — `internal/agyprompt` binds them to the render's data. That is
why `NewSet(nil)` works for the step strings.

The digest pinning moved with the bundle
(`internal/agytemplates/agytemplates_test.go`): the seventeen files are the
reference's, and an accidental whitespace fix in them would silently change
what the clone sends.

---

## 3. `internal/agystep` — the step-string layer

### The data model (`step.go`)

The templates read a *step proto* through getters. `internal/agystep/step.go`
answers exactly those names:

```
.Step.GetRunCommand.Get{ProposedCommandLine,CommandLine,CommandId,
                         CombinedOutput,CombinedOutputSnapshot,ExitCode,
                         SandboxBypassDisabled,SandboxboxedTrustedCli,
                         SandboxOverride,SandboxErrors,IsTrustedCli,
                         StdoutOutput,StderrOutput,TerminalId}
.Step.GetStatus.String
.Step.GetMetadata.GetSource.String
.Step.GetMetadata.GetToolCall
.Step.GetError.GetShortError
.Step.GetSendCommandInput.Get{Output,Running,ExitCode}
```

The status and source enums stringify to the **proto** names
(`CORTEX_STEP_STATUS_DONE`, `CORTEX_STEP_SOURCE_USER_EXPLICIT`), because
the templates compare against those.

### The renderer (`render.go`)

`RunCommandString(step)` and `SendCommandInputString(step)` execute the two
recovered templates and return their output **verbatim** — including the
leading newline, which is the template's own whitespace handling and is part
of the reference's 2026-10-20 record.

The two renderings that matter:

```
plain  = "\nThe command exited with code 0.\nOutput:\nhi\n\n"
bg     = "\nBackground command ID: cmd-1\nOutput snapshot:\npartial\n\n"
empty  = "\nThe command exited with code 0.\nNo output\n"
input  = "\nInput sent successfully.\nThe command is still running.\nOutput:\nso far\n\n"
```

`internal/agystep/step_test.go` walks **every** branch the template has —
the edited command line, the background id, the empty output, the terminal
id, the sandbox guidance, the trusted-CLI branch, the canceled step, the
failed step, the running step — so a template that lost one cannot pass
unnoticed.

### The reader (`result.go`)

`ParseRunCommandResult(text)` reads **either** shape and returns the exit
code, the output and whether there was an output block. It handles CRLF
payloads (the reference's stored results carry CRLF, because the command's
output arrives from a pty) and the truncation-preview shape.

---

## 4. The live bug this fixed

`internal/tuitools.CommandExitCode` matched

```go
regexp.MustCompile(`The command exited with code (-?\d+)\.`)
```

— the step string's wording. The clone's own `run_command` result says
`Command exited with code N.` (no "The"), so:

- `internal/cliapp/tui_tools.go:333` got `ok == false` for every command,
  and a **failed command was never marked as failed**;
- `tuitools.CommandDetailText` fell through to the raw result, so the
  **`Created At` / `Completed At` stamp reached the screen** — contradicting
  cycle 285's measurement that the timestamps never get there.

`internal/convstore.cleanToolResult` had the same blind spot from the other
side: it stripped `The command exited with code…\nOutput:\n` and nothing
else, so a reference transcript written by 1.4.7 (which stores the *current*
shape) kept its whole boilerplate in the resumed-conversation view.

Both now go through `agystep.ParseRunCommandResult`. The regression tests
are in `internal/tuitools/detail_test.go` (the current shape, a failed
command, an empty output) and `internal/convstore/agy_test.go` (the current
shape, with and without the stamp).

---

## 5. Where the template is now used

### `send_command_input` — the template's own wording

The clone's `send_command_input` returned `t.status()`, a tab-separated line
of its own invention (`<id>\t<state>\t<command>\t<elapsed>s\n<body>`). The
reference's wording is "Input sent successfully." / "The command is still
running." / "The command exited with code N." / "Output:" — and each of
those strings appears **exactly once** in the reference's binary, inside
`step_strings/send_command_input.tmpl`. There is no other source, so the
template is the reference's wording for this result.

The clone now builds the step and renders it:

```
Input sent successfully.
The command is still running.
Output:
<the command's output so far>
```

### The background branch of `run_command`

Same argument: "Background command ID:" and "Output snapshot:" appear
exactly once in the binary, inside `step_strings/run_command.tmpl`. The
clone's background result was "Started cmd-1 in the background." plus a
status line — its own invention. It is now the template's background
branch:

```
Background command ID: cmd-1
Output snapshot:
<the output so far>
```

The **plain** `run_command` result is untouched: it stays on the CLI's own
formatter, which is what the reference sends today and what
`internal/tools/command_output_truncation_test.go` pins.

### The leading newline

Both new results drop the template's leading newline. The reference's step
strings are concatenated *after* a result stamp, so the newline lands
between the stamp and the text (that is exactly the blank line in the
2026-10-20 record). The clone's two results have no stamp, so the newline
would be a stray blank first line. `internal/agystep` returns the template's
output verbatim — the golden test needs the exact bytes — and the trimming
happens at the tool layer, in `internal/tools/background_result.go` and
`internal/tools/tools_tasks.go`.

---

## 6. 硬约束 (hard constraints)

- **The plain `run_command` result text is not the template's.** It is the
  CLI's own formatter, byte for byte, and it is pinned by
  `internal/tools/command_output_truncation_test.go` and
  `internal/resultparity`. Rendering the template there would be a
  regression against the reference's 1.4.7 traffic.
- **The template's rendering is verbatim.** `agystep` returns what the
  template produced; the leading newline is part of the reference's record
  and the golden test depends on it.
- **`agytemplates.NewSet` parses the whole bundle.** The step strings
  cannot be parsed alone — they call `defaultStatusOutputString`.
- **The seventeen recovered files are byte-pinned.** Their digests live in
  `internal/agytemplates/agytemplates_test.go` and in
  `internal/agytemplates/templates/PROVENANCE.md`.
- **The two addresses never mix.** Nothing in this cycle read the binary;
  every address-free claim here comes from the string table or from the
  reference's own artifacts.

## 7. 存疑 (open questions)

- **The reference's own background-command result is a third text, and the
  clone still does not speak it.** 【一手】 the reference's 1.4.7 store holds
  587 records of the shape:

  ```
  Created At: 2026-10-25T03:51:19+08:00
  Tool is running as a background task with task id: 45fde9e1-…/task-607
  Task Description: grep -rn "noble-bolt" ~/.gemini/ ~/.config/ ~/.local/ 2>/dev/null
  Task logs are available at: file:///Users/gear/.gemini/antigravity-cli/brain/45fde9e1-…/.system_generated/tasks/task-607.log
  Log output:
  <the log so far>

  YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: A) either proceed to other relevant work (if any) or, B) simply update the user with a short message (that you have launched the command and will wait for it to finish) and end the turn.
   DO NOTHING ELSE.
  ```

  and the completion notification is

  ```
  Task id "45fde9e1-…/task-56" finished with result:
  Command exited with code 0.

  Command output:
  <the output>

  Log: file:///…/task-56.log
  ```

  That is a **task subsystem**, not a text: the ids are
  `<conversation-id>/task-<n>`, the logs are files under
  `brain/<conv>/.system_generated/tasks/`, and the notification rides the
  task channel. The clone's background commands are a local process table
  with `cmd-<n>` ids and an in-memory log. Porting it is the next parity
  gap; it is not a text swap. (The clone's `schedule` tool already speaks
  the reference's shape for *its* tasks — `internal/tools/tools_tasks.go`.)
- **Which path renders the step string at 1.4.7.** The converter has both
  `ToInputString` and `ToOutputString`; only the latter has a template. The
  reference's 1.4.7 store holds the *CLI formatter's* text in the step's
  `content`, so the step string is not what the model sees today. What the
  Cortex path does with it — display, another transport — is not measured.
- **The clone's `send_command_input` result has no live capture.** The
  argument for the template being the reference's wording is the string
  table (each string appears exactly once, inside the template). The
  reference's own artifacts on this machine never used the tool.
- **The reference's `command_status` description** is
  "Get the status of a previously executed terminal command by its ID. …
  Do not try to check the status of any IDs other than Background command
  IDs." — the clone's catalogue entry is a short paraphrase
  ("Check the status of a previously started command."). The reference's
  description names the *Background command ID*, the same concept the
  template's background branch uses. Aligning the catalogue's descriptions
  with the reference's is a separate cycle.

## 8. Limitations

- The step-string rendering is exercised by the reference's **1.4.2**
  record; the 1.4.7 reference no longer produces it, so the template's
  output is verified against history rather than against live traffic.
- The reference's **IDE** binary carries the same templates and the same
  literals, and was not run this cycle — the CLI's artifacts were the
  evidence.
- `internal/agystep`'s data model answers the getters the two recovered
  templates read. The reference's other step strings (there are more in the
  binary than the two recovered here) would need more of the step proto.
- The `internal/tuitools` and `internal/convstore` fixes were verified
  against unit tests and the existing TUI tests; no live TUI session was
  driven for them.
