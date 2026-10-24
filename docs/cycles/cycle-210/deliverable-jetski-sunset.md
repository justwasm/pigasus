# Cycle 210 — sunsetting the clone's loop from `--print`

## TL;DR

Cycle 209 put PiG's `coding.Runtime` behind a runtime switch; cycle 210 removes
the switch. A `-p` turn now runs on PiG's session and **only** on PiG's session.
Deleted from the print path:

* the `if printShouldUsePigRuntime(...) { pig } else { … }` fork and the whole
  `internal/agent.Loop` fallback it guarded, including the cascade observer and
  the `cascade.RealLifecycle` shadow walk that only ever logged into
  `io.Discard`;
* the `OPENAGY_RUNTIME=jetski` rollback switch.

Two things that used to keep `-p` on the clone's loop now work on PiG's:

* **The offline mock/shim.** `backend.NewMock()` has no agw endpoint, so it had
  nothing to hand a session. `piagentbridge.BackendProvider` (new) wraps *any*
  `backend.Client` as an `ai.Provider` by reusing `BackendStreamFn`, the
  translation the clone's loop already exercised.
* **Resumed conversations (`-c`).** `pigrt.Runtime.SeedHistory` appends the
  store's earlier turns to the session log before the first send; PiG
  re-projects that log before every provider request
  (`coding/session_transcript.go:prepareRequest`), so the model sees the same
  history the old loop replayed as `backend.Message`s.

Nothing under `internal/agent`, `internal/tools` or `internal/cascade` was
deleted: `internal/agent.Loop` is still the **interactive TUI**'s engine
(`cmd/wagyu/tui_run.go`), which this cycle does not touch. The sunset is
complete for the headless surface and pending for the TUI; see *Remaining*.

## What "the runtime" is now

```
cmd/wagyu runPrintMode
  └─ internal/pigrt ──► coding.Session
        model = agent.NativeProvider(be).Model(name)      // agw: native SSE provider
              | piagentbridge.NewBackendProvider(be, name) // mock/shim: BackendStreamFn
        ExtraTools     = tools.Catalog (the reference's 60 names)
        MCP            = PiG's mcp extension ← agy mcp.json
        skills         = the agy skill roots (PiG resource loader)
        BeforeToolCall = the recovered permission policy
        history        = rt.SeedHistory(pigHistory(sel))   // `-c` only
```

The output contract is still not forked: `print_pig.go` synthesises
`backend.StreamChunk`s into the same handler closure `runPrintMode` always had,
so the sanitizer, the `user_input` / `agent_response` steps, the `usage` block,
the trailing newline and the exit codes stay in one place.

The transport is still the clone's, and the two providers must stay
behaviourally identical: `BackendProvider.ID()` reports `piagentbridge.ProviderID`
(`"agw"`), and for an agw client `agent.NativeProvider` still wins, so the
gateway URL, `ExternalModel` remap, fingerprint and `User-Agent` are unchanged.

## Files

| File | Change |
|---|---|
| `internal/piagentbridge/backendprovider.go` | **new** — `BackendProvider`: any `backend.Client` → `ai.Provider`, over `BackendStreamFn`; `NewBackendProvider`, `Model(label)`, `Stream` |
| `internal/pigrt/history.go` | **new** — `HistoryTurn`, `(*Runtime).SeedHistory`, `historyMessage` |
| `internal/pigrt/runtime.go` | `RunWith` flushes the event barrier and recovers a failure PiG encoded in the messages (`turnFailure`); ctx cancellation still outranks it |
| `cmd/wagyu/print_pig.go` | dropped `printShouldUsePigRuntime` / `runtimeJetski`; added `pigPrintModel`, `pigHistory`, `History` in the config, and the terminal error chunk |
| `cmd/wagyu/main.go` | print path is now straight-line: one `runPigPrintTurn` call; the `else` loop, cascade observer, lifecycle shadow and `toolRunner` are gone (diff `-w`: ≈ −150 lines) |
| `cmd/wagyu/print_pig_test.go` | new: mock-backend turn, history replay, provider-error surfacing; the selection-rule test is replaced by a provider-derivation test |
| `internal/pigrt/history_test.go` | **new** — replay roles, `turnFailure` table |
| `internal/piagentbridge/backendprovider_test.go` | **new** — id/label + event stream |

## Evidence

All of it is from this host (agy 1.3.4 target), `go build -o /tmp/wagyu ./cmd/wagyu`.

### The mock now runs on PiG

```
$ ANTIGRAVITY_HOME=<tmp> OPENAGY_BACKEND=shim /tmp/wagyu -p 'hi there'
mock reply from model=Gemini 3.8 Flash (Medium): hi there
rc=0
```

`TestRunPigPrintTurnMockBackend` pins it; before this cycle the mock could not
enter the PiG path at all.

### MCP is unchanged (agw stub + stdio echo server)

`<AGENT_HOME>/mcp.json` = `{"servers":{"echo":{"type":"stdio","command":"python3",
"args":["/tmp/mcpecho.py"],"enabled":true}}}`:

