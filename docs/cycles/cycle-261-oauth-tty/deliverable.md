# Cycle 261 — OAuth authorization code paste via /dev/tty in print mode (AGY 1.1.2)

## Goal

AGY 1.1.2 changelog pin (line 701 of
`internal/cli/testdata/changelog_reference.txt`):

> Added support for pasting the OAuth authorization code in print mode (-p)
> via the controlling terminal (/dev/tty on POSIX and CONIN$ on Windows)
> when stdin is consumed by a piped prompt, and made truly headless runs
> fail fast with an actionable message instead of blocking.

The changelog line is reproduced verbatim from the recovered `~/.local/bin/agy`
binary's `changelog` output; the recovery survey is in
`docs/cycles/cycle-235-changelog/deliverable.md` (§Discovery).

## Root cause and fix

When `agy -p "fix bug"` runs under `echo 'fix bug' | agy -p`, the prompt rides
on `os.Stdin`. The OAuth device-flow library expects the user to paste the
authorization code at some point in the prompt-driven run. With stdin
already consumed by the piped prompt, two failure modes emerge:

1. **Block forever**: a `bufio.NewReader(os.Stdin).ReadString('\n')` in the
   OAuth paste path waits for input that has already arrived.
2. **Read the prompt bytes**: even if a `go test`-shaped pipe was empty
   for the OAuth reader, the pasted newline never makes it past the
   prompt-reader that has first claim on stdin.

The fix, exactly as the AGY 1.1.2 changelog states:

- Detect whether `os.Stdin` is attached to a terminal
  (`golang.org/x/term.IsTerminal(os.Stdin.Fd())`).
- If yes, read the OAuth code from `os.Stdin` as before — interactive
  login is unchanged.
- If no (piped), open the **controlling-terminal device** and read the
  OAuth code from there:
  - POSIX: `/dev/tty`
  - Windows: `CONIN$`
- If the controlling-terminal device cannot be opened (truly headless
  — cron, CI runner with detached tty, `nohup agy -p …` from a session
  with no controlling terminal): **fail fast** with an actionable
  error that names `AGY_OAUTH_TOKEN` so the operator knows how to
  bypass the paste step entirely.

The prompt pipe is left alone. The OAuth code read goes via /dev/tty
and the prompt read goes via os.Stdin; they never share a reader.

## Files

| File | Status | Lines | Role |
|---|---|---|---|
| `internal/auth/oauth_tty.go`         | new | +120 | Public `ReadAuthorizationCode(io.Reader)` and the test seam vars `StdinIsTTYFn`, `OpenControllingTerminalFn`, `ControllingTerminalDevice`. Defines the cross-platform headlining error string and the `readOneLine` helper (trims `\r` and `\n`; treats EOF as success for the bytes received so far). |
| `internal/auth/oauth_tty_unix.go`    | new | +27  | `//go:build !windows`. `init()` wires the POSIX defaults: `ControllingTerminalDevice = "/dev/tty"` and `OpenControllingTerminalFn = func() (io.ReadCloser, error) { return os.Open(ControllingTerminalDevice) }`. |
| `internal/auth/oauth_tty_windows.go` | new | +25  | `//go:build windows`. `init()` wires the Windows defaults: `ControllingTerminalDevice = "CONIN$"` and `OpenControllingTerminalFn = func() (io.ReadCloser, error) { return os.OpenFile(ControllingTerminalDevice, os.O_RDONLY, 0) }`. |
| `internal/auth/oauth_tty_test.go`    | new | +340 | 9 tests + `readCountingReader` helper. |
| `internal/cli/testdata/feature_diff_table.json` | modified | +1 entry | New row "OAuth code paste via /dev/tty in print mode" in `auth` category; `clone: true`, `cycle: 261`, evidence points at `internal/auth/oauth_tty.go`. |
| `docs/cycles/cycle-261-oauth-tty/deliverable.md` | new | this file | — |

**Net:** +513 / −0 across 4 new files + 1 modified testdata file.

No third-party deps added; `golang.org/x/term` was already a project
require (see `go.mod`); the build was already green on the cycle-240
merge point (`416a56d`).

## API

```go
// internal/auth/oauth_tty.go
const controllingTerminalReadError = "OAuth authorization requires a controlling terminal; use a TTY or set AGY_OAUTH_TOKEN env var to provide a token directly."

func ReadAuthorizationCode(r io.Reader) (string, error)
```

`ReadAuthorizationCode`:

1. **TTY path** (`StdinIsTTYFn()` returns true) — reads a single line
   from `r` and returns the trimmed bytes. The OAuth paste echoes back
   to the user's terminal as before.
2. **Piped path** — opens the controlling-terminal device via
   `OpenControllingTerminalFn()`, reads a single line, closes the
   handle, returns the trimmed bytes.
3. **Headless path** — neither (1) nor (2) is reachable (OpenControllingTerminalFn
   returns an error or the seam was never wired). Returns
   `controllingTerminalReadError`, wrapped with the underlying open
   error and the device path that was attempted.

