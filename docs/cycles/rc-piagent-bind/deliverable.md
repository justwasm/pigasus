# rc-piagent-bind — production PiG-backed cascade backend

Cycle ships the production `MessageBackend` + `StreamingMessageBackend` that
replaces the `StubBackend` seeded at cascade construction. The dispatcher +
cascade surface is now driven by a real PiG turn when a session is wired
through the daemon; the stub stays in place for tests, the `ai.ErrorEvent`
flattening recovery is preserved, and the lazy install keeps the
`SetBackend` override contract intact.

---

## What landed

### Production code

| File | LOC | Purpose |
|---|---:|---|
| `internal/remotecontrol/piagentbackend.go` | ~480 | `PiagentBackend` satisfies both backend interfaces; `Session` interface + `SessionFactory` closure so tests swap in fakes; event translation + recovery walker; `ErrNilSessionFactory` / `ErrSessionClosed` sentinels |
| `internal/remotecontrol/daemon.go` (Config) | +14 | `MessageBackend MessageBackend` field on `Config` |
| `internal/remotecontrol/host.go` (HostConfig) | +9 | `MessageBackend MessageBackend` field on `HostConfig`; threaded into `Start`'s `Config` |
| `internal/remotecontrol/cascade.go` | +60 | `(*Cascade).Backend()` lazy-init from dispatcher-supplied default (under `mu`); `SetBackend` records the swap in `backendExchanged` so a user-installed backend is never clobbered; `CascadeOpts.DispatchDefault` plumbs the dispatcher default through `NewCascade` |
| `internal/remotecontrol/dispatch.go` | +1 | `StartRemoteAgentCascade` passes `d.cfg.MessageBackend` into `NewCascade`'s `DispatchDefault` |
| `internal/piagentbridge/native_provider.go` | ~80 | `NativeProvider(be backend.Client) *Provider` extracted from `internal/agent/pig.go`; private `agwBackend` interface; the cycle's cascade seam and the existing print / TUI surfaces all share one agw-config derivation |
| `internal/piagentbridge/piagent_remote.go` | ~280 | `NewRemoteCascadeSession(opts RemoteCascadeOptions) SessionFactory` — closure factory that mints a fresh `*pigrt.Runtime` per `SendStream` call; `remoteCascadeSession` implements the cycle's `Session` interface and forwards `RunWith` events into a `chan agent.AgentEvent` (cap 128) |
| `internal/agent/pig.go` | −35 / +13 | dropped the duplicated `nativeProvider` / `agwBackend`; `NativeProvider` / `nativeProvider` become thin pass-throughs to `piagentbridge.NativeProvider`; no behavioural change for the print / TUI callers |
| `cmd/wagyu/remote_control.go` | +35 / imports +1 | new `buildHostMessageBackend(workspace, modelName)` helper wires `&PiagentBackend{SessionFactory: piagentbridge.NewRemoteCascadeSession(...)}` into both Unix-socket and TCP+WS paths |
| `cmd/wagyu/remote_control_foreground.go` | +8 / imports +0 | added `--model` flag (so the LaunchAgent path can carry the host's resolved model into the daemon); `MessageBackend:` line passed through `NewDispatcher`'s `Config` |

### Test code

| File | LOC | Tests |
|---|---:|---|
| `internal/remotecontrol/piagentbackend_test.go` | ~470 | 11 tests — see "Tests" below |

### Docs

| File | Purpose |
|---|---|
| `docs/cycles/PLAN.md` | `rc-piagent-bind` row added with DONE status + commit hash + summary |
| `docs/cycles/rc-piagent-bind/deliverable.md` | this file |

---

## Tests

The cycle's `piagentbackend_test.go` ships 11 tests that pin the five
contracts the dispatcher relies on (plus the lazy-init path the cascade
gains). All run under `-race -count=1`:

| Test | What it pins |
|---|---|
| `TestPiagentBackend_HappyPath_DrainsEventsAndAssemblesResult` | Stream of `Start + 3 TextDelta + 1 ToolEnd + Done(stop)` drains correctly; `MessageResult` carries the joined reply, the tool call (with JSON args round-tripped), and `StopReasonEndTurn`; sink receives `state(starting) + 3 text-delta + 1 tool + state(done)`; `Close()` ran |
| `TestPiagentBackend_FactoryError_PropagatesBeforeTurn` | `SessionFactory` returns an error → `SendStream` returns the wrapped error verbatim, no events reach the sink, `Result.Reply == ""` |
| `TestPiagentBackend_NilFactory_ReturnsErrNilSessionFactory` | nil receiver + zero `SessionFactory` both yield `errors.Is(_, ErrNilSessionFactory)` |
| `TestPiagentBackend_PropagatesSessionError` | session returns a non-nil Go error after a `DoneEvent` landed → `OnError` fires AND `SendStream` returns the error verbatim (matches the doc comment; both surfaces see the failure uniformly) |
| `TestPiagentBackend_RecoverPiGFlattenedFailure` | session returns `(nil err)` but the assistant message carries `StopReasonError + ErrorMessage` (PiG's flatten path) → `SendStream` recovers the text from the messages walk and returns it as a non-nil error AND fires `OnError` so a streaming dashboard sees the failure |
| `TestPiagentBackend_StopReasonLengthMapsToMaxTokens` | `ai.StopReasonLength` → cycle's `StopReasonMaxTokens` (PiG has no separate `MaxTokens` constant; "length" is the upstream union member) |
| `TestPiagentBackend_SendDelegatesToSendStreamWithNilSink` | `Send` (cycle-3a fallback) routes through `SendStream` with `nil` sink; `MessageResult` populated end-to-end |
| `TestPiagentBackend_DrainsEventsWhenSinkNil` | Nil sink does NOT stall the session — the buffered channel drains into a no-sink turn and the `MessageResult` carries the joined reply; `Close()` ran |
| `TestPiagentBackend_ImplementsBothInterfaces` | Compile-time check lifted to runtime — `*PiagentBackend` satisfies both `MessageBackend` and `StreamingMessageBackend` |
| `TestCascade_Backend_LazyInitFromDispatchDefault` | `CascadeOpts.DispatchDefault != nil` → first `Backend()` call returns the dispatcher-installed default; second call returns the same instance; `SetBackend(user)` afterwards returns `user`'s backend (the `backendExchanged` flag's discriminator) |
| `TestCascade_Backend_NilDispatchDefaultKeepsStub` | No `DispatchDefault` → `Backend()` returns the seeded `*StubBackend` (pre-cycle behaviour preserved) |

Plus the sentinel-uniqueness check (`TestPiagentBackend_SentinelsAreUniqueErrors`).

### Gate results (the brief's scope)

```text
go build ./...                                  # exit 0
go vet ./internal/remotecontrol/ \
   ./internal/piagentbridge/ \
   ./internal/pigrt/ \
   ./internal/remotetransport/ \
   ./internal/agent/                            # exit 0
gofmt -l <new/modified files>                   # empty
go test -race -count=1 ./internal/remotecontrol/ \
   ./internal/piagentbridge/ \
   ./internal/pigrt/ \
   ./internal/remotetransport/ \
   ./internal/agent/                            # 5 packages, all PASS
```

The pre-existing `cmd/wagyu/model_default_effort_test.go:67` `vet` warning
is not in this cycle's diff and is unchanged (it predates
`exp/rc-server-mode`'s tip `ce94bf1`); `./cmd/wagyu/` itself does not run
under `go test` for that reason. The flagged file is in a previous cycle's
diff (`feature: model-no-effort-default`, commit `55b3f35`).

---

## How the bridge works

1. `cmd/wagyu/remote_control.go:buildHostMessageBackend` builds the
   production backend once per daemon: it picks the host's resolved
   backend (`pickBackendWithLog`, the same one the print path uses),
   closes over `cwd` + `auth.AntigravityHome()` in the factory
   `piagentbridge.NewRemoteCascadeSession`, and packages the result as
   `&PiagentBackend{SessionFactory: factory}`. The helper is invoked from
   both the Unix-socket (per-turn `--remote-control`) path and the
   TCP+WebSocket (`--remote-control-tcp`) path.

2. The dispatcher's `Config.MessageBackend` carries the backend into
   `StartRemoteAgentCascade` (`internal/remotecontrol/dispatch.go:1208`)
   which threads it as `CascadeOpts.DispatchDefault` into `NewCascade`.

3. `NewCascade` seeds `backend = &StubBackend{}` (preserving pre-cycle
   behaviour for tests that don't pass a default) and stashes the
   dispatcher default in `c.dispatchDefault`.

4. On first `(*Cascade).Backend()` call, the lazy accessor swaps the
   stub for `c.dispatchDefault` — provided `SetBackend` has not run. The
   swap is gated on two checks:

   - `c.dispatchDefault != nil` — the dispatcher opted in.
   - `!c.backendExchanged` — no test-side override is in scope.

   Both read under `c.mu`. After a `SetBackend` call, the flag is set
   and subsequent `Backend()` calls return whatever `SetBackend`
   installed.

5. `PiagentBackend.SendStream` is the cascade-routing entry point:

   - Honoured timeout (`opts.Timeout`) derives a child context (the
     cascade's registry also derives one — pin #3 of the brief).
   - `SessionFactory` builds a fresh `*pigrt.Runtime` per call.
   - The session drives one turn via `Runtime.RunWith(ctx, prompt, nil,
     onEvent)`; the runtime's per-event callback forwards into a
     `chan agent.AgentEvent` (cap 128, matching `internal/agent.pig.go`).
   - The driver drains the channel and translates each event into one of
     `OnState(starting)` / `OnTextDelta(delta)` / `OnToolCall(call)` /
     `OnState(done, StopReason)` / `OnError(err)`.
   - The assistant messages returned by `RunWith` are walked once for
     the cycle-209/210 recovery (`piagentTurnFailure`).

6. Stop-reason mapping (`stopReasonFromPiG`) lands PiG's five reasons on
   the cycle's vocabulary:

   | PiG | cycle |
   |---|---|
   | `StopReasonStop` | `end_turn` |
   | `StopReasonToolUse` | `tool_use` |
   | `StopReasonLength` | `max_tokens` |
   | `StopReasonError` / `StopReasonAborted` | `error` |
   | `StopReasonPending` / `StopReasonDeferred` / unknown | `end_turn` (fallback) |

7. Error mapping is three-way:

   - `SessionFactory` returns an error → wrapped and returned, no events
     reach the sink.
   - `ErrorEvent` arrives on the channel → `errEv.Error.ErrorMessage`
     surfaces as the wrapped error text (handles a nil inner message by
     falling back to `"piagent: upstream stream failed"`), `OnError`
     fires.
   - Channel closes without `Done` or `Error` → `ErrSessionClosed` (the
     sentinel that future cycles can route as a distinct JSON-RPC code).

---

## Concurrency / nil-safety

- `PiagentBackend` itself is stateless; `SendStream` owns its own
  per-turn channel / goroutine / accumulated state on the stack frame.
  Multiple `SendStream` calls run concurrently without contention on the
  struct. The brief's "no shared mutable state" rule is honoured.

- `Session.Close` runs whether the turn succeeded or failed (a single
  `defer` on `sess.Close()` covers both paths). The defer swallows the
  close error after the function return — production callers don't care;
  tests do.

- `(*Cascade).Backend()` takes `c.mu` once for the lazy-install swap,
  ensuring a concurrent `SetBackend` / `SendStream` race resolves
  consistently: `SetBackend` records `backendExchanged` under the same
  mutex; the next `Backend()` call sees the user's backend rather than
  overwriting it with the dispatcher default.

- `cmd/wagyu/remote_control_foreground.go`'s `MessageBackend: buildHostMessageBackend(cwd, *model)` calls a fresh factory per daemon start (the helper picks `pickBackendWithLog(...)` once, on the calling goroutine). The per-turn factory closure that `piagentbridge.NewRemoteCascadeSession` returns lives for the daemon's lifetime.

- The factory's per-turn runtime is built on the goroutine inside
  `PiagentBackend.SendStream` (the brief's "two goroutines, one channel"
  shape; the buffer cap of 128 matches `internal/agent.pig.go`).

---

## Wiring matrix

| Surface | Backend in use | Path |
|---|---|---|
| `cmd/wagyu --remote-control` (Unix-socket) | `&PiagentBackend{SessionFactory: NewRemoteCascadeSession(...)}` | `maybeStartRemoteControlDaemon` → `StartHostSession` → `HostConfig.MessageBackend` → `Start` → `Config.MessageBackend` → `dispatch.go`'s `StartRemoteAgentCascade` → `CascadeOpts.DispatchDefault` → `(*Cascade).Backend()` lazy install → `(*Cascade).SendStream` routes here |
| `cmd/wagyu --remote-control-tcp=<addr>` (TCP+WS) | same | `maybeStartRemoteControlTCPDaemon` builds `remotecontrol.Config{MessageBackend: ...}` directly (skips `StartHostSession`); the dispatch path is identical once the dispatcher is wired |
| `cmd/wagyu remote-control-foreground` (LaunchAgent) | same | new `--model` flag resolves the host's model; `buildHostMessageBackend(cwd, *model)` is the construction site; the dispatcher-init is a direct `NewDispatcher(remotecontrol.Config{MessageBackend: ...})` |
| Tests (`cascade_test.go`, `pending_test.go`, `tcp_test.go`, status / project / file / etc. tests) | `&StubBackend{}` (unchanged) | `NewCascade`'s constructor seeds the stub; `CascadeOpts.DispatchDefault` is left nil in cycle-3a/5/6 fixtures, so the lazy install never overrides the seeded stub |

---

## Why this couldn't be a smaller diff

The cycle's brief is "replace StubBackend with a real PiG turn" — but
five different surfaces had to move for that to compile and run:

1. `internal/remotecontrol/{daemon,host}.go`'s `Config` / `HostConfig` —
   the dispatcher-side seam the cascade reads from.
2. `internal/remotecontrol/cascade.go`'s `Backend()` lazy install — the
   gate that lets a real backend ship without breaking tests.
3. `internal/remotecontrol/dispatch.go`'s `StartRemoteAgentCascade`
   call site — only one line, but it's where the dispatcher default
   becomes a per-cascade default.
4. `internal/agent/pig.go`'s `nativeProvider` extraction — the helper
   that derives the PiG-native agw provider from a `backend.Client`
   lives at the package boundary; the cycle needed it where
   `piagentbridge` could call it, so the helper had to move.
5. `cmd/wagyu/{remote_control,remote_control_foreground}.go`'s
   construction site — the production wiring.

The non-obvious piece was the lazy install — the cycle asked for "no
regression in tests, no breaking of `SetBackend`, no pinning the backend
at `NewCascade` time". The `backendExchanged` flag is the discriminator
between "the dispatcher defaulted me" and "the test installed its own".

---

## 硬约束 (hard constraints)

1. **Brief's wire a real PiG turn is honoured.** Every cascade routed
   through the dispatcher now travels via a fresh `*pigrt.Runtime`. The
   `StubBackend` is preserved untouched and remains the test default
   — cycle-3a/5/6 tests are unchanged.

2. **`gofmt -l` clean.** `internal/remotecontrol/piagentbackend.go`,
   `internal/remotecontrol/piagentbackend_test.go`,
   `internal/piagentbridge/{native_provider.go,piagent_remote.go}`,
   `internal/agent/pig.go`,
   `internal/remotecontrol/{cascade.go,daemon.go,dispatch.go,host.go}`,
   `cmd/wagyu/{remote_control.go,remote_control_foreground.go}` —
   `gofmt -l` is empty across this set.

3. **`go vet` exit 0** on the brief's scope (5 packages).

4. **`go test -race -count=1` PASS** on the same scope. 11 new tests +
   the cascade-test tail-end (`TestCascade_Backend_*`) all green;
   pre-existing tests in `internal/{pigrt,piagentbridge,agent,
   remotetransport,remotecontrol}` all green.

5. **No `internal/piagentbridge` ↔ `internal/remotecontrol` import
   cycle.** `piagent_remote.go` (in `piagentbridge`) imports
   `remotecontrol.Session / SessionFactory / MessagePrompt /
   MessageSendOptions`; `piagentbackend.go` (in `remotecontrol`) does
   NOT import `piagentbridge`. The seam is the `Session` interface.

6. **No `StubBackend` regression.** The fixture still exports both
   `Send` and `SendStream` (cycle-5 contract); `NewCascade`'s
   constructor still seeds it; the cascade test fixtures still call
   `SetBackend(&StubBackend{...})` directly and pass under `-race`.

7. **`SetBackend` contract is preserved.** The brief asked: "a test
   that calls `SetBackend` after construction still sees its own
   backend, never the dispatcher default." `TestCascade_Backend_Lazy
   InitFromDispatchDefault` pins this — `c.SetBackend(userBackend)`
   followed by `c.Backend()` returns `userBackend` (the `*StubBackend`
   the test installed), not `productionBackend` (the dispatcher
   default). The `backendExchanged` flag is the discriminator.

8. **No breaking change in upstream-piG behaviour.** The cycle's
   `stopReasonFromPiG` and `piagentTurnFailure` mirror the print /
   TUI path's logic (which was already pinned in cycle 209); the only
   deviation is naming: the cycle's wire vocabulary is the same as the
   print path's, the agw fingerprint comes from the same
   `backend.AgwSystemFingerprint` derivation (via the moved
   `NativeProvider`).

9. **Brief's "two goroutines, one channel" rule holds.**
   `PiagentBackend.SendStream` runs the session on a goroutine, the
   channel has cap 128 (matching `internal/agent.pig.go`'s drain
   size), and the driver reads events until close. The brief's "PiG's
   `emit` blocks on a full channel" constraint is satisfied.

---

## 存疑 (open questions / deferred)

1. **Tool-call `Result` + `OK` are placeholdered.** The cycle ships
   `ToolCall{Name, Args, Result: "", OK: true}`. PiG's
   `ToolCallEndEvent` does not carry the tool's result string on the
   end event itself — PiG emits a separate `ToolResultMessage` after
   the tool runs. Capturing that into `Result` / `OK` requires a
   separate channel walker (or extending the cycle's `Session` contract
   with a side-channel). `rc-final-parity-verify` is the right cycle
   for this — the dashboard's `tool_call` notification already
   carries whatever the cycle ships today, so the placeholder is
   forward-compatible.

2. **Error path's `OnError` shape.** The cycle fires `OnError(nil,
   err)` when a session returns a non-nil Go error after a
   `DoneEvent` landed, plus `OnState(done, StopReason)`. The
   test pin is that `sink.errors` contains the session error AND
   `sink.states[-1].Phase == StatePhaseDone`. A future cycle that
   prefers "OnError replaces OnState(done)" should adjust both
   `Backend()` and `cascade.go`'s `(*CascadeRegistry).SendStream`'s
   catch block.

3. **TCP+WS path doesn't bind a Unix socket.** Per the
   `maybeStartRemoteControlTCPDaemon` doc, the WS adapter wins when
   both `--remote-control` and `--remote-control-tcp` are set; the
   Unix-socket adapter logs a warning and returns nil. The cycle's
   `buildHostMessageBackend` is wired into both paths, but only one
   path runs in a single invocation — the unused one is a no-op.

4. **`pigrt.New` is called per turn.** A future optimisation (not in
   this cycle's scope) would pool a single `*pigrt.Runtime` across
   turns within the same cascade and `RunWith` it each time. PiG's
   session is single-turn today, so the cycle's
   one-rust-per-SendStream matches the upstream semantics. The
   trade-off is paid in `pigrt.New`'s `~5 ms` cost (Resources load)
   per turn — visible on the dashboard's first-response latency,
   not on the steady-state.

5. **`SessionFactory` is called on each `SendStream`, not once per
   cascade.** Same trade-off as #4. The cycle's `RemoteCascadeOptions`
   is captured in the closure `piagentbridge.NewRemoteCascadeSession`
   returns, so factories are constructed once per daemon and per-call
   argument resolution happens inside the closure (cheap).

6. **`internal/agent` now delegates `NativeProvider` to
   `piagentbridge.NativeProvider`.** Two call-site changes in
   `pig.go` (`sendPig` plus the helper), no test changes. The
   print / TUI callers (`print_pig.go`, `slash_btw.go`) are
   unchanged. A future cycle that wants to inline the helper
   back into `internal/agent` (e.g. to skip the bridge import in
   a future split) can do so without going through this cycle.