```
POST 1 tools=61 msgs=1        ← 60 reference names + mcp__echo__echo
POST 2 tools=61 msgs=3        ← tool result fed back
stdout: done                     rc=0
mcp child: initialize / notifications/initialized / tools/list / tools/call {"text":"hi"}
```

### `-c` replays history into the session

A copy of a real 445-turn conversation in a temp agent home, against a logging
gateway:

```
$ … /tmp/wagyu --conversation 4638a7b0-… -p 'summarize the last message'
POST msgs=31 tools=60
  [0] user: 继续啊
  [1] assistant: <think>The user wants me to continue …
  …
  [30] user: summarize the last message
rc=0
```

`TestRunPigPrintTurnReplaysHistory` pins the same thing with a synthetic pair of
turns.

### Provider failures now reach the headless reporter (a 209 regression, fixed)

`pigrt.RunWith` in cycle 209 only consulted the out-of-band `ErrorSlot` when
`coding.Session.Send` returned a Go error. It never does for a provider failure:
PiG's agent loop turns the error into an assistant message and returns
`(msgs, nil)` (`agent_loop.go:494`). A 429 therefore looked like an empty
success in 209 — verified against the 209 binary, which printed nothing and
exited 0 on the same stub.

Cycle 210 recovers the failure from the messages (`turnFailure`) and returns the
recorded `*backend.HTTPError`, and emits the terminal error chunk so the turn
accounting stays right:

```
$ … /tmp/wagyu -p 'hi'                     # stub: HTTP 429 RESOURCE_EXHAUSTED
rc=3
error: Resource has been exhausted (e.g. check quota).
AGY_ERROR: {"short_error":"RESOURCE_EXHAUSTED (code 429): …","status":"RESOURCE_EXHAUSTED","error_code":429,"code_kind":"http","retryable":true,…}

$ … /tmp/wagyu -p 'hi' --output-format stream-json
{"event":"step_update",…,"step_type":"user_input"}
{"event":"step_update",…,"step_type":"error_message"}
{"event":"result","result":{…,"status":"ERROR","num_turns":1,…}}
```

That is the recovered reference shape (`num_turns: 1` with the `user_input`
step). `TestRunPigPrintTurnSurfacesProviderError` pins it.

### `--print-timeout` is unchanged

```
$ … /tmp/wagyu -p 'hello world' --print-timeout=1ms
rc=0
stderr: [wagyu] print timeout after 1ms with turn in progress; returning partial output
json:   {"status":"SUCCESS","response":"","num_turns":0,…}
```

Ctx cancellation is deliberately checked *before* the failure recovery, because
a deadline is the caller's own failure (the same order `internal/agent`'s
`sendPig` uses).

## 硬约束 / not verified

1. **The TUI still runs on the clone's loop.** `internal/agent.Loop` remains in
   the binary and is what `tui_run.go` drives. The "jetski sunset" is therefore
   complete for `--print` only; `internal/agent`, `internal/tools`,
   `internal/cascade` are all still live dependencies.
2. **The turn budget was not aligned.** The old loop ran `MaxTurns: 1`; PiG's
   session uses its own default tool-iteration budget. With tools advertised the
   PiG path can now loop past one model response within a single `-p`, which the
   clone's loop could not. Nothing in the captured corpus distinguishes the two
   (the captures either have no tools configured or fail before the loop), so
   this is unverified rather than known-divergent.
3. **MCP `http` transport was not exercised.** Only `stdio` was driven live; the
   `http` translation is covered by `TestLoadAgyMCPTranslate` alone.
4. **`stream-json` `init` still reports only the clone's 60 tools**, not the
   extra `mcp__*` the session actually advertises (the init event is written
   before the session starts). Pre-existing; unchanged by this cycle.
5. **Mathematical parity of the failure path.** The recovered `AGY_ERROR`
   envelope's `error_id` is randomly generated per run, so only its shape was
   compared, not its bytes.
6. **With PiG's retry enabled (the default), a retryable failure burns its
   backoff before reporting.** The measured run took 14 s across 8 POSTs. The
   reference's own retry policy was not probed, so whether these match is
   unknown. `{"retry":{"enabled":false}}` in `settings.json` makes it a single
   round trip and is what the tests use.

## Remaining

1. Move the TUI onto `pigrt` (it already renders `backend.StreamChunk`s from a
   `Handler`, so the same synthesis applies; it needs `RunWith`'s `onEvent` to
   drive `toolStepMsg`/`thoughtStartMsg`).
2. Turn on PiG's built-in tools (`SkipBuiltinTools: false`) — capture-breaking,
   so it needs a deliberate pass over the parity fixtures.
3. Align the turn budget (item 2 above) with whatever the reference does.
4. Delete the now-unused print-mode scaffolding: `permissionRunner` /
   `registryRunner` are only reached from `--print`'s denial counting, which the
   `BeforeToolCall` hook now owns.
