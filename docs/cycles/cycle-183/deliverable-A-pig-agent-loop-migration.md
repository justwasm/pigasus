# Cycle 183 — deliverable A: `internal/agent.Loop` is PiG's loop

## Scope

Cycle 182 (`../openagy-pigloop` branch `exp/pig-agent-loop`, commit `d5273ae`)
answered *can* the clone's agent loop be replaced by
[`github.com/MichaelKinsy/PiG`](https://github.com/MichaelKinsy/PiG)'s `agent`
package, with a running spike and no change to either side.

This cycle does it. Three things land:

1. **The description gap is closed.** `tools.Tool` can now hand the
   model-facing description it has always carried in `referenceSpecs` to a
   caller that re-advertises the registry.
2. **`internal/agent.Loop.Send` is PiG's agent loop.** The public surface of
   `internal/agent` did not move: `Options`, `Send`, `TurnInput`, `ToolRunner`,
   `Handler`, `Cascade` and the `backend.StreamChunk` vocabulary are all
   unchanged, so `cmd/wagyu`, `cmd/openai-shim`, `cmd/anthropic-shim` and the
   Cascade observers are edited by **zero lines**.
3. **A native PiG provider backed by agw**, written from scratch against the
   OpenAI Chat-Completions streaming dialect (`internal/piagentbridge/agwprovider.go`).
   It is opt-in, and §"Why the default transport did not move" explains why.

Everything below is 【一手】 unless marked otherwise. Every claim about PiG's
API was read from the cloned tree at `../PiG`; every behavioural claim is
backed by a named test or by the demo's output.

## 1. The description gap

`internal/tools/catalog.go` has carried a `refSpec.desc` for all 42 advertised
tools since the catalog was recovered, but `Tool` (`Name`/`Schema`/`Run`) had
no way to expose it. The text was reachable only by reading the table.

Added:

```go
type Describer interface{ Description() string }
func Describe(t Tool) string   // "" when the tool carries none
```

and a `describedTool` wrapper applied in `Catalog()`:

```go
case s.back != nil && s.back.Name() == s.name:
    reg.Register(&describedTool{Tool: s.back, desc: s.desc})
```

`aliasTool` and `pendingTool` already had the field, so they only grew the
method. Wrapping rather than adding a field to each executor keeps
`referenceSpecs` the single source of truth for the text — the executors
(`ReadFile`, `RunCommand`, …) predate the requirement and carry no copy.

No type assertion anywhere outside `internal/tools` depends on the concrete
type returned by `Catalog()` (checked by grep across `cmd/` and `internal/`),
so the extra wrapper layer is invisible to every existing caller.

`piagentbridge.toolAdapter.Schema()` now uses `tools.Describe` instead of its
`"<name> (openagy tool)"` synthetic fallback, which is retained only for tools
that genuinely carry no description.

Pinned by `TestCatalogToolsCarryDescriptions` (every entry in `List()` is a
`Describer` with non-empty text), `TestDescribeWithoutDescription`, and
`TestTools_CarriesTheReferenceDescriptions` /
`TestTools_FallsBackWhenNoDescriptionIsCarried` on the bridge side.

## 2. The migration

### 2.1 The seam

`Loop.Send` keeps its body up to and including the
`cascadeTransition(l.cascade, StateRead, …)` line, then delegates:

```go
// internal/agent/loop.go
return l.sendPig(ctx, input)
```

`streamOnce` — the hand-rolled "open the stream, forward chunks, collect tool
calls, loop" body — is deleted. It is 65 lines shorter and PiG now owns the
turn cycle, the message history, the tool-call extraction and the event stream.

`internal/agent/pig.go` (new) is the whole translation layer.

### 2.2 Translation, out

| clone | PiG |
|---|---|
| `Options.Backend` (`backend.Client`) | `agent.StreamFn` via `piagentbridge.BackendStreamFn` |
| `backend.ToolDecl` + `Options.ToolRunner` | one `runnerTool` per declaration, `Execute` → `ToolRunner.Run` |
| `backend.Message{Role:"user"}` | `AgentMessage{User: &UserMessage{Content: ai.UserText(…)}}` |
| `backend.Message{Role:"assistant"}` | `AgentMessage{Assistant: &AssistantMessage{Content: [TextContent, ToolCall…]}}` |
| `backend.Message{Role:"tool"}` | `AgentMessage{ToolResult: &ToolResultMessage{ToolCallID, ToolName, Content}}` |
| `backend.Message{Role:"system"}` | `AgentMessage{System: &ai.SystemMessage{Content: ai.SystemText(…)}}` |
| `Options.SystemPrompt` | `AgentOptions.SystemPrompt` (rides on the initial system message) |
| `Options.MaxToolIterations` | `AgentOptions.MaxTurns` |
| `TurnInput.MaxTokens` / `.Temperature` | `ai.StreamOptions` (injected in the StreamFn closure — PiG has no per-turn view) |

### 2.3 Translation, in

PiG's `AgentEvent` stream is drained by a dedicated goroutine (PiG's `emit`
**blocks** until the event is received) and folded into the chunk vocabulary:

| PiG | `backend.StreamChunk` |
|---|---|
| `MessageUpdateEvent{TextDeltaEvent}` | `{Text: delta}` |
| `MessageUpdateEvent{ToolCallEndEvent}` | `{ToolCalls: [call]}` |
| `MessageEndEvent` (assistant, normal stop) | `{Done: true, Meta: …}` |
| `MessageEndEvent` (assistant, `StopReasonError`/`Aborted`) | `{Error: …, Done: true}` |

One PiG assistant message == one upstream request == one `Done` chunk, which is
exactly the cadence the old `streamOnce` produced, so the TUI's
`if chunk.Done && !done` guard and both shims see the same sequence they
always did.

### 2.4 The four behaviours that had to survive

`internal/agent/loop_tools_test.go` (pre-existing, unmodified) pins them:

| Test | Behaviour | How PiG is made to match |
|---|---|---|
| `TestLoop_ExecutesToolCallsThenStreamsFinal` | tool runs, result is echoed back on a second request, both requests advertise the tools | `runnerTool` → `ToolRunner`; PiG's own transcript carries the tool result |
| `TestLoop_NoToolCallsIsSingleTurn` | one upstream call, no tool run | natural |
| `TestLoop_ToolCallsWithoutRunnerAreSurfacedNotExecuted` | with a nil runner: calls reach the handler, never executed, exactly one stream | `MaxTurns = 1`, no tools advertised, PiG's `ErrMaxTurnsReached` read as a finished turn |
| `TestLoop_ToolIterationBudgetStopsRunaway` | budget 3 → exactly 3 tool runs then an error containing "budget" | `MaxTurns = 3`; `ErrMaxTurnsReached` → `agent: exceeded tool-iteration budget (3)` |

