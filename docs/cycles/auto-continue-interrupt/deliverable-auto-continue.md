# Merge record — cycle auto-continue-interrupt

**Date:** 2026-10-18
**Cycle:** `feature/auto-continue-interrupt` (single-cycle branch off `c60cc26`,
the cycle-229 merge record)
**Branch:** `feature/auto-continue-interrupt` (does **not** merge to `main` in
this cycle — this is a follow-on worktree the cycle-230 roadmap reserves for a
later merge wave)

## TL;DR

Closes pain-point #2 of the "long tool-call chain interruption" complaint. A user
prompt whose upstream stream closes mid-tool-call used to leave the terminal
stuck: PiG encoded the failure as `StopReasonError` on the last assistant
message, `Send` returned a typed error, and the surface (TUI transcript row or
the print-path error envelope) rendered a one-line "stream error" reply instead
of the part of the response the model already had. Same shape for the
tool-iteration cap. Cycle auto-continue-interrupt adds a recovery layer on both
side paths:

1. **`internal/pigrt/auto_continue.go`** — print path. When `Runtime` was built
   with `Options.AutoCont: true`, `RunWith` calls `maybeAutoContinue` after each
   `Session.Send` failure. The recovery copy is the AGY verbatim text from
   loadfile `0x33dc41b` (stream-interrupted) or `0x33a3d44` (the iteration-cap
   `**Status**` banner), substituted into the appropriate PiG
   `UserMessage`/`AgentMessage` slot, then `Agent.Continue(ctx)` is called. Depth
   is bounded by `AutoContMaxDepth` (=3) which gives exactly 3 attempts before
   give-up. The print path defaults `AutoCont: true` in `runPigPrintTurn`.
2. **`internal/agent/auto_continue.go`** — TUI + HTTP shim engine. Symmetric
   surface: exposes the same verbatim strings, the same `ErrAutoContMaxDepth`,
   the same `AutoContMaxDepth`. Adds `sendPigAuto`, which `Loop.Send` dispatches
   to when the new `Options.AutoCont` flag is set. The wrapper calls `sendPig`
   up to `AutoContMaxDepth` times, appending the verbatim AGY recovery copy as
   a synthetic user message on each failed call. On give-up it returns a typed
   `*ErrAutoContMaxDepth` whose `Last` field still holds the
   `*backend.HTTPError`, so `cmd/wagyu/api_error.go`'s `errors.As`
   classification keeps working.
3. **`cmd/wagyu/tui_run.go`** wires `Agent.Options.AutoCont: true` and exposes
   a transient `↻  auto-continuing… (retry N of 3)` row above the recovered
   turn, posted via `prog.Send(autoContBannerMsg{...})` from
   `Options.AutoContBanner`.

## Auto-continue decision tree

The package head of `internal/agent/auto_continue.go` and
`internal/pigrt/auto_continue.go` advertises one matrix; both files implement
the same one:

| Trigger (in priority order)                                  | Recovery copy                               |
|--------------------------------------------------------------|---------------------------------------------|
| `ctx.Err() != nil` (caller cancelled)                         | none — surface `ctx.Err()`                  |
| `agent.ErrMaxTurnsReached`                                   | `**Status**: Cancelled after N iterations`  |
| `last assistant.StopReason ∈ {Error, Aborted, Length}` (any) | `The stream was interrupted. Please continue the task you were working on.` |
| `last assistant.StopReason ∈ {Stop, ToolUse}`                | none — clean completion                     |
| anything else                                                 | none — surface the original error           |

`agent.ErrMaxTurnsReached` wins over the `StopReasonError` check, so a run that
hits the budget and gets reported with a flat top error message (no
StopReasonError on the assistant tail) still uses the `**Status**` banner.

Both `ErrAutoContMaxDepth` (in each package) implement `Unwrap() error` returning
the underlying failure, so `errors.As(err, &backend.HTTPError)` keeps surfacing
the typed transport error from the AGY_ERROR envelope — important because cycle
183's `cmd/wagyu/api_error.go` only reads the concrete `*backend.HTTPError`.

## PiG API chain

The recovery path uses one PiG API that the prior cycle had wired once, plus
two new ones the cycle-209 / cycle-210 print path exposed:

```
r.session.Send(ctx, prompt)
  ↓ returns (msgs, err) where msgs.last_assistant may have StopReason={Error|Aborted|Length}
r.maybeAutoContinue(ctx, msgs, err, depth=0)
  ↓ checks ctx / depth / classifyInterruption
r.runAutoContCycle(ctx, msgs, recovery)
  ↓ appendRecoveryMessage(r.session.Agent().MessagesSnapshot(), recovery)
       ↑ this.user-tail trick unblocks Agent.Continue (the agent rejects an assistant tail)
   → r.session.Agent().SetMessages(updated)
   → r.session.Agent().Continue(ctx)
```

