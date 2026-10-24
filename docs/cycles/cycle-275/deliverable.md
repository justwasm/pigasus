# Cycle 275 — the reference's tool surface: `run_command` is a shell, paths are absolute

## Why

> 在 ./cmd/wagyu 里面容易出现 tool call failed 的情况，这个我已经提到很多遍了，但是从来没有解决过
> 尽管他们实际好像是成功的

The user runs the clone as their daily driver and has reported, repeatedly, that
tool calls come back `(Failed)` **while the operation actually happened**. Cycles
133 and 148 both went after the *rendering* of `(Failed)` (wire-key casing, the
status→err mapping on resumed rows). This cycle found the defect was in the two
executors the transcript's failing rows actually went through.

The rows in question (from the user's own session):

```
● Read /Users/gear/GitHub/openagy/README.md (Failed)
● Read /Users/gear/GitHub/openagy/go.mod (Failed)
● Bash (head -100 README.md) (ctrl+o to expand)      ← no shell syntax: OK
● Bash (ls internal/ | head -80) (Failed)
● Bash (ls -la out/ 2>&1 | head -50) (Failed)
● Bash (find out -maxdepth 2 -type d 2>&1 | head -30) (Failed)
● Bash (ls docs/cycles/ 2>&1 | tail -10) (Failed)
```

Two families, both exactly reproducible:

1. **`view_file` rejected the absolute path the reference requires.** The model
   is told `AbsolutePath … Must be an absolute path` (the reference's own
   declaration), so it sends one; the clone's `workspacePath` refused every
   path starting with `/`.
2. **`run_command` was not a shell.** Pipelines, redirects and `&&` reached the
   program as literal argv. `ls internal/ | head -80` ran `ls` with `|` as a
   filename: the listing was printed, then `ls` exited 2 — hence "it actually
   seems to succeed" — and `Dispatch` throws the output away on a non-nil
   error, so the transcript showed `(Failed)` and the model got *nothing*.

Coverage before/after (a temp workspace, `Catalog(dir)` + `Dispatch`):

| Call | Before | After |
|---|---|---|
| `{"command":"ls / | head -3"}` | `exit status 2` (no output) | the 3-line listing |
| `{"CommandLine":"echo hello"}` | `empty command` | `hello` |
| `{"path":"/etc/hosts"}` | `expected path /etc/hosts to not start with /` | the file |
| `{"AbsolutePath":"/abs/x.txt"}` | `read <workspace>: is a directory` | the file |

## Evidence

### 一手 — the reference's captured tool declarations

Read out of the request bodies the `cmd/agy` wrapper captured
(`~/.openagy/captures/*-streamGenerateContent.json`):

* `view_file.AbsolutePath` — *"Path to file to view. **Must be an absolute
  path.**"* with `ContentOffset`, `StartLine`, `EndLine`; required
  `AbsolutePath, toolSummary, toolAction`.
* `run_command` — *"Make sure to specify **CommandLine exactly as it should be
  run in the shell**."* … *"Shell: zsh."* … *"The Cwd (working directory) MUST
  be within the user's workspace."* with `Cwd`, `IsDaemon`,
  `RequestedTerminalID`, `RunPersistent`, `WaitMsBeforeAsync`; required
  `Cwd, WaitMsBeforeAsync, CommandLine, toolSummary, toolAction`.
* `write_to_file.TargetFile` and `replace_file_content.TargetFile` — *"Must be
  an absolute path."*
* The advertised surface lists `run_command` and **no** `run_shell_command`
  (`internal/tools/testdata/advertised_tools_1.3.6.txt:49`), so the clone's
  "use run_shell_command when the model wants pipes" note was pointing at a tool
  the model is never offered.

### 一手 — the reference's result text

The same captures carry the `functionResponse` the model receives (`output` is a
plain string, never an error):

```
Created At: 2026-10-20T02:04:43+08:00
Completed At: 2026-10-20T02:04:43+08:00

The command exited with code 0.
Output:
agent
agentsdir
agyprompt
```

and, for `ls /definitely-not-here`:

```
The command exited with code 2.
Output:
ls: cannot access '/definitely-not-here': No such file or directory
```

**A non-zero exit is data, not a failure.** The clone returned
`(output, exec.ExitError)` instead, and `Dispatch` discarded the output.

### 一手 — live turns on the official 1.4.2 binary

Run through `cmd/agy` with MiniMax M3 (the Google path is location-blocked for
this account, see the wrapper limitations):

| Prompt | Result |
|---|---|
| `view_file AbsolutePath=/Users/gear/Documents/GitHub/openagy/AGENTS.md` | reads it; first heading `# AGENTS.md` |
| `view_file AbsolutePath=/Users/gear/GitHub/openagy/AGENTS.md` (the `~/GitHub` symlink spelling) | reads it |
| `view_file AbsolutePath=/etc/hosts` | reads it — **reads are not workspace-bound** |
| `run_command CommandLine="ls internal \| head -3"` | exit 0, the 3-line listing |
| `run_command CommandLine="ls /definitely-not-here"` | exit 2, stderr in `Output`, no error |

### 一手 — the clone, before and after

Print mode against the same model
(`/tmp/wagyu -p … --model "MiniMax M3"`), before:

```
The command exited with status 2, but no stdout was captured. …
This strongly suggests /Users/gear/Documents/GitHub/openagy/internal does not exist.
```

and after:

```
Raw stdout:

    agent
    agentsdir
    agyprompt
```

and for the file read, after: *"The first heading (verbatim) is: `# AGENTS.md`"*.

## What changes

| File | Change |
|---|---|
| `internal/tools/command.go` | `RunCommand` parses `CommandLine` through `mvdan.cc/sh/v3/interp` (same interpreter as `run_shell_command`), honours `Cwd`, returns the reference's `Created At / Completed At / The command exited with code N. / Output:` text with **nil error**, including for a non-zero exit (124 on timeout, 130 on interrupt). New `resolveCwd`, `formatCommandResult`, `runCommandDescription`, `refOSName`, `runCommandSchema`. |
| `internal/tools/filesystem.go` | `ReadFile` (advertised as `view_file`) uses the new `readPath`, supports `ContentOffset`, decodes the reference keys, and advertises the captured `viewFileSchema` + `viewFileDescription`. `ListDir` and the six write tools (`CreateFile`, `EditFile`, `AppendFile`, `DeleteFile`, `RenameFile`, `CopyFile`) go through `argsOf` + `workspacePath` instead of `filepath.Join`, which silently turned `/etc/x` into `<ws>/etc/x`. |
| `internal/tools/nonnetwork.go` | `workspacePath` no longer rejects every absolute path: an absolute path inside the workspace is accepted (compared after symlink resolution, so `~/GitHub` and `~/Documents/GitHub` are one directory), one outside it keeps the recovered message, a relative path may still not escape. New `readPath` (any absolute path — reads are unrestricted, per the live turn), `canonicalPath`, `within`. `toolKeyAliases` gains `CodeContent`/`TargetContent`/`ReplacementContent`. |
| `internal/tools/catalog.go` | A spec-supplied schema now outranks the executor's own `Schema()`, so `run_command`/`view_file` advertise the captured declarations. Description + schema for those two come from the capture. |
| `cmd/wagyu/main.go` | `toolTarget` reads the reference's capitalised keys (`CommandLine`, `AbsolutePath`, `TargetFile`, `DirectoryPath`, `SearchPath`) — the sandbox and the settings allow/deny rules were matching nothing on a real call. |

Tests: `TestWorkspacePath_Absolute`, `TestWorkspacePath_SymlinkedSpelling`
(replace the old `TestWorkspacePath_RejectsAbsolute`, which pinned the wrong
rule), `TestRunCommandParseError` (renamed; the diagnostic is now `parse`),
`TestRunCommand_GitCommitNoMessage_FailsFast` (asserts the exit code in the
result text, not an error), `TestToolTarget` (capitalised keys),
`TestTools_CarriesTheReferenceDescriptions` (asserts the captured prose).

## What does NOT change in cycle 275

- The `(Failed)` rendering. It is still `err: err != nil`; it is now correct
  because the executors no longer report a failure for a command that ran.
- `run_shell_command` / `exec_command`. They keep their own shape and their
  `(output, exitStatus)` convention; neither is advertised.
- The write tools' *schemas*. `write_to_file`/`replace_file_content` still
  advertise the clone's `path`/`content` and `path`/`oldString`/`newString`
  shapes. Their executors now understand the reference's key names
  (`TargetFile`, `CodeContent`, `TargetContent`, `ReplacementContent`), but not
  yet the reference's line-range semantics (see limitations).
- `background: true` — a clone extension kept from cycle 43. It still
  tokenises with shlex and cannot express a pipeline.
- `internal/tools/testdata/advertised_tools_1.3.6.txt`: the 60 advertised
  *names* are untouched.

## 硬约束 / 存疑 / limitations

- 【一手】 for the declarations, the result text, and every live-turn result
  above; 【推测】 where marked. The captures are byte-for-byte request bodies
  (`~/.openagy/captures/`, unredacted by design), so the declarations can be
  re-diffed after any `agy` release.
- **The shell is zsh; the clone's `mvdan/sh/v3/interp` is a deviation.**
  Corrected 2026-10-20 — the earlier wording of this bullet ("the interpreter
  the recovered binary itself embeds") was wrong.

  The reference executes `CommandLine` through a **real shell**, not through
  `mvdan/sh`. Verified live on 1.4.2 through `cmd/agy` (【一手】):

  ```
  $0=zsh        $-=569X       ISTTY0=yes   ISTTY1=no
  PARENT=/Users/gear/.local/bin/agy         (i.e. the agy binary itself)
  ```

  `569X` is exactly what `zsh -c '…'` reports (no `i`, no `l`), so it is a plain
  `zsh -c "<CommandLine>"` with **stdin on a pty** and stdout/stderr on pipes.
  The `\r\n` in the reference's result text (confirmed in the raw
  `functionResponse`) comes from that pty path, not from the program.

  The binary's own `mvdan/sh` is a **parser only**: 1.4.2 has 317
  `mvdan/sh/syntax` + 1 `fileutil` symbols and **zero** `mvdan/sh/interp`,
  **zero** `mvdan/sh/expand` (1.3.4: 292 + 1, also zero). Every caller of
  `syntax.(*Parser).Parse` is analysis or policy —
  `cortex/command/command.parseCommand`, `command.walkCommandCalls`,
  `cortex/policyguardian/vetting.SplitLiteralCommand`,
  `vetting.isReadOnlyCommand`, `security/safetext/shprintf.SprintfLang`,
  `terminal.parseShell` — never execution.

  The shell *name* is `terminal.GetDefaultShellName()`, a hardcoded per-OS
  switch: darwin→`zsh` (【一手】, disassembly of the type switch + the live
  turn above), linux→`bash` and windows→`powershell`/`pwsh` (【推测】: the two
  remaining branches; the Windows one probes `os/exec.LookPath("pwsh")`). It
  does **not** consult `$SHELL`: the literal string `SHELL` does not occur
  anywhere in the binary, and on this machine — where `$SHELL` is
  `/opt/homebrew/bin/bash` — `agy` still ran `zsh`. The same function supplies
  the `Shell: %s` placeholder in the tool declaration
  (`cortex/config.defaultConversationalCascadeConfig` calls it).

  Consequences for the clone: pipes, redirects, `&&`/`||`, `$VAR` and POSIX
  globbing work, but **zsh-only syntax does not** (`**/*(.)` qualifiers,
  `setopt`, `$(<file)`, process substitution) — and the clone's `$0` / `$-` /
  `test -t 0` differ from the reference. The clone emits `\n`, not `\r\n`.
  Switching `RunCommand` onto `zsh -c` (the reference behaviour) is tracked as
  cycle 276 and is **not** part of this cycle.
- `WaitMsBeforeAsync`, `IsDaemon`, `RunPersistent`, `RequestedTerminalID` are
  accepted and **ignored**: the clone runs synchronously and has no persistent
  terminal. A command the reference would background is run in the foreground
  until `timeout` (or the caller's context) expires. The clone's `background:
  true` extension is the only async path, and it is still tokenised.
- **Reads are unrestricted.** `view_file`/`list_dir` will read any absolute
  path, matching the one observed reference turn (`/etc/hosts`). The permission
  policy and the terminal sandbox remain in front of them; with the clone's
  default allow policy that means a read is not gated. This is a deliberate
  mirror of observed behaviour, not a vetted security decision.
- **Writes stay workspace-bound.** The reference's declarations also say
  `TargetFile … Must be an absolute path`, but I did not observe a live
  out-of-workspace write, so `workspacePath` still refuses one. A write to an
  artifact directory (`<AppDataDir>/brain/<conv>`) therefore still fails; that
  predates this cycle.
- `ContentOffset` is honoured only when no line range is given, and the
  reference's own caps (800 lines, 46,080 bytes, 100 MB) are **not** applied —
  the clone still has no default view cap.
- `replace_file_content` keeps literal search/replace semantics: the model's
  `StartLine`/`EndLine`/`AllowMultiple` are decoded and ignored. A model that
  relies on the line window can therefore replace the wrong occurrence.
- The advertised surface is unchanged for the other 58 names; only
  `run_command` and `view_file` carry a captured schema. The rest are still the
  clone's reconstruction (【推测】, cycle 98).
- The `~/GitHub` symlink is a property of this machine; `canonicalPath` resolves
  the longest existing prefix, so it also covers a workspace reached through any
  other symlink, but it resolves nothing for a path whose every ancestor is
  missing.
- Verification is macOS/arm64 + MiniMax M3 through `agw.k0s.io` (the account's
  Google path is location-blocked). Linux/Windows paths, the Google model, and
  the TUI's own rendering of the fixed steps were not exercised end to end; the
  TUI consumes the same `tools.Catalog` + `Dispatch`, so the fix reaches it by
  construction rather than by observation.
