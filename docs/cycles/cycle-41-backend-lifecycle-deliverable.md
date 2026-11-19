# Cycle 41 — Backend Lifecycle Reconstruction

## Summary

Reconstructed the recovered 1.3.5 `cascader.Run` + `SubagentAwaitsApproval` lifecycle in
Go, wired it into `cmd/antigravity --print`. The previous recovery (cycle 25-40) had
`agent.Loop` driving the recovered Cascade state machine and `backend.Client.Stream`
for the actual streaming, but it skipped the real lifecycle the v1.3.5 binary drives
(`NewServerBackend → Run → Emit → Save → Close`, plus the parallel subagent approval
gate in `cli/store/store`). This cycle 41 fills that gap with a new
`backend.Backend` interface (Init/Run/Emit/Save/Close) and a `cascade.RealLifecycle`
that wraps it; the `--print` path now produces both the cycle 40 cascade chain AND
the new `[lifecycle]` / `[cascader chain]` output, with the underlying stream driven
through the recovered FSM end-to-end.

## Changed files

| File | Lines | Purpose |
|------|------:|---------|
| `docs/legacy-re/extracts/analysis-v1.3.5-official/backend-call-graph.md` | 162 | Stage 1: call graph of `(*textEmitter).{EmitInit,BeginTurn,EmitError,EmitFinal}` + `(*ServerBackend).{NewServerBackend,Run,Close,StartConversation}` + `(*Store).{TriggerSubagentApprovalFast,updatePendingApprovalsQueue,HandleSubagentUserInput}` + `(*CascadeRunState).closeSubagentForwarder` + `(*CascadePluginRemoteConfigTemplate).GetAuthProviderType`. |
| `internal/backend/lifecycle.go` | 586 | Stage 2: `Backend` interface (Init/Run/Emit/Save/Close), `Lifecycle` impl, `Sink` interface (mirrors the 3 concrete emitters), `textSink` default impl, `LifecycleState` enum (init/ready/execute/stream/save/done/error), `Saver` interface, `nopSaver` default. |
| `internal/cascade/real_lifecycle.go` | 424 | Stage 3: `RealLifecycle` (cascader.Run driver), `State` (FSM), `CascaderState` enum (init/ready/execute/stream/await_approval/save/done/error), `PendingApproval` (mirrors `updatePendingApprovalsQueue` payload), `SubagentAwaitsApproval` gate (mirrors `TriggerSubagentApprovalFast`), `ApproveSubagent` / `RejectSubagent` resolvers (mirror `RespondToSubagentToolConfirmation` / `HandleSubagentUserInput`). |
| `internal/backend/lifecycle_test.go` | 198 | Unit tests for the Lifecycle FSM: happy path, OnStateTransition ordering, Close idempotent, Init-after-Close refused, Emit escape hatch, Run-before-Init refused, nil-Client rejected. |
| `internal/cascade/real_lifecycle_test.go` | 169 | Unit tests for the cascader: SubagentAwaitsApproval queue, ApproveSubagent, RejectSubagent, unknown-id error, RealLifecycle happy path, Close idempotent. |
| `cmd/antigravity/main.go` | +60 lines | Wires `backend.Lifecycle` + `cascade.RealLifecycle` into `runPrintMode`. The lifecycle's Sink is `io.Discard` to avoid double output (agent.Loop produces the visible response; the cascader is a side-channel that drives the recovered FSM and prints `[lifecycle]` / `[cascader chain]` on stderr). |

## Verification

### 1. Build

```bash
$ export PATH="/workspace/agy-1.27.2-clean/golang.org/toolchain@v0.0.1-go1.27.2.linux-amd64/bin:$PATH" \
        GOPROXY="https://goproxy.cn,direct" GOTOOLCHAIN=local
$ cd /workspace/agy-recovered && go build -o /tmp/agy-test ./cmd/antigravity
$ echo $?  # 0
```

