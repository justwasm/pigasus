# The tool surface

> Twelve wire declarations versus sixty advertised names, the result texts, and the output budget.

## The tool surface (`internal/tools`, cycle 275)

There are **two** tool lists, and they are not the same list (cycle 293):

| Surface | Built from | Contents |
|---|---|---|
| Model declarations | `internal/tools/model_tools_1.4.6.json` via `ModelDeclarations()` | **12** function declarations, capture order |
| `init.tools` / available | `internal/tools/catalog.go` (`referenceSpecs`, `Catalog`) | **60** names |

The reference's `-p --output-format stream-json` init event lists 60 names, but
the CloudCode request body carries twelve declarations:
`read_file, run_command, manage_task, send_message, schedule, invoke_subagent,
define_subagent, manage_subagents, write_file, edit_file, search_web,
ask_question` — identical in 1.4.3, 1.4.4 and 1.4.6. The clone used to send the 60 as
declarations under 1.3.6 names, so reference-shaped calls failed twice
(`Tool read_file not found`, then a required-field mismatch). `Catalog` remains
the 60-name list for `init.tools` and is unchanged; `RuntimeCatalog` adds
`read_file` / `write_file` / `edit_file` executors under the reference's names.

**1.4.6 added `run_command`'s `BypassSandbox` BOOLEAN** — the only declaration
that changed (the wire grew 13,530 → 13,701 bytes; the other eleven are
byte-identical). `tools.BypassSandboxRequested` reads it and both command
gates (`internal/toolrunner/runner.go`, `internal/printmode/main_print_tools.go`)
pass it to `sandbox.CheckCommandWithBypass`, so a call that asks to run
outside the sandbox is not blocked by the clone's path sandbox. The prompt
half of the same feature is the new `<terminal_sandbox>` section. Report:
`docs/cycles/reference-drift-146/`.

**1.4.8 changed nothing on the main surface** — the twelve declarations are
byte-identical to the 1.4.6 capture (【一手】, live 1.4.8 request). Without
`--sandbox` the reference omits `run_command.BypassSandbox`; with it, the
whole surface matches the capture byte for byte. The *custom-agent* surface
(`internal/agenttools`) is the one that moved — see
[`prompt.md`](prompt.md#custom-agents---agent-cycle-custom-agents). Report:
`docs/cycles/reference-drift-147/`.

Two traps: the captured schemas use protojson's uppercase `type` values, which
are *not* valid JSON Schema and are rejected by PiG's metaschema check before
any call runs — `tools.SchemaParams` lowercases them for PiG and the transport
uppercases them again on the wire; and when a PiG subagent extra owns the same
name (`invoke_subagent`, `send_message`, `manage_subagents`), the captured
declaration must win on the wire while the extra still executes
(`declOverrideTool` in `internal/agent`, `SurfaceTools` in
`internal/piagentbridge`). Report: `docs/cycles/cycle-293-model-tool-surface/`.

`internal/tools/catalog.go` is the available-surface catalogue
(`referenceSpecs`); each executor beside it lives in `command.go` /
`filesystem.go` / `filesystem_edit.go` / `filesystem_manage.go` /
`tools_search.go`. Two executors used to fail calls the
reference completes, and the failure looked like a *stale* `(Failed)` badge
because the command really had run:

- **`run_command` is a shell, not an argv splitter.** The reference advertises no
  `run_shell_command` at all, and its declaration says "Make sure to specify
  CommandLine exactly as it should be run in the shell". `RunCommand` runs
  `CommandLine` through the host's default shell (cycle 276 — `DefaultShellArgv`
  → `zsh -c` / `bash -c` / `powershell -Command`) and returns the reference's
  result text verbatim: `Created At…` / `Completed At…` /
  `Command exited with code N.` / `Command output:` (or `No output from the
  command.`), with no blank line after the stamp and no trailing newline.
  **A non-zero exit is a `nil`-error result, not a failure** — the model reads
  the code. (Returning an error made `Dispatch` discard the output.) Text and
  truncation are captured in `docs/cycles/command-output-truncation/`; the
  cycle's `captures.md` is the ground truth, not the older reconstructions.

  **The clone's `mvdan/sh/v3/interp` is a deviation, and the reference's own
  `mvdan/sh` is NOT an interpreter.** The 1.4.2 binary embeds only
  `mvdan/sh/syntax` (317 funcs) + `fileutil` — **zero** `interp`, zero `expand`
  (same in 1.3.4: 292 + 1). `syntax.(*Parser).Parse` is called from
  `cortex/command/command.parseCommand` / `walkCommandCalls`,
  `cortex/policyguardian/vetting.{SplitLiteralCommand,isReadOnlyCommand}`,
  `security/safetext/shprintf` and `terminal.parseShell` — i.e. **command analysis
  and auto-run policy**, never execution. Execution is a real shell:
  `zsh -c "<CommandLine>"` on macOS (verified live: `$0=zsh`, `$-=569X`,
  stdin on a pty, parent = the `agy` binary), spawned through
  `cortex/command/command.(*goRunner).Run` → `google3/devtools/ai/sandbox/exebox`
  → `google3/third_party/golang/pty`. The shell name is
  `terminal.GetDefaultShellName()`, a **hardcoded per-OS default** — darwin→`zsh`
  (【一手】, disassembly + live turn), linux→`bash` and windows→
  `powershell`/`pwsh` (【推测】: the two remaining branches of the same type
  switch, one of which calls `os/exec.LookPath("pwsh")`). It does **not** read
  `$SHELL` — the string `SHELL` does not occur in the binary, and on a machine
  whose `$SHELL` is `/opt/homebrew/bin/bash` it still runs `zsh`. That value is
  also what fills `Shell: %s` in the declaration
  (`cortex/config.defaultConversationalCascadeConfig` calls it).
  **Cycle 276 fixed both**: `run_command` spawns that shell
  (`DefaultShellArgv`), and `runCommandDescription` advertises
  `DefaultShellName()` instead of `$SHELL`. Only `run_shell_command` — a
  clone-only compat alias the reference does not advertise — still uses the
  pure-Go `mvdan/sh/v3/interp`.
- **stdin is a pty, and the result text is ANSI-stripped.** The
  reference gives the child a pty on **stdin only** — stdout/stderr stay pipes
  (`TTY0=/dev/ttys005`, `TTY1=not a tty`, `speed 9600 baud; rows 24; columns
  80; icanon echo isig icrnl opost onlcr`, `$-=569X`). Its output is
  `stripANSI` + `CR → LF`: `printf 'a\nb\n'` → `a\nb\n` (no CRLF rewrite),
  `printf 'a\r\nb\n'` → `a\n\nb\n`, a bare `\r` becomes a line break, a
  100-char line is **not** wrapped at 80, and SGR is gone. There is no
  terminal emulation. **Cycle 277 recorded a naive `LF → CRLF` rewrite
  instead — that capture is byte-identical to the clone's own formatter and
  never came from the reference**; the live 1.4.4 request bodies in
  `docs/cycles/command-output-truncation/captures.md` supersede it.
  `RunWithPTYStdin` (pty details) is still cycle 277's and still right. Three
  measured traps live in `pty_unix.go`: the parent must close its copy of the
  slave fd after `Start` (else the child wedges in `exit()`, `STAT ?Es`,
  `wait4` never returns); the
  master must be **drained** (the pty echoes our own input back, and undrained
  it costs a fixed ~600 ms per command on Darwin); and the master must not be
  closed while the child lives (SIGHUP). One deliberate deviation, and it is
  now **conditional**: the clone's *legacy* foreground path (a call without
  `WaitMsBeforeAsync`) writes a `VEOF` (^D) instead of blocking on a stdin
  read. The reference-shaped path does not — since cycle 302 the clone
  honours `WaitMsBeforeAsync`, so the reference's disposition (the pty's
  input end stays open, a command that reads stdin blocks, the wait window
  elapses and the command is handed to the background as a task) is
  reproduced, and the VEOF deviation is retired on that path.
- **Path arguments are absolute.** `view_file.AbsolutePath`,
  `write_to_file.TargetFile` and `replace_file_content.TargetFile` all say "Must
  be an absolute path" in the capture, and a live turn read `/etc/hosts`, so
  reads are not workspace-bound (`readPath`); writes are (`workspacePath`, and it
  compares after symlink resolution, so `~/GitHub` and `~/Documents/GitHub` are
  one workspace). Never `filepath.Join(workspace, modelPath)` — it turns
  `/etc/x` into `<ws>/etc/x` with no error.

