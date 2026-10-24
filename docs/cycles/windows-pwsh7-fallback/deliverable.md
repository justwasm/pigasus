# Cycle windows-pwsh7-fallback — Windows PowerShell 5.1 → 7+ shell resolution

## Goal

AGY 1.3.3 changelog line 47:

> Fixed Windows users without `powershell` (legacy 5.1) failing command
> execution — the CLI now falls back to `pwsh` (PowerShell 7+) when
> `powershell` is not available on PATH.

Cycle windows-pwsh7-fallback wires the same fix into the clone. Before
this cycle `cmd/wagyu/main.go dispatchTool` ran `exec.Command("sh", "-c",
cmdStr)` unconditionally — which silently fails on Windows (no `sh`) and
shells out to `/bin/sh -c` everywhere the agent actually runs. After this
cycle the `--tool run_command` path picks `powershell.exe` first, then
falls back to `pwsh.exe`, on Windows only; macOS / Linux keep the
existing `/bin/sh -c` path verbatim.

## Verify-before-dispatch

```bash
$ grep -r -E 'powershell|pwsh|windowsShell|PowerShell' cmd/ internal/
internal/cli/testdata/changelog_reference.txt:   # reference data only
cmd/wagyu/testdata/print_changelog.md:           # reference data only
internal/tools/command.go:                       # mvdan.cc/sh/v3 import (POSIX shell)
docs/seeds-v1.3.4/seeds.txt:                     # pclntab strings (binary)
out/local-macos/seeds/seeds.txt:                 # pclntab strings (binary)
```

No source-level `powershell`/`pwsh`/`windowsShell`/`PowerShell` resolution
exists in the clone. The gap is real.

## Scope

| Surface | Before | After |
|---|---|---|
| `internal/tools.SelectWindowsShell()` (new) | absent | probes PATH for `powershell.exe`, falls back to `pwsh.exe`, returns `""` on non-Windows or when neither is installed |
| `cmd/wagyu/main.go dispatchTool` `run_command` case | `exec.Command("sh", "-c", cmdStr)` unconditionally | Windows → `tools.SelectWindowsShell()` + `[-NoProfile, -NonInteractive, -Command, cmdStr]`; non-Windows → unchanged `/bin/sh -c` |
| `internal/tools.RunShellCommand` (mvdan.cc/sh/v3) | unchanged | unchanged — already platform-independent |
| `internal/tools.RunCommand` / `ExecCommand` (execvp) | unchanged | unchanged — direct execvp is host-agnostic |

The fix is scoped to `dispatchTool run_command` because that is the only
place the clone shells out to a host-provided interpreter. The
`run_shell_command` tool already runs through `mvdan.cc/sh/v3` (cycle
169), which does not depend on the system PowerShell; `run_command` /
`exec_command` use `exec.Command` with explicit argv, also host-agnostic.

## Files

| Path | Role |
|---|---|
| `internal/tools/winshell.go` (new, 96 lines) | `SelectWindowsShell()` (public, runtime-gated) + `selectWindowsShellOnOS(goos string)` (internal, OS-parameterised so the suite is portable); two `const`s for the candidate shell names |
| `internal/tools/winshell_test.go` (new, 6 tests) | covers non-Windows returns "", both-present picks powershell, only-pwsh falls back to pwsh, no-shell returns "", PATH-order honoured; uses `t.TempDir` + `t.Setenv("PATH", …)` to fake the PATH |
| `cmd/wagyu/main.go` (+13 / −1) | new `"runtime"` import; `dispatchTool run_command` now branches on `runtime.GOOS == "windows"` and shells out via `tools.SelectWindowsShell()`; error message names both candidate shells verbatim so the model can adapt |

**Net:** +109 lines, 0 deletions in unrelated files, 1 import added.

## Wire form — what the dispatchTool branch now does

```go
var c *exec.Cmd
if runtime.GOOS == "windows" {
    shell := tools.SelectWindowsShell()
    if shell == "" {
        return fmt.Errorf("run_command: no powershell found on PATH (looked for powershell.exe, then pwsh.exe)")
    }
    c = exec.Command(shell, "-NoProfile", "-NonInteractive", "-Command", cmdStr)
} else {
    c = exec.Command("sh", "-c", cmdStr)
}
```

The `-NoProfile` / `-NonInteractive` flags match the v1.3.3 reference
behaviour for command-tool shell invocations: `-NoProfile` skips the
user's `$PROFILE` (so the agent does not accidentally execute a
user-installed module loader); `-NonInteractive` blocks prompts that the
agent cannot answer.

When `SelectWindowsShell()` returns `""` (Windows host with neither
shell on PATH — Windows-on-ARM S-mode, Server Core, stripped-down CI
images), the error names both candidate shells verbatim. This is the
same pattern `cmd/wagyu/api_error.go:102` uses for HTTP errors — the
model needs to see the actual candidate set so its retry can suggest
`pwsh.exe` install steps.

## Tests