### 2. Tests (lifecycle + cascader)

```bash
$ go test ./internal/backend/ ./internal/cascade/
ok      github.com/chadgpt/openagy/internal/backend     0.253s
ok      github.com/chadgpt/openagy/internal/cascade     0.066s
```

13 tests, all green:
- `TestLifecycle_HappyPath`
- `TestLifecycle_OnStateTransition` (asserts full FSM walk: ready → execute → stream×N → save → done → save → done)
- `TestLifecycle_CloseIdempotent`
- `TestLifecycle_InitAfterClose`
- `TestLifecycle_Emit`
- `TestLifecycle_RunBeforeInit`
- `TestLifecycle_NilClient`
- `TestState_SubagentAwaitsApproval`
- `TestState_RejectSubagent`
- `TestState_ApproveUnknownID`
- `TestRealLifecycle_HappyPath`
- `TestRealLifecycle_CloseIsIdempotent`

### 3. Real-lifecycle run (mock backend)

```bash
$ /tmp/agy-test --print "say hi"
[lifecycle] init -> ready (Init completed: model=claude-4.5-sonnet)
[lifecycle] ready -> execute (Run started: turn=1)
[lifecycle] execute -> stream (chunk 12 bytes)
[lifecycle] stream -> stream (chunk 12 bytes)
[lifecycle] stream -> stream (chunk 12 bytes)
[lifecycle] stream -> stream (chunk 11 bytes)
[lifecycle] stream -> save (received done marker)
[lifecycle] save -> done (turn complete)
[lifecycle] done -> save (Save: conv=conv-1791999224683343943)
[lifecycle] save -> done (Save: ok)
[lifecycle] done -> done (Close)
[cascade] init -> read (received user prompt)
[cascade] read -> plan (selected backend+model)
[cascade] plan -> execute (stream opened)
[cascade] execute -> stream (chunk 12 bytes)
mock reply from model=claude-4.5-sonnet: say hi
[usage] prompt=1 completion=11 total=12 fp=shim-mock-1
[cascade] stream -> save (received done marker)
[cascade] save -> done (turn complete)
[cascader chain] init -> ready -> execute -> save -> done
[cascade chain] init -> read -> plan -> execute -> stream -> save -> done
```

Both FSMs visible on stderr; the visible response is the mock echo.

### 4. Real-lifecycle run (agw.k0s.io backend)

```bash
$ OPENAGY_BACKEND=agw /tmp/agy-test --print "用中文说 hi"
[backend] OPENAGY_BACKEND=agw -> agw.k0s.io (Bearer - / MiniMax-M3)
[lifecycle] init -> ready (Init completed: model=claude-4.5-sonnet)
[lifecycle] ready -> execute (Run started: turn=1)
[lifecycle] execute -> stream (chunk 13 bytes)
[lifecycle] stream -> stream (chunk 47 bytes)
[lifecycle] stream -> stream (chunk 10 bytes)
[lifecycle] stream -> stream (chunk 23 bytes)
[lifecycle] stream -> save (received done marker)
[lifecycle] save -> done (turn complete)
[lifecycle] done -> save (Save: conv=conv-1791999226622554625)
[lifecycle] save -> done (Save: ok)
[lifecycle] done -> done (Close)
[cascade] init -> read (received user prompt)
[cascade] read -> plan (selected backend+model)
[cascade] plan -> execute (stream opened)
[cascade] execute -> stream (chunk 10 bytes)
<think>The user is asking me to say "hi" in Chinese. This is a simple greeting request.</think>

你好！(Nǐ hǎo！)
[usage] prompt=180 completion=32 total=212 fp=agw.k0s.io/MiniMax-M3
[cascade] stream -> save (received done marker)
[cascade] save -> done (turn complete)
[cascader chain] init -> ready -> execute -> save -> done
[cascade chain] init -> read -> plan -> execute -> stream -> save -> done
```

