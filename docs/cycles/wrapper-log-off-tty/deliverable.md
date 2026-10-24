# Cycle wrapper-log-off-tty — `agyproxy:` lines were smearing agy's TUI

## Symptom

Running `./cmd/agy` (the cycle-274 wrapper) interactively corrupted the TUI.
The user's report:

> 现在一个问题是 ./cmd/agy stderr 输出会破坏 tui
>
> 就是 agyproxy 字样的日志啊

Confirmed by inspection: every proxy line carries the `agyproxy: ` prefix and
was written to **fd 2**.

## Root cause

`devutils/agyproxy.New` defaults an unset `Config.Logf` to
`log.New(os.Stderr, "agyproxy: ", 0)`:

```go
logf := cfg.Logf
if logf == nil {
    l := log.New(os.Stderr, "agyproxy: ", 0)
    logf = l.Printf
}
```

The launcher declares the proxy config **without** `Logf`:

```go
srv, err := agyproxy.New(agyproxy.Config{
    Addr: ..., Upstream: ..., Models: ..., CaptureDir: ...,
})          // ← no Logf
```

and then execs agy with `cmd.Stderr = os.Stderr`. So the proxy and the TUI
write to the same terminal. The proxy emits roughly one line per RPC
(`POST /v1internal:… -> 200 (1.6s)`), plus a `listening on …` line and capture
warnings — all landing in the middle of the alt-screen.

Measured before the fix, with the streams split:

```
$ ./cmd/agy models 2>/tmp/w.err >/tmp/w.out
stdout lines: 15   stderr lines: 11
agyproxy: listening on http://127.0.0.1:63409 (upstream …)
Fetching available models...                       ← agy's own line, legitimate
agyproxy: POST /v1internal:loadCodeAssist -> 200 (1.638s)
agyproxy: POST /v1internal:fetchAdminControls -> 200 (127ms)
```

Eleven proxy lines on fd 2. `Fetching available models...` is agy's own stderr
output and is not ours to remove.

## Fix

Log to a file by default; stderr becomes an explicit, labelled escape hatch.

`cmd/agy/log.go` (new):

```go
OPENAGY_WRAPPER_LOG unset             → ~/.openagy/wrapper.log (append, 0600,
                                        truncated past 4 MiB)
                     stderr|fd2|debug → os.Stderr   (debugging only)
                     off|none|discard → discard
                     <path>           → that file
```

`cmd/agy/launcher.go` now opens that sink and passes it:

```go
logf, closeLog := openLog()
defer closeLog()
...
srv, err := agyproxy.New(agyproxy.Config{ …, Logf: logf })
```

Two deliberate choices:

1. **Failure falls back to discarding, never to stderr.** An unwritable log
   file must not turn a working agy into a smeared screen — and it must not stop
   agy from starting. Silently losing log lines is the lesser evil.
2. **The logs stay discoverable.** `agy wrapper log [-n N]` tails the file, and
   `agy wrapper doctor` prints the active sink on a `log` line. Moving output
   off the terminal is only acceptable if it is still reachable.

File logs carry timestamps (`log.LstdFlags`) because several runs share one
file; the stderr sink keeps the old bare `agyproxy: ` prefix.

## Verification

Streams split after the fix:

```
$ ./cmd/agy models 2>/tmp/w.err >/tmp/w.out
stderr lines: 1      ← only agy's own "Fetching available models..."
$ cat /tmp/w.err
Fetching available models...
```

The proxy lines moved to the file:

```
$ agy wrapper log -n 3
/Users/gear/.openagy/wrapper.log (last 3 lines)

agyproxy: 2026/10/20 01:28:56 POST /v1internal:loadCodeAssist -> 502 (462ms)
agyproxy: 2026/10/20 01:28:56 POST /v1internal:retrieveUserQuotaSummary: upstream error: …
agyproxy: 2026/10/20 01:28:56 POST /v1internal:retrieveUserQuotaSummary -> 502 (463ms)
```

