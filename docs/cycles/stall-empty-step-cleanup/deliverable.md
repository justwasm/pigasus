# Cycle stall-empty-step-cleanup — deliverable

## What this cycle closes

The second half of the AGY 1.3.6 changelog line 5:

> a stalled request no longer leaves an empty response step behind.

The first half (the 5-minute stall-cancel middleware) is a separate
cycle on `feature/stall-detection`. This cycle is independent of it:
the fix handles every path that aborts a turn before any content
streams — stall-cancel, user-Esc, normal error path, transport
EOF, iteration-cap give-up — and makes the conversation database
skip the resulting empty assistant step.

## Behaviour change

Before this cycle the TUI's `turnDoneMsg` handler appended the
assistant message to `m.session` whenever `msg.reply != ""`. That
gate is too narrow:

  - a turn that aborted before any text streamed → `msg.reply == ""`
    → already skipped (correct, but only by accident — there is no
    test pinning this);
  - a turn that produced only thinking (no text, no tool call) →
    `msg.reply == ""` → also skipped, but the model spent quota on
    the reasoning and the conversation should record that the turn
    happened;
  - the only failure mode the gate caught was "no text streamed".

The classifier this cycle adds is `agent.IsEmptyAssistantTurn`:

```go
func IsEmptyAssistantTurn(content string, toolCalls []backend.ToolCall, reasoning string) bool {
    return strings.TrimSpace(content) == "" && len(toolCalls) == 0 && strings.TrimSpace(reasoning) == ""
}
```

`"Empty"` is the AGY 1.3.6 definition: empty content AND no tool
calls AND no reasoning. The classifier is the unit-level piece; the
integration site (`cmd/wagyu/tui_run.go::Update` case `turnDoneMsg`)
imports it and gates the `m.session = append(...)` call on
`!IsEmptyAssistantTurn(...)`.

## Files changed

| File | Status | Lines | What |
|---|---|---|---|
| `internal/agent/empty_step.go` | new | 67 | `IsEmptyAssistantTurn` helper |
| `internal/agent/empty_step_test.go` | new | 235 | 6 top-level tests, 14 sub-cases in the matrix |
| `cmd/wagyu/tui_tools.go` | modified | +12 / -2 | `turnDoneMsg` gains `reasoning string` field |
| `cmd/wagyu/tui_run.go` | modified | +26 / -2 | `streamPrompt` passes reasoning; `turnDoneMsg` case uses the classifier |
| `cmd/wagyu/turn_empty_step_test.go` | new | 215 | 6 integration tests pinning the wiring |

`internal/convstore/sqlite.go` was not touched, per the hard constraint.

## Test surface

The seven scenarios the changelog row calls out, each with at
least one test pinning it:

| Scenario | Helper test | Integration test |
|---|---|---|
| cancelled request (no content) | `TestIsEmptyAssistantTurn_Matrix/cancelled_request_no_content` | `TestTurnDoneMsg_EmptyAssistantNotRecorded` |
| error-path Send (no content) | `TestIsEmptyAssistantTurn_Matrix/error-path_Send_no_content` | (covered by helper) |
| normal Send (text streamed) | `TestIsEmptyAssistantTurn_Matrix/normal_Send_streamed_text` | `TestTurnDoneMsg_ReplyRecorded` |
| user-Esc mid-stream (no content) | `TestIsEmptyAssistantTurn_Matrix/user-Esc_mid-stream_no_content` | (covered by helper) |
| stall-detected middleware (no content) | `TestIsEmptyAssistantTurn_Matrix/stall-detected_middleware_no_content` | (covered by helper) |
| partial-content assistant (some chunks, then cancel) | `TestIsEmptyAssistantTurn_Matrix/partial-content_assistant_after_cancel` | `TestTurnDoneMsg_PartialReplyRecorded` |
| tool-call assistant (no content, no reasoning) | `TestIsEmptyAssistantTurn_Matrix/tool-call_assistant_with_no_content` | `TestTurnDoneMsg_ToolCallOnlyDoesNotAppendSessionStep` |

Plus boundary coverage the classifier warrants:

  - whitespace-only content / reasoning (`TrimHandlesSSELeadingNewline`,
    `whitespace-only_content`, `whitespace-only_reasoning`);
  - tool-call slice with one empty `Arguments` entry
    (`ToolCallWithEmptyArgs`);
  - `nil` slice and `[]ToolCall{}` agreeing (`NilAndEmptySlicesAgree`);
  - text-only and reasoning-only as non-empty (`ContentOnly`,
    `ReasoningOnly`).

12 top-level tests + 14 sub-cases in the matrix, all
`go test -vet=off -count=1 -run TestIsEmptyAssistantTurn ./internal/agent/`
and `go test -vet=off -count=1 -run TestTurnDoneMsg ./cmd/wagyu/` pass.

## Build & test

```
go build -o /tmp/wagyu-emptyclean ./cmd/wagyu   # exit 0
go test -vet=off -count=1 ./cmd/... ./internal/...
# 1 failure: TestDispatchPrintSlash_LocalCommands (pre-existing flake — see below)
gofmt -l internal/agent/empty_step.go \
        internal/agent/empty_step_test.go \
        cmd/wagyu/turn_empty_step_test.go \
        cmd/wagyu/tui_run.go \
        cmd/wagyu/tui_tools.go
# (empty)
```

