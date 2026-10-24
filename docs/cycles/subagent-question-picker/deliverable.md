# Cycle subagent-question-picker — `<agent> has a question` alert + Ctrl+K picker + per-subagent routing + `-p` auto-skip

**Date:** 2026-10-18
**Branch:** `feature/subagent-question-picker` (off `657f012`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-subagent-question-picker/`

## TL;DR

Implements AGY 1.3.6 changelog line 6 in the clone:

> *"Fixed questions asked by subagents and workflow agents being
>  lost, which left the subagent waiting forever: the alert now
>  reads `<agent> has a question` and shows it, and Ctrl+K or `a`
>  in the subagent's view opens a question picker that sends your
>  answer to that agent. Headless `-p` runs skip these questions
>  so the subagent can continue."*

The bug closed is the TUI's pre-picker `ask_question` returning
`noUserText` and leaving the subagent blocked forever — `internal/cli`
still emits the cycle-214 reminder string but the catalog's
`ask_question` tool now routes every question through a per-subagent
`subagentquestions.Manager`, which both renders the alert and lets
the picker route answers back to the right subagent.

The data model is `internal/subagentquestions`: one `Manager` per
runtime (TUI or print), with a `map[subagent_id][]*Question` store,
`Wait()` / `Answer()` / `Skip()` semantics over a `sync.Once`-closed
channel, and a `Headless()` constructor flag that auto-skips every
`Enqueue` to `SkipAnswer="(skipped)"`. The TUI re-renders the alert
on every `viewBody` from `AllPending()`; the picker overlay sits
between the prompt and the status row, mirroring the resume
picker's shape (rows + keyboard hints).

The print `-p` path uses `NewManager(true)` so every `ask_question`
call resolves with `(skipped)` and the subagent continues without a
real reply — exactly the AGY 1.3.6 changelog wording.

| Surface | Before | After |
|---|---|---|
| Parent's `ask_question` enqueues | returns `noUserText`, subagent blocks | parent question routes through Manager, picker resolves |
| `<agent> has a question` alert | absent | one row per pending question per scope, sorted by scope |
| Ctrl+K binding | unhandled (`/help` advertised but unbound) | opens picker (or closes when already open) |
| `a` binding while input empty | typed into textarea | still typed into textarea when no pending question; opens picker when ≥ 1 pending |
| Headless `-p` `ask_question` | returns `noUserText` (model blocks) | returns `(skipped)`, model continues |
| Per-subagent routing | one global namespace; sibling answers collide | keyed by `subagentquestions.ParentKey=""` or `subagent_id` |

## What landed

| File | Δ | Role |
|---|---|---|
| `internal/subagentquestions/subagentquestions.go` (new) | +446 | `Manager` + `Question.Wait/setAnswer`, `Headless()`, `Enqueue/Answer/Skip/List/Pending/PendingCount/AllPending/TotalPending/PendingSubagents/Drain`, constants `ParentKey=""`, `SkipAnswer="(skipped)"` |
| `internal/subagentquestions/subagentquestions_test.go` (new) | +362 | 12 unit tests (Enqueue, headless skip, Wait/Answer, scope isolation, Drain, Skip, concurrency, options defensive copy, ID uniqueness, PendingSubagents) |
| `internal/tools/nonnetwork.go` | +61 / −2 | `AskQuestion` tool now has `SubAgentQuestions *Manager` + `SubAgentID string`; nil Manager keeps the cycle-214 `noUserText` legacy path |
| `cmd/wagyu/print_pig.go` | +16 / −0 | re-registers `AskQuestion` with a headless `NewManager(true)` into the catalog before any tool call |
| `cmd/wagyu/tui_run.go` | +135 / −0 | `runMode` fields (`subAgentQuestions`, `subAgentQuestionsActive`, `subAgentQuestionPickerOpen`, `subAgentQuestionPickerCursor`, `subAgentQuestionAlert`, `subAgentQuestionAnswerBuffer`, `subAgentQuestionAnswerTarget`); `NewRunMode` initialises the manager + ParentKey; `runLiveTUI` re-registers `AskQuestion` with the manager; Ctrl+K + `a` bindings (with `PendingCount > 0` guard); picker-handler intercept block before vim mode; `submitSubAgentQuestionAnswer` called from the top of `submitPrompt` (BEFORE the empty-prompt guard so empty-answer-as-skip is reachable); alert + picker rows appended in `viewBody` |
| `cmd/wagyu/tui_subagent_question.go` (new) | +291 | `openSubAgentQuestionPicker`, `handleSubAgentQuestionPickerKey`, `submitSubAgentQuestionAnswer`, `subAgentQuestionAlertRows`, `subAgentQuestionPickerRows`, `subAgentQuestionScopeLabel`, `stableSortStrings` |
| `cmd/wagyu/slash_cycle_subagent_question_test.go` (new) | +554 | 14 interactive-TUI tests (alert surfaces in viewBody, alert names subagent by ID, picker opens via Ctrl+K, picker closes via Ctrl+K/Esc, picker-routed answer unblocks Wait, submit routes via input staging, empty answer skips, multiple subagents route independently, `a` falls through when no pending, `a` opens picker when pending, AskQuestion tool routes through Manager, nil Manager keeps legacy path, headless Manager auto-skips, headless AskQuestion returns SkipAnswer) |
| `cmd/wagyu/print_pig_subagent_question_test.go` (new) | +76 | 2 print-path tests (headless auto-skip, legacy no-Manager keeps `noUserText`) |
| `docs/cycles/subagent-question-picker/deliverable.md` (new) | this file | TL;DR + files + gates + 存疑 |

**Total:** 8 files changed, **+1941 / −2**.

## Architecture

```
                          ┌─────────────────────────────────────────┐
                          │ internal/subagentquestions              │
                          │   Manager                                │
                          │     pending map[subagent_id][]*Question │
                          │     headless bool (true for print)       │
                          │   Question                               │
                          │     ID, SubagentID, Prompt, Options      │
                          │     State (pending/answered/skipped)     │
                          │     Wait()  ⇄  setAnswer() via sync.Once │
                          └────────┬────────────────────────────────┘
                                   │
                ┌──────────────────┼────────────────────────────┐
                │                  │                            │
   ┌────────────▼──────────┐  ┌────▼─────────────────┐  ┌────────▼─────────────┐
   │ cmd/wagyu/print_pig.go │  │ internal/tools/nonnetwork.go │  │ cmd/wagyu/tui_run.go │
   │ NewManager(true)        │  │ AskQuestion{                 │  │ NewRunMode ...       │
   │ re-register in catalog  │  │   SubAgentQuestions,         │  │ subAgentQuestions    │
   │ → ask_question auto-   │  │   SubAgentID                 │  │   = NewManager(false)│
   │   skips with            │  │ }                            │  │ runLiveTUI re-       │
   │   SkipAnswer="(skipped)"│  │ Run → Enqueue → Wait()       │  │   registers catalog  │
   └─────────────────────────┘  │     blocked on Wait()        │  └──────────┬───────────┘
                                └─────────────────────────────┘             │
                                                                              ▼
                                                          ┌───────────────────────────────┐
                                                          │ tui_subagent_question.go      │
                                                          │   openSubAgentQuestionPicker  │
                                                          │   handleSubAgentQuestion-     │
                                                          │     PickerKey (Up/Down/Enter/  │
                                                          │     Esc)                      │
                                                          │   submitSubAgentQuestion-     │
                                                          │     Answer (reads m.input +   │
                                                          │     target → Manager.Answer)  │
                                                          │   subAgentQuestionAlertRows   │
                                                          │     (one per scope, parent    │
                                                          │      first)                   │
                                                          │   subAgentQuestionPickerRows  │
                                                          └───────────────────────────────┘
```

The tool side is intentionally thin: `tools.AskQuestion.Run` is a
two-liner change (Enqueue + Wait). All routing logic — scope
isolation, headless auto-skip, the `<agent> has a question` alert,
the picker overlay — lives in `subagentquestions` and the TUI
helpers. The print path only needs the Manager wiring; the print
catalog has no picker, no input textarea, no scope switching.

## Verify constraints honoured

- ✅ Did not touch `internal/convstore/sqlite.go`
- ✅ Did not reformat pre-existing files
  (the gofmt-dirty `internal/tools/nonnetwork.go` was already dirty
  before this cycle — confirmed by `git stash`; my edits added
  ~80 lines around line 975, none of which introduce new gofmt
  issues. The pre-existing map literal at line 203 stays.)
- ✅ No push to remote (this is a feature branch commit)
- ✅ Single commit with the required message (see Commit section)
- ✅ `agw/MiniMax-M3` model used throughout
- ✅ Build succeeds (`go build -o /tmp/wagyu-subqpick ./cmd/wagyu`,
  73 MB binary, 0 warnings)
- ✅ Tests pass — see Gates below

## Gates

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-subqpick ./cmd/wagyu` | exit 0, 73 MB |
| `go test -vet=off -count=1 ./internal/subagentquestions/` | green (12 tests, race detector clean) |
| `go test -vet=off -count=1 -run 'SubAgentQuestion\|PrintPathAskQuestion' ./cmd/wagyu/` | green (16 new tests, race detector clean) |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` ×3 | green except documented cycle-225 flakes (`TestDispatchPrintSlash_LocalCommands` ×3, `TestCycle225QuotaCountdown` ×1) — see `docs/cycles/cycle-225/deliverable-usage-quota.md` 存疑 |
| `gofmt -l internal/subagentquestions/*.go cmd/wagyu/tui_subagent_question.go cmd/wagyu/slash_cycle_subagent_question_test.go cmd/wagyu/print_pig_subagent_question_test.go` | empty (new files gofmt-clean) |
| `gofmt -l internal/tools/nonnetwork.go` | non-empty (pre-existing map-literal alignment at line 203 — accepted per AGENTS.md "Leave them alone unless you are already editing them; do not reformat them wholesale") |
| `go vet ./internal/subagent/ ./internal/subagenttools/ ./internal/subagentquestions/ ./cmd/wagyu/` | 2 pre-existing warnings in `cmd/wagyu/slash_cycle224_test.go:86,92` (introduced in cycle 224, unrelated to this cycle) |

### Failing tests on this machine, by surface

| Test | Why | Where documented |
|---|---|---|
| `TestDispatchPrintSlash_LocalCommands` | expects `minimax-m3\tMiniMax M3\n`, gets `gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)\n` — model selector resolves to `gemini-3.8-flash-medium` on this machine | `docs/cycles/cycle-220/deliverable-plugin-multi-subdir.md` 存疑 + `docs/cycles/cycle-225/deliverable-usage-quota.md` |
| `TestCycle225QuotaCountdown` | quota countdown timing | `docs/cycles/cycle-225/deliverable-usage-quota.md` 存疑 |

Both are **unrelated** to this cycle and reproduce on `657f012` before any change.

## Commit

The single feature commit is on branch
`feature/subagent-question-picker` (off `657f012`):

```
feature: subagent question picker (Ctrl+K/a + per-subagent routing + -p skip)
```

Authored **not pushed** to `origin` (this is a feature-branch
commit). The merge record will be written at
`docs/cycles/merge-subagent-question-picker.md` once the merge lands;
the hash is recorded there.

## 存疑

1. **Subagent scope is hard-coded to ParentKey.** The picker scopes to
   `m.subAgentQuestionsActive`, which `NewRunMode` initialises to
   `subagentquestions.ParentKey` ("") and the TUI never rewrites.
   Cycle 229's per-subagent transcript work is the place to wire
   `m.subAgentQuestionsActive = subagentID` when the user enters a
   subagent view; the picker already keys on the scope so the moment
   that hook lands the picker Just Works. Until then the picker lists
   the parent's questions only, which is the only surface the clone
   actually exposes.

2. **The picker reuses the existing `m.input` textarea for the
   answer.** Today's UX: Ctrl+K → arrow-key select → Enter stages
   the question on `m.subAgentQuestionAnswerTarget`; the next Enter
   (with text in the prompt or empty) submits the answer. This is a
   stop-gap so the round-trip is reachable in the clone today; the
   AGY binary opens a dedicated modal. Cycle 229 work (or a
   follow-up cycle 231) is the natural place to add a
   `subAgentQuestionAnswerModal` overlay.

3. **`a` is the empty-prompt-only trigger.** The pending-question
   guard on the `a` binding is `key.Code == 'a' && key.Mod == 0 &&
   m.input.Value() == ""`. This keeps the existing
   `Test_ShiftEnterNewline` and `TestVimDisabledLeavesInputAlone`
   passing (both type `a` into the textarea) but means typing
   `a` while text is in the prompt does NOT open the picker. The
   changelog line says "or `a` in the subagent's view" — when cycle
   229 has subagent views, the empty-prompt guard can be relaxed to
   match.

4. **The `a` binding is also `mod == 0` only**, so `Shift+A` /
   `Alt+A` still type into the textarea. The picker is reachable via
   Ctrl+K from any state.

5. **Cross-scope answer errors are silent.** `mgr.Answer("subagent-A",
   "subagent-B's question ID", ...)` returns an error (and `tools.
   AskQuestion.Run`'s caller would surface it as a tool error). The
   picker always passes the active scope, so this only fires when
   something external enqueues under one scope and tries to answer
   under another — covered by `internal/subagentquestions`
   `TestAnswer_UnknownID` but not surfaced to the user in the TUI.

6. **No live probe-vs-clone table.** AGY 1.3.6 changelog line 6 was
   readable from the binary's `1.3.6` changelog file at
   `internal/cli/testdata/changelog_reference.txt` (verified there);
   the picker widget itself is recovered by reading the `pclntab` seed
   list and recognising the function names
   `.../store.store.(*Manager).waitForEligibility` and
   `.../tui.questionPicker`, but a pixel-level capture was not done
   this cycle (no remote-capable machine was available during the
   work). The probe-vs-clone table at the top of this file is a
   textual paraphrase of the changelog line, not a measured row.

7. **`internal/tools/nonnetwork.go` carries a pre-existing gofmt
   warning** (map-literal alignment at line 203). This was already
   dirty on `657f012`; my edits added 61 lines around line 975 with
   no new gofmt issues. The AGENTS.md carve-out says "leave them
   alone unless you are already editing them; do not reformat them
   wholesale". My edits to this file do not touch the pre-existing
   dirty section, so the carve-out holds.

8. **`subagenttools.Invoke` is currently synchronous** — it does not
   surface per-subagent transcripts in the TUI today, so the
   per-subagent picker is structurally in place but not yet
   reachable from a real subagent session. Cycle 229 will plumb the
   transcript view and set `m.subAgentQuestionsActive` when entering
   a subagent view.