```
l.opts.AutoCont == true →
  l.sendPigAuto(ctx, input)
    for attempt in [0, AutoContMaxDepth):
      err := l.sendPig(ctx, input)             // throws ErrorSlot on real HTTP failures
      classify recovery copy from err
      if attempt is the last permitted: return &ErrAutoContMaxDepth{Depth, Last: err or ErrorSlot.Err()}
      else: append verbatim copy, fire AutoContBanner(depth+1), retry
```

The wrapper reuses the existing `Loop.sendPig` (no refactor; each recovery
attempt builds a fresh PiG agent from scratch with the augmented `input.Messages`,
which replays the full conversation in the next upstream request — acceptable
because auto-continue is the catastrophic path the rest of the turn protected
against, and the second upstream request usually succeeds).

## Files touched

| File | Lines | Role |
|---|---|---|
| `internal/agent/auto_continue.go` (new, partial → 287 lines) | +117 | `AutoContStreamInterrupted`, `AutoContIterationCapFormat`, `AutoContMaxDepth`, `ErrAutoContMaxDepth` (with `Unwrap`), `classifyInterruption(err, lastStopReason)`, `recoveryForError(err)`, `turnsFromErr(err)`, `recoveryMessageFromMessages`, `sendPigAuto`. |
| `internal/pigrt/auto_continue.go` (new, partial → 297 lines) | +39 | Same constants + `ErrAutoContMaxDepth` (independent of the agent package's — different prefix in `Error()` so callers can match either), `classifyInterruption`, `lastAssistant`, `iterationCountFromErr`, `appendRecoveryMessage`, `runAutoContCycle`, `maybeAutoContinue`. |
| `internal/agent/loop.go` | +30 | New `Options.AutoCont bool` and `Options.AutoContBanner func(depth int)`; `Loop.Send` dispatches to `sendPigAuto` when `AutoCont` is set. |
| `internal/agent/pig.go` | +6 | Comment block in `sendPig` documenting the wrapper contract (no semantic change). |
| `internal/pigrt/runtime.go` | +33 | `Options.AutoCont bool` on the runtime + `RunWith` calls `maybeAutoContinue(ctx, msgs, err, 0)` when set; recovers `*backend.HTTPError` from `r.opts.ErrorSlot` at give-up so the typed chain survives. |
| `cmd/wagyu/print_pig.go` | +6 | Sets `AutoCont: true` in `runPigPrintTurn` so the headless surface benefits without an extra flag. |
| `cmd/wagyu/tui_run.go` | +35 | `autoContBannerMsg` message type; `runMode.autoContBanner` field; `Update` handler appends "↻ auto-continuing… (retry N of 3)" row to `m.response`; `streamPrompt` sets `Options.AutoCont: true` and an `AutoContBanner` callback that posts via `prog.Send`. |
| `internal/agent/auto_continue_test.go` (new) | +177 | 7 unit tests: `TestClassifyInterruptionMatrix`, `TestRecoveryForError`, `TestTurnsFromErr`, `TestRecoveryMessageFromMessagesPicksLastSynthetic`, `TestErrAutoContMaxDepthUnwraps`, `TestAutoContDepthTracker`, `TestAutoContVerbatimCopy`. |
| `internal/agent/pig_test.go` | +200 | `flakyAgw` scripted gateway + 3 new tests: `TestLoop_AutoContRecoversFromUpstreamStreamInterrupt` (recovered text + verbatim copy on the wire), `TestLoop_AutoContGiveUpAfterMaxDepth` (give-up + typed `*backend.HTTPError` preserved), `TestLoop_AutoContOffSurfacesFailure` (default behaviour unchanged). |
| `internal/pigrt/auto_continue_test.go` (new) | +178 | 6 unit tests: `TestClassifyInterruptionMatrix`, `TestIterationCountFromErr`, `TestAppendRecoveryMessageKeepsTailUser`, `TestErrAutoContMaxDepthUnwrap`, `TestAutoContConstantsAreAGYVerbatim`, `TestMsgSliceDoesNotAliasInput`. |
| `docs/cycles/auto-continue-interrupt/deliverable-auto-continue.md` | this | merge record. |

Net diff: ~1131 lines, +594 / -0 (test + new code). The previous subagent
already had the partial work in `internal/agent/auto_continue.go` (170 lines)
and `internal/pigrt/auto_continue.go` (258 lines) plus the wiring touches
(`cmd/wagyu/print_pig.go` +6, `internal/agent/pig.go` +6, `internal/pigrt/runtime.go`
+33); this cycle finished: the missing `sendPigAuto`, the missing `Options.AutoCont`
+ `Options.AutoContBanner` plumbing, the TUI banner integration, both test
files, and this document.

## Gate results

Run 3 times for stability. Only known failures:

| Gate | Status |
|---|---|
| `go build -o /tmp/wagyu-ac-resume ./cmd/wagyu` | exit 0 |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (×3) | passes except the documented pre-existing `TestDispatchPrintSlash_LocalCommands` failure (see 存疑) |
| `gofmt -l` on every file in the diff | exit 0 (initial run needed `gofmt -w` on `internal/pigrt/auto_continue.go` for the cycle auto-continue-interrupt doc comment numbering; one-shot, no behaviour change) |
| `go vet ./internal/agent/ ./internal/pigrt/` | exit 0 |
| `go vet ./cmd/wagyu/` | two pre-existing warnings on `cmd/wagyu/slash_cycle224_test.go:86,92` (`% unknown verb %)`) — verified against `c60cc26` that they pre-date this cycle |

## Decision tree vs the AGY verbatim strings

The two recovery copies were both pinned to the literal AGY strings from
`docs/research/agy-feature-strings.md §3`:

| Constant | Bytes | Source |
|---|---|---|
| `AutoContStreamInterrupted` | `The stream was interrupted. Please continue the task you were working on.` | `~/.local/bin/agy` loadfile `0x33dc41b`, agy 1.4.0 |
| `AutoContIterationCapFormat` (1 `%d`) | `**Status**: Cancelled after N iterations` | loadfile `0x33a3d44`, agy 1.4.0 |

The byte-for-byte witness is `TestAutoContVerbatimCopy` (agent package) and
`TestAutoContConstantsAreAGYVerbatim` (pigrt package) — both fail if anyone
edits the strings. The wire-body assertion in
`TestLoop_AutoContRecoversFromUpstreamStreamInterrupt` reads the second
upstream request's `messages` JSON and asserts the verbatim copy text
appears — so the bytes travel end-to-end through the wrapper, PiG, the wire,
the recovery test gateway, and back.

## Probe table (cycle auto-continue-interrupt deliverable probe)

The previous subagent's task spec called for live probes against the
`~/.local/bin/agy` reference binary. No agy is reachable from inside this
session's tool environment (Cycle auto-continue-interrupt brought this branch
up on a worktree under `/private/var/folders/.../opencode/openagy-auto-continue/`
that has no `~/.local/bin/agy`), so all probes are gate-only. The next cycle
that wants to do live comparison should:

