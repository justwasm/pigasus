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
  closed while the child lives (SIGHUP). One deliberate deviation: the clone
  is synchronous and ignores `WaitMsBeforeAsync`, so instead of blocking on a
  stdin read it writes a `VEOF` (^D) — the reference backgrounds the command
  and only *eventually* reports the same `…:empty` result.
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

**Result texts are replayed, not remembered.** `internal/resultparity` parses
`internal/tools/testdata/result_texts_1.4.4.txt` (32 captured call/result pairs)
and replays each one against the real executors — 28 compared, 4 skipped with a
recorded reason, and a bijection test that fails if a section is neither. Run
`go test ./internal/resultparity/` after touching any tool result text. The
fixture has **no `run_command` section** — the most-used tool's text lives in
`docs/cycles/command-output-truncation/captures.md` and is pinned by
`internal/tools/command_output_truncation_test.go` instead. It calls
`Tool.Run`, not `Dispatch`, because `Dispatch`/`piagentbridge` add
`ApplyOutputBudget` on top (see below).

**That budget is a guess and contradicts the capture.** `DefaultMaxResultTokens
= 8000` (cycle 248, self-described as "the reference binary ships a
model-class-dependent value") with a `/4` chars-per-token estimate caps
model-visible text near 32 KB and wraps it in `_truncated: true` / `_preview: |`.
The 1.4.4 capture shows the reference handing the model a 46080-byte `read_file`
body whole, with no wrapper. Report: `docs/cycles/result-text-replay/deliverable.md`.
