# Cycle stream-error-frames — an upstream error record must fail the turn, not empty it

## Symptom

The live TUI, on a long turn against MiniMax M3 via `agw.k0s.io`, sometimes
finished with **an empty transcript and no error**: the spinner stopped, no
assistant text appeared, and the only recovery was to send the prompt again
(the "continue" workaround). `agwprovider-eof-without-done` (cherry-picked as
`0683a5f`) covered the **zero-frame** version of this; this cycle covers the
version where the gateway answers with an **error record**.

Measured before the fix, with a one-record SSE body
`data: {"error":{"message":"upstream exploded"}}`:

```
event types = [start done]
StopReason="stop" ErrorMessage="" text=""
```

That is, the record was counted as "one frame" (so the zero-frame guard did
not fire), its `error` member was dropped, and the run ended as a clean stop.

## Root cause

Two independent gaps, both surfaced by reading PiG v0.5.0's release notes
(v0.5.1 has **no code changes** — it only retracts v0.5.0 to fix the 500 MB
`go install` limit — so all of the notes below are 0.5.0 behaviour):

1. **The agw adapter ignored error records.**
   `internal/piagentbridge/agwprovider_stream.go`'s `chatChunk` decoded only
   `choices` and `usage`, and `pumpScan` skipped every line that did not start
   with `data:` (so `event: error` was skipped too). PiG's OpenAI decoder
   (`ai/openai_stream_decoder.go`) does the opposite: a record with a
   **truthy top-level `error` member**, or an **`event: error`** record, is
   thrown as an SDK `APIError` before anything else is read. Because the clone
   owns the decoder for the agw transport (PiG only sees our `ai.Provider`),
   PiG could not do this for us.
2. **The TUI's retry budget did not recognise PiG's gateway phrases.**
   0.5.0: *"PiG retries on the gateway error `Unable to forward upstream
   stream`."* `internal/agent/transient_network.go` carried the AGY changelog's
   socket phrases only, so on the TUI path:

   ```
   "agw: upstream 500: Unable to forward upstream stream"  transient=false
   "upstream_stream_read_error"                            transient=false
   "stream was interrupted"                                transient=false
   ```

   `sendPigAuto` picks `AutoContMaxNetworkDepth` (8) only for transient
   errors, so those got `AutoContMaxDepth` (3). The print path already
   retried them through PiG's own `ai.IsRetryableAssistantError` (whose
   pattern includes `unable to forward upstream`, `upstream request failed`,
   `stream was interrupted`, `ended without`), so the two transports
   disagreed on the same failure.

## Fix

### 1. Error records are terminal (`agwprovider_stream.go`)

- `chatChunk` gained `Error json.RawMessage \`json:"error"\``.
- `pumpScan` now tracks `event:` lines for the current SSE block (reset on a
  blank line) and recognises both forms. The message extraction mirrors
  `APIError.makeMessage`:
  - a truthy object's string `message` wins;
  - otherwise the JSON text of the value;
  - a falsy value (absent / `null` / `false` / `0` / `""`) is **not** an
    error unless the event itself is `error:`;
  - `event: error` with no usable value reports
    `(no status code or body)`.
- Returning the text routes the record through `streamState.finish(errText)`,
  which closes the open text block, syncs any tool calls, sets
  `StopReasonError`, and pushes `pi.ErrorEvent`. Text streamed **before** the
  error record is preserved (verified by
  `TestProvider_ErrorFrameAfterTextPreservesText`).
- Deliberate deviation: an `event: error` whose data is not JSON returns the
  raw payload instead of the SDK's *"Error reading response: malformed
  server-sent event JSON."* The payload is what the user needs; the SDK text
  would hide it. `apiErrorMessage` also returns the record's JSON as written
  rather than re-canonicalising it (the text is user-facing, not wire).

This covers both live transports: the TUI and `--print` both use
`agent.NativeProvider` → `piagentbridge.NativeProvider` → this SSE pump. The
`backend.StreamChunk` channel pump (`stream.go`) already receives typed
errors from `backend.Client` and is unchanged.

### 2. Retry classification (`transient_network.go`)

Added the PiG 0.5.0 phrases that can reach us over agw/OpenAI-compatible SSE:

```
unable to forward upstream, upstream request failed,
upstream_stream_read_error, stream was interrupted,
http/2 stream failed, other side closed,
socket connection was closed, reset before headers, ended without
```

Node-only wording (`getaddrinfo`, `ENOTFOUND`, `fetch failed`) stays out. The
effect: `sendPigAuto` grants these `AutoContMaxNetworkDepth` (8) attempts,
matching PiG's print-path policy, instead of 3.

## Why not report every truncated tool call too?

