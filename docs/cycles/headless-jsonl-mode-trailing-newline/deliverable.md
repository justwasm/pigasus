# Deliverable — `headless-jsonl-mode-trailing-newline`

**Cycle name:** `feature/headless-jsonl-mode-trailing-newline`
**Branch from:** `main` at `54db67b` (cycle 224 slash test Errorf format)
**Changelog gap (AGY 1.2.7):** *"Fixed `--output-format=stream-json --verbose` headless
mode not emitting a final newline after the assistant message's trailing JSON
event, leaving the next shell prompt glued to the last line of output."*

## What changed

1. `--verbose` is now a hidden bool flag on the root CLI
   (`internal/cli/parser_specs.go`). It is absent from `--help` so the v1.3.5 21-flag
   count is preserved; only the Jetski flag table grew (22 long + 3 short = 25 in
   `StandardFlags()`, 27 once `--version` and `--verbose` are folded in by the parser).
2. `--output-format stream-json` with `--verbose` now emits one extra `assistant_message`
   JSONL event per turn, before the closing `result` envelope
   (`cmd/wagyu/print_format.go:streamEventAssistantMessage`,
   `cmd/wagyu/print_format.go:jsonAssistantMessage`,
   `cmd/wagyu/print_format.go:emitAssistantMessage`). The event carries the assistant
   text body the model produced plus the model id (omitempty when unset).
3. Every stream-json event line — including the new `assistant_message` —
   is emitted through `writeJSONLine`, which appends exactly one `\n`. This is
   the load-bearing contract that closes the AGY 1.2.7 trailing-newline bug.
4. `runPrintMode` (`cmd/wagyu/main.go`) reads the `--verbose` flag at the closing
   envelope site and routes through `emitAssistantMessage(w, env.Response, modelName)`
   when (a) the format is `stream-json`, (b) the turn actually began,
   (c) no timeout cut it off, and (d) the buffered response text is non-empty.
   Each gate is documented inline so a future refactor sees the constraint.

## Wire format

The new event lands on stdout between the closing `step_update` and the
`result` envelope:

```json
{"event":"assistant_message","assistant_message":{"message":"<turn text>","model":"<model id>"}}\n
```

Followed immediately by the existing closing line:

```json
{"event":"result","result":{...the json envelope...}}\n
```

A `--verbose=false` (or unset) run preserves the existing four-event contract:

```
{init}{step_update user_input}{step_update agent_response}{result}
```

## Tests added

| Test | File | Pin |
|---|---|---|
| `TestAssistantMessageFieldOrder` | `cmd/wagyu/print_format_test.go` | Wire shape + omitempty on `model`, two cases |
| `TestEmitAssistantMessage` | `cmd/wagyu/print_format_test.go` | Helper emits exactly one trailing `\n`, no double-newline |
| `TestEmitAssistantMessageWithModel` | `cmd/wagyu/print_format_test.go` | Variant carries the `model` field with trailing newline |
| `TestEmitAssistantMessageEmpty` | `cmd/wagyu/print_format_test.go` | Empty-message variant still emits a single line + newline (gate is at runner) |
| `TestStreamJSONTrailingNewline` | `cmd/wagyu/print_format_test.go` | 3-event sequence: each line has exactly one `\n`, full output has 3 newlines |
| `TestStreamJSONVerboseNoDoubleNewline` | `cmd/wagyu/print_format_test.go` | Verbose assistant_message + result back-to-back, exactly one `\n` each, no `\n\n` |
| `TestJSONEnvelopeValidAfterAssistantMessage` | `cmd/wagyu/print_format_test.go` | json envelope unchanged by the new event type |
| `TestTextModeUnchangedByVerbose` | `cmd/wagyu/print_format_test.go` | `--verbose` does not alter `printOutputFormat`'s text/json/stream-json resolution |
| `TestPrintVerboseFlagResolution` | `cmd/wagyu/print_format_test.go` | Bool flag default + explicit values |

9 tests, well clear of the cycle's 5-test minimum.

## Hard-constraint compliance

