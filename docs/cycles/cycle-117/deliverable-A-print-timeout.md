# Cycle 117 — `--print-timeout`, and the two hidden 60-second caps

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** `--print-timeout` was registered in the parser since cycle 101 but did
nothing, and the print path had **two** independent 60-second caps that the
reference does not have. The changelog says the default headless timeout changed
from 5 minutes to *unlimited*; the clone was capping at 60 seconds in two
places. This cycle implements the flag and removes both caps.

---

## 1. What the reference does 【一手】

All captured from 1.3.6 on this machine.

```
$ agy -p='say hi' --print-timeout=1ms                       # rc 0
(no stdout)
stderr: [agy] print timeout after 1ms with turn in progress; returning partial output
```

The warning is also a literal in the binary's string table, and the reference
prints the same `[agy]` tag when the binary is renamed — it is hardcoded.

The two structured formats:

```
$ agy -p='say hi' --print-timeout=1ms --output-format json  # rc 0
{"conversation_id":"991276c7-…","status":"SUCCESS","response":"","duration_seconds":0,"num_turns":0,"usage":{…}}

$ agy -p='say hi' --print-timeout=1ms --output-format stream-json   # rc 0
{"event":"init","conversation_id":"5d18f62d-…","init":{…}}
{"event":"result","result":{"conversation_id":"5d18f62d-…","status":"SUCCESS","response":"","duration_seconds":0,"num_turns":0,"usage":{…}}}
```

Three things that pins down:

1. **A deadline is not a failure.** `status` is `SUCCESS`, there is no `error`
   member, and the exit code is `0`. Only the warning on stderr marks it.
2. **`num_turns` is 0.** The turn had not begun when the deadline fired.
3. **No `step_update` at all** — not even `user_input`. Compare the 429 failure
   (cycle 114/115), which emits `user_input` then `error_message` and reports
   `num_turns: 1`. So `user_input` marks the turn actually starting, not the
   session starting.

Non-positive values mean unlimited, and a local command is unaffected:

```
$ agy -p='say hi' --print-timeout=0    -> runs to completion (no warning)
$ agy -p='say hi' --print-timeout=-5s  -> runs to completion (no warning)
$ agy -p=/help   --print-timeout=1ms   -> the command table, rc 0, no warning
```

And the flag's own error, which is *not* stdlib's:

```
$ agy -p='say hi' --print-timeout=abc  # rc 2
invalid value "abc" for flag -print-timeout: time: invalid duration "abc"
Usage of agy:
  …
```

## 2. The two caps

`--print-timeout` was dead code because the run was bounded twice, both at 60 s:

```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)   // ×2
…
c.httpDo = (&http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}).Do  // 60s default
```

The context caps were obvious. The second was found by measurement: with a
server that answers immediately and then stalls for 70 s, a plain
`-p='hi'` returned after exactly 60 s with

```
stderr: agw: sse read: context deadline exceeded (Client.Timeout or context cancellation while reading body)
```

