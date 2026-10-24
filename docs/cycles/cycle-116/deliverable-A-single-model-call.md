# Cycle 116 — print mode issued the model turn twice

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** a bug rather than a missing flag. Every `-p` run sent the prompt to
the model **twice** — once through the recovered `cascade.RealLifecycle` shadow
walk (whose answer was discarded into `io.Discard`) and once through
`agent.Loop.Send` (whose answer was printed). The reference issues one call.

---

## 1. How it was found

Cycles 114–115 added `usage` reporting and a `duration_seconds` measurement to
the print envelope, and the numbers looked wrong: a one-sentence reply reported
`duration_seconds: 4`–`6`, and `usage` came back with `input_tokens: 3370` for a
two-word prompt. That is roughly twice a single turn's cost.

The cause is visible with a counting endpoint. Pointing `OPENAGY_AGW_URL` at a
server that logs each POST:

```
$ OPENAGY_AGW_URL=http://127.0.0.1:8799/v1/chat/completions agy -p='hi'
req 1 /v1/chat/completions
req 2 /v1/chat/completions
```

`runPrintMode` built two backends. The visible one is `pickBackendWithLog(...)`,
handed to `agent.New`. The invisible one is the cycle-41 shadow walk:

```go
beLifecycle, lcErr := backend.NewLifecycle(backend.LifecycleConfig{
	Client: be,                            // ← the same live backend
	Sink:   backend.NewTextSink(io.Discard),
	…
})
cascader := cascade.NewRealLifecycle(beLifecycle)
_ = cascader.CascaderRun(ctx, modelName, prompt)   // Init + Run → a real call
```

`cascader.CascaderRun` calls `r.be.Init(ctx, model)` and `r.be.Run(ctx, prompt)`
(`internal/cascade/real_lifecycle.go:369,382`). Because `Sink` is
`io.Discard`, the whole walk was invisible — including its cost.

## 2. Fix

Drive the shadow walk with `backend.NewMock()` instead of the live `be`:

```go
Client: backend.NewMock(),
```

The recovered FSM still walks `init -> ready -> execute -> stream -> save -> done`
and still emits its silenced `[lifecycle]` / `[cascader chain]` log lines, so the
cycle-41 code path and its observability are preserved — but the print run now
costs exactly one model call, like the reference.

Verified with the same counting endpoint:

```
$ OPENAGY_AGW_URL=http://127.0.0.1:8801/v1/chat/completions agy -p='hi'
req 1 /v1/chat/completions
```

## 3. Why it matters beyond cost

* **`duration_seconds` was inflated** by the discarded call. A real turn now
  reports `duration_seconds: 2` where it previously reported 4–6.
* **`usage` was doubled.** The envelope's `input_tokens` came from
  `loop.Send`'s turn, so the reported number was already the correct one, but the
  account was billed for both.
* **It would have broken `--print-timeout`.** The shadow walk runs *before*
  `agent.Loop.Send` and shares the run's context. With a deadline, the discarded
  call would consume the whole budget, so the real turn would start already past
  its deadline and produce no output — where the reference, making one call,
  would have produced partial output. Removing the duplicate is a prerequisite
  for implementing `--print-timeout` faithfully (next cycle).

## 4. Verification

* Counting endpoint: **1** POST per run, down from 2.
* A real turn through the gateway still works in all three formats:
  `pong` / `{"status":"SUCCESS","response":"pong","duration_seconds":2,…}` /
  the `command_result` stream.
* The recovered shadow FSM still runs — `internal/cascade` and
  `internal/backend` tests (which drive `RealLifecycle` directly) are untouched
  and pass.
* `go build`, `go vet`, `go test ./cmd/... ./internal/...` — clean.
* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** (both documented diffs).
* Two harness136 runs reported 85/87 with a `124/…` row on a *different* tag each
  time (`slash_title`, then `slash_logout`); the next two runs returned 86/87.
  The reference is quota-degraded and intermittently hangs past the harness's
  timeout — `124/…` rows are reference noise, not regressions.

## 5. Divergences and limitations

* **The shadow walk is now a second, mock-backed turn.** It still fabricates a
  turn the reference does not have; it just no longer costs anything. A stricter
  reading of parity would delete the block outright. It was kept because it is
  the cycle-41 recovered-FSM scaffolding and removing it would discard recovered
  work for no observable gain — but note that its output is *entirely silenced*
  (`printModeSilenceLogger` → `io.Discard`), so nothing user-visible depends on
  it.
* **The cycle-40 `Cascade` (`loop.Cascade()`) is a third, separate FSM.** It is
  driven by the real turn and also silenced. It was not touched.
* **No test covers `runPrintMode` end to end.** The duplicate was found by
  measuring a live run, not by the suite. There is still no regression test for
  "exactly one model call per print run"; adding one needs a counting backend
  injected through `pickBackendWithLog`, which the current signature does not
  allow.
* **`--print-timeout` remains unimplemented**, and the default headless timeout
  is still the clone's 60-second cap rather than the reference's unlimited. That
  is the next cycle; this one was done first because the duplicate call would
  have made the deadline behave incorrectly.