**The actual complaint, tested on the actual TUI** (tmux, 120×40, 20 s):

```
$ tmux -L agytui274 send-keys -t t "/tmp/agy-wrapper" Enter
$ tmux -L agytui274 capture-pane -p -t t | grep -c agyproxy
0
```

and the pane is intact — including the injected model in the header:

```
      ▄▀▀▄        Antigravity CLI 1.4.2
     ▀▀▀▀▀▀       navigaid@gmail.com (Antigravity Starter Quota)
    ▀▀▀▀▀▀▀▀      MiniMax M3
   ▄▀▀    ▀▀▄     /tmp
```

Meanwhile the log file kept receiving the traffic
(`POST /v1internal:listExperiments -> 200`), i.e. nothing was lost by moving the
sink.

Sink variants:

| invocation | stderr lines | file written |
|---|---|---|
| default | 1 (agy's) | `~/.openagy/wrapper.log` |
| `OPENAGY_WRAPPER_LOG=off` | 0 | no |
| `OPENAGY_WRAPPER_LOG=stderr` | 17 | no (old behaviour, opt-in) |
| `OPENAGY_WRAPPER_LOG=<path>` | 1 (agy's) | that path |

Tests (`cmd/agy/log_test.go`, all pass): default writes under `$HOME` and
carries the `agyproxy: ` prefix; `off` creates nothing; `stderr`/`debug` is
labelled with a "smear" warning; a custom path creates parent dirs; an
unwritable sink degrades to discard without panicking; `tailLines` ring
behaviour; and a guard that the default sink is not stderr.

```
$ go test -count=1 ./cmd/agy/... ./devutils/... ./internal/models/...
ok  	…/cmd/agy	0.553s
ok  	…/devutils/agybin	0.869s
ok  	…/devutils/agyproxy	0.557s
ok  	…/internal/models	0.935s
$ go vet ./cmd/agy/...        # clean
$ gofmt -l cmd/agy            # empty
```

## Files

| File | Change |
|---|---|
| `cmd/agy/log.go` | new — sink resolution, file open with size cap, `wrapper log` tail |
| `cmd/agy/log_test.go` | new — 7 tests pinning "file by default, never fd 2" |
| `cmd/agy/launcher.go` | modified — opens the sink and passes `Logf` |
| `cmd/agy/wrapper.go` | modified — `log` subcommand, usage text, `doctor` sink line |
| `cmd/agy/README.md` | modified — Logging section + env row |
| `AGENTS.md` | modified — the wrapper section's "things that bite" gains the log rule |

## 硬约束 / 存疑 / limitations

- **"Never smears" is proven for the default and `off` sinks only.** With
  `OPENAGY_WRAPPER_LOG=stderr` the wrapper still writes to fd 2 on purpose —
  that is the documented debugging mode, and `doctor` labels it.
- **agy's own stderr output is untouched.** `agy models` still prints
  `Fetching available models...` on stderr; the TUI verdict above is for the
  interactive path, where agy's own writes are its business, not ours.
- **No rotation.** Past 4 MiB the file is truncated on next open, so a long
  session followed by a crash loses the early history of that session.
- **Writes are unsynchronised.** `log.Logger` serialises each line, but the
  proxy may log from several goroutines; ordering between concurrent requests
  is not guaranteed (same as before the fix — this cycle did not change
  concurrency).
- **The 4 MiB cap is a constant, not configurable.** A user who wants more must
  point `OPENAGY_WRAPPER_LOG` at their own path, which still gets the cap.
- **Windows/`HOME`.** `os.UserHomeDir` honours `$HOME` on Unix; the tests use
  `t.Setenv("HOME", …)`. Windows path resolution is untested (same caveat as
  cycle 274).
- **Not verified:** whether some terminal emulator still redraws badly because
  of the *volume* of file I/O (no evidence of it; the writes no longer touch
  the tty).