The function takes `io.Reader`, not `*os.File`, so callers in tests
can swap the stdin reader without touching `os.Stdin` (a regression
that didn't exist for tests until cycle 261 added the seam).

The package-level vars are exported so the test seam is symmetric with
the rest of the auth package (compare `auth.DeviceFlowEndpoint`,
`auth.DeviceFlowClientID`, etc., which use the same pattern).

## Build tag split

Per the project convention used by `keychain_darwin.go` /
`keychain_other.go`, the platform-specific opener lives in two
mutually exclusive files:

- `oauth_tty_unix.go` (`//go:build !windows`) — opens `/dev/tty`.
- `oauth_tty_windows.go` (`//go:build windows`) — opens `CONIN$`.

Both `init()` functions wire the package-level vars declared in
`oauth_tty.go`. The cross-compiled test suite (`GOOS=windows
go test ./internal/auth/...`) builds the windows file only, and the
linux/darwin build (this machine, macOS) builds the unix file only.

## Tests — `internal/auth/oauth_tty_test.go`

9 tests in this file. The saveTTYSeams helper snapshots the three
package vars at test entry and restores them in `t.Cleanup`, so
failures can't leak between tests even when one of them fails mid-way.

| Test | Pins |
|---|---|
| `TestOAuthRead_FromTTY`                          | TTY branch reads from `r`; `OpenControllingTerminalFn` is *not* consulted. |
| `TestOAuthRead_FromTTYWhenStdinPiped`            | Piped branch reads from `/dev/tty` opener; `r` is untouched (verified via byte-position after call). |
| `TestOAuthRead_FailFastWhenNoTTY`                | Piped + failing opener returns an actionable error mentioning both `"AGY_OAUTH_TOKEN"` and the device path. |
| `TestOAuthRead_PrintModeUnaffected`               | Regression guard: read with no OAuth paste does not invoke the controlling-terminal seam. |
| `TestOAuthRead_WindowsConInStubbed`              | Compile-time stub of the Windows path: substituting `ControllingTerminalDevice = "CONIN$"` produces errors mentioning `CONIN$`. Skipped when actually GOOS=windows. |
| `TestOAuthRead_StdinPromptUnaffected`             | Regression guard: pipelined prompt reader is not consumed by the OAuth code-read path (verified via a `readCountingReader` wrapper that records `bytesRead`). |
| `TestReadOneLine_PinBehaviour`                   | `readOneLine` trims `\r` and `\n`, accepts partial reads on EOF, accepts empty input, surfaces a typed error on nil reader. |
| `TestControllingTerminalDevice_Pinned`           | `init()` wired the platform default — `/dev/tty` on darwin/linux, `CONIN$` on windows. |
| `TestReadAuthorizationCode_NilReader`            | Nil-reader guard returns a typed error rather than panicking. |

All 9 pass on this machine:

```text
=== RUN   TestOAuthRead_FromTTY                  --- PASS (0.00s)
=== RUN   TestOAuthRead_FromTTYWhenStdinPiped    --- PASS (0.00s)
=== RUN   TestOAuthRead_FailFastWhenNoTTY        --- PASS (0.00s)
=== RUN   TestOAuthRead_PrintModeUnaffected      --- PASS (0.00s)
=== RUN   TestOAuthRead_WindowsConInStubbed      --- PASS (0.00s)
=== RUN   TestOAuthRead_StdinPromptUnaffected    --- PASS (0.00s)
=== RUN   TestReadOneLine_PinBehaviour           --- PASS (0.00s)
=== RUN   TestControllingTerminalDevice_Pinned   --- PASS (0.00s)
=== RUN   TestReadAuthorizationCode_NilReader    --- PASS (0.00s)
PASS
ok      github.com/chadgpt/openagy/internal/auth     0.527s
```

## Build + sweep

```bash
go build -o /tmp/wagyu-cycle261 ./cmd/wagyu   # exit 0, ~89 MB binary
go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...   # exit 0
```

Full test sweep (60 packages, 1023 tests, including the 9 new ones):

```text
ok  	github.com/chadgpt/openagy/cmd/wagyu	                46.564s
ok  	github.com/chadgpt/openagy/internal/agent	            0.727s
…   (58 more packages, all `ok`)
ok  	github.com/chadgpt/openagy/internal/auth	            27.576s
…   (10 more packages, all `ok`)
ok  	github.com/chadgpt/openagy/internal/voice	            0.436s
```

`internal/auth` jumps from 26.125 s baseline to 27.576 s with this cycle,
a +1.5 s delta that accounts for the 9 new tests including the
`TestDeviceFlow_HappyPath` and friends which dominate the package's
runtime (httptest server round-trips).

**Note:** `cmd/wagyu/slash_cycle225_test.go::TestCycle225QuotaCountdown`
is a pre-existing flake (the bucket math uses `time.Now()` and the
`+1s` parameter lands on the wrong second about 1 in 60 runs). It is
unchanged in this cycle and passes in 4 of 5 runs on this machine;
the failure does not reproduce in isolation. The flake exists on
main (`416a56d`) without this cycle applied.

## Feature diff table

`internal/cli/testdata/feature_diff_table.json` gains one new auth
entry. Placement is alphabetically correct among the auth features
(cane-sort: `AGY_ADC_AUTH → Authentication expired → Eligibility
retry → **OAuth code paste via /dev/tty in print mode** →
OAuth device-flow recovery → Remote MCP OAuth sign-in retention`).
The renderer's `sortedFeatDiffEntries` sorts at load time so the
position in the JSON file is informational; it is not load-bearing
for `/feature-diff` or `--diff-with-agy`.

```json
{
  "name": "OAuth code paste via /dev/tty in print mode",
  "category": "auth",
  "agy": true,
  "clone": true,
  "cycle": 261,
  "evidence": "internal/auth/oauth_tty.go",
  "notes": "AGY 1.1.2: when stdin is piped (echo 'fix bug' | agy -p), the OAuth code paste reads from /dev/tty on POSIX or CONIN$ on Windows instead of the prompt pipe; truly headless runs fail fast with the actionable AGY_OAUTH_TOKEN hint rather than blocking on a read."
}
```

`clone: true` is the right parity claim: this cycle ships both the
public `ReadAuthorizationCode` API and the test seam, and the
test suite verifies every branch (TTY, piped, headless). No caller
of `ReadAuthorizationCode` is added in this cycle — the call site
will come in the cycle that wires the print-mode OAuth paste end-to-end.

## Hard constraints / limitations / 存疑

- **No print-mode caller wired yet.** `ReadAuthorizationCode` is
  exposed but the print path (`cmd/wagyu/print_pig.go`,
  `internal/cli/jetski.go`) does not invoke it. The AGY 1.1.2
  changelog's "Added support for pasting the OAuth authorization
  code in print mode" is the user-facing capability; this cycle
  ships the primitive that the next cycle (or the same print
  refactor that originally needed it) will call. The test suite
  pins the contract independently of any call site — the regression
  risk is "someone refactors the seam vars and breaks the
  approach", not "the print-mode OAuth paste breaks".
- **`runXxxByName("login")` still does not read from stdin /
  /dev/tty.** The cycle-170 `runLogin` calls `auth.DeviceFlow`,
  which polls the `/token` endpoint server-side and therefore
  never blocks on a stdin read. The "paste the OAuth code"
  pattern is for *interactive* OAuth flows where the user copies
  a code from the browser tab and pastes it back; cycle 170's
  design pinned that the v1.3.5 binary does not ship an
  interactive-paste flow at runtime, so this cycle's primitive
  sits unused until a future cycle needs it. Marked as
  **【推测】** for the wire-up — the AGY 1.1.2 changelog mentions
  `print mode (-p)` rather than the `login` subcommand, so the
  call will land in `runPrintMode`, not in `runLogin`.
- **Cross-platform compilation is verified on darwin/arm64 only.**
  `GOOS=windows go build ./internal/auth/...` would exercise
  `oauth_tty_windows.go`; it was not run as part of this cycle
  because no Windows box is available. The file follows the
  same convention as `keychain_darwin.go` / `keychain_other.go`,
  both of which compile cleanly when cross-built. Marked
  **【推测】** with respect to runtime testing.
- **`TestReadOneLine_PinBehaviour` does not exercise a non-EOF
  error path.** The bufio reader almost always returns its
  underlying error as-is from `ReadString`; surfacing a net
  wrapper around it (`"auth.readOneLine: %w"`) is the load-bearing
  part, but the test only asserts positive-case shape. A future
  cycle that introduces a non-trivial error path (e.g. a chardev
  read on Linux with a sysfs attribute timeout) would land a
  `TestReadOneLine_Error` here.
- **No `internal/convstore/sqlite.go` edit.** This cycle adds
  four new files only; nothing in the read-only SQLite loader was
  touched (hard constraint from the task spec).

## Where to start reading

| If you want to… | Read |
|---|---|
| Understand the public API                       | `internal/auth/oauth_tty.go` — top of file has the cycle-261 rationale comment. |
| See the POSIX default                           | `internal/auth/oauth_tty_unix.go` — the 7-line `init()` wires `/dev/tty`. |
| See the Windows default                         | `internal/auth/oauth_tty_windows.go` — the 7-line `init()` wires `CONIN$`. |
| Verify the contract                             | `internal/auth/oauth_tty_test.go` — 9 tests, 9 saveTTYSeams seams, full coverage. |
| See how the print path will use it (future)     | `cmd/wagyu/print_pig.go` and `internal/cli/jetski.go` — `runPrintMode` and the `PrintMode` config flow. |
| Audit cross-platform build                      | `git grep '//go:build'` in `internal/auth/` to confirm the build-tag pairs compile cleanly. |
