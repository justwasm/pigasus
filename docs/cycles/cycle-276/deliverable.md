# Cycle 276 — `run_command` executes through the real shell (not `mvdan/sh`)

## Symptom

Cycle 275 made `run_command` a shell, but it ran the command line through
`mvdan.cc/sh/v3/interp` — a **pure-Go POSIX** interpreter — while the tool
declaration the model sees promised `Shell: zsh.` (macOS). The mismatch is
user-visible:

- zsh-only syntax fails: `$(<file)`, `setopt`, `**/*(.)`, process
  substitution, `=(...)`.
- `$0`, `$-`, `test -t 0` disagree with the reference.
- An unclosed quote produced a **clone-side parse error** (`run_command:
  parse: …`) instead of the reference's data-shaped result.

Cycle 275's report also stated, wrongly, that `mvdan/sh/v3` is "the interpreter
the recovered binary itself embeds". This cycle corrects that claim and the
code that rested on it.

## What the reference actually does

Two independent lines of evidence, both against **agy 1.4.2**
(SHA-256 `df2d4fb5…`).

### ① The binary's `mvdan/sh` is a parser only

`rea-go-seeds` was re-run against 1.4.2 (the committed
`out/local-macos/seeds/` is still 1.3.4 — see the note below):

| symbol group | 1.3.4 | 1.4.2 |
|---|---|---|
| `mvdan/sh/syntax` | 292 | **317** |
| `mvdan/sh/fileutil` | 1 | 1 |
| `mvdan/sh/interp` | **0** | **0** |
| `mvdan/sh/expand` | **0** | **0** |

Every caller of `syntax.(*Parser).Parse` is analysis, policy or quoting —
never execution:

| caller | role |
|---|---|
| `cortex/command/command.parseCommand` | split the model's command line |
| `cortex/command/command.walkCommandCalls` | enumerate invoked binaries |
| `cortex/policyguardian/vetting.SplitLiteralCommand` | permission policy |
| `cortex/policyguardian/vetting.isReadOnlyCommand` | auto-run decision |
| `security/safetext/shprintf.SprintfLang` | safe shell quoting |
| `terminal.parseShell` | terminal-side command analysis |

### ② Execution is a real `zsh -c`

Live turn through the `cmd/agy` wrapper (MiniMax M3), asking the reference to
run a probe:

```
P0=zsh
FLAGS=569X
ISTTY0=yes
ISTTY1=no
PARENT=/Users/gear/.local/bin/agy
```

`569X` is exactly what `zsh -c '…'` reports (locally: `/bin/zsh -c 'echo $-'
→ 569X`; adding `-i` or `-l` adds `i` / `l`). `P0=zsh` (not `/bin/zsh`) means
`argv[0]` is the bare name from `LookPath`. stdin is a pty; stdout/stderr are
pipes. The chain is
`cortex/command/command.(*goRunner).Run` → `sandbox/exebox` →
`third_party/golang/pty`.

### ③ The shell name is a per-OS constant, not `$SHELL`

`terminal.GetDefaultShellName` (disassembled at `0x100e25ea0` in 1.4.2) is a
type switch over `go_utils.GetOsVersion()`:

| branch | returns |
|---|---|
| windows | `powershell`, or `pwsh` when `os/exec.LookPath("pwsh")` finds it |
| unix A | `bash` |
| unix B | `zsh` |
| default | `bash` |

Decisive negative evidence: the literal string **`SHELL` does not occur
anywhere in the binary**, and this machine's `$SHELL` is
`/opt/homebrew/bin/bash` — yet `agy` still ran `zsh` and still advertised
`Shell: zsh.` in the captured declaration. The same function feeds that
placeholder (`cortex/config.defaultConversationalCascadeConfig` calls it).

## Change

| file | before | after |
|---|---|---|
| `internal/tools/command.go` | `RunCommand` → `syntax.NewParser().Parse` + `interp.New(...).Run` | `RunCommand` → `exec.CommandContext(DefaultShellArgv(src))`, `Dir=resolveCwd`, `Env=AgentShellEnv()`, combined stdout+stderr |
| `internal/tools/command.go` | `runCommandDescription` took the shell from `filepath.Base(os.Getenv("SHELL"))` | uses `DefaultShellName()` |
| `internal/tools/command.go` | background path shlex-split argv | `backgroundTasks.start(..., DefaultShellArgv(src), ...)` — `background: true` can now express a pipeline |
| `internal/tools/command.go` | new | `DefaultShellName()`, `DefaultShellArgv(src)` |
| `cmd/wagyu/main.go` | `--tool run_command` used `sh -c` on Unix | `tools.DefaultShellArgv` (same shell as the agent path) |
| `go.mod` | `github.com/google/shlex` direct require (cycle 166) | removed — the only importer was `RunCommand`'s tokeniser |
| `internal/tools/tools_test.go` | `TestRunCommandParseError` | `TestRunCommandUnclosedQuoteIsShellError` (exit 1 + shell diagnostic, nil error) |
| `internal/tools/tools_test.go` | new | `TestRunCommandPipeline`, `TestRunCommandNonZeroExitIsData`, `TestDefaultShellNameIgnoresDollarSHELL`, `TestDefaultShellArgv` |