The declarations and result texts are 【一手】, read out of the request bodies the
proxy captures (`~/.openagy/captures/`), and the shipped
`testdata/advertised_tools_1.3.6.txt` still holds the 60 advertised *names*.
Only `run_command` and `view_file` have a captured schema; the rest are still
cycle-98 reconstructions. Reports: `docs/cycles/cycle-275/deliverable.md`
(the surface), `docs/cycles/cycle-277/deliverable.md` (pty + CRLF/ANSI).

## The descriptions (`internal/tools/descriptions.go`, cycle 305)

The reference has **three** tool surfaces, and only the third one describes
anything:

| surface | where | what it carries |
|---|---|---|
| the advertised list | `init.tools` | **names only** |
| the converter layer | `cortex/tools/*`, `gemini_coder/framework/toolconverters` | the prose that assembles a tool *definition* |
| the runtime layer | `cortex/core/tools/tools`, `oneharness/internal/tools/tools` | the `Description()` methods — **twelve** of them are the declarations the reference sends |

The catalogue's descriptions used to be the clone's own paraphrase for every
advertised tool: of the sixty names, exactly **one** (`view_file`) matched the
reference's text. They are now the reference's own bytes, committed at
`internal/tools/testdata/tool_descriptions_1.4.7.json` and fetched with
`refDesc(name)` — **thirty-six** names carry text (the browser surface and
`delete_knowledge` carry none anywhere in the 1.4.8 binary, and
`run_workflow` only fragments), and the catalogue keeps its own prose for the
rest, which it now says out loud.

**Never type a description back into `catalog.go`** — `refDesc` is the only
source, so a paraphrase cannot creep back in.

**The names: 63 known, 60 advertised.** The reference's binary holds a
`[]string` of **63** tool names (slice header at vmaddr `0x10b5ba450`, backing
array at `0x10b623bb0`), but a live 1.4.8 session's `init.tools` holds **60**:
the reference withholds `code_search`, `moma_search` and `skill_search`. The
clone advertises what the reference *prints*, so it stays at 60; the binary's
list is committed as `testdata/tool_names_1.4.7.txt` and the split is pinned
by `TestAdvertisedListIsTheLiveSubset`.

**`run_command`'s prose is the reference's converter template**, rendered per
host (the operating system and the shell) — the template is the string
literal at file offset `0x36cb4c0` (vmaddr `0x1036cb4c0`) of the 1.4.8
binary, with two more literals for the paragraphs it joins (`0x1036d0bd3`
and `0x1036b6def`). It was byte-identical to the reference
except for one stray `\n`; that is gone.

**The descriptions are still dead text on the default path** — `tools.Describe`
has one caller and `piagentbridge.Tools()` has no non-test caller; the twelve
declarations the clone actually sends come from
`internal/tools/model_tools_1.4.6.json`. Report:
`docs/cycles/cycle-305-tool-descriptions/deliverable.md`.

## The declaration variants (`internal/tools/variants.go`, cycle 306)

agy 1.4.8 holds **two** variants of four model-facing descriptions
(`run_command`, `send_message`, `manage_subagents`, `edit_file`), and a
`strings` hit proves a text *exists*, never that it is *sent*.

The live request bodies captured on 2026-10-22 (`/tmp/agy-cap-147`,
`/tmp/agy-cap-sb`) settle it: a default 1.4.7 session sends the **older**
variant of all four — `run_command` 397 B with no
`NotificationTimeoutSeconds` parameter, `send_message` 89 B,
`manage_subagents` 261 B, `edit_file` 393 B. That is byte-for-byte what
`internal/tools/model_tools_1.4.6.json` holds, so **the wire does not
change**. The capture itself could not be re-taken at 1.4.8 (no live
session on this machine), but every address in the variants fixture was
re-derived against the 1.4.8 binary and the texts are byte-identical —
only the addresses moved
(`docs/cycles/cycle-359-agy-1.4.8-reanchor/deliverable.md`).

The newer variants are recorded in
`internal/tools/testdata/tool_variants_1.4.7.json` with the vmaddr and
length of every literal they are built from. Read them with
`tools.ReferenceVariants()`. The descriptions fixture now carries the
**default** text for the three tools where cycle 305 had recorded the newer
one — the catalogue must not advertise text the reference never sends.
Report: `docs/cycles/cycle-306-declaration-variants/deliverable.md`.

**Result texts are replayed, not remembered.** `internal/resultparity` parses
`internal/tools/testdata/result_texts_1.4.7.txt` (32 captured call/result pairs)
and replays each one against the real executors — 26 compared, 6 skipped with a
recorded reason, and a bijection test that fails if a section is neither. Run
`go test ./internal/resultparity/` after touching any tool result text. The
fixture has **no `run_command` section** — the most-used tool's text lives in
`docs/cycles/command-output-truncation/captures.md` and is pinned by
`internal/tools/command_output_truncation_test.go` instead. It calls
`Tool.Run`, not `Dispatch`, because `Dispatch`/`piagentbridge` add
`ApplyOutputBudget` on top (see below).

**How the capture is taken.** `devutils/refcapture` is the harness: a mock
CloudCode server that forwards every RPC *except* `streamGenerateContent` to the
real `https://daily-cloudcode-pa.googleapis.com`, answers the agent turn with one
scripted `functionCall`, and then reads `functionResponse.response.output` out of
the follow-up request the reference sends back. The forwarding is load-bearing:
at 1.4.7 the reference runs an eligibility check against `loadCodeAssist` before
the first turn, and a stubbed response cannot satisfy it (it retries with
"Eligibility check attempt 1 failed transiently"). The reference is driven in
print mode (`agy -p`) with `--dangerously-skip-permissions` and the workspace at
`/tmp`; `view_file` / `generate_image` / `read_url_content` need a custom agent
(a workspace `.agents/agents/<name>.md` plus `--agent <name>`), because those
three are only advertised to one.

