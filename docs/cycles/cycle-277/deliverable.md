# Cycle 277 — `run_command` runs on a pty, and the result text is CRLF + ANSI-stripped

## Symptom

Cycle 276 made `run_command` execute through the real shell (`zsh -c` on
macOS), but left two gaps, both listed in its own limitations section:

- **No pty.** The reference puts the child's *stdin* on a pseudo-terminal
  (stdout/stderr stay pipes). The clone passed a nil stdin, so `test -t 0`
  disagreed, `tty` failed, `$(<file)`-style tools that branch on `isatty(0)`
  took the pipe path, and `$-` was `569X` only by luck of the shell.
- **The result text ended `\n`, not `\r\n`.** The reference's captured
  `functionResponse.Output` is CRLF-terminated and has ANSI escapes stripped;
  the clone emitted the raw bytes. Cycle 276 recorded this as the one visible
  difference in its parity table (unclosed quote: `…unmatched "\r\n\n` vs
  `…unmatched "\n`).

This cycle closes both, and the result is **byte-identical** to the reference
for the probe that used to differ.

## What the reference does

All probes are live turns through the `cmd/agy` wrapper (MiniMax M3) against
**agy 1.4.2**, SHA-256 `df2d4fb5…`, captured as raw request/response JSON.

### ① stdin is a tty; stdout and stderr are pipes

```
TTY0=/dev/ttys005
TTY1=not a tty
ISTTY0=yes
ISTTY1=no
PARENT=/Users/gear/.local/bin/agy
speed 9600 baud; rows 24; columns 80;
icanon echo isig icrnl opost onlcr ixany imaxbel -iutf8
FLAGS=569X
```

`569X` is exactly `zsh -c 'echo $-'`; `-i` adds `i`, `-l` adds `l`. The chain
is `cortex/command/command.(*goRunner).Run` →
`google3/devtools/ai/sandbox/exebox` → `google3/third_party/golang/pty`
(`pty.open` / `pty.ptsname` / `pty.ioctl`) — the same openpty-via-ioctl shape
`creack/pty` implements.

### ② The result text is `stripANSI` + a naive LF → CRLF rewrite

Decisive probe — ask the reference to run exactly:

```sh
printf "a\r\nb\n"; printf "\033[1mBOLD\033[0m\n"; echo TOERR >&2; \
  printf "tab\there\n"; echo END
```

Captured `Output` (raw, `repr`):

```
'a\r\r\nb\r\nBOLD\r\nTOERR\r\ntab\there\r\nEND\r\n\n'
```

Four things fall out of that one string:

| observation | means |
|---|---|
| `a\r\r\n` | the rewrite is `strings.ReplaceAll(out, "\n", "\r\n")` — **no CR awareness**, an existing CRLF becomes CR CR LF |
| `BOLD` | ANSI/SGR is stripped before the result is built (`exebox.(*stripANSI)`) |
| `TOERR` | stderr is merged into the same stream |
| `tab\there` | tabs are data, not escapes |

Two further probes rule out a terminal emulator:

```sh
printf "x\rY\n"                      -> 'x\rY\r\n'      # bare CR passed through, not an overstrike
printf "0123456789%.0s" {1..10}      -> 100 chars, not wrapped at 80 columns
printf "no-newline-tail"             -> 'no-newline-tail\n'  # the formatter's own "\n", not a rewritten one
```

So the model is a **byte filter**, not a screen: strip ESC sequences, then
rewrite every LF.

### ③ The reference does not close stdin — it backgrounds

```sh
read x; echo READ_DONE:${x:-empty}
```

went to `Tool is running as a background task with task id: …` after the
model's `WaitMsBeforeAsync` (5000 ms in every capture that carries a value),
and only later produced `READ_DONE:empty` with exit 0. The input end of the
pty is a *persistent terminal*, so a blocking read really blocks.

## Change

| file | what |
|---|---|
| `internal/tools/pty_unix.go` | **new.** `RunWithPTYStdin(cmd)`: opens an 80×24 pty, sets `cmd.Stdin` to the slave, `Setsid+Setctty+Ctty:0`, `Start`, closes the parent's slave fd, drains the master, writes one `VEOF`, `Wait`. |
| `internal/tools/pty_windows.go` | **new.** `RunWithPTYStdin` = `cmd.Run()`. `creack/pty` ships only an `ErrUnsupported` stub on Windows and nothing was probed there, so no pty is claimed. |
| `internal/tools/ansi.go` | **new.** `stripANSI` (CSI / OSC / two-byte designators / lone trailing ESC) and `normalizeCommandOutput` = `stripANSI` + `strings.ReplaceAll(s, "\n", "\r\n")`. |
| `internal/tools/command.go` | `cmd.Run()` → `RunWithPTYStdin(cmd)`; `formatCommandResult(..., out.String())` → `..., normalizeCommandOutput(out.String())`. |
| `cmd/wagyu/main.go` | the `--tool run_command` debug path gets the same pty disposition. (Note: `--tool` is not a registered flag — see limitations — so this path is currently unreachable.) |
| `go.mod` | `github.com/creack/pty v1.1.24` promoted to a **direct** require. `go.sum` already carried it (`v1.1.24 h1:bJrF4RRf…`), so no network and no `go mod tidy`. |
| `internal/tools/tools_test.go` | `TestRunCommandPipeline` now expects `a\r\n`; new `TestRunCommandStdinIsTTY`, `TestRunCommandOutputIsCRLF`, `TestRunCommandStripsANSI`, `TestRunCommandStdinReadGetsEOFInsteadOfBlocking`, `TestRunCommandDoesNotStallOnPTYEcho`. |
| `internal/tools/ansi_test.go` | **new.** Table tests for `stripANSI` and `normalizeCommandOutput`, expectations lifted from the captured `Output` strings above. |

