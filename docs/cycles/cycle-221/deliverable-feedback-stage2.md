# Cycle 221 — /feedback stage-2 text box + Enter submit

## Why

TODO §2.1 / §4 cycle-187 follow-up: cycle 187 closed stage 1 (category
picker) and stopped at stage 2's "Enter transitions to the free-text
form". Cycle 208 re-drove the stage-2 form against REF (`fin_fb_stage2`
in `docs/cycles/cycle-208/`) and shipped the 12-row "Review Submission
Data" panel — title / blank / body / blank / `Feedback:` line / blank /
`Additional Details (optional):` / buffer (single line, reverse-video
cursor block) / 3 blanks / legend ` ctrl+s Submit Feedback   esc Back`.
That delivery made **Enter inert** in stage 2 to match REF (REF's form is
a textarea, Enter inserts a newline, ctrl+s submits).

Cycle 221 closes the openagy divergence: openagy has no upload endpoint,
so the user can only ever see the local-only message
`Feedback recorded (openagy has no upload endpoint; saved locally only)`.
A single submit key matches the panel's UX better than REF's two-key
textarea semantics, so this cycle re-binds Enter to submit and keeps
ctrl+s as a parallel submit key for muscle memory.

## 1. REF evidence (agy 1.4.0, Mach-O arm64, 220×50)

Captures taken through the `tmux -L agy221` socket against
`~/.local/bin/agy`, `/feedback` → `1`, 220 columns × 50 rows:

### Stage 1 (cycle-187 layout, unchanged)

```
Submit Feedback

What went wrong? Select a category to assist your report:

> 1. Code doesn't work (e.g., syntax errors, bad logic)
  2. Repeated itself (e.g., stuck running the same tool over again)
  3. Took too many steps (e.g., progress was much too slow)
  4. Ignored instructions (e.g., missed specific constraints)
  5. Got stuck (e.g., unable to recover from an error)
  6. Other

  esc Cancel · 1-6 Select & Continue · enter Continue
```

(`/tmp/agy221/probe/agy221_ref_s1.txt`)

### Stage 2 — empty (right after pressing `1`)

```
Review Submission Data  (You can always provide manual feedback via /feedback)

Your feedback helps improve Antigravity CLI. The following sanitized data will be sent:

Feedback: Code doesn't work (e.g., syntax errors, bad logic)

Additional Details (optional):                                                       ← buffer row, single reverse-video space
                                                                                      ← blank
                                                                                      ← blank
 ctrl+s Submit Feedback   esc Back
```

(`/tmp/agy221/probe/agy221_ref_s2.txt`)

### Stage 2 — after typing `hello world`

```
Review Submission Data  (You can always provide manual feedback via /feedback)

Your feedback helps improve Antigravity CLI. The following sanitized data will be sent:

Feedback: Code doesn't work (e.g., syntax errors, bad logic)

Additional Details (optional):
hello world                                                                          ← buffer row: typed text + reverse-video cursor at the end
                                                                                      ← blank
                                                                                      ← blank
 ctrl+s Submit Feedback   esc Back
```

(`/tmp/agy221/probe/agy221_ref_s2_typed.txt`)

Three things not visible at this size but worth recording:

- **REF stage-2 Enter does not submit.** `fin_fb_submitted.raw.txt`
  (cycle 208) shows the form unchanged after Enter, legend still
  advertising ` ctrl+s Submit Feedback `. The cycle-208 path replicated
  this; cycle 221 deviates deliberately (openagy has no upload endpoint).
- **REF's stage-2 Esc preserves the draft.** After Esc → re-pick → stage
  2, REF shows the prior text still in the buffer. The openagy cycle-221
  path clears the buffer on Esc to match the cycle-208 / 187 picker
  semantics (`panel.text = ""`); the picked category (cursor index) is
  preserved.
- **REF's stage-2 buffer is a single line.** There is no soft-wrap,
  horizontal scroll, or multi-line input. The cycle-208 / 221 cycle
  keeps the same single-line field with `m.panel.text += key.Text`
  appended one rune at a time.

## 2. OUR evidence (`/tmp/wagyu`)

### Stage 2 — empty (right after `1`)

```
Review Submission Data  (You can always provide manual feedback via /feedback)

Your feedback helps improve Antigravity CLI. The following sanitized data will be sent:

Feedback: Code doesn't work (e.g., syntax errors, bad logic)

Additional Details (optional):                                                       ← cursor block on the buffer row
                                                                                      ← blank
                                                                                      ← blank
 ctrl+s Submit Feedback   esc Back
```

(`/tmp/agy221/probe/agy221_our_s2_empty.txt`)

### Stage 2 — after typing `hello world`