The budget mapping is exact rather than approximate: PiG checks
`turnIndex >= MaxTurns` at the top of each iteration
(`agent/agent_loop.go:159`), so `MaxTurns = N` runs the tool batch N times and
then reports exhaustion — the same arithmetic the clone's `for iter := 0; iter
< maxIter; iter++` produced.

Two smaller fidelity notes:

- **An empty conversation gets one empty user message.** PiG's `SendMessages`
  is a documented no-op on an empty slice (`agent/agent.go:1126`) because
  upstream's `prompt(messages)` always has something to start from; the clone's
  loop would issue a request with no messages at all. `pigMessages` synthesizes
  the one message, so `TurnInput{}` still produces exactly one upstream call.
  Every real caller hands over a populated conversation.
- **A canceled context outranks the provider's own error.** The pre-183 loop
  returned `ctx.Err()` and surfaced `{Error: ctx.Err(), Done: true}`; `sendPig`
  keeps that by checking `ctx.Err()` before the sink's recorded failure.

### 2.5 The Cascade state machine

Unchanged and still driven from `internal/agent`: `StateRead` in `Send`, then
`StatePlan` before the first request, `StateExecute` when a stream is opened,
`StateStream` per text chunk, and `StateSave` → `StateDone` / `StateError` at
the end. `cmd/wagyu/main.go`'s `OnStateTransition` observer sees the same edges
in the same order.

## 3. The native agw provider

`internal/piagentbridge/agwprovider.go` (new) implements `ai.Provider`
directly rather than wrapping `backend.Client`:

```go
type ProviderConfig struct{ URL, APIKey, Model string; Timeout time.Duration; HTTPClient *http.Client; Headers map[string]string }
func NewProvider(ProviderConfig) *Provider
func (p *Provider) ID() string                       // "agw"
func (p *Provider) Stream(ctx, transcript, opts) (*ai.AssistantMessageEventStream, error)
func (p *Provider) Model(id string) *ai.Model        // binds Provider to one wire model
```

`ai.Provider.Stream` does **not** receive the model
(`agent/agent_loop.go:412` passes only the transcript), so a provider that can
serve several models must be pinned per model — exactly as PiG's own providers
are. `Provider.Model(id)` returns a `*ai.Model` whose `Provider` field is a
model-scoped view sharing one `*http.Client`.

It speaks the wire itself: OpenAI Chat-Completions, `stream: true`,
`stream_options.include_usage`, tools in the `tools[].function` shape; and it
parses `choices[0].delta` for `content` and for fragmented `tool_calls` keyed
by `index` (arguments concatenated). Unlike the clone's parser — which
assembles the calls and flushes them as one chunk at the end — the provider
emits PiG's full event union, including `ToolCallStart`/`ToolCallDelta`/
`ToolCallEnd` as the fragments arrive.

It always returns a usable stream: every failure (bad request body, non-2xx,
read error, cancellation) is encoded as an `ErrorEvent`, which is the contract
PiG's loop documents for a stream function
(`agent/agent_loop.go:415`).

`OPENAGY_AGW_URL` / `OPENAGY_AGW_KEY` are honoured, mirroring
`backend.AgwK0sConfig.withDefaults`.

It drives the loop end to end: `TestNewAgent_NativeProviderDrivesTheToolRoundTrip`
(bridge) and `TestLoop_NativeProviderDrivesTheToolRoundTrip` +
`TestLoop_NativeProviderSurfacesUpstreamFailure` (through the clone's public
`Loop` API, with `Options.Provider` set and no `Backend` at all).

## 4. The transport switch (part 2)

This section replaces an earlier draft that argued the transport should *not*
move. The draft was written from reading `Provider.stream` alone, and it was
wrong about the cause: the thing it blamed was not the provider's fault.

### 4.1 The correction

The draft claimed the provider loses `*backend.HTTPError` because it encodes a
non-2xx response as an in-stream `ErrorEvent`. That part is true. What the draft
missed is that **`BackendStreamFn` loses it the same way**, so the loss was not
a property of the provider at all:

```go
// PiG agent/agent_loop.go:494 requestStream
stream, err := streamFn(r.ctx, r.model, transcript, streamOpts)
if err != nil {
    errorAssistant := &AssistantMessage{ StopReason: error, ErrorMessage: err.Error(), ... }
    r.a.emit(MessageStartEvent{...}); r.a.emit(MessageEndEvent{...})
    return errorAssistant, nil, nil   // ← the concrete error is gone; nil is returned
}
```

PiG copies only `err.Error()` into the assistant message and returns `nil`.
`internal/agent.chunkSink.endAssistant` then does `errors.New(text)`, so
`cmd/wagyu/api_error.go`'s `errors.As(err, &he)` stopped matching **as soon as
cycle 183 landed**, whichever transport was in use. The migration commit
regressed the headless error path and the report did not say so.

Two things follow. First, this is a migration defect, not a reason to keep the
old transport — so it is fixed here rather than deferred. Second, the fix has to
recover the type *across PiG's string-only boundary*, which no amount of provider
politeness can do from inside a stream.

### 4.2 The three items

| Item | Where it lives now | How |
|---|---|---|
| `*backend.HTTPError` on non-2xx | `internal/agent/pig.go` | Both transports report a **pre-stream** failure as `(nil, err)` instead of an in-stream event. That is what `backend.Client.Stream` already did, and it is fully supported by PiG (`requestStream` synthesizes the error message). A per-`Send` `errorSlot` records the concrete value from the stream function, and `sendPig` returns it in preference to the sink's stringified copy. |
| `ExternalModel` remap | `internal/piagentbridge/agwprovider.go` | `ProviderConfig.Model` is the wire model and is always what goes out; `Provider.Model(label)`'s argument is only the label PiG records. This mirrors `AgwK0sConfig.ExternalModel`, which also ignores the requested model. `WireModel()` exposes it. |
| `SystemFingerprint` | `internal/agent/pig.go` + `internal/piagentbridge/stream.go` | `ai.Usage` has no field for it, so it rides *beside* the chunks: `BackendStreamFnWithObserver` gained an observer hook that sees each raw `backend.ChunkMeta`. The native path supplies `backend.AgwSystemFingerprint(provider.WireModel())` instead. |

A fourth difference turned up while writing the tests and is fixed too: the
clone identifies itself with `User-Agent: openagy-recovered/1 (agw-k0s; cycle
40)`. That string is now `backend.AgwUserAgent`, used by both routes, so the
upstream cannot tell which one is talking.

`metaFromPig` now takes a `transportMeta{present, fingerprint}` rather than a
bare string, because PiG hands the loop a non-nil *zeroed* `ai.Usage` when the
stream carried none — so "no metadata" and "metadata with zero tokens" are
indistinguishable from `Usage` alone. The observer only fires for a chunk that
really had a `ChunkMeta`, and the native path reports `present` unconditionally
because agw always sends usage (`stream_options.include_usage`). Verified by
`TestLoop_NoUsageMeansNoMetadata` (StreamFn path: no meta, exactly like the
clone) and `TestLoop_NativeProviderStampsTheAgwFingerprint`.

### 4.3 How the switch happens without touching `cmd/wagyu`

`cmd/wagyu` builds its backend in one place (`pickBackendWithLog`) but hands it
to the loop from **two** — `main.go:721` for `--print` and `tui_run.go:1955` for
the TUI — and `tui_run.go` is another workstream's active file. Threading an
explicit `Provider` through both would mean editing it.

So the loop derives one instead:

```go
// internal/agent/pig.go
if provider == nil {
    provider = nativeProvider(l.opts.Backend)   // needs AgwConfig()
}
```

`*backend.AgwK0sClient` gained `AgwConfig()`, returning its resolved config
(defaults applied). Any client that can describe itself gets served by the
provider built from that description; `backend.NewMock()` and the lifecycle
wrappers do not, and keep the `BackendStreamFn` path. `Options.Provider` remains
the explicit override.

`TestLoop_AgwBackendGoesThroughTheDerivedProvider` proves the switch with a
backend whose `Stream` method is poisoned: reaching the StreamFn path fails the
test. It also pins the remap, asking for `claude-sonnet-4-6` and asserting
`MiniMax-M3` on the wire.

### 4.4 What is still different

- The provider sends `accept: text/event-stream`; the clone sends only
  `content-type`, `authorization` and `User-Agent`. An added header, not a
  changed one, and the endpoint answers with SSE either way.
- The provider reads the whole error body (`io.ReadAll`) exactly as the clone
  does, but `HTTPError.Message()` truncates at `MaxErrorBody` while
  `Provider.stream`'s own pre-stream errors are plain `fmt.Errorf`s.
- Nothing above has been exercised against real `agw.k0s.io` — see §7.

## 5. Verification

All run in this repository.

Worktree `/Users/gear/Documents/GitHub/openagy-pigloop` (branch
`exp/pig-agent-loop`, PiG resolved through its own `go.work` pointing at the
sibling clone `../PiG`):

```
go build ./cmd/... ./internal/...      exit 0
go vet   ./cmd/... ./internal/...      exit 0
go test  ./cmd/... ./internal/...      exit 0 — 27 packages ok, 0 failures
gofmt -l <every file touched>          empty
```

Ported to `main` in the main checkout, with PiG resolved from the module proxy
(`require github.com/MichaelKinsy/PiG v0.4.1`) instead of the sibling clone:

```
go build ./cmd/... ./internal/...      exit 0
go vet   ./cmd/... ./internal/...      exit 0
go test  ./cmd/... ./internal/...      exit 0 — 27 packages ok, 0 failures
go run ./cmd/pigloop-demo -mode loop   full two-turn tool round trip
```

The published tag and the clone agree on every symbol the adapter uses — the
same 27 packages pass against both. That matters, because it means the
migration does not depend on a private checkout.

New tests this cycle: 26 (11 on the provider, plus the transport-switch set in
part 2 — 4 on `internal/agent` behaviour, 1 on the provider's wire model, 1 on
the provider's identity header; 3 on the migrated loop, 2 on the bridge
description path, 2 on the catalog description path, plus the interface
assertions). `internal/agent` gained `pig_test.go`; `internal/piagentbridge`
gained `agwprovider_test.go`.

Part 2 was also run under the race detector, because the fingerprint and the
error slot are cross-goroutine state:

```
go test -race ./internal/agent/ ./internal/piagentbridge/   exit 0
```

The demo exercises both shapes:

```
$ go run ./cmd/pigloop-demo -mode loop
mode=loop provider=scripted model=claude-sonnet-4-6 tools=1
  chunk  TOOL   read_file {"path":"go.mod"}
  chunk  DONE   prompt=24 completion=11 total=35 reasoning=0
  chunk  TEXT   "This repository is"
  chunk  TEXT   "the Go module git"
  chunk  TEXT   "hub.com/chadgpt/op"
  chunk  TEXT   "enagy."
  chunk  DONE   prompt=96 completion=15 total=111 reasoning=3