PiG 0.5.0 also says *"Malformed final tool-call arguments are reported instead
of running with the parsed prefix."* The clone's `decodeArguments`
(`internal/piagentbridge/stream.go:400`) still returns `pi.JsonObject{}` for a
truncated argument string, so a stream cut in the middle of a tool call can
execute the tool with empty arguments. That is the same family of bug but a
different fix (it changes tool-execution semantics and needs its own decision
on which errors are retryable). It is parked as follow-up cycle
`malformed-tool-args-reported` in the limitations below.

## Tests

| File | Test | Asserts |
|---|---|---|
| `internal/piagentbridge/agwprovider_test.go` | `TestProvider_ErrorFrameSurfacesError` | top-level `error` record → trailing `error` event, `StopReasonError`, gateway message in `ErrorMessage`, empty content |
| `internal/piagentbridge/agwprovider_test.go` | `TestProvider_ErrorEventSurfacesError` | `event: error` with a `message`-only record → the same |
| `internal/piagentbridge/agwprovider_test.go` | `TestProvider_ErrorFrameAfterTextPreservesText` | prefix streamed before the error survives; diagnostic rides below |
| `internal/piagentbridge/agwprovider_test.go` | `TestProvider_FalsyErrorMemberIsIgnored` | `"error":null` + a normal chunk → clean `done`, text `ok` |
| `internal/piagentbridge/agwprovider_test.go` | `TestStreamErrorText` | the `APIError.makeMessage` extraction table (object message, message-less object, bare string, null event, absent member) |
| `internal/agent/transient_network_test.go` | `TestIsTransientNetworkErr_PiG051GatewayPhrases` | the nine new phrases classify transient; 403/429 stay permanent |
| `internal/agent/pig_test.go` | `TestLoop_ErrorFrameSurfacesAsTurnFailure` | end-to-end `Loop.Send`: handler sees `StreamChunk{Error}`, `Send` returns the gateway message, no text |

The `fakeAgw` test gateway gained a `rawSSE` field so an `event:` line can be
written verbatim (the existing writer only emits `data: <payload>`).

## Gates

```
go build -o /tmp/wagyu-sef ./cmd/wagyu                              exit 0
go vet ./internal/piagentbridge/ ./internal/agent/                  clean
go test -race -count=1 ./internal/piagentbridge/ ./internal/agent/
  ./internal/pigrt/ ./internal/backend/                             PASS
go test -count=1 ./cmd/...                                          PASS
go test -count=1 ./internal/...                                     PASS
gofmt -l <touched files>                                            empty
```

Pre-existing issues left alone: the `cmd/wagyu/model_default_effort_test.go:67`
vet warning, the `remote_control_foreground_test.go` fd-in-defer failure, the
`TestRunShellCommand_GitEditorSet` race, and the 279-era gofmt dirt in
`internal/agent/auto_continue.go`.

Two `cmd/wagyu` tests are timing-fragile under a loaded full-suite run and were
observed failing once here, then passing 3/3 in isolation and in a solo
`./cmd/...` run:

- `TestCycle225QuotaStubRefills` — its assertion window is ~84 ns
  (`want in [t, t+84ns]`), so any scheduler delay fails it.
- `TestSlashCodesearchStreaming` — a 2 s "search did not settle" deadline.
- `TestRunStatusLineScript_SpawnEcho` / `TestRunStatusLineScript_NonZeroExit`
  — both carry a 1 s subprocess deadline; a second full-suite run failed these
  two instead (1.00 s / 1.01 s), and `./cmd/...` alone is green.

None touches this cycle's code paths.

## 硬约束 / limitations

- **No vendor patching.** `~/.local/bin/agy` untouched.
- **No `go test ./...`** — `scratch/` does not compile.
- **No live capture of an error record.** The shapes are PiG's decoder
  contract and OpenAI's documented error object; the production
  `agw.k0s.io` error frame was not recorded. A live reproduction is parked
  with `rc-wire-live-verify`.
- **Half-streamed replies without an error record** still end as a clean stop
  (`agwprovider-eof-without-done` trade-off #1). With this cycle such a reply
  is at least no longer *silently* empty when the gateway did send an error
  record; a cut with no record is still indistinguishable from a normal
  agw.k0s.io close. Parked as `rc-stream-partial-warning`.
- **`event: error` data that is not JSON** returns the raw payload, not the
  SDK's syntax-error wording (deliberate; see Fix §1).
- **Malformed final tool-call arguments** still execute with `{}` — parked as
  `malformed-tool-args-reported`.

## Relation to the previous cycle

`agwprovider-eof-without-done` (`0683a5f` on `main`) fixed the zero-frame
shape. This cycle extends the same user-visible guarantee ("a turn that got no
answer says why") to streams that carried an error record. The two are
independent: either can fire without the other.