`run_shell_command` — a **clone-only compat alias the reference does not
advertise** — deliberately stays on `mvdan/sh/v3/interp`; it is
platform-independent and has no reference counterpart to match. The
`mvdan/sh` imports for that path are unchanged.

Exit-code mapping: `0` normal; `*exec.ExitError.ExitCode()` otherwise;
`124` on context deadline; `130` on cancellation; `128` when killed by a signal
(the exact `128+signum` is platform-specific and is not reconstructed).

## Verification

```
$ go build ./internal/tools/ ./cmd/wagyu/            (clean)
$ go vet ./internal/tools/ ./cmd/wagyu/              (clean)
$ go test -count=1 ./internal/tools/                 ok   4.1s
$ go test -count=1 ./cmd/... ./internal/...          all ok
```

Live clone turn (`/tmp/wagyu -p … --model "MiniMax M3"`), using zsh-only
`$(<file)`:

```
P0=zsh
ZSH=zsh-only-marker
FLAGS=569X
DONE
```

Before this cycle the same command failed inside `interp` (`$(<…)` is a zsh
extension). Parity check on the unclosed-quote case:

| | result text |
|---|---|
| reference | `The command exited with code 1.\nOutput:\nzsh:1: unmatched "\r\n\n` |
| clone | `The command exited with code 1.\nOutput:\nzsh:1: unmatched "\n` |

The only difference is `\r\n` vs `\n` (see below).

## 硬约束 / 存疑 / limitations

- 【一手】 the 317/1/0/0 symbol counts (regenerated from the installed 1.4.2
  binary), the `syntax.(*Parser).Parse` caller list, the
  `GetDefaultShellName` return values, the live `$0` / `$-` / `TTY` probe,
  and both result texts in the parity table above.
- 【一手】 **the committed `out/local-macos/seeds/` is stale.** It was produced
  from 1.3.4 (`9da414c0…`); the installed binary is 1.4.2 (`df2d4fb5…`), so
  every address in it is shifted. Disassembling a 1.3.4 address against 1.4.2
  yields float arithmetic that looks like nonsense. Regenerate with
  `cd rea-go-seeds && go run . ~/.local/bin/agy <outdir>` (2 s, 158,955 names)
  before trusting any address. The seeds directory was **not** overwritten by
  this cycle — it is still the 1.3.4 artifact.
- 【推测】 `linux → bash` and `windows → powershell/pwsh` are the two remaining
  branches of the same type switch; only the darwin branch was confirmed live.
  The Windows branch's `LookPath("pwsh")` call is 【一手】 from the callee list.
- 【一手】 `GetDefaultShellName` does **not** read `$SHELL`; `SHELL` is absent
  from the binary's strings. But whether the *reference* would honour a
  user-configured shell in some other config path was not tested.
- **No pty.** The reference puts the child's stdin on a pty, which is why its
  result text uses `\r\n`. The clone runs with inherited-nil stdin and plain
  pipes, so its output uses `\n`. Programs that branch on `isatty(0)` (pagers,
  some REPLs, `read -p` prompts) will behave differently. This is the largest
  remaining gap and is why `test -t 0` differs.
- **No terminal sandbox.** The reference routes through
  `google3/devtools/ai/sandbox/exebox` (sandbox profiles, `stripANSI`, escape
  handling). The clone still relies on its own `internal/sandbox` +
  permission policy in front of the tool; this cycle did not touch it.
- **Environment is inherited, not matched.** `AgentShellEnv()` appends the
  editor/pager overrides to `os.Environ()`. The reference builds the child env
  from the terminal session (`dedupEnvLastWinsForOS`, `selectEnvByNameForOS`,
  `appendPathEnv`); the clone does not reproduce that normalisation.
- The zsh startup files are **not** read: `-c` is non-interactive and
  non-login (no `i`/`l` in `$-`), which matches the reference. `.zshenv` *is*
  read by zsh in this mode in both.
- `TestRunCommandPipeline` / `TestRunCommandNonZeroExitIsData` /
  `TestRunCommandUnclosedQuoteIsShellError` skip on Windows; only the POSIX
  path has a live counterpart.
- `run_shell_command` still uses `mvdan/sh/v3/interp` on purpose. If the
  reference ever starts advertising it, that decision has to be revisited.
- `go.sum` still carries the `github.com/google/shlex` entries. `go mod tidy` was
  **not** run wholesale: it also promotes `charmbracelet/x/term` and
  `muesli/cancelreader` to direct requirements because `scratch/tea.go` imports
  the former — and `scratch/` has never compiled, so that churn is unrelated to
  this cycle. Only the `go.mod` require line was removed.
