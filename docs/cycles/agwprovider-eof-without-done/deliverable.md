# Cycle agwprovider-eof-without-done — TUI stream dropped before first byte must surface an error

## Symptom (user report)

> TUI 跑长 turn 时中断,最终没有任何输出,必须手动按继续才能进行下去。

The user reported a TUI turn where the spinner ran for a while, then the
turn ended with **zero text in the transcript** and no visible error. The
only way to recover was to send a fresh message — `continue` / `继续` /
anything — which would start a brand-new turn. The original prompt was
silently dropped on the floor.

## Root cause

`internal/piagentbridge/agwprovider_stream.go:344-345` (pre-cycle) and
`internal/piagentbridge/stream.go:209-216` (pre-cycle) both implement the
"transport closed without a terminal marker" path as a clean stop:

```go
// agwprovider_stream.go, pre-cycle
// The body ended without [DONE]; treat whatever we have as the answer.
return ""

// stream.go, pre-cycle
// The channel closed without a Done chunk. backend.Client promises one,
// but treating the close as a clean stop keeps a misbehaving transport
// from hanging the agent.
s.finish(stopReasonFor(s.partial), "")
```

That decision dates from cycle `agwprovider-stream-stall-detect` (5-minute
idle watcher) and was specifically scoped to "what we have is the answer".
The problem: a transport that closes **before sending a single frame**
(connection severed on connect, gateway idle-evict before the first byte,
model request dropped at the LB) ends up in this branch too, with
**zero bytes streamed**. The downstream cascade is:

1. `streamState.finish(ctx, "")` → `stopReasonFromFinish("", false)` →
   `pi.StopReasonStop` (because `s.finishReason == ""` and no tool calls).
2. PiG pushes `pi.DoneEvent{Reason: StopReasonStop, …}` instead of
   `pi.ErrorEvent`.
3. `chunkSink.endAssistant` (`internal/agent/pig.go:543`) sees
   `StopReason == StopReasonStop` and walks the **default** branch:
   cascade transitions `StateSave → StateDone`, emit
   `StreamChunk{Done: true}` — **no error**, **no text**.
4. The TUI's handler at `cmd/wagyu/tui_run.go:4044-4113` sees Done with
   neither Text nor Error, so the turn is committed as "finished, empty
   body". The user sees the spinner stop and an empty transcript.
5. To recover, the user must type something (e.g. `continue`) which fires
   a fresh `Loop.Send`, which **does not** re-send the original prompt —
   the user has to remember it and retype.

## Fix

Distinguish "transport closed cleanly without a terminal marker" into two
shapes:

| Pattern | Old behaviour | New behaviour |
|---|---|---|
| EOF-without-[DONE] **with at least one frame** received | clean stop | **clean stop** (unchanged — this is `agw.k0s.io`'s normal behaviour; see `cmd/wagyu/scriptedAGW` and the production gateway) |
| EOF-without-[DONE] **and no frames at all** | clean stop (BUG) | `StopReasonError` with diagnostic `EOF: stream ended without [DONE] marker and no frames` |

The same fix in two places (one for `agwprovider.Stream` — SSE pump, one
for `BackendStreamFn` — `backend.StreamChunk` channel pump):

- `internal/piagentbridge/agwprovider_stream.go` — `pumpScan` tracks
  `gotDone bool` AND `gotAnyFrame bool`. Falls into the error branch only
  when `!gotDone && !gotAnyFrame`.
- `internal/piagentbridge/stream.go` — `bridgePumpState.run` tracks
  `sawDone bool` AND `sawAnyChunk bool`. Same condition.

Both pumps now pass the diagnostic through `streamState.finish` /
`bridgePumpState.finish`'s existing `errText != ""` branch, which already
pushes `pi.ErrorEvent{Reason: StopReasonError}` and routes the chunkSink
through the **error** branch:

```go
case pi.StopReasonError, pi.StopReasonAborted:
    text := message.ErrorMessage
    if text == "" {
        text = "agent: upstream stream failed"
    }
    failure := errors.New(text)
    s.record(failure)
    cascadeTransition(s.cascade, StateError, fmt.Sprintf("stream error: %v", failure))
    s.emit(backend.StreamChunk{Error: failure, Done: true})
```

The TUI handler (`cmd/wagyu/tui_run.go:4044-4079`) already renders
`StreamChunk.Error` into `m.response` via `prog.Send(backendChunkMsg(errmsg))`
(cycle 263), so the diagnostic rides inline below the empty body and the
user no longer needs to type `continue` — the next prompt is up to them.

## Trade-off accepted

The fix targets the **0-frame EOF-without-[DONE]** case only. A half-streamed
reply (transport sent some frames then died) is left as a clean stop,
because:

1. The user usually sees the partial text in the transcript — the
   spinner stops, the partial reply is visible, and they can re-prompt
   with the missing bit.
2. `agw.k0s.io`'s documented default is "no [DONE] sentinel" (see
   `cmd/wagyu/print_pig_test.go:scriptedAGW` and the production gateway
   trace). Pinning "any frame received = clean stop" preserves
   back-compat with every existing live run.