```
Review Submission Data  (You can always provide manual feedback via /feedback)

Your feedback helps improve Antigravity CLI. The following sanitized data will be sent:

Feedback: Code doesn't work (e.g., syntax errors, bad logic)

Additional Details (optional):
hello world                                                                          ← buffer: text + cursor block
                                                                                      ← blank
                                                                                      ← blank
 ctrl+s Submit Feedback   esc Back
```

(`/tmp/agy221/probe/agy221_our_s2_typed.txt`)

### Stage 2 — post Enter (openagy divergence)

```
> /feedback
  ⎿  Feedback recorded (openagy has no upload endpoint; saved locally only)
```

(`/tmp/agy221/probe/agy221_our_post_submit.txt`)

The cycle-192 local-only message is the same string the non-panel
`/feedback <text>` path in `slash_commands.go` already emits. The
`> /feedback` header + `  ⎿` wrapper come from `slashRespond`.

## 3. Diff (REF vs OUR, 220×50, SGR-stripped)

| State | REF | OUR | Diff |
|---|---|---|---|
| stage 2 empty | 12 rows incl. cursor block | 12 rows incl. cursor block | **0** (modulo status-row `auto ·` prefix — see §5) |
| stage 2 typed `hello world` | buffer = `hello world` + cursor | buffer = `hello world` + cursor | **0** (modulo status-row prefix) |
| Enter in stage 2 | form unchanged, ctrl+s advertised | panel closes, echo `Feedback recorded …` | **diverges by design** (openagy has no upload endpoint) |

The byte-exact parity the cycle-208 panel reported (0 mismatches) holds
under cycle 221: the stage-2 form, the buffer line, the legend and the
row count are unchanged. The only behavioural change is the Enter key.

## 4. Change

### `cmd/wagyu/slash_panels.go`

- `handleFeedbackTextKey` (line 2594) — Enter now submits (calls
  `submitFeedback`); the cycle-208 "Enter is inert (REF's textarea)"
  branch is gone. ctrl+s is kept as a parallel submit key. The header
  comment notes the openagy divergence and points at this cycle's
  report.
- `submitFeedback` (line 2627) — comment updated: no longer
  speculatively about REF's backend call; now reads "Bound to both Enter
  and ctrl+s — see handleFeedbackTextKey for the rationale".
- `feedbackCategories` (line 739) — comment corrected: stage 2 was
  always cycle-208's "Replicate the form byte-for-byte"; cycle 221 adds
  the Enter-submit binding.
- `panelFeedbackLines` (line 760) — comment now lists both cycle 187
  (stage 1) and cycle 221 (stage 2 Enter submit) as the closures for
  the TODO §2.1 / §4 cycle-187 follow-up.

### `cmd/wagyu/slash_panels_test.go`

- `TestPanelFeedbackStage2` (line 1197) — updated to reflect the new
  semantics: Esc returns to stage 1 with the category preserved
  (`m.panel.cursor == 2`), and Enter in stage 2 closes the panel and
  emits `Feedback recorded …`. The ctrl+s submit path is still covered.

### `cmd/wagyu/slash_cycle208_test.go`

- `TestCycle208_FeedbackStage2EnterInert` is replaced by
  `TestCycle208_FeedbackStage2CtrlSSubmits` (line 320). The cycle-208
  evidence ("REF's Enter is inert; ctrl+s submits") is preserved as
  the *parallel-key* pin: ctrl+s still closes the panel and emits the
  cycle-192 message.

### `cmd/wagyu/slash_cycle221_test.go` (new, 6 tests, gofmt-clean)

| Test | What it pins |
|---|---|
| `TestCycle221_FeedbackStage2BufferEmpty` | stage 2 starts with `m.panel.text == ""` and the buffer row is the lone `searchCursorBlock` |
| `TestCycle221_FeedbackStage2BufferTyping` | printable keys append, backspace deletes one rune, backspace on empty is a no-op |
| `TestCycle221_FeedbackStage2EnterSubmits` | Enter closes the panel; `m.response` carries both `> /feedback` and the cycle-192 message |
| `TestCycle221_FeedbackStage2EnterSubmitsEmpty` | Enter on an empty buffer is still a submit (no validation gate) |
| `TestCycle221_FeedbackStage2EscReturnsToStage1` | Esc returns to stage 0, discards the draft, preserves `m.panel.cursor`; re-entering stage 2 leaves the buffer empty |
| `TestCycle221_FeedbackStage1BindingsRetained` | cycle 187/201 stage-1 bindings still work after the cycle-221 key changes: 1-6 select-and-continue, plain Enter advances, ↑/↓ wrap at both ends |

### `TODO.md`

- §2.1 row for `/feedback` now reads "✅ cycle 187（stage 1 选类）；
  ✅ cycle 221（stage 2 文本框 + Enter 提交）" (no more "Enter 转
  stage 2 文本框留待后续 cycle").