— an `http.Client.Timeout`, not the run context. A long agent turn (the exact
case the changelog's "unlimited" change is about) therefore failed in the clone
and would not in the reference.

## 3. Implementation

* `cmd/antigravity/print_format.go`
  * `printTimeout(fs)` — parses `--print-timeout`; `0` for absent, `0s`, negative
    or unparseable, meaning "no deadline".
  * `printContext(d)` — a context bounded by the deadline, or unbounded.
  * `printTimeoutWarning(d)` — the recovered stderr line, with the tag derived
    from argv0 (the clone's convention for the program name; identical whenever
    the binary is invoked as `agy`).
* `cmd/antigravity/main.go` — both 60 s contexts become `printContext(timeout)`;
  the turn's `user_input` step and the envelope's `num_turns` are now gated on
  the turn having started *before* the deadline; a `DeadlineExceeded` is turned
  into the warning + `runErr = nil`; the print path builds the agw backend with
  `backend.AgwTimeoutUnlimited`.
* `internal/backend/agw_k0s.go` — `AgwTimeoutUnlimited = -1` disables the
  client's per-request timeout so the caller's context is the only bound. `0`
  still means the 60 s default, which the interactive path keeps.
* `internal/cli/jetski.go` — durations are registered through jetski's own
  `durationValue` instead of `flag.DurationVar`, because stdlib's
  `durationValue.Set` **replaces** `time.ParseDuration`'s error with the generic
  `parse error`. The reference reports the real error. This is a general parser
  fix; `--print-timeout` is the only duration flag today.

## 4. Verification

* **Byte-level, flag error**: `--print-timeout=abc` — stdout, stderr (including
  all 40 lines of the usage block) and rc 2 are byte-identical to the reference.
* **Byte-level, timeout warning**: `--print-timeout=1ms` in all three formats.
  * text: empty stdout, the warning on stderr, rc 0 — identical.
  * json: `num_turns: 0`, `status: "SUCCESS"`, no `error` — matches the capture.
  * stream-json: `init` then `result`, no `step_update` — normalised comparison
    against the reference capture is a line-for-line `MATCH`.
* **Byte-level, unaffected paths**: `-p=/hooks --print-timeout=1ms
  --output-format json` (stdout identical, no warning), `-p=/clear
  --print-timeout=1ms` (rc 2, both streams identical), `-p=/help
  --print-timeout=5s` (text identical).
* **The 60 s cap is gone.** A server that stalls for 70 s now completes:
  `rc=0 elapsed=71s`, full answer on stdout, empty stderr. With
  `--print-timeout=2s` against the same server: `rc=0 elapsed=2s`, partial answer
  on stdout, warning on stderr.
* **Mid-turn timeout** (partial answer arrives, then the deadline fires): text
  prints the partial answer; json carries it in `response` with `num_turns: 1`;
  stream-json emits `init`, `user_input`, `result` and no closing step. The
  warning and rc 0 hold in all three.
* **Unlimited values**: `--print-timeout=0` and `--print-timeout=-5s` both run
  the turn to completion with no warning, as the reference does.
* `go build ./...`, `go vet`, `go test ./cmd/... ./internal/...` — clean;
  `gofmt` clean. New tests: `TestPrintTimeout`, `TestPrintTimeoutWarning`,
  `TestPrintContext` (cmd/antigravity), `TestDurationFlagErrorIsVerbatim`,
  `TestDurationFlagParses` (internal/cli), `TestAgwTimeoutDefaults`
  (internal/backend).
* `harness137.sh` — **23/25**, both documented diffs, unchanged.
* `harness136.sh` — **79/87** this session, but the reference is now degraded:
  the harness runs each case under `timeout 6`, and the reference is timing out
  (`rc 124`) on a *rotating* set of tags. An A/B against the previous commit
  proves it is environmental:

  | binary | result | extra diffs |
  |---|---|---|
  | cycle 116 code (HEAD, rebuilt from a worktree) | **76/87** | 11, every one a `124/…` reference timeout |
  | cycle 117 code | **79/87** | 8, every one a `124/…` reference timeout |

  The same cycle-116 code scored 86/87 twice earlier in the session. The two
  permanent diffs are the documented `/plugin` pre-auth case and `update`
  (the clone does no network I/O). **Treat `124/…` rows as reference noise.**
* The reference binary's SHA-256 is unchanged after all of the above.

## 5. Divergences and limitations

* **The `[agy]` tag is argv0-derived.** The reference hardcodes it (verified by
  renaming the binary and re-running). The clone follows its own convention —
  `filepath.Base(os.Args[0])` — which is `agy` in every harness run but would
  differ for a renamed clone binary. Deliberate, and consistent with the
  program-name divergence already recorded for `Usage of %s:`.
* **`--input-format` is still unimplemented.** The changelog describes
  `--input-format stream-json` (read one NDJSON message per line from stdin and
  run a turn for each, requiring `--output-format stream-json`). The clone parses
  the flag and ignores it. Not part of this cycle.
* **A mid-turn timeout's envelope is unrecovered.** The only capture is the 1 ms
  case, where the turn never began (`num_turns: 0`, no steps). The clone's
  behaviour once a turn *has* begun and then times out — `num_turns: 1`, the
  partial text in `response`, a `user_input` step and no closing step — is a
  coherent extrapolation, not a capture.
* **The deadline starts when `runPrintMode` begins**, so it includes backend
  selection and the tool-catalogue build. The reference's clock start is
  unverified; at 1 ms the difference is invisible because both are already past
  the deadline.
* **Only a context deadline is treated as a timeout.** An interrupt (Ctrl+C) or
  any other error still fails the run. That matches the changelog's "interrupts
  still exit non-zero", but the interrupt path itself is not exercised here.
* **The interactive path keeps its 60 s agw client timeout.** The reference's
  interactive behaviour is not something this session could measure, and the
  changelog entry is explicitly about headless runs. The TUI's cap is therefore
  left in place — if the reference is also unlimited there, that is a remaining
  divergence.
* **The 30-minute background-task cap is not modelled.** The changelog mentions
  waiting for background tasks up to a 30-minute cap; the clone has no background
  tasks, so there is nothing to cap.