| Constraint | Result |
|---|---|
| `go build -o /tmp/wagyu-hjn ./cmd/wagyu` exits 0 | ✅ rc=0 |
| `go test -vet=off -count=1 -run 'TestStream\|TestJson\|TestHeadless\|TestOutput' ./cmd/wagyu/ ./internal/.../` passes | ✅ all packages `ok` |
| `gofmt -l` empty on every changed file | ✅ no output |
| No edits to `internal/convstore/sqlite.go` | ✅ untouched |
| Pre-existing not-clean files (`internal/` + `picker_view.go` + `tui_v135_test.go`) not bulk-reformatted | ✅ not touched |
| Single commit | ✅ |
| Branch not pushed | ✅ |

## Files touched

```
internal/cli/parser_specs.go          (+13 lines: --verbose flag spec + comment)
internal/cli/parser_test.go           (+5 lines:  flag expectation + count bump + count comment)
cmd/wagyu/print_format.go             (+58 lines: streamEventAssistantMessage, jsonAssistantMessage,
                                                jsonStreamEvent.AssistantMessage, emitAssistantMessage,
                                                printVerbose + accompanying docs)
cmd/wagyu/print_format_test.go        (+260 lines: 9 new tests + TestJSONStreamEventFieldOrder comment rewrite)
cmd/wagyu/main.go                     (+15 lines: printVerbose-aware assistant_message emission at the
                                                closing envelope site in runPrintMode)
```

## 硬约束 / what this cycle did NOT verify

* 【推测】The AGY 1.2.7 reference's exact `assistant_message` member order was not
  captured from a live 1.2.7 binary — the test pins the closure's own schema
  (`message` then `model` with omitempty), and will need a live cross-check the
  next time a 1.2.7-class binary is in the bin/ directory.
* 【推测】Whether AGY 1.2.7's bug was a missing `\n` or a `\r\n` (CRLF on
  Windows). The clone emits a single `\n`. If the reference emits `\n\r` on
  Windows a parity test will fail there, not in this cycle.
* 【推测】The `assistant_message` event is emitted only when the turn actually
  produced visible text (`env.Response != ""`). A turn that returned an error
  during chunk delivery and emitted an empty `response` is a no-op for the
  verbose channel — the closing `result` event still carries the failure in
  its `error` field, which is the path the AGY 1.2.7 fix left alone. The gate is
  documented inline in `runPrintMode`; no separate test was added because the
  contract is the same one the existing `--print-timeout` partial-output path
  already exercises.
* No re-derivation of the binary's `--verbose` flag was run end-to-end against
  a real model turn — the cycle ships the wire contract and the helper plumbing,
  and the `runPigPrintTurn` integration test in `print_pig_test.go` is the
  endpoint that would have run it. The missing piece is the actually-driving
  fixture (an `--output-format stream-json --verbose "say hi"` integration test).
  That's work for a follow-up cycle — adding the test would require a live or
  recorded model run, which the existing harness does not set up here.

## How to verify locally

```bash
cd /private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-headless-jsonl-mode-trailing-newline

# Build clean.
go build -o /tmp/wagyu-hjn ./cmd/wagyu

# Run the targeted suite the cycle was pinned against.
go test -vet=off -count=1 -run 'TestStream|TestJson|TestHeadless|TestOutput' \
  ./cmd/wagyu/ ./internal/.../

# Confirm gofmt is clean on every file we touched.
gofmt -l \
  cmd/wagyu/print_format.go cmd/wagyu/print_format_test.go \
  internal/cli/parser_specs.go internal/cli/parser_test.go \
  cmd/wagyu/main.go

# Spot-check the existing --output-format stream-json output still ends with \n.
echo hi | /tmp/wagyu-hjn -p /help --output-format stream-json | od -c | tail -3

# Spot-check --verbose is accepted (local /help does not invoke the model and so
# does not emit assistant_message — the flag is recognised but the no-op is
# intentional for no-model-turn runs).
/tmp/wagyu-hjn -p /help --output-format stream-json --verbose | tail -c 30 | od -c
```