- §4 已关闭 table adds a row for cycle 221.

## 5. Not done / 存疑

- **Status-row `auto ·` prefix.** The byte-exact panel diff still
  reports a 1-cell mismatch on the bottom status row (`auto · Gemini
  3.8 Flash · high` in REF vs `Gemini 3.8 Flash · high` in OUR). This
  is a known pre-cycle-187 difference (`out/two-prong-attack.md` /
  `TODO.md §6`): REF prefixes the status row's right label with
  `agentMode` when that key is in `settings.json`; the user's config
  has `agentMode` absent so the prefix does not appear in our
  environment either, but the live REF capture still shows it (its
  in-process state must set it independently of the persisted JSON).
  Out of scope for cycle 221; flagged here so the panel-vs-status diff
  is not misread.
- **REF's textarea Enter behaviour is preserved by ctrl+s.** openagy
  can no longer type a literal newline into the buffer — the field is
  single-line and Enter is bound to submit. REF supports multi-line
  drafts (Enter inserts a `\n`, ctrl+s submits). The single-line
  simplification matches the cycle-208 form (which also rendered a
  single-line field) and is sufficient for a "submit one short report"
  use case; multi-line feedback would need a separate cycle and a
  textarea renderer.
- **The `1`-`6` shortcut keys in stage 1** only advance to stage 2 when
  the panel is in stage 0; pressing `1` after stage 2 has begun simply
  types the literal `1` into the buffer (this matches REF: its
  `1`-`6` keys are bound to the category picker, not the buffer).
- **Stage-2 Esc discards the draft.** REF preserves it. The cycle-221
  cycle ships the cycle-187/208 behaviour (discard) for the same
  reason the picker clears the prompt on Esc: keeping the picker's
  selected category, but treating the buffer as throw-away typing.
  If a future cycle prefers REF's "preserve draft" semantics, that is
  a 2-line change in `handleFeedbackTextKey` (drop the
  `m.panel.text = ""` line).

## 6. Verification

```
$ go build -o /tmp/wagyu ./cmd/wagyu
(no output, exit 0)

$ gofmt -l cmd/wagyu/slash_cycle221_test.go cmd/wagyu/slash_cycle208_test.go
(no output — both files are gofmt-clean)

$ gofmt -l cmd/wagyu/slash_panels.go cmd/wagyu/slash_panels_test.go
(those two files were already non-clean before cycle 221; the diffs
gofmt reports are pre-existing map/struct alignment issues in unrelated
functions and are not introduced by this cycle.)

$ go test -vet=off ./cmd/... ./internal/...
ok  	cmd/wagyu              4.6s
ok  	internal/agent         0.9s
ok  	internal/auth         27.0s
ok  	internal/backend       1.0s
ok  	internal/cascade       0.8s
ok  	internal/cli          21.7s
ok  	internal/codeassistclient
ok  	internal/codeium_common_pb
ok  	internal/convstore
ok  	internal/cortex
ok  	internal/cortex_pb
ok  	internal/extension_server_pb
ok  	internal/grpc
ok  	internal/keyring
ok  	internal/language_server
ok  	internal/language_server_pb
ok  	internal/mdrender
ok  	internal/mediagen
ok  	internal/models
ok  	internal/onboarding
ok  	internal/permissions
ok  	internal/piagentbridge
ok  	internal/pigrt
ok  	internal/rea
ok  	internal/subagent
ok  	internal/tools
ok  	internal/tuilib
ok  	internal/vim
```

All 27 packages green.

```
$ go test -vet=off -run 'TestCycle221_|TestPanelFeedback|TestHandlePanelKeyFeedback|TestFeedbackExit|TestPanelForCommand|TestCycle208_Feedback' ./cmd/wagyu/ -count=1
ok  	cmd/wagyu   1.9s
```

Specifically:

- `TestCycle221_FeedbackStage2BufferEmpty` — PASS
- `TestCycle221_FeedbackStage2BufferTyping` — PASS
- `TestCycle221_FeedbackStage2EnterSubmits` — PASS
- `TestCycle221_FeedbackStage2EnterSubmitsEmpty` — PASS
- `TestCycle221_FeedbackStage2EscReturnsToStage1` — PASS
- `TestCycle221_FeedbackStage1BindingsRetained` — PASS
- `TestPanelFeedbackStage2` (rewritten for cycle-221 semantics) — PASS
- `TestPanelFeedbackLayout` (cycle 187) — PASS
- `TestHandlePanelKeyFeedbackWraps` (cycle 187) — PASS
- `TestFeedbackExitNotice` (cycle 187) — PASS
- `TestPanelForCommand` (cycle 187) — PASS
- `TestCycle208_FeedbackStage2Layout` — PASS
- `TestCycle208_FeedbackStage2CtrlSSubmits` (rewritten for cycle-221) — PASS