### Three measurements that shaped `RunWithPTYStdin`

Each of these was a real failure mode, found by bisecting a standalone
program with the same syscall sequence.

1. **The parent must close its copy of the slave fd immediately after
   `Start`.** Holding it open leaves the child wedged in `exit()` — `ps` shows
   `STAT ?Es` (the `E` flag means "trying to exit") and `wait4` never returns,
   for even a trivial `zsh -c 'echo hello'`. `creack/pty`'s own `Start` closes
   it there too.
2. **The master must be drained.** The pty echoes our `VEOF` back up to the
   master; with nobody reading it, Darwin holds the child's exit for a fixed
   **~600 ms** waiting for that output to drain:

   | case | not drained | drained |
   |---|---|---|
   | `echo hello` | 0.61 s | 0.01 s |
   | `sleep 1` | 1.62 s | 1.02 s |

   Draining (rather than clearing `ECHO`) keeps the termios identical to the
   reference's `icanon echo isig icrnl opost onlcr …` probe. Without the
   drain every command would silently gain half a second — hence
   `TestRunCommandDoesNotStallOnPTYEcho`.
3. **The master must not be closed while the child lives.** That delivers
   SIGHUP to the session's controlling process group: the probe died with
   `err=signal: hangup` and empty output. It is closed after `Wait`.

### The one deliberate deviation: `VEOF` instead of blocking

The reference's pty is a persistent terminal, so `read x` blocks until
`WaitMsBeforeAsync` hands the command to the background and the model can
`send_input`. The clone's `run_command` is **synchronous** and ignores
`WaitMsBeforeAsync` entirely, so copying that behaviour would wedge the whole
turn with no escape (and would be a regression: with the pre-cycle nil stdin,
`cat` got EOF immediately).

`RunWithPTYStdin` therefore writes a single `0x04` (`VEOF`, i.e. ^D) into the
line discipline right after `Start`. The first canonical read sees EOF and
returns:

| | `read x; echo READ_DONE:${x:-empty}` |
|---|---|
| reference, eventually (after backgrounding) | `READ_DONE:empty`, exit 0 |
| clone, synchronously | `DONE:eof`, exit 0 — same shape |

Commands that never touch stdin are unaffected. This is an inference about
*which* result to converge on, not a reconstruction of the reference's
control flow; it is called out again under limitations.

## Verification

```
$ go build ./internal/tools/ ./cmd/wagyu/ ./cmd/agy/   (clean)
$ go vet ./internal/tools/ ./cmd/wagyu/                (clean)
$ gofmt -l  (new/changed files)                        (clean; tools_test.go's
                                                        pre-existing TestRunCommandEmpty
                                                        hunk left alone)
$ go test -count=1 ./internal/tools/                   ok  4.5s
$ go test -count=1 ./cmd/... ./internal/...            ok  65 packages
```

### ① Byte-for-byte A/B on the decisive probe

Same command line as probe ② above, run through the clone's
`tools.RunCommand` (the exact path the agent uses) and against the reference
via the wrapper:

```
reference (captured)  'Created At: 2026-10-20T02:51:15+08:00\nCompleted At: 2026-10-20T02:51:15+08:00\n\nThe command exited with code 0.\nOutput:\na\r\r\nb\r\nBOLD\r\nTOERR\r\ntab\there\r\nEND\r\n\n'
clone     (measured)  "Created At: 2026-10-20T03:15:29+08:00\nCompleted At: 2026-10-20T03:15:29+08:00\n\nThe command exited with code 0.\nOutput:\na\r\r\nb\r\nBOLD\r\nTOERR\r\ntab\there\r\nEND\r\n\n"
```

Identical apart from the timestamps. This is the string that used to differ.

### ② The cycle-276 parity table is now closed

| | result text |
|---|---|
| reference | `The command exited with code 1.\nOutput:\nzsh:1: unmatched "\r\n\n` |
| clone (276) | `The command exited with code 1.\nOutput:\nzsh:1: unmatched "\n` |
| clone (277) | `The command exited with code 1.\nOutput:\nzsh:1: unmatched "\r\n\n` |

### ③ Other probe parity