3. The TUI already shows the partial text in `m.response`; an error
   banner would *cover* it. Trade-off #1 (accepted explicitly in the
   code comments on `pumpScan` and `bridgePumpState.run`): the
   half-streamed reply stays visible, no error rides below it.

The half-streamed case remains a known gap; a future cycle could add a
"partial-response" warning if dashboards need the signal.

## Tests

Two new test pairs, one for each pump, plus a renamed back-compat pin:

| File | Test | Asserts |
|---|---|---|
| `internal/piagentbridge/agwprovider_test.go` | `TestProvider_EmptyBodySurfacesError` | gateway sends **zero frames**; stream's last event is `error`; `result.StopReason == StopReasonError`; `result.ErrorMessage` mentions `[DONE]`; `assistantText(result.Content) == ""` |
| `internal/piagentbridge/agwprovider_test.go` | `TestProvider_BodyEndingWithoutDoneWithFramesStillTerminates` | gateway sends `contentFrame("no done marker")` only (no `[DONE]`); stream's last event is `done`; `assistantText(result.Content) == "no done marker"` (agw.k0s.io back-compat) |
| `internal/piagentbridge/bridge_test.go` | `TestBackendStreamFn_EmptyChannelSurfacesError` | `backend.StreamChunk` channel closed without any chunk; same asserts as the agw twin |
| `internal/piagentbridge/bridge_test.go` | `TestBackendStreamFn_ChannelClosedWithoutDoneWithChunksStillTerminates` | channel closes after one text chunk with no Done; trailing `done`, text preserved |

The pre-cycle `TestProvider_BodyEndingWithoutDoneStillTerminates` and
`TestBackendStreamFn_ChannelClosedWithoutDoneStillTerminates` are
**removed**: both pinned the buggy "treat whatever we have as the answer"
behaviour. Their replacements above are the canonical pins going forward.

## Gates

```
go build -o /tmp/wagyu ./cmd/wagyu                                   exit 0
go vet ./internal/piagentbridge/ ./internal/agent/ ./internal/pigrt/  clean
go test -race -count=1 ./internal/piagentbridge/ ./internal/agent/ ./internal/pigrt/ ./internal/backend/  PASS
go test -count=1 -run 'TestRunPigPrintTurn' ./cmd/wagyu/              PASS (3 tests, prints path)
go test -count=1 ./cmd/... ./internal/...                             PASS (TestRunShellCommand_GitEditorSet pre-existing race stays as-is)
```

## Hard constraints preserved

- **No vendor binary patching.** `~/.local/bin/agy` is byte-identical
  before and after (SHA-256 `df2d4fb5…`).
- **`gofmt -l`** clean on touched files
  (`internal/piagentbridge/agwprovider_stream.go`,
  `internal/piagentbridge/stream.go`,
  `internal/piagentbridge/agwprovider_test.go`,
  `internal/piagentbridge/bridge_test.go`).
- **No `go test ./...`** — `scratch/` does not compile. `./cmd/...
  ./internal/...` only.
- **Pre-existing `cmd/wagyu/model_default_effort_test.go:67`** 3-arg
  `Errorf` vet warning stays as-is.
- **Pre-existing `cmd/wagyu/remote_control_foreground_test.go:117-157`**
  fd-in-defer bug stays as-is.
- **Pre-existing `internal/tools/TestRunShellCommand_GitEditorSet`**
  race detector failure stays as-is (unrelated — `mvdan.cc/sh/v3` +
  `os/exec`).

## Limitations

- **Half-streamed replies** (transport sent some frames then died) still
  appear as a clean stop with a truncated transcript. The user sees the
  partial text but no error banner; they'd have to manually re-prompt
  with the missing tail. Recording under TODO-rc-parity.md § "follow-up
  cycles parked in the gap-closure deliverables" → candidate cycle
  `rc-stream-partial-warning`.
- **The print path** (`-p` / `--print`) goes through `pigrt.Runtime.RunWith`
  not `pigrt.turnFailure`. Pre-cycle, `turnFailure` only caught
  `StopReasonError / StopReasonAborted / StopReasonLength` on the last
  assistant message — *not* a missing transport entirely. Post-cycle the
  new `StopReasonError` from the pump is captured, but the print
  envelope still surfaces the diagnostic through `pigrt.turnFailure`'s
  existing path. End-to-end print behaviour is unchanged; the
  `TestRunPigPrintTurnSurfacesProviderError` test still pins the
  429-quota path.
- **No live wire verification.** A real `agy remote-control start` daemon
  was not replayed with a 0-frame EOF-without-[DONE] stream; that
  requires the production gateway to send an actual zero-byte body and
  is parked in TODO-rc-parity.md § "Priority 1 — wire-level parity
  verification".

## Future cycles (parked, not in this commit)

| Cycle ID | Goal |
|---|---|
| `rc-stream-partial-warning` | surface a non-fatal "partial response" warning when the transport dies mid-stream after sending ≥1 frame but before [DONE]; today's half-streamed reply is silent |
| `rc-wire-live-verify` | actually exercise a 0-frame EOF-without-[DONE] against the production `~/.local/bin/agy remote-control start` daemon and pin the wire envelope byte-for-byte |