cascade done
```

That is the clone's `Loop` API — `Options`, `Handler`, `ToolRunner`,
`TurnInput` — with PiG walking the model → tool → model cycle underneath it and
the clone's real `tools.ReadFile` executing the call. `-provider agw` switches
the same run onto the native provider.

## 6. Cost

Measured on this machine, `go build ./cmd/wagyu`:

| build | bytes |
|---|---|
| `b36b3cb` (pre-migration) | 24,940,034 |
| part 1 (`43e8ab4`) | 37,600,066 |
| part 2 (this commit) | 37,618,386 |

**+12,678,352 B (+12.1 MB, +50.8 %)** for `PiG/agent` + `PiG/ai` linked in.
That is the real price of the migration and it is paid by every user of
`cmd/wagyu`, not only by whoever asks for PiG's extra features. Cycle 182
measured +17.1 MB for the bridge alone against a backend+tools-only baseline;
the smaller figure here is the same dependency measured against the full CLI.

This is the cost of the migration, and it is paid by every user of
`cmd/wagyu`, not only by whoever asks for PiG's extra features. Cycle 182
measured +17.1 MB for the bridge alone against a backend+tools-only baseline;
the smaller figure here is the same dependency measured against the full CLI.

Part 2 did not change the binary's dependencies: the provider was already linked
in by part 1's `internal/piagentbridge`. The decision it resolved was whether to
*use* it, and §4 now records that it is used.

## 6.1 What the dependency actually costs the module graph

Cycle 182 counted "34 direct + 217 indirect" module requirements and treated
that as the price, because that is what PiG's own `go.mod` lists. That number
is wrong for a consumer: Go 1.26's module graph pruning lists only what is
reachable from the packages actually imported. Importing `agent` + `ai` adds

- **1 direct** requirement (`github.com/MichaelKinsy/PiG v0.4.1`), and
- **24 indirect** requirements,

not 217. The heaviest of them is `aws-sdk-go-v2` (13 modules), pulled in by
`PiG/extensions/sdk`, which `agent` reaches for its extension host.

So the decision is a 24-module, 12 MB decision — real, but far smaller than
cycle 182's estimate suggested.

## 7. 硬约束 / 存疑 / limitations

**Not verified 【推测】:**

- **No end-to-end run against a real model provider.** Every test and demo run
  uses `scriptedClient`, `backend.NewMock`, or an `httptest` gateway. The
  native provider has never spoken to `agw.k0s.io` (no credential on this
  machine) and the migrated loop has never spoken to the real Antigravity
  backend. Real-world latency, streaming back-pressure and long-context
  behaviour are unmeasured. **This now includes the default transport**: since
  part 2, `backend.NewAgwK0sWithConfig` is routed through the provider, and the
  only thing standing behind that is `httptest` gateways.
- **No TUI verification.** `cmd/wagyu` is edited by zero lines, so nothing in
  `cmd/wagyu` was exercised against the new loop except the package tests,
  which drive `agent.New`/`Send` directly with scripted backends.
- **No semantic comparison** of PiG's retry, compaction, queueing,
  steering-message and subagent machinery against the clone's. PiG brings all
  of it; this cycle neither enables nor disables any of it, and none of it is
  covered by a test here. `MaxTurns`, `ToolExecutionMode` and `StreamFn` are
  the only options the adapter sets.
- **`ai.ImageContent` is still dropped** on the way into `StreamRequest`
  (cycle-182 finding, unchanged).
- **`Thinking*` events are never emitted.** The clone's recovered
  "Thought for Ns, N tokens" step is fed by `agent.Sanitizer` scraping
  ` thinking` out of the text stream, as before; PiG's thinking-level plumbing
  is untouched.

**Known behavioural deltas:**

- Part 1 regressed `cmd/wagyu`'s headless error classification and part 2 fixes
  it (§4.1). The fix is a `(nil, err)` return from both transports plus a
  per-`Send` `errorSlot`; it is covered by
  `TestLoop_AgwBackendFailureKeepsTheHTTPError` and
  `TestProvider_HTTPErrorIsReturnedConcrete`. It is worth stating plainly that
  the regression existed in the committed part-1 commit and was found by reading
  PiG's `requestStream`, not by a test — part 1 had no test asserting the
  concrete error type survived.
- The transport switch is **implicit**: the loop derives a provider from any
  backend that implements `AgwConfig()`. Reverting means removing the
  `nativeProvider` call in `sendPig`, not passing a flag. There is no build tag
  or environment variable that selects between the derived provider and
  `BackendStreamFn`; `OPENAGY_BACKEND=shim` (the mock) is the closest thing,
  and it changes the backend rather than the transport.
- The observer that carries `SystemFingerprint` runs on the transport's pump
  goroutine while `sendPig` reads the value after the run settles. It is
  mutex-guarded and race-clean, but it relies on the metadata chunk arriving
  before the stream ends — true for `backend.AgwK0sClient` (usage frame, then
  `[DONE]`) and for `backend.NewMock`, and no test constructs a transport that
  violates it.
- `Options.Provider` cannot be combined with `Options.Backend`; the provider
  wins if both are set, and `TestNewAgent_PrefersProviderOverClient` pins that
  for the bridge.
- Without a `ToolRunner`, `MaxTurns` is forced to 1 and PiG's
  `ErrMaxTurnsReached` is swallowed into a `nil` return. That reproduces the
  clone's "surface the calls, run nothing" contract, but it means the
  turn-limit error cannot be used to signal budget exhaustion on that path —
  which is fine, because that path has no budget.
- A handler error no longer aborts the in-flight stream. The pre-183 loop
  returned it immediately and stopped reading; PiG's run is already in flight,
  so the error is recorded and returned once the run settles. The returned
  error is the same; the amount of upstream work done is not.

**Build-graph cost:** PiG is now a first-class dependency of this module —
`require github.com/MichaelKinsy/PiG v0.4.1` in `go.mod` (see §6.1 for what
that actually pulls in). It resolves from the public module proxy, so no
sibling checkout is needed: `go build ./cmd/wagyu` works in a fresh clone with
network access to `proxy.golang.org`.

Two things this cycle did **not** do and that should be decided deliberately:

- No `go.sum` audit of the 24 new indirect modules. They are whatever
  `go mod tidy` resolved at `v0.4.1`; nobody has reviewed them here.
- No vendoring. `go mod vendor` would pin the whole graph into the tree, which
  is a policy call for this repository (it currently vendors nothing).

**Not reversible in one step:** the loop swap is not behind a flag. Reverting
`internal/agent/loop.go` + `internal/agent/pig.go` restores the hand-rolled
loop, but it also means dropping the `require` and re-running `go mod tidy`;
there is no build tag or option that selects between the two implementations.