| probe | reference | clone |
|---|---|---|
| `tty` | `/dev/ttys005` | `/dev/ttys005` |
| `[ -t 0 ]` | yes | yes |
| `[ -t 1 ]` | no | no |
| `echo $-` | `569X` | `569X` |
| `stty -a` | `speed 9600 baud; rows 24; columns 80;` | same |
| `printf "x\rY\n"; printf "no-newline-tail"` | `x\rY\r\n…no-newline-tail\n` | `x\rY\r\nno-newline-tail\n` |

### ④ Live clone turn (agent path, `-p`, MiniMax M3)

Prompt: *"Use run_command to execute exactly this command line … Then show me
the tool's Output section verbatim."* The model relayed:

```
/dev/ttys005
ISTTY
PIPE1
K=569X
RED
```

i.e. a real tty on stdin, a pipe on stdout, `$-` = `569X`, and the SGR-wrapped
`RED` arriving without its escapes.

## 硬约束 / 存疑 / limitations

- 【一手】 every string in the tables above: the termios/tty/`$-` probe, the
  CRLF+ANSI probe, the `read x` backgrounding observation, the
  `WaitMsBeforeAsync: 5000` values, and all four `Output` strings. All were
  captured through the `cmd/agy` wrapper on the installed 1.4.2 binary.
- 【一手】 the three pty failure modes (slave-fd wedge `STAT ?Es`, ~600 ms echo
  drain, `signal: hangup` on early master close) — measured locally with a
  standalone `creack/pty` program, not inferred from the binary.
- 【推测】 **`VEOF` is a choice, not a reconstruction.** The reference never
  returns a synchronous result for a stdin-reading command; it backgrounds.
  The clone picks the reference's *eventual* result over its *timing*. If
  `WaitMsBeforeAsync` ever lands in the clone, this should be replaced by real
  backgrounding and `send_command_input` on the pty.
- **`WaitMsBeforeAsync` is still ignored.** `RunCommand` parses the
  `run_command` declaration but not that field, and `background: true` is a
  clone-only extension that routes to `bgTable` *without* a pty. So:
  long-running commands are not auto-backgrounded, and a task started with
  `background: true` sees a pipe on stdin, not a tty. Closing that is the next
  natural cycle and is bigger than this one.
- **Not every execution path got a pty.** `RunShellCommand` (`mvdan/sh/v3/interp`,
  clone-only, unadvertised), `ExecCommand`, the background path
  (`bgTable.start`, which needs `StdinPipe` for `send_command_input`) and the
  `git_*` tools all keep their old stdio. Only the reference's advertised
  `run_command` is aligned.
- **Output written to `/dev/tty` is discarded.** The drain goroutine throws
  away everything on the master, so a command that writes to the terminal
  directly (not fd 1/2) vanishes. Not observed to differ — the reference's
  captured `Output` contains stdout+stderr only — but it is unproven whether
  the reference's persistent terminal would surface `/dev/tty` writes into the
  result.
- **`stripANSI` is a reconstruction of a filter, not of its source.** The
  escape classes handled (CSI, OSC, `ESC (`/`)`/`#`/`%`/space designators,
  two-byte escapes, lone/truncated ESC) are the ones a VT parser strips; only
  SGR (`\033[…m`) was confirmed live. BEL, `\b` and other C0 controls are
  passed through — passthrough of CR and TAB *was* confirmed.
- **Windows has no pty.** `RunWithPTYStdin` is `cmd.Run()` there. The
  reference's own Windows branch (powershell/pwsh) was never probed, and
  `creack/pty` has no Windows implementation.
- **The `--tool run_command` debug path cannot be exercised.** The comment in
  `cmd/wagyu/main.go` says "`--tool NAME --tool-args JSON`", and
  `dispatchTool` reads those flags, but **no such flag is registered** — the
  CLI rejects them (`flags provided but not defined: -tool`), so the branch is
  dead code. The edit there is consistency, not tested behaviour.
- **New direct dependency.** `github.com/creack/pty v1.1.24` was already in
  `go.sum` (pulled in transitively) and in the module cache; only the
  `go.mod` require line is new. `go mod tidy` was **not** run (see cycle 276's
  note about `scratch/tea.go`). The import is ~1 small module; it is the same
  openpty/ioctl shape the reference's own `pty` package uses.
- **Environment observation, unrelated to this change.** A full
  `go test ./cmd/... ./internal/...` run left a `wagyu remote-control-foreground`
  LaunchAgent installed
  (`~/Library/LaunchAgents/com.openagy.remote-control-gear.<host>.plist`,
  pointing at a `go-build` cache binary), which then made
  `TestRemoteControlStatus_1to1` and `TestRemoteControlStatus_JSONOutput` fail
  — they assume "no LaunchAgent registered". The leaked agent was booted out
  and its plist moved aside; both tests pass again. The leak itself (a test
  installs a persistent daemon and does not always remove it) is a
  pre-existing test-isolation defect and was **not** fixed here.
  `TestSlashCodesearchStreaming` ("search did not settle in 2s") also failed
  once under the full-suite load and passes in isolation — pre-existing
  timing sensitivity.