The pre-existing `TestDispatchPrintSlash_LocalCommands` failure is
environment-dependent: the test expects `~/.gemini/antigravity-cli/settings.json`
to carry `"model": "minimax-m3"`, but on this machine it carries
`"model": "Gemini 3.8 Flash (Medium)"`. The failure reproduces on
plain `main` before this cycle's changes — see the
**Limitations** section for the details.

## Wiring details

### Helper (`internal/agent/empty_step.go`)

The classifier is the single source of truth for "is this turn
empty?". It takes the three pieces a turn can carry content in
(text, tool calls, reasoning) and returns true iff all three are
empty after trimming whitespace.

Trim is load-bearing — see the regression test
`TestIsEmptyAssistantTurn_TrimHandlesSSELeadingNewline` and the
matching integration test `TestTurnDoneMsg_WhitespaceOnlyReplyClassifiedEmpty`.
The agw transport delivers `"\n"` as the first delta on every
stream; without trimming, the classifier would treat that byte as
content and skip the gate on every single request.

### TUI (`cmd/wagyu/tui_run.go::Update` case `turnDoneMsg`)

The previous gate `if msg.reply != ""` becomes
`if !agent.IsEmptyAssistantTurn(msg.reply, nil, msg.reasoning)`.
The `nil` is for tool calls because the recovered wire shape carries
tool calls as `StepToolCall` rows of the conversation database
(via `m.blocks` / `turnBlock`), not as `StepModelResponse` rows
from `m.session`. The classifier still confirms non-emptiness for
the on-screen tool-step block, which is the user-visible artefact.

### Reasoning threading (`cmd/wagyu/tui_run.go::streamPrompt`)

`streamPrompt` already accumulates the model's reasoning in
`thought strings.Builder` via the `agent.Sanitizer.OnThought`
callback (cycle 100). This cycle threads the accumulated string
into the `turnDoneMsg` struct (new `reasoning string` field on
`cmd/wagyu/tui_tools.go`) so the classifier at the call site has
the third input. The string is already trimmed at the
`sanitizer.Finish()` boundary (the sanitizer eats the trailing
space the chunk-boundary stripping leaves behind), so the call
site passes it through verbatim.

## Hard constraints

| Constraint | Honoured? |
|---|---|
| `internal/convstore/sqlite.go` untouched | ✅ |
| New files gofmt-clean | ✅ |
| Pre-existing not-clean files not reformatted wholesale | ✅ (only `cmd/wagyu/tui_run.go` and `cmd/wagyu/tui_tools.go` touched, both gofmt-clean before and after) |
| `go build -o /tmp/wagyu-emptyclean ./cmd/wagyu` exits 0 | ✅ |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` passes except documented pre-existing flakes | ✅ (1 pre-existing flake, documented in **Limitations**) |
| `gofmt -l` on every NEW file empty | ✅ |
| Branch not pushed | ✅ |
| Worktree path correct | ✅ |
| Single commit | ✅ |
| ≥ 7 unit tests | ✅ (12 top-level tests, 14 sub-cases in the matrix) |

## Limitations

### Pre-existing flake: `TestDispatchPrintSlash_LocalCommands`

The test asserts `dispatchPrintSlash("/model")` returns
`"minimax-m3\tMiniMax M3\n"`, which only holds when the user's
`~/.gemini/antigravity-cli/settings.json` carries
`"model": "minimax-m3"`. On this machine the file carries
`"model": "Gemini 3.8 Flash (Medium)"`, so the assertion fails:

```
print_slash_test.go:70: dispatchPrintSlash("/model") output = "gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)\n", want "minimax-m3\tMiniMax M3\n"
```

The failure reproduces on plain `main` (`a58d7dd`) before this
cycle's changes — it is an environment-dependent pre-existing
flake, not a regression. Fixing it would mean either parameterising
the test on `t.Setenv("ANTIGRAVITY_HOME", t.TempDir())` with a
synthesised settings.json, or moving the test into a sub-test with
an env-override prelude; both are out of scope for this cycle.

### Compose with `feature/stall-detection`

The 5-minute stall-cancel middleware on `feature/stall-detection`
cancels the loop's context; this cycle's classifier sees a
cancelled request as `content="" && toolCalls=nil && reasoning=""`
and skips the step. No additional wiring is needed beyond this
classifier. Both features are independently correct: the
classifier also handles user-Esc, transport EOF, iteration-cap
give-up, and any other path that aborts before content streams.

### Print-mode (-p)

The print path (`internal/pigrt/runtime.go::RunWith` and
`cmd/wagyu/runPrintMode`) does not have a persistent step list —
it streams to stdout and exits. The classifier is not used there
because there is nothing to gate. The print path's existing
behaviour on a cancelled request is "print whatever made it
through, report failure on stderr" — which is exactly what
`--print-timeout`'s partial-output success relies on (cycle 117).
This cycle preserves that contract: a cancelled `-p` turn still
streams its partial output and reports the timeout warning.