| Test | Asserts |
|---|---|
| `TestSelectWindowsShell_NonWindows_ReturnsEmpty` | the public helper returns `""` on the host (darwin) |
| `TestSelectWindowsShellOnOS_NonWindows_ReturnsEmpty` | parameterised: every non-windows `goos` value (`darwin`, `linux`, `freebsd`, `openbsd`, `plan9`, `js`) returns `""` |
| `TestSelectWindowsShellOnOS_BothPresent_PrefersPowershell` | PATH contains both → powershell.exe wins |
| `TestSelectWindowsShellOnOS_OnlyPwsh_FallsBackToPwsh` | PATH contains only pwsh.exe → pwsh.exe is returned; explicit assertion that powershell.exe is **not** returned |
| `TestSelectWindowsShellOnOS_NoShell_ReturnsEmpty` | PATH contains neither → `""` |
| `TestSelectWindowsShellOnOS_PATHOrderHonored` | PATH manipulation: dir1=pwsh, dir2=powershell, four permutations of PATH order; verifies powershell.exe wins on the candidate list (independent of PATH ordering between the two) |

Six tests, all runnable from macOS / Linux / Windows because
`selectWindowsShellOnOS` accepts an explicit `goos` parameter. The
helper is split so the platform gate is testable from non-Windows CI.

## Constraints honoured

- Branch: `feature/windows-pwsh7-fallback`, branched from
  `54db67b` (= `main` at session start). Single commit.
- **No push.** Branch is local-only.
- `internal/convstore/sqlite.go` untouched (hard constraint).
- Pre-existing not-clean files (`internal/` x 50 + `cmd/wagyu/picker_view.go`
  + `cmd/wagyu/tui_v135_test.go`) untouched. The only file edited in
  `cmd/` is `cmd/wagyu/main.go`; the diff is limited to adding one
  import and updating the one `case "run_command":` branch.
- `gofmt -l` clean on the two new files and on `cmd/wagyu/main.go`.

## Build / test evidence (this branch)

| Command | Result |
|---|---|
| `go build -o /tmp/wagyu-pwsh ./cmd/wagyu` | exit 0, binary 73,306,562 B |
| `gofmt -l internal/tools/winshell.go internal/tools/winshell_test.go cmd/wagyu/main.go` | exit 0, no output |
| `go vet ./internal/tools/ ./cmd/wagyu/` | exit 0 |
| `go test -vet=off -count=1 -v -run SelectWindowsShell ./internal/tools/` | 6 / 6 PASS, 0.288 s |
| `go test -vet=off -count=1 ./internal/tools/` | 71 / 71 PASS, 5.982 s (65 prior + 6 new) |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` | 1 documented pre-existing flake only |

The one failing test is `TestDispatchPrintSlash_LocalCommands`
(`cmd/wagyu/print_slash_test.go:70`): it asserts the default model is
`minimax-m3` but the local environment resolves to
`gemini-3.8-flash-medium`. Verified to fail identically on `main`
(stashed my changes, re-ran, same failure) — pre-existing, unrelated
to this cycle.

## Hard constraints / 存疑 / limitations

- **The clone does not run on Windows.** This branch is verified on
  macOS/arm64 (Go 1.27.2, host `runtime.GOOS == "darwin"`). The
  Windows branch of `dispatchTool` was not exercised end-to-end. The
  5 `selectWindowsShellOnOS("windows")` tests do exercise the PATH
  logic from macOS, but `exec.LookPath("powershell.exe")` resolves on
  macOS through the Unix code path (file must exist + executable bit),
  not through Windows PATHEXT. On a real Windows host PATHEXT extension
  resolution is honoured automatically — `powershell.exe` is in the
  default PATHEXT.
- **`exec.LookPath` semantics differ by platform.** On Unix the file
  must have the executable bit set; the `fakeShell` test helper
  chmods 0o755 for that reason. On Windows there is no executable bit
  and `exec.LookPath` succeeds on any file matching PATHEXT — so a
  Windows test should drop the chmod call. Not exercised here.
- **No PowerShell-in-Windows integration test.** The behaviour on a
  real Windows host (where `powershell.exe` is on PATH by default) is
  inferred from `exec.LookPath` semantics; no end-to-end `exec.Command`
  invocation has been observed against a Windows shell.
- **`-NoProfile -NonInteractive` flags.** These match the v1.3.3
  reference behaviour inferred from string-table hits
  (`out/decompiled-v1.3.4/strings-new-in-v1.3.4.txt` has both `powershell`
  and `pwsh` literals, and the 1.3.3 changelog says the fix targets
  `powershell`-absent hosts). The exact flag set cannot be confirmed
  from the binary alone; if the reference uses `-Command` with a
  different flag prefix (e.g. `-EncodedCommand` for `-c`), the
  dispatchTool path will diverge. The behaviour matches the public
  docs for PowerShell `-Command` invocation.
- **Scope is `--tool run_command` only.** `internal/tools.RunCommand`
  and `internal/tools.ExecCommand` use `exec.CommandContext(name, …)`
  with no shell, so they do not need shell resolution. The agent's
  `--print` path (`internal/pigrt`) goes through `backend.NewMock()`
  in this clone and never reaches shell exec. The bash-mode
  PowerShell resolution in the reference binary's TUI
  (1.3.3 changelog line 793) is out of scope for this cycle; the
  recovered build does not have a bash-mode code path that reaches
  PowerShell.