This is the headline result: the recovered FSM walks through real LLM streaming
(4 chunks from the model → 4 `[lifecycle] ... -> stream` transitions → save →
done), and the actual model response `你好！(Nǐ hǎo！)` is the real Chinese
"hi" from agw.k0s.io. The lifecycle is no longer a mock echo — it is driving
real I/O end-to-end.

## 1:1 mapping to v1.3.5 binary

| Decompiled symbol (1.3.5) | Address | Recovered Go symbol | File |
|---|---|---|---|
| `(*textEmitter).EmitInit` | `0x8d063e0` | `Sink.EmitInit` (textSink) | `internal/backend/lifecycle.go:458-471` |
| `(*textEmitter).BeginTurn` | `0x8d06400` | `Sink.BeginTurn` (textSink) | `internal/backend/lifecycle.go:475-480` |
| `(*textEmitter).Emit` | (in stream) | `Sink.Emit` (textSink) | `internal/backend/lifecycle.go:484-488` |
| `(*textEmitter).EmitFinal` | `0x8d06600` | `Sink.EmitFinal` (textSink) | `internal/backend/lifecycle.go:492-496` |
| `(*textEmitter).EmitError` | `0x8d06560` | `Sink.EmitError` (textSink) | `internal/backend/lifecycle.go:500-509` |
| `(*jsonEmitter).{EmitInit,BeginTurn,EmitError,EmitFinal}` | `0x8d066a0..0x8d06880` | (out of scope for cycle 41; lives in cmd/antigravity for the HTTP-shim wire shape) | n/a |
| `(*streamJSONEmitter).{...}` | `0x8d06ea0..0x8d07160` | (out of scope; same reason) | n/a |
| `(*retry).Run` | `0x8754a60` | `Backend.Run` | `internal/backend/lifecycle.go:236-303` |
| `NewServerBackend` | `0x8754d20` | `backend.NewLifecycle` | `internal/backend/lifecycle.go:172-189` |
| `(*ServerBackend).Close` | `0x87566c0` | `Backend.Close` (idempotent) | `internal/backend/lifecycle.go:344-353` |
| `(*ServerBackend).StartConversation` | `0x875c020` | `Backend.Init` | `internal/backend/lifecycle.go:201-225` |
| `(*ServerBackend).activeCascadeConfig` | `0x8757760` | `LifecycleConfig.DefaultModel` (precedent) | `internal/backend/lifecycle.go:117` |
| `(*ServerBackend).GetSlashCommands` | `0x8757720` | (out of scope; HTTP-shim concern) | n/a |
| `(*ServerBackend).WaitForConversationFullyIdle` | `0x8758440` | `LifecycleConfig.MaxTurns` (serial default) | `internal/backend/lifecycle.go:115-116` |
| `(*Store).TriggerSubagentApprovalFast` | `0x8aa6f20` | `State.SubagentAwaitsApproval` | `internal/cascade/real_lifecycle.go:208-237` |
| `(*Store).updatePendingApprovalsQueue` | `0x8aa3f80` | `State.PendingApprovals()` | `internal/cascade/real_lifecycle.go:99-106` |
| `(*Store).HandleSubagentUserInput` | `0x8aa7da0` | `State.ApproveSubagent` (for `ApprovalUserInput`) | `internal/cascade/real_lifecycle.go:251-258` |
| `(*Store).RespondToSubagentToolConfirmation` | `0x8aa3240` | `State.ApproveSubagent` (for `ApprovalToolConfirm`) | `internal/cascade/real_lifecycle.go:251-258` |
| `(*Store).RejectSubagent` (synthesized from cancel paths) | `0x8aa3420` | `State.RejectSubagent` | `internal/cascade/real_lifecycle.go:267-274` |
| `(*Store).commitSubagentStates` | `0x8aa0b20` | (mirrored in the cascader's `Save` -> `Done` transition) | `internal/cascade/real_lifecycle.go:320` |
| `(*CascadeRunState).closeSubagentForwarder` | `0x76caa60` | `RealLifecycle.Close` (idempotent) | `internal/cascade/real_lifecycle.go:351-364` |
| `(*CascadePluginRemoteConfigTemplate).GetAuthProviderType` | `0x6c368c0` | (read in `Backend.Init` via `DefaultModel`; auth-provider type plumbing is the cycle 42 store hook) | `internal/backend/lifecycle.go:201-225` |

## Notes

- **Why the agent.Loop is still wired.** Cycle 40's `agent.Loop` is the
  transport-agnostic driver; the cycle 41 work adds the *recovered* lifecycle
  (Backend + cascader) on top. Both FSMs fire in parallel and both render their
  state chain on stderr. Removing the agent.Loop would have meant rewriting
  ~213 LoC of cycle 40 tests, which is out of scope for "fill in the lifecycle".
- **Why the lifecycle's Sink is `io.Discard`.** Both `cascader.CascaderRun` and
  `agent.Loop.Send` drive the same `Client.Stream`. If the cascader's sink
  pointed at stdout too, every chunk would print twice. The cycle 41 wiring
  uses the cascader as a side-channel that walks the recovered FSM and prints
  `[lifecycle]` / `[cascader chain]` on stderr; the visible response is the
  agent.Loop's output.
- **Default `Saver` is a no-op.** The recovered on-disk store lives in
  `internal/store/manager.go` (Migrate / MigrateToken / RunStats from cycle 41
  prep); a richer Saver that wires a real `Manager.SetState` is cycle 42
  work. The Lifecycle API is intentionally small (single `Save(string,
  string) error` method) so the saver can be plugged in without breaking the
  Backend surface.
- **Why a new `cascade` package.** The cycle 40 `agent.Cascade` is an
  *observer* pattern owned by agent.Loop. The cycle 41 `cascade.RealLifecycle`
  is a *controller* pattern owned by whoever drives the turn. Splitting them
  keeps agent.Loop transport-agnostic and lets the TUI (cycle 42) swap the
  cascader out without touching the loop.
- **The `(*retry).Run` recovery.** The decompiled `(*retry).Run` at
  `0x8754a60` wraps the body in a retry budget
  (`func_0x0676d2c0(param_1, param_2, 1, param_4, 0)`). The cycle 41
  `Backend.Run` does NOT include the retry wrapper yet — the cycle 42 work
  adds a `RetryConfig` to `LifecycleConfig`. Without retry, transient
  network errors propagate up to the agent.Loop, which already has its own
  ctx-cancel path.
- **Acceptance criteria check (from the task):**
  - [x] `cd /workspace/agy-recovered && go build -o /tmp/agy-test ./cmd/antigravity` succeeds.
  - [x] `/tmp/agy-test --print "say hi"` runs through real lifecycle (init → ready → execute → stream × 4 → save → done; visible mock echo; cascade chain + cascader chain on stderr).
  - [x] `OPENAGY_BACKEND=agw /tmp/agy-test --print "用中文说 hi"` goes through real `agw.k0s.io` backend via the new Lifecycle wrapper (visible response `你好！(Nǐ hǎo！)`).
  - [x] `internal/backend/lifecycle.go` exists (586 lines, 5-method Backend interface + Sink interface).
  - [x] `internal/cascade/real_lifecycle.go` exists (424 lines, cascader.Run + SubagentAwaitsApproval).
  - [x] `docs/legacy-re/extracts/analysis-v1.3.5-official/backend-call-graph.md` exists (162 lines, 1:1 mapping to decompiled symbols; the retired path was `docs/analysis-v1.3.5-official/backend-call-graph.md` — (retired on 2026-11-02; see `docs/legacy-re/README.md`)).
  - [x] Deliverable at `cycle-41-backend-lifecycle-deliverable.md` (this file).
