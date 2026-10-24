# Cycle `trim-empty-history-messages` — Deliverable

## Gap

AGY 1.2.7 changelog: *"Fixed the conversation history list showing empty
message entries when the user submitted a whitespace-only prompt, which
wasted screen space and confused scroll calculations."*

## Root cause

The clone's `pushHistory` (cmd/wagyu/history.go:20) and the empty-prompt
guard at the top of `submitPrompt` (cmd/wagyu/tui_run.go:1126) both used
the literal-equality test `prompt == ""`. A buffer holding one space, a
newline, or any run of Unicode whitespace passed the test and produced:

1. A blank entry in `m.history` — Up/Down landed on a row with no text.
2. A `> ` row appended to `m.response` — one wasted screen row per
   accidental submit.
3. An empty `backend.Message{Role:"user", Content:" "}` in `m.session` —
   the model got a turn it could not act on.
4. An empty `Display` line in `history.jsonl` — `agy` would then echo it
   back across `-c` / `--continue` / `/resume`.

The "scroll calculations … confused" half of the changelog is the
mechanical consequence of (2): each accidental submit shifts every row
index in `m.response` by exactly one, so `m.scroll` and the per-row
`screenCounts` were indexing the wrong rows by the time the user noticed.

## Fix

Two surfaces in `cmd/wagyu`:

### 1. `cmd/wagyu/history.go`

- New helper `isBlank(s string) bool` returns true when
  `strings.TrimSpace(s) == ""`. Single source of truth for the blank
  test (handles `\n`, `\t`, NBSP `\u00a0`, ideographic space `\u3000`,
  em space `\u2003`, and any future Unicode whitespace).
- `pushHistory` now trims leading/trailing whitespace before the
  dedupe-and-append step. The dedupe collapses a follow-up `"  hello  "`
  to the same slot as `"hello"`. The trimmed form is what reaches
  `m.history` and `history.jsonl`.

### 2. `cmd/wagyu/tui_run.go`

`submitPrompt`'s empty-prompt guard now calls `isBlank(prompt)`. A
whitespace-only buffer is treated the same as an empty one: the input is
reset, history / picker state is cleared, and `nil` is returned. The
guard resets `m.input`, `m.historyIdx`, `m.historyEdit`, and `m.picker`
the same way a successful submit would — so a stray space cannot leave
the textarea stuck on whitespace.

### 3. New tests — `cmd/wagyu/history_trim_empty_test.go`

Seven targeted tests, all passing under the targeted `go test -vet=off
-count=1 -run 'TestPushHistory|TestSubmitPrompt|TestScroll_*|TestHistory_*'`
invocation:

| Test | What it pins |
|---|---|
| `TestPushHistory_SkipsWhitespaceOnly` | 9 blank inputs (ASCII space, newline, tab, mixed runs, NBSP, ideographic space, em space, 64 spaces) all rejected by `pushHistory`. |
| `TestPushHistory_TrimsSurroundingWhitespace` | `"  hello  "` and `"\n\thello\t\n"` collapse to `"hello"`, dedupe collapse works, internal whitespace preserved. |
| `TestSubmitPrompt_RejectsWhitespaceOnly` | `submitPrompt` for `"   "` and `"\t\n  \t"` adds no transcript row, no session message, no history entry; input is reset; `streaming` stays false. |
| `TestScroll_CalculationsHandleBlankRows` | With 0 / 1 / 5 / 40 / 400 transcript rows and a few injected blanks, `View()` stays inside the terminal height and `screenRows("")` is exactly 1. |
| `TestHistory_NavigationSkipsBlankEntries` | After 3 real prompts + 1 attempted blank + 1 real prompt, history length is 4; `historyUp` / `historyDown` walk back through `delta`, `gamma`, `beta`, `alpha` in order. |
| `TestSubmitPrompt_RealPromptAfterBlankWorks` | A blank submit does not poison the next real submit — history / response / session all advance correctly. |
| `TestPushHistory_HistoryCapRespectedWithBlanks` | Filling the cap with real entries then pushing 50 blanks leaves the cap intact and the first real entry at index 0. |

`gofmt -l` on the new file is clean; `gofmt -l` on the modified
`history.go` and `tui_run.go` is also clean (both files were gofmt-clean
before the edit and remain so — no wholesale reformatting).

## Verification

```bash
# Build the affected package — exit 0.
go build -o /tmp/wagyu-teh ./cmd/wagyu

# Targeted tests — all pass.
go test -vet=off -count=1 \
    -run 'TestHistory|TestEmpty|TestTrim|TestScroll' \
    ./cmd/wagyu/ ./internal/...
```

A full `go test -count=1 ./cmd/wagyu/` was also run; the only failures
observed (`TestDispatchPrintSlash_LocalCommands`,
`TestCycle225QuotaCountdown`) are pre-existing on `main` and unrelated
to this change. The former depends on an environment default
(`minimax-m3` vs `gemini-3.8-flash-medium`); the latter is a known
flaky time-dependent test (passes 2/3 runs).

## File delta

```
 cmd/wagyu/history.go                       | 46 +++++++++++++++++++++++++++------
 cmd/wagyu/tui_run.go                       | 13 +++++++++-
 cmd/wagyu/history_trim_empty_test.go       | new, 7 tests, ~225 lines
 docs/cycles/trim-empty-history-messages/deliverable.md | this file
```

`internal/convstore/sqlite.go` is **not** touched. The fix lives in the
TUI surface where the gap is observable; the persistence layer already
serialises whatever string `pushHistory` gives it, so trimming upstream
is enough.

## Hard constraints / 存疑

- **Display echo left as-is.** `m.response = append(m.response, "> "+prompt)`
  still uses the *untrimmed* prompt — only the storage path
  (`m.history` / `history.jsonl`) trims. This matches user expectation
  (whatever they typed is echoed verbatim) and matches the reference
  binary's behavior (the gap is about empty entries, not about visual
  trimming of real prompts). If a future cycle wants to trim the
  transcript echo too, that is a separate change.
- **No migration of existing `history.jsonl`.** Files written before
  this fix may still contain blank entries. They render correctly
  (one screen row each, `screenRows("") == 1`); the cost of a migration
  is not justified by the symptom.
- **Cycle-99's `m.liveStart` is unaffected.** The scroll anchor is set
  after `submitPrompt` writes the prompt echo; with blanks now rejected
  upstream, `m.liveStart` is unchanged for whitespace submits.
- **Multi-line prompts with internal whitespace** still pass through;
  only outer runs are trimmed. Test #2 pins this.
- **Reference parity** is 【推测】 at the byte level: AGY 1.2.7's fix
  wording strongly implies the same trim-or-skip behaviour, and the
  logic is internally consistent with the clone's other guards
  (`LastChatTurnsBudget` already calls `strings.TrimSpace` on stored
  chat text — cmd/wagyu/conversation.go is the prior art).