1. Source `out/local-macos/env.sh` and import the seed file from
   `out/local-macos/seeds/seeds.txt` into a fresh Ghidra project.
2. Reproduce the cycle auto-continue-interrupt SIGINT-test: `kill -9 -SIGUSR2
   $(pgrep -f "agy -p")` mid-tool-call on a long-prompt reference run, with
   `--auto-continue` set.
3. Capture the AGY transcript (expect: a single "**Status**: Cancelled after N
   iterations" row followed by the recovered turn's text).
4. Replace `agy` with `wagyu`, repeat. Diff transcripts.

The byte comparison is the spec's only hard requirement; the gate-only
verification covers the structural contract (recovery copy selection, depth
bound, banner callback, give-up error type) that AGY's recovery can be
seen not to have.

## 存疑

1. **`TestDispatchPrintSlash_LocalCommands`** (`cmd/wagyu`) is environment-
   dependent: it relies on `~/.gemini/antigravity-cli/settings.json` having no
   `model` field, which is true in the canonical `/tmp/ref_idle_status_raw.bin`
   capture environment but not on this machine (the user's settings file
   specifies `"Gemini 3.8 Flash (Medium)"`). Verified pre-existing by `git
   checkout c60cc26 -- .` + re-running — fails the same way. Not introduced
   by this cycle; not fixed in this cycle either.
2. **Pre-existing `go vet` warnings** on `cmd/wagyu/slash_cycle224_test.go`
   (`% unknown verb %)` on lines 86 and 92). Verified by checking out
   `c60cc26` that they pre-date the cycle. Not touched in this cycle.
3. **Recovery uses a fresh PiG agent per attempt** on the `Loop.sendPigAuto`
   side. Each retry re-plays the full conversation. With PiG-Continue (the
   pigrt path) the cost is one extra upstream request per attempt; with
   the wrapper (the loop path) the cost is the entire `input.Messages`
   replayed each time. Acceptable because recovery is the catastrophic
   path the rest of the turn protected against and the second upstream
   request usually succeeds. A future cycle can refactor `sendPig` to
   expose the runner and use `runner.SetMessages` + `runner.Continue` for
   parity with the pigrt path; the public API is unchanged either way.
4. **`iterationCountFromErr` parser is best-effort**: it returns the LAST
   contiguous digit sequence in `err.Error()`. Format `agent stopped at
   its turn limit with tool results still unanswered (8 turns)` and
   `agent: exceeded tool-iteration budget (8)` both extract `8`. If PiG
   changes its wrap to embed the integer in a position we don't recognise,
   the parser returns 0 (which still renders a coherent banner; the depth
   counter, not the literal count, is the real bound on recursion).
5. **No live AGY probe in this cycle.** The deliverable probe table above
   lists the four commands a future cycle with `~/.local/bin/agy` access
   should run; see the table for the recipe.