**Forwarding also decides what the fixture is worth.** The 1.4.4 mock answered
*every* RPC, so the reference's own model calls got fabricated answers — and for
`search_web` and `generate_image` the mock could not fabricate the right one,
so the 1.4.4 fixture ended up carrying the reference's *failure* texts where
the reference had no failure at all (cycle 293 said so at the time: "an image
response would have to be faked in the mock"). Read the 1.4.4 failure rows as
**the mock's limitation**, not as the reference's behaviour. `generate_image`
reaches the image service through a *separate, non-streaming* `generateContent`
RPC — `requestType: "image_gen"`, `model: "gemini-3.1-flash-image"`,
`generationConfig.imageConfig.aspectRatio` — not through the agent turn.

**The fixture keeps the full bodies.** At 1.4.4 the 800-line and 46080-byte
`read_file` / `view_file` bodies were abbreviated with `…`, so those sections
could only be compared prose-wise. The 1.4.7 capture carries them in full and
the clone matches **byte for byte**, so every section is compared byte for byte
now; `proseBody` survives only for the 1.4.4 capture, which is kept beside it as
the previous evidence. Report:
`docs/cycles/cycle-296-result-texts-and-seeds-1.4.7/deliverable.md`.

**That budget is a guess and contradicts the capture.** `DefaultMaxResultTokens
= 8000` (cycle 248, self-described as "the reference binary ships a
model-class-dependent value") with a `/4` chars-per-token estimate caps
model-visible text near 32 KB and wraps it in `_truncated: true` / `_preview: |`.
The 1.4.4 capture shows the reference handing the model a 46080-byte `read_file`
body whole, with no wrapper. Report: `docs/cycles/result-text-replay/deliverable.md`.

## A command's result has two shapes (`internal/agystep`, cycle 298)

The reference prints a command's outcome in **two** different texts, and both
are live — one per era. A consumer that only knows one of them silently
mis-reads the other.

| | Shape | Where it comes from |
|---|---|---|
| **current** | `Command exited with code N.` + `Command output:` (or `No output from the command.`) | the CLI's own formatter — the live 1.4.4 capture (`docs/cycles/command-output-truncation/captures.md`) and 1,023 records in the reference's own conversation store |
| **step string** | `The command exited with code N.` + `Output:` (or `No output`) | the reference's step-string template `step_strings/run_command.tmpl` — what the reference produced at 1.4.2/1.4.3, and what eleven of its own `transcript.jsonl` records from 2026-10-19/20 still carry |

`internal/agystep` owns both:

- `RunCommandString` / `SendCommandInputString` **render** the reference's own
  recovered templates (`internal/agytemplates/templates/step_strings/*.tmpl`).
  `RunCommandString` + a result stamp reproduces the reference's 2026-10-20
  record byte for byte — `internal/agystep/step_test.go` pins it.
- `ParseRunCommandResult` **reads** either shape and hands back the exit
  code and the output. `internal/tuitools` (the TUI's step detail and the
  failed-command badge) and `internal/convstore` (the resumed-conversation
  reader) both go through it.

**Before cycle 298 both consumers only knew the step string**, so the clone's
own results did not parse: a failed command was never marked as failed, and
the `Created At` / `Completed At` stamp reached the screen — contradicting
cycle 285, which measured that the timestamps never get there.

The **plain** `run_command` result stays on the CLI's own formatter — the
model-facing bytes are pinned by
`internal/tools/command_output_truncation_test.go` and by the live capture.
The template's rendering is used where the reference has no other source for
the wording: `send_command_input` ("Input sent successfully." / "The command
is still running." / "The command exited with code N." — each string appears
exactly once in the reference's binary, inside the template). Report:
`docs/cycles/cycle-298-step-strings/deliverable.md`.

## A failed tool call is an error, not a result (`internal/tools/failure.go`, cycle 350)

**agy 1.4.8 changed one thing on the wire**, and it is the one thing the
model reads. The reference's own release notes:

> Fixed failed tool calls, including calls to a tool that doesn't exist and
> canceled or interrupted calls, being passed back to the agent as ordinary
> results; the agent now sees them as errors and can recover on its next step.

The 1.4.7 capture says the same thing from the other side — its header reads
*"Every result is a plain string under `functionResponse.response.output`,
never an error, even for the refusal."*

【一手】 The new shape, captured live on 2026-10-27 against the 1.4.8 binary
(`internal/tools/testdata/tool_errors_1.4.8.txt`):

| the call | the envelope | the body |
|---|---|---|
| `read_file` on a file that exists | `{"output": …}` | the two stamp lines, then the result |
| `read_file` on a file that does not exist | `{"error": …}` | the two stamp lines, then `Encountered error in tool execution: file does not exist` |
| `view_file` — a known name that the model-facing surface does not carry | `{"error": …}` | the two stamp lines, then `Encountered error in tool validation: unknown tool: "view_file" — check spelling` |

The **body** did not change: it is the same string the clone already writes,
stamp lines and all. What changed is the **member** it travels in — `error`
instead of `output` — and that is what tells the model it may recover.

The clone reads the signal in one place and carries it in one field:

- `internal/tools/failure.go` — `IsFailureText` asks whether a result's body
  is the reference's failure text. The check is deliberately narrow: the
  marker has to be the first thing in the body, after the two stamp lines, so
  a file that merely *mentions* the sentence is an ordinary result.
- `internal/piagentbridge/stream.go` — the transcript's tool result becomes a
  `backend.Message` with `IsError` set when the runtime flagged the call **or**
  the text is the failure text. The clone's own tools fail by writing the
  reference's wording into an ordinary result, so the text is the only signal
  there is.
- `internal/backend` — the two dialects that are the reference's own
  (CloudCode and the Gemini API-key path) pick the envelope:
  `{"error": …}` for a failure, `{"output": …}` otherwise.

The **gateway** dialects (OpenAI and Anthropic, for a model *you* configured)
are deliberately untouched: they are not the reference's dialect, and they
carry their own conventions. The Anthropic `tool_result` block has an
`is_error` member the clone does not set; the OpenAI dialect has no error
channel at all.

## An image the model cannot see (`internal/tools/image_probe.go`, cycle 362)

**agy 1.4.9** changed what a corrupt image does to a conversation:

> Fixed conversations failing every later model request with `INVALID_MEDIA`
> after the agent viewed a corrupt image or one with text in front of its
> header, such as a screenshot captured with `adb exec-out screencap -p >
> screenshot.png` that also captured a warning; such images are now rejected
> with an error that says where the real image header starts, so the agent
> can fix the command.

The failure is not local: the model has the bad image **in its history**,
so every later request carries it and every later request fails. The fix is
therefore to reject the image *at the tool*, with an error the model can
act on.

| where | what the clone does |
|---|---|
| `imagery/header.go` | `HeaderOffset` finds the real header of a payload whose first bytes are not the header; `HeaderOffsetError` carries the reference's own wording — `invalid %s: header found at byte offset %d after leading non-image bytes` |
| `internal/tools/image_probe.go` | `read_file` and `view_file` reject such an image, the same shape as the WebM probe |
| `internal/clipboard/paste.go` | a pasted payload with leading non-image bytes is rejected with the same error |

The rule is deliberately narrow: a payload whose header is **not** at byte 0
is rejected; a payload with **no** header at all keeps the old answer — the
upstream note's "says where the real header starts" has no offset to report
for a payload that has no header.

## The background-task subsystem

Since cycle 302 a backgrounded command is a **task** in the reference's
shape, not a local process:

| | shape |
|---|---|
| **id** | `<conversation-id>/task-<n>` — the table key, so `command_status`, `manage_task` and `send_command_input` all address a task the way the model was told |
| **log** | `<home>/brain/<conv>/.system_generated/tasks/task-<n>.log` — created empty when the task starts, filled with the command's raw output when it exits |
| **result** | the reference's launch acknowledgement (`Created At:` / `Tool is running as a background task with task id:` / `Task logs are available at:` / `YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: …`) — the same function (`taskLaunchResult`) renders it for `schedule` and for `run_command`, so the two cannot drift |
| **completion** | `Task id "<id>" finished with result:\n<result>\nLog: file://<log>` |

`run_command` honours the reference's own `WaitMsBeforeAsync` (milliseconds
before the command is sent to the background) and `IsDaemon` (background it
straight away). A command that finishes inside the window returns the
foreground result; one that is still running when the window closes stays
registered as a task. The reference's *step content* for such a step
carries two extras the model never sees — a `Task Description:` line and a
`Log output:` section — so the clone sends the shorter shape.

The delivery of the completion notification to the model is **not** wired
yet: `internal/agent.BgFinishCoordinator` is the seam, it has no production
caller, and the text it would send is now the reference's
(`agent.TaskNotificationText`). Report:
`docs/cycles/cycle-302-background-task-subsystem/deliverable.md`.

## The implicit trajectory (`internal/implicit`, cycle 304)

A `!` command never reaches the conversation. The reference records it in a
**separate, per-workspace store** — `<gemini-dir>/implicit/<uuid>.pb`, one
AES-256-GCM encrypted file per workspace — as a `CORTEX_STEP_TYPE_RUN_COMMAND`
step, and the cumulative-prompt handler later converts that trajectory into
chat messages for the next prompt.

| | the reference | the clone |
|---|---|---|
| **store** | `<gemini-dir>/implicit/<uuid>.pb`, AES-256-GCM, key `safeCodeiumworldKeYsecretBalloon`, `nonce(12) ‖ ciphertext ‖ tag`, 100 files | `internal/implicit` — the same format, byte for byte |
| **scope** | per workspace (`file://<workspace>`) | per workspace (`implicit.WorkspaceURI(m.cwd)`) |
| **record** | one step per command: type 21, status 3 (or 6 when canceled), source 4, the command written twice, the exit code written even when it is zero | the same |
| **written by** | the language server's shell-command tracker, when the command finishes | `internal/cliapp/tui_bash_mode.go`: `finishBashCommand`, and `preemptBashCommand` for the command it interrupted (as canceled) |

The clone's encoder reproduces the reference's own bytes: the committed
`internal/implicit/testdata/reference_step_1.4.7.pb` (155 B, cut out of the
reference's own file on this machine) re-encodes to the identical bytes, so
the field numbers, the enum values and the field order are all pinned.

**The model still does not see the command.** The reference's
cumulative-prompt handler converts the persistent trajectory into chat
messages for the next prompt; the clone has no equivalent yet. Report:
`docs/cycles/cycle-304-implicit-trajectory/deliverable.md`.

## The enum numbering (`internal/cortex_pb`, cycle 307)

`internal/cortex_pb`'s enums used to be an `iota` reconstruction — consecutive
numbers, and member names the reference does not have (`PlanStatusApproved`,
`AgentModeChat`, `ModelTierSonnet`). The reference's numbering is **sparse**
and different: `CORTEX_STEP_TYPE_RUN_COMMAND` is **21**, not 3.

The source of truth is the reference's own descriptor, decoded verbatim from
the binary: `out/local-macos/protos/cortex_1.4.6.proto` (487 messages, 86
enums). `devutils/gen_cortex_enums.py` regenerates the package from it, and
`internal/cortex_pb/enums_proto_test.go` compares the two (it skips when the
`out/` tree is absent).

The numbers the trajectory writes — type 21, status 3 (6 when canceled),
source 4, trajectory type 1 — are the reference's own, read out of a real
record; `internal/implicit` now borrows them from `internal/cortex_pb`
instead of repeating them.

`GoalState` is the one enum the clone owns: the .proto has no such enum, so
its members are the reconstruction's and it stays out of the package's
machine-readable `EnumMembers` table. Report:
`docs/cycles/cycle-307-cortex-enums/deliverable.md`.

## The notification timeout (`internal/tools`, cycle 308)

`run_command` takes a `NotificationTimeoutSeconds` parameter: if the command
is still running that many seconds after the wait window
(`WaitMsBeforeAsync`) closed, **the system** tells the model — so the model
does not have to poll for the status itself.

The reference bolts the field onto its schema *after* the reflection step
(`utils.AugmentSchemaField`), with its own name, its own type
(`integer`) and its own 386-byte description — all three read out of the
1.4.7 binary. The value is a real field on the recorded step:
`CortexStepRunCommand.notification_timeout_seconds = 30` (uint64).

| | the reference | the clone |
|---|---|---|
| **the schema field** | added only when the tool has the notification capability | the catalogue always carries it (the clone has no such gate) |
| **the timer** | armed in the handler's listener loop, after the wait window | armed where the command goes to the background |
| **the message** | title `Notification timeout reached`, body `Background command is still running after %ds: %s`, sent **via the task** | the same two literals, delivered through `tools.SetTaskNotifier` |
| **the wire declaration** | the default declaration does **not** carry the field | untouched — `model_tools_1.4.6.json` stays as captured |

The delivery into the turn loop is cycle 309's work — see
[below](#the-task-notifications-delivery-internalagent-internalcliapp-cycle-309).
Report: `docs/cycles/cycle-308-notification-timeout/deliverable.md`.

## The task notification's delivery (`internal/agent`, `internal/cliapp`, cycle 309)

When a background task is done, **the system** tells the model: the
notification carries the command's result and names the task log, and it
reaches the model as the reference's own `SYSTEM_MESSAGE` envelope.

```
The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.

<SYSTEM_MESSAGE>
[Message] timestamp=2026-10-24T19:15:22Z sender=<conv>/task-56 priority=MESSAGE_PRIORITY_HIGH content=Task id "<conv>/task-56" finished with result:
Command exited with code 0.

Command output:
…

Log: file://<the task log>
</SYSTEM_MESSAGE>
```

The envelope's three literals are the reference's own (read out of the
1.4.8 binary), and the whole text is pinned against the reference's own
capture — `internal/tools/testdata/reference_background_task_1.4.7.txt`,
the `completion notification (the SYSTEM_MESSAGE)` section.

The delivery has three pieces:

| piece | where | what it does |
|---|---|---|
| the fire | `internal/tools` | the notification is built when the command is gone; the result is the CLI's own formatter body, without the `Created At:` / `Completed At:` stamp |
| the queue | `internal/agent` (`TaskInbox`) | the session-level queue — the TUI builds a fresh `agent.Loop` per turn, so a per-turn queue would drop every notification that arrives between turns |
| the delivery | `internal/cliapp` | the notification joins the session as a **system** message and reaches the model in the user-content position, without the `<USER_REQUEST>` envelope |

Two consequences worth knowing:

- The role in the session is **`system`**, and the turn's message builder
  turns it into a user message on the way to the model. The reference's own
  lead-in says the message "was not actually sent by the user", which is
  why it travels where a user message travels — and a system-prompt
  position would fold it into the system prompt, which is the wrong place.
- The trajectory stores it under the reference's own step type,
  `CORTEX_STEP_TYPE_SYSTEM_MESSAGE = 101` (`convstore.StepSystemNotice`).

When the agent is idle the TUI starts a turn of its own for the
notification; a turn that is already in flight is not interrupted — the
notification waits for the next one.

Report: `docs/cycles/cycle-309-task-notification-delivery/deliverable.md`.

## The three MCP tools (`internal/tools`, `mcp`, cycle 310)

The reference exposes three MCP tools to the model. All three are
advertised, and all three have real executors:

| tool | parameters | what it does |
|---|---|---|
| `call_mcp_tool` | `ServerName`, `ToolName`, `Arguments` | call a tool on a server the model discovered lazily |
| `list_resources` | `ServerName` | list a server's resources |
| `read_resource` | `ServerName`, `Uri` | read one resource |

Their **parameter schemas are the reference's own**, recovered from the
1.4.8 binary's Go type metadata — the reference reflects over a struct and
turns the struct tags into a JSON schema, and a stripped binary keeps the
tags in the generic instantiation's own name:

```
…ToJsonSchemaString[go.shape.struct {
    ServerName string "jsonschema:\"required\" jsonschema_description:\"Name of the MCP server.\"";
    ToolName   string "jsonschema:\"required\" jsonschema_description:\"Name of the tool to call.\"";
    Arguments  encoding/json/jsontext.Value "jsonschema:\"required\" jsonschema_description:\"Arguments to pass to the tool.\""
}]
```

So the catalogue advertises the reference's **PascalCase** names, the
reference's descriptions, and the reference's required set — and
`read_resource` declares *no* required set, because neither of its fields
carries a `jsonschema:"required"` tag.

One rule is load-bearing: **`call_mcp_tool`'s `Arguments` payload is
forwarded verbatim.** The clone's canonicaliser lower-cases a model's keys
(`ServerName` → `server_name`), which is right for the clone's own tools
but wrong here — the nested keys belong to the MCP *server*, which owns
those names. The three tools therefore read their arguments themselves
instead of going through `argsOf`.

The client underneath is `mcp.Pool`: one connection per configured
server, opened lazily (a call is the first thing that spawns a server
process) and reused afterwards. It reads the reference's
`~/.gemini/antigravity-cli/mcp.json` — the file `mcp/agyconfig.go`
parses, and `internal/pigrt/mcpconfig.go` aliases — and speaks the MCP
wire protocol through PiG's `mcp` package.

`internal/tools` depends on the pool only through the `MCPProvider`
interface, so the tools package carries no MCP dependency; the pool is
installed process-wide by `internal/printmode.InstallMCPProvider`, which
both the print path and the TUI call.

Known limitation: in the print path the clone's pool and PiG's MCP
extension each open a connection to the same server (the first serves the
three meta-tools, the second the always-loaded `mcp__<server>__<tool>`
surface). The TUI still has no always-loaded MCP tool surface at all.

Report: `docs/cycles/cycle-310-mcp-tool-executors/deliverable.md`.

## The browser subagent (`internal/browsersubagent`, cycle 311)

`browser_subagent` is one of the sixty-three names the 1.4.8 binary knows.
The reference's production variants carry it next to the other subagent
tools (`invoke_subagent`, `define_subagent`, `manage_subagents`,
`send_message`), so it is a **tool**: it hands a browsing task to a browser
agent and answers with the agent's result. The reference records the call as
`CortexStepBrowserSubagent` — its first field is `task`, its second is
`result`.

Its **description is the reference's own text**, recovered from the 1.4.8
binary. Four variants live in `cortex/customizations/builtin/agents`:

| vmaddr | bytes | shape |
|---|---|---|
| `0x1036c8c48` | 458 | Chrome, no recording note — **the one the clone ships** |
| `0x1036d8b82` | 683 | Chrome, with the recording note |
| `0x1036da128` | 709 | webview |
| `0x1036db1f8` | 724 | webview |

The clone ships the first: it drives Chrome over the DevTools Protocol
(`browsercdp`) and records nothing, so the recording sentence
would be a lie. The fixture
(`internal/tools/testdata/tool_descriptions_1.4.7.json`) holds it under the
layer `agent catalogue (builtin/agents)` — the fourth surface the
reference's tool text lives in.

The browser agent itself is `internal/browsersubagent`:

- the prompt opens with the reference's own sentence, `You are a browser
  agent.` (the literal at `0x1035588eb`); the rest is the clone's own
  inventory of the tools it actually has;
- the tool set is the clone's twenty-three browser tools, and nothing else
  — or, on the **live** surface, the bridge's seventeen (see *The live
  browser* below);
- the delegation reuses `internal/subagenttools.Invoke`, the same machinery
  `invoke_subagent` uses.

To let a subagent carry a tool set of its own, `subagenttools.Invocation`
gained a `Tools []piagent.AgentTool` field, which
`createSubagentSession` hands to PiG's `SessionStartOptions.ExtraTools`.
A subagent with no tools of its own still has none — the seam is a
parameter, not a default.

The executor lives in `internal/tools/browser_subagent.go` and calls through
a `BrowserSubagentProvider` seam, like the three MCP tools' seam, so
`internal/tools` carries no dependency on the delegation machinery. The
provider is installed process-wide where the session's model is known: the
print path (`internal/printmode/print_pig.go`) and the TUI
(`internal/cliapp/tui_runmode_agent.go`).

One rule is deliberately **not** carried over: the reference's browser agent
is told never to launch Chrome itself — the *program* launches the browser,
not the agent ("Browser starts automatically on first tool call", the
reference's own skill 【一手】). The clone's browser client launches its own
Chrome the same way, so the rule would tell the agent to do something the
clone cannot do. On the **live** surface the rule comes back to life,
because there the browser is the user's own and is already there.

Report: `docs/cycles/cycle-311-browser-subagent/deliverable.md`.

### The `/browser` command

The reference's own slash menu describes the command as *"Invoke a browser
agent for web tasks."* 【一手 — `internal/tuilib/commands.go`, the
catalogue recovered from the v1.3.5 binary】. The clone's `/browser` does
that: it runs the browser agent and answers with the agent's final text
(`internal/cliapp/slash_browser.go`).

| | |
|---|---|
| `/browser` (no task) | `Usage: /browser <task>` |
| `/browser <task>` | the browser agent runs; its answer is printed under the command's echo |
| no browser, no session or no model | the reason, and only the reason |

The command builds **its own** runner: the runner the `browser_subagent`
*tool* uses is installed per turn, and that turn's session services are
closed when the turn ends. The run happens off the UI goroutine — a
browsing task can take minutes — and the answer comes back as
`browserAgentDoneMsg`.

Two rules of the command are the clone's own:

- **The browser is checked before the model is asked.** Without a browser
  every browser tool fails, and the agent — which only knows that *a tool
  failed* — tries another one, and another. The check
  (`browserProbe` → `browsercdp.CheckBrowser`) answers in a second; it
  starts nothing, because the browser starts on the first tool call — the
  reference's rule.
- **The run is bounded** at ten minutes (`browserAgentTimeout`). The
  reference's own cap is not recoverable; a slash command that never
  returns is a defect in the TUI.

The browser starts by itself — see *The browser starts by itself* below.

**A tool must not write to the terminal.** The TUI owns the screen, so a
write to `os.Stderr` from a tool lands on top of the rendered frame — the
browser tools used to do that, and the answer, the input box's border and
the status line were drawn over each other. Errors travel through the
return value.

`devutils/browserdemo` drives the same agent from the command line — the
end-to-end check for the whole browser surface:

```bash
go run ./devutils/browserdemo \
  -task "open https://example.com and report the page title"
```

Report: `docs/cycles/cycle-342-browser-slash-command/deliverable.md`.

## The browser transport (`browsercdp`)

The clone's browser tools drive Chrome over the DevTools Protocol. The
transport is `github.com/chromedp/chromedp` (v0.14.2): the client dials
the browser's websocket endpoint (`/json/version`'s
`webSocketDebuggerUrl`) **and attaches to a page target**, so the
page-scoped commands (`Page.*`, `Runtime.*`, `Input.*`, `Emulation.*`)
carry a session id. On a browser-level connection Chrome answers
`'Page.navigate' wasn't found` (-32601) — a page-scoped command needs the
page.

Two halves, and only one of them is the library's:

- **the policy is the clone's own** — `findChromeBinary` (the macOS app
  paths, the Linux binaries), the `--headless=new` flag set, the
  persistent profile dir, and the "attach to a running Chrome, otherwise
  start one" rule. chromedp's `ExecAllocator` would have replaced all of
  it with a different policy.
- **the protocol is chromedp's** — the websocket, the message ids, the
  event pump. The client routes a method to the browser connection
  (`Browser`, `Target`, `SystemInfo`, `Tracing`) or to the page session
  (everything else), and renders a protocol error as
  `cdp <method>: <message> (code <n>)`.

The console log and the network log are the two event streams the tools
read; both keep the format they always had (`[LOG] hello`, and a
`NetworkRequest` with the id, url, method, status and type).

`browsercdp.Actions` talks to a `Caller` interface, so the 16
action methods are tested against a fake transport, and
`browsercdp/realchrome_test.go` drives a real Chrome end to end
(navigation, evaluation, console, network) — off by default, on with
`PIGASUS_BROWSER_TEST=1`.

### The browser starts by itself

The reference's browser surface starts its browser on the first tool call,
with a persistent profile. The reference says so in its own words — the
`chrome-devtools-mcp` skill it ships:

> **Browser lifecycle**: Browser starts automatically on first tool call
> using a persistent Chrome profile.

【一手 — `out/ide-language-server/skills/chrome-devtools/SKILL.md`; the
language server starts that MCP server itself
(`--use_ls_chrome_devtools_mcp`, default true — `out/ide-language-server/README.md`)】

So the clone does the same, and there is **no opt-in**:

| | |
|---|---|
| no flag | the clone drives the **Chrome that is already running** when that Chrome offers a debugging port — the session is the user's — and starts a **visible** Chrome of its own when there is none |
| `--use_local_chrome` | the Chrome that is already running on this machine is the one that gets driven, and the clone starts none |
| `--local_chrome_headless=false` | the Chrome the client starts gets a **window** (it has one by default) |
| `--local_chrome_headless=true` | the Chrome the client starts is headless |
| `--local_chrome_user_data_dir=<dir>` | the profile the started Chrome uses |
| `--cdp_port=<n>` | the port of the local Chrome |
| `CHROME_REMOTE_DEBUGGING_PORT` | the clone's own override: attach to that port |
| `CHROME_BIN` | the clone's own override: the binary to start |
| no Chrome binary at all | `no Chrome browser is available: install Google Chrome, or set CHROME_BIN to the browser binary, or CHROME_REMOTE_DEBUGGING_PORT to attach to a running one` |

The browser the clone starts stops with the clone: `cmd/pigasus` calls
`browsercdp.Shutdown()` before it exits, so no Chrome is left behind
holding the profile and the port. A Chrome the clone only *attached* to is
left alone — it is not the clone's to kill.

### The session is the user's

The browser the clone drives is the **user's own browser**. That is the
whole point of the order above: the session — the cookies, the logins,
the extensions — belongs to the user, so nothing has to be logged in
again.

Two roads reach it:

- **attach** — the clone finds a Chrome that is already running and
  answers on a DevTools port, and drives that one. The order is the
  `DevToolsActivePort` file of the standard Chrome profiles (Chrome
  writes it when remote debugging is on), then `CHROME_REMOTE_DEBUGGING_PORT`,
  then `--cdp_port` (9222). Chrome 136 and later ignore
  `--remote-debugging-port` on the default profile; the way to turn it on
  for a running Chrome is `chrome://inspect/#remote-debugging`, which is
  also the page the reference's own browser agent points the user at.
- **seed** — when there is no such Chrome, the clone starts one and copies
  the user's session into the profile it starts with (`browsercdp/profile.go`):
  the cookies, the saved passwords, and the `Local State` that holds the
  key they are encrypted with. The copy is best effort and never fails
  the browser — on macOS the profile of another application is protected
  from an app without Full Disk Access, and where the file cannot be read
  the copy is skipped.

The clone drives a **tab of its own** in a Chrome it attached to (a
`PUT /json/new` on the DevTools endpoint opens it): the session is the
user's, but the tabs the user has open are theirs, and the agent does
not navigate them away. The tab is closed again when the clone stops.

The browser the clone starts shows a **window**. The reference's own flag
table keeps `--local_chrome_headless` at its default of `true` — the
clone's flag table is the reference's — but the clone honours the flag
once it is *given*, and its own default is the visible browser: the
browser is the user's, and a browser that works without a window on the
screen cannot be watched, logged into or taken over.

### The browser's four flags

The reference CLI hides four flags for the browser, and they are the
whole knob set:

| flag | default | what it does |
|---|---|---|
| `--cdp_port` | `9222` | the Chrome DevTools Protocol port of the local Chrome |
| `--local_chrome_headless` | `true` | whether the Chrome runs headless |
| `--local_chrome_user_data_dir` | `""` | the Chrome's user data dir |
| `--use_local_chrome` | `false` | whether to use the local Chrome |

【一手 + 反编译 — `out/local-macos/FINDINGS.md` §2, items 4-7: the
reference's `main.hideFlagsForExternal` marks these four hidden; the
names, the types, the defaults and the usage strings ("Port for Chrome
DevTools Protocol", "Chrome user data directory.", "Whether Chrome runs
in headless mode.", "Whether to use local chrome") are in the 1.3.4
strings dump 【strings】. The same four exist on the language server
(`out/langserver/flags.txt`)】

The clone registers them with the same names, types, defaults and usage
strings (`internal/cli/parser_flags.go`), and its browser client reads
them (`browsercdp.Config`, wired in
`internal/cliapp/browser_flags.go`). They stay **hidden**: the reference
hides them, so they never show up in `--help`.

One of the four is read differently from the others:
`--local_chrome_headless` is honoured when it is **given**, and the
clone's own default — a visible browser — stands when it is not. The
flag's *declared* default is still the reference's `true`
(`internal/cli/parser_flags.go`).

The reference's visible mode is the opt-in — its own skill documents it:

```bash
chrome-devtools start --headless=false # Start with visible browser window
```

【一手 — `out/ide-language-server/skills/chrome-devtools-cli/SKILL.md`】

so the clone's visible window is `pigasus --local_chrome_headless=false`
— and the default.

Report: `docs/cycles/cycle-339-library-audit/deliverable.md` §10.5,
`docs/cycles/cycle-342-browser-slash-command/deliverable.md`, and
`docs/cycles/cycle-343-browser-session/deliverable.md`.

## The live browser (`browserbridge`)

The DevTools route reaches the user's browser by *attaching* to it — a
debugging port, a profile, and (on Chrome 136 and later) the user turning
remote debugging on by hand. There is a second road, and it needs none of
that: the **Open Claude in Chrome** extension, which runs inside the
browser the user already has open and holds their tabs and their logins.

The clone speaks that project's own wire protocol directly
(`browserbridge`) — **no MCP, no node**, one TCP socket and
newline-delimited JSON:

```
extension ⇄ native messaging (4-byte LE length prefix + JSON)
          ⇄ native host (a dumb pipe)
          ⇄ TCP 127.0.0.1:18765
          ⇄ the bridge's primary — or the clone
```

The messages are few:

| | |
|---|---|
| `{"type":"client_hello"}` | the greeting; the answer is `{"type":"client_ack","clientId":"N"}` |
| `{"type":"tool_request","id":…,"tool":…,"args":{…}}` | a tool call |
| `{"id":…,"type":"tool_response","result":…}` | the answer |
| `{"id":…,"type":"tool_error","error":"…"}` | the failure |

【一手 — the project's own runtime
(`../open-claude-in-chrome/host/tool-runtime.js`) and its native host
(`host/native-host.js`); the extension's own reply is `tool_response` /
`tool_error` (`extension/background.js`). The protocol is an interface;
the clone's implementation is its own — the project is PolyForm
Noncommercial, so none of its code or text is copied.]

**Two roles.** The port's owner is the *primary*; everybody else is a
*client*. The clone takes the client role when something already serves
the port (the project's own MCP server, another agent) and joins it —
leaving that server alone. When the port is free, the clone takes the
primary role itself: the extension's native host retries every 1.5s, so
it finds the clone within a second or two and the browser becomes
drivable with nothing else running. As a primary the clone also serves
the project's other clients — their requests are forwarded to the native
host and the answers routed back — so taking the port does not break a
Claude Code session that starts later.

**The surface.** `--use_local_chrome` is the switch: *"Whether to use
local chrome"* 【strings — the 1.3.4 dump】. The clone reads it as what
it says — the user's own Chrome — and reaches it through the extension
when the extension is there; when it is not, the clone falls back to the
DevTools road it already had, which is the reference's own mechanism for
the same flag.

The live surface has **its own tools** (`internal/tools/browserbridge.go`),
seventeen of them, named as the project names them — `tabs_context_mcp`,
`navigate`, `computer`, `find`, `form_input`, `get_page_text`,
`read_page`, `javascript_tool`, `read_console_messages`,
`read_network_requests`, `resize_window`, `upload_image`, `file_upload`,
`set_tab_focus`, `switch_browser`. They are **not** in the advertised
catalogue: that catalogue is the reference's own list of sixty names, and
the bridge tools belong to the browser agent alone
(`internal/browsersubagent/surface.go`).

The browser agent's prompt follows the surface: the managed surface
speaks of the Chrome DevTools Protocol, the live surface of the user's
own browser — and there the reference's own rule comes back to life
("**Do NOT Attempt to Launch Chrome**"), because the browser is already
there.

The probe answers before the model is asked: `/browser` asks the
extension for the session's tabs, which opens nothing and moves nothing,
and it is the only way to know that the extension is attached to the
browser the user is actually using.

Report: `docs/cycles/cycle-344-browser-bridge/deliverable.md`.

### Three details of the wire the clone now matches

The project's own runtime is the authority on the wire, and three
places where the clone's first implementation differed have been
brought into line:

| | the project's runtime | the clone |
|---|---|---|
| **a dead primary** | the client promotes itself: two seconds after the primary's socket closes it takes the port, and if another process won the race it joins that one instead | the same (`browserbridge/bridge.go`, `promote`) — without it a client whose primary died was stranded: every later call failed with "the extension is not connected", for good |
| **arguments off the wire** | `coerceArgs`: a `tabId` that arrived as a string becomes a number, and `coordinate` / `start_coordinate` / `region` that arrived as JSON strings become arrays | the same (`browserbridge/coerce.go`) — and a value that does not parse is left as it arrived, so the extension reports the real problem instead of the bridge's guess |
| **the URL `navigate` takes** | with or without a protocol (https:// is assumed), and `"forward"` / `"back"` for the browser's history | the same — the description used to say "must be absolute", which sent the model away from two of the three forms the tool really takes |

The promotion is the one that matters in practice: the extension's
native host retries every 1.5s, so a promoted client is found within a
second or two.

Report: `docs/cycles/cycle-348-browser-bridge-protocol/deliverable.md`.

## How the model opens a browser (`invoke_subagent` → `chrome_devtools`)

The model's twelve wire declarations
(`internal/tools/model_tools_1.4.6.json`) carry `invoke_subagent` and
**no browser tool at all** — not `browser_subagent`, not one of the
twenty-three `browser_*` names. So a model that only sees the
reference's own declarations cannot reach a browser by calling a
browser tool. The reference reaches its browser agent a different way:
the browser agent is one of the built-in agent **types**, and the model
names the type in `invoke_subagent`.

The type's name is the reference's own: **`chrome_devtools`** — the MCP
server the reference's browser agent drives 【一手 — the browser agent's
own description: *"The chrome_devtools MCP server is started
automatically when this agent is invoked"*
(`internal/tools/testdata/tool_descriptions_1.4.7.json`)]. The
reference's second browser agent is `webview_browser`, the IDE's built-in
webview browser 【strings — the 1.4.8 string blob at `0x1034b3b17`,
where `research-google`, `chrome_devtools`, `webview_browser` and
`image-generator` sit in one row; `image-generator` is the one the
reference's prompt lists as an available subagent, which is what makes
that row the agents' name vocabulary]. The clone ships the Chrome one.

**The clone's wiring** is three pieces:

| | |
|---|---|
| `internal/builtinsubagent/browser.go` | the name — `KindBrowser` |
| `internal/browsersubagent/builtin.go` | the catalogue line, the prompt entry, and the installer |
| the session | installs it, per turn, and lists it in the prompt |

`InstallBuiltin` registers a handler in the session's built-in
registry. The handler builds its runner **per call**, so the surface it
drives is the one in force at that moment — the live surface when the
extension answers, the managed one otherwise. The registry the session
installs is the one the catalogue is rendered from, so the name the
model reads and the name the router answers to cannot drift.

**Where the model reads the name** — two places, and both must carry it:

- the **tool description**: `invoke_subagent`'s own description is
  spliced with the built-in catalogue (`BuiltinSubagentCatalog`), so
  `chrome_devtools` appears there;
- the **system prompt**: the "Available subagents" list, where the
  browser agent sits with the other built-ins, ahead of the user's own
  agents.

The reference's list is *conditional* — `CalculateAllowedSubagents`
decides which built-in types a session may use, which is why the 1.4.6
capture lists only `self`, `research` and `image-generator`: the
browser was off. The clone always lists it, because the clone always
has a browser to drive. It is **not** part of `agyprompt.BuiltinSubagents()`
— that list is pinned against the 1.4.4 capture and stays as it is.

**The routing** accepts both spellings of the parameter: `TypeName`
(the reference's captured schema — what the model actually writes) and
`agent_name` (the clone's own). Both reach the built-in registry, and
both skip the parent's session: the handler answers with the browser
agent's final text, in the same envelope the image-generator uses:

```
{"ok":true,"agent_name":"chrome_devtools","text":"Example Domain", …}
```

**The trap worth knowing about.** The built-in registry is seeded
lazily: the first read builds the default. A session that installs its
own registry *before* that first read used to lose it — the lazy seed
replaced the installed registry with the default, and the browser agent
was silently never routed to. The symptom was invisible: the model
simply saw a catalogue without the browser agent. The fix is in
`SetBuiltinSubagentRegistry` (it marks the lazy seed as done before the
assignment), and the regression test is
`TestSetBuiltinSubagentRegistrySurvivesTheFirstRead`.

Report: `docs/cycles/cycle-345-browser-subagent-type/deliverable.md`.

## The subagent authorization model (`internal/subagenttools`, cycle 349)

The reference's `define_subagent` carries **three switches** and one
sentence that fixes the base set 【一手 — the captured 1.4.6
declaration, `internal/tools/model_tools_1.4.6.json`:

```json
"EnableMCPTools":    { "description": "Grant access to MCP tools." },
"EnableSubagentTools":{ "description": "Grant tools to define and invoke its own subagents." },
"EnableWriteTools":  { "description": "Grant tools to create and edit files, and run commands." }
```

**The declaration did not move at 1.4.8.** The twelve model-facing
declarations are byte-for-byte identical to this capture (【一手】, live
1.4.8 request — cycle-350), so the three switches' spelling is still the
reference's. `internal/subagenttools/reference_declaration_test.go` pins
that: it reads the capture and drives the clone's parser with the
capture's *own* property names, in both directions — a property the
capture carries that the clone does not read fails the test, and so does
one the clone reads that the capture has lost.

and the tool's own description:

> Define a new subagent type that can then be invoked repeatedly.
> Subagents always have tools to research the codebase and to
> communicate with other agents.

So the reference's authorization model is **three switches on top of a
base set**, and the base set is named in that sentence: the research
tools and the messaging tool are always there.

The clone implements it as `subagenttools.Grant`
(`internal/subagenttools/grant.go`):

| | tools | names |
|---|---|---|
| **base** (always) | research + messaging | `view_file` `grep_search` `list_dir` `search_web` `read_url_content` + this package's `send_message` |
| **`EnableWriteTools`** | create and edit files, run commands | `write_to_file` `replace_file_content` `multi_replace_file_content` `notebook_edit` `sed_file` `run_command` `command_status` |
| **`EnableMCPTools`** | the MCP tools | `call_mcp_tool` `list_resources` `read_resource` |
| **`EnableSubagentTools`** | define and invoke its own subagents | `define_subagent` `invoke_subagent` |

The names are the **catalogue** names (`internal/tools`), not the
twelve model-facing declarations: a subagent's tools are drawn from the
parent's catalogue, the same way the parent's own executors are.

### The two spellings

The **parent** sees the reference's own declaration — the captured one,
with `Name`, `Description`, `SystemPrompt` and the three capitalised
switches — because that declaration is what goes on the wire. A
**granted subagent** sees this package's own schema for
`define_subagent`, which spells the same fields in snake_case
(`name`, `system_prompt`, `enable_write_tools`): the granted tool set
is handed to the child's session as an extra tool
(`coding.SessionStartOptions.ExtraTools`), not through the bridge that
overrides a declaration with the captured one.

Both spellings are parsed (`define.go`, `rehydrate.go`), so a model
that asks either way gets the same grant — and a definition that is
rehydrated from a trajectory keeps it whichever spelling it was
recorded with.

### The type decides the tool set

`invoke_subagent` names a **type**, and the type carries the
authorization:

| the type names | the tool set | the prompt |
|---|---|---|
| a type the conversation defined | the grant its definition recorded | the definition's own prompt |
| `self` | the parent's whole catalogue | the parent's own prompt, verbatim |
| `research` | the base set (read-only) | the research instruction |
| nothing | the base set | the parent's prompt |

`self` is the reference's own type — *"Subagent that inherits the parent
agent's full configuration including tools, system prompt, and model"* —
so it is the one type that gets **everything**: the parent's catalogue
(with the four subagent tools replaced by this package's own
implementations), the parent's system prompt (no role prefix — `self`
*is* the parent), and the parent's model.

`research` is the reference's second built-in type — *"Research
subagent with read-only tools for exploring the codebase, searching
the web, and reading files"*. It used to be unreachable in the clone
(`subagent "research" not found or not allowed to be invoked`); it is
now a type with the base set.

### Nesting

`EnableSubagentTools` is what makes a subagent able to open subagents
of its own: the granted tool set carries **this package's own**
`define_subagent` and `invoke_subagent`, wired to the **same
definition store** and the same deps as the parent's. So a grandchild
is defined in the same conversation store and is invoked with the same
rules — and its own grant decides its own tool set. The recursion is
unbounded in principle and bounded in practice by the model.

The definitions are **conversation-scoped** (the reference's own
scoping), so a nested definition is visible to the parent as well, and
a resumed conversation gets its definitions back from the trajectory
(`RestoreDefinition`, which restores the three grants too).

### What the parent hands over

`subagenttools.Deps` grew two fields:

| | |
|---|---|
| `Catalogue []piagent.AgentTool` | the parent's tool catalogue, adapted to PiG's tool interface — the pool every grant draws from |
| `SystemPrompt func() string` | the parent's own system prompt, resolved on demand (building it walks the skills directories, so a turn that spawns no subagent should not pay for it) |

The catalogue is injected rather than imported: `internal/tools`
depends on `internal/browsersubagent`, which depends on
`internal/subagenttools` — so `internal/subagenttools` **cannot**
import `internal/tools` without closing an import cycle. The caller
adapts instead: `piagentbridge.Tools(registry)`.

The print mode hands over the prompt through
`pigrt.SystemPromptFor(options)` — the same text `pigrt.New` computes,
so the prompt a `self` delegation inherits is the parent's own prompt
and not a second, slightly different build of it.

**What did not reach the subagent then.** The permission policy, and
the delegation itself was synchronous. Both are closed in cycle 352 —
see *The asynchronous delegation* and *The permission gate reaches the
subagent* below.

Report: `docs/cycles/cycle-349-subagent-grants/deliverable.md`.

## The asynchronous delegation (`internal/subagenttools`, cycle 352)

The reference does **not** wait for a delegated subagent. `invoke_subagent`
creates the subagent's conversation, hands it the task and returns an
**acknowledgement** at once; the subagent reports back later, as a
message to the parent. The capture (【一手】 agy 1.4.4,
`docs/cycles/subagent-async-framing-capture/deliverable.md`) pins the
acknowledgement's bytes:

```
Created At: 2026-10-22T11:24:33+08:00
Completed At: 2026-10-22T11:24:33+08:00
Created the following subagent:
{
  "conversationId":  "73ab3617-5a99-4e26-b1f1-c88e712efe39",
  "logAbsoluteUri":  "file:///Users/gear/.gemini/antigravity-cli/brain/73ab3617-5a99-4e26-b1f1-c88e712efe39/.system_generated/logs/transcript.jsonl",
  "workspaceUris":  [
    "file:///private/var/folders/…/T/opencode"
  ]
}
The subagent will send you a message when it has completed its task or requires guidance. There is no need to poll for its response.
```

Two details survive into the clone: the block is **protojson** (two
spaces after the colon, a two-space indent, one URI per line — a plain
`json.MarshalIndent` is one space after the colon and is *not* the same
bytes), and the acknowledgement ends the parent's turn
(`pi.AgentToolResult.Terminate`).

### The registry

`internal/subagenttools/registry.go` holds the conversation's live
children: each `Child` carries its conversation id, its type, its role,
its worktree, its state and the mailbox the parent's messages arrive
in. A child stays alive after it reports — the parent can message it
again — so `Registry.Busy()` (is any child *running*?) is what the print
path waits on, not the number of children.

`Registry.SetNotify` is the delivery channel. The parent installs it and
receives `(sender, content)`; the TUI turns it into a task notification
(the existing inbox, `internal/agent/task_inbox.go`), the print mode
turns it into the next turn's prompt.

### The delivery envelope

The report does not travel as a tool result. It is a **plain user-role
content part** carrying the reference's own `<SYSTEM_MESSAGE>` envelope
(【一手】, same capture):

```
The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.

<SYSTEM_MESSAGE>
[Message] timestamp=2026-10-22T03:24:39Z sender=73ab3617-5a99-4e26-b1f1-c88e712efe39 priority=MESSAGE_PRIORITY_HIGH content=PONG
</SYSTEM_MESSAGE>
```

The rendering lives in a **neutral package**, `internal/agentmessage`,
because two callers need the same bytes: `internal/agent`'s task
notification (the background-task subsystem) and the subagent delivery.
The timestamp is **UTC** — every other time in the reference's request
is `+08:00`, and this one is not.

The mirror image is the child's **first** user message: the same
envelope, carrying the parent's `Prompt`, with `sender` = the parent's
conversation id.

### The two callers

| | the TUI | `--print` |
|---|---|---|
| the registry | `internal/cliapp/tui_subagent_async.go` — one per conversation, closed when the conversation changes | built by `runPigPrintTurn`, closed when the run ends |
| the delivery | the task inbox: the notification waits, and the turn's end starts the next turn | the next turn's prompt: `executePigPrintRunWith` loops while a child is working |
| the wait | none — the user keeps typing | `waitForSubagentDelivery`, bounded by `deliveryWaitTimeout` (10 min) |

The print mode's loop is the reference's own shape: the launch
acknowledgement ends the first invocation, and the delivery starts the
next one. The bound is the clone's own addition — a subagent that never
reports must not hang a headless run forever.

### The messaging tools

`send_message` answers with the reference's own two texts:
`Message sent to "<id>".` when the recipient is live, and
`Encountered error in tool execution: recipient "<id>" not found` when
it is not (【一手】 agy 1.4.7's captured result texts). The clone's
version is only truthful when the recipient is live — the registry
decides, not a string comparison.

`manage_subagents` answers with the reference's own sentences too:
`You have %d active subagent(s):` followed by one
`{type, role, conversationId, state}` entry per child, and
`Successfully killed %d subagent(s) and their descendants.` with the
killed roles.

## The peers, the broadcast and the failure notice (`internal/subagenttools`, cycle 353)

Three more of the reference's answers, all read off the 1.4.8 binary
(【反编译】; the addresses are in
`docs/cycles/cycle-353-subagent-peers-interrupt/deliverable.md`).

### `list_peers`

`manage_subagents` has **four** actions, not three: `list`,
**`list_peers`**, `kill`, `kill_all`. The peers are the **parent
conversation's** subagents — not the caller's own children:

| the caller | the answer |
|---|---|
| the conversation that owns the registry (the root) | `You have no parent conversation, so you have no peer subagents:\n[]` |
| a subagent | `You have %d active peer subagent(s) (parent %s):\n%s` — the parent's subagents, marshalled as `list` marshals them |

The reference asks the hub for the *parent's* direct subagents, so the
asking subagent is in its own peer list.

What distinguishes the two callers is the **owner**: the manage tool
carries the conversation id of the agent that runs it, and the registry
belongs to the conversation that owns it. A subagent shares the parent's
registry, so `rebindMessaging` gives the child its own owner when it is
launched.

A `kill` with no ids now says what the reference says:
`the "kill" action requires ConversationIds; run "list" to get the ids of
the active subagents, or use "kill_all" to kill all of them`.

### `@children`

`send_message`'s `Recipient` has a second form: `@children` broadcasts to
every live direct subagent. The summary takes the reference's three
shapes:

| | |
|---|---|
| one recipient | `Message sent to "<id>".` |
| a broadcast that reached everyone | `Message sent to %d subagents: %s.` (the ids joined with `", "`) |
| a broadcast with failures | `Message sent to %d of %d subagents: %s.` plus one ` Failed to deliver to %q: %v.` per failure |

`@children` is a recipient, not an id: it never reaches the
`recipient "%q" not found` sentence.

### The failure notice

A subagent that stops on an error tells the parent the reference's own
sentence: `The subagent %s with id %q encountered an error and has either
stopped or failed to start execution: %s` — the subagent's type (its role
when the call named one), its conversation id, and the error.

### The two declarations in the binary

The 1.4.8 binary carries **two** `manage_subagents` parameter schemas, and
they disagree: the **CLI's** (`cortex/tools`) has a three-value `Action`
enum, the **harness's** (`oneharness/internal/tools`) has the four-value
one with the long description. The clone is a clone of the CLI, so the
advertised declaration stays as captured (`internal/tools/model_tools_1.4.6.json`),
while the tool's *runtime* schema carries the four values — the CLI's own
handler knows the fourth action.

## The permission gate reaches the subagent (`internal/permissions`, `internal/toolrunner`, `internal/sandbox`, cycle 352)

The reference's model is one policy per conversation — *"allowing an
action for the conversation, in the main agent or in a subagent, applies
across the main conversation and all of its subagents"* (the 1.4.6
changelog). The clone now honours that.

### Two names for one tool

A subagent calls the **catalogue's** spelling of a tool (`write_to_file`)
while the parent calls the **declaration's** (`write_file`), and the
user's rules are written against the declaration. So the call is judged
under both:

```go
internal/tools.PermissionNames("write_to_file") // ["write_to_file", "write_file"]
```

`internal/permissions.Policy.DecideNames(target, names...)` is the
decision: a deny under **any** of the names denies, an allow under any
of them allows, and only then do the mode's rules decide — the same
order `Policy.Decide` uses for a single name. `internal/toolrunner.Decide`
is the same decision as a pure function, and `PermissionRunner.Run` is
now a thin wrapper around it.

### The worktree

A subagent that runs in its own directory must be allowed to work in
it. `sandbox.Sandbox` grew `ExtraPaths` — the fourth kind of allowed
root, next to the workspace, the artifact directory and the per
conversation scratch directory. The gate is built **for** the
subagent's directory (`Deps.PermissionGateFor func(worktree string)`),
so each child gets its own sandbox with its own worktree added.

### Where the gate is installed

The subagent's session runs the gate as a PiG `BeforeToolCall` hook
(`Deps.beforeToolCallHooks(worktree)`). The two callers supply their
own gate:

| | the TUI | `--print` |
|---|---|---|
| the policy | the conversation's own `*permissions.Policy` | the same |
| the sandbox | the session's own, plus the worktree | `printModeSandbox` plus the worktree |
| `--dangerously-skip-permissions` | the session's own flag | read through `Policy.SkipPermissions` |

Report: `docs/cycles/cycle-352-subagent-async/deliverable.md`.

## The image pipeline (`imagery`, cycle 363, agy 1.4.10)

【一手】 agy 1.4.10: "Fixed conversations with many or large images, such
as screenshots or pasted images, being rejected for exceeding the model's
request size limit: images are now capped at 2000 pixels per side and
large PNGs are sent as JPEGs when that is much smaller, and an image saved
as JPEG but labeled PNG, such as a pasted image or browser screenshot, is
no longer rejected for a mismatched type."

Three rules, and one entry point:

| rule | where |
|---|---|
| the cap is **2000** px per side (it was 2576 — the 1.3.2 value) | `imagery.MaxSide` |
| a **large** PNG travels as a JPEG when the JPEG is **much** smaller | `imagery.LargePNGBytes` (64 KiB), `imagery.JPEGMuchSmaller` (2×) |
| the media type follows the **bytes**, never the file name | `imagery.Prepare` returns the format it produced |

`imagery.Prepare(data)` is the single entry point of the three attachment
paths — `internal/clipboard/media.go` (a pasted picture),
`internal/clipboard/paste.go` (the drag-and-drop route) and
`internal/remotecontrol/message_image.go` (the Web UI's attachment) — and
each of them sends the format `Prepare` reports. That is what makes "a
JPEG saved as a PNG" work: the bytes say JPEG, so the request says
`image/jpeg`. The PNG→JPEG conversion flattens transparency onto white
(a JPEG has no alpha channel).

The two thresholds are the clone's own choice (【推测】): the note names
neither "large" nor "much smaller". `imagery.ScaleIfOversized` survives as
the scale-only wrapper it became — the format-changing half moved into
`Prepare`.

Report: `docs/cycles/cycle-363-agy-1.4.10-alignment/deliverable.md` §5.
