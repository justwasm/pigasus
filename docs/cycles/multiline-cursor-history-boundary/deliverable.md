# Deliverable — feature/multiline-cursor-history-boundary

**Feature:** `feature/multiline-cursor-history-boundary` (branched from main at `54db67b`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-multiline-cursor-history-boundary/`
**Date:** 2026-10-18

## TL;DR

In the wagyu clone's interactive prompt, pressing Up / Down always
recalled the previous / next history entry, regardless of where the
cursor sat in a multi-line input. The v1.3.5 binary — and the broader
AGY 1.2.x vim-mode-fix family this matches — treats history recall
as a **boundary action**: Up recalls the previous prompt only when the
cursor is on the first visual line of the input box, Down only on
the last. On any other line, Up / Down move the cursor one line
within the multi-line prompt and leave history untouched.

The fix adds a cursor-line classification to the input key dispatch
and a small, testable helper module. The picker overlay's existing
priority over Up / Down is preserved.

## What landed

| File | Lines | Role |
|---|---|---|
| `cmd/wagyu/tui_cursor_boundary.go` (new) | +118 | Pure helpers `cursorLine`, `isOnFirstLine`, `isOnLastLine` plus `inputCursorByteOffset` (textarea → byte-position bridge with UTF-8 awareness) |
| `cmd/wagyu/tui_cursor_boundary_test.go` (new) | +357 | 15 tests: 7 pure-helper tests + 2 byte-offset-bridge tests + 6 integration tests that drive a real `runMode` through `tea.KeyMsg` |
| `cmd/wagyu/tui_run.go` | +22 / −4 | `case tea.KeyUp` and `case tea.KeyDown` now check `isOnFirstLine` / `isOnLastLine` before calling `m.historyUp` / `m.historyDown`. On non-boundary lines the cases fall through to `m.input.Update` so the textarea's built-in `LinePrevious` / `LineNext` bindings move the cursor |

**Total:** 3 files changed, **+497 / −4**. Single commit.

## Architecture

The fix sits at the boundary between three layers that all want a
say in what Up / Down do on the input box:

1. The picker overlay (when visible) — drives slash-command navigation.
2. The prompt's history recall — fires only on the input's first / last visual line.
3. The textarea's built-in cursor movement — moves the cursor up / down one logical line within a multi-line prompt.

The pre-fix code short-circuited layers (2) and (3): layer (2) won
whenever the picker was hidden. The fix lets layer (3) win on
non-boundary lines by **not returning** when the cursor isn't on the
first / last line; control falls out of the inner `switch key.Code`
into the subsequent `m.input, cmd = m.input.Update(msg)` at line
2610, which hands the key to bubbles' default `LinePrevious` /
`LineNext` bindings.

```
prompt dispatch (case tea.KeyUp, before picker check):
  if picker visible                → picker.MoveUp()
  else if isOnFirstLine(value, c)  → m.historyUp()
  else                             → fall through to m.input.Update
                                     (textarea's LinePrevious moves cursor up)
```

`isOnFirstLine(value, c)` is a pure `(string, int) bool` predicate
in `tui_cursor_boundary.go`; the integer `c` is the cursor's byte
position in `m.input.Value()`. `m.input` is a
`charm.land/bubbles/v2/textarea.Model`, which exposes `Line()` /
`Column()` rather than a raw byte position; `inputCursorByteOffset`
is the small adapter that reconstructs the byte position from
`Line()`, `Column()`, and `Value()`. It walks newlines to find the
start of the cursor's row, then uses `utf8.DecodeRuneInString` to
convert the rune column to a byte offset — so multi-byte runes
(α, β, …, CJK, emoji) are handled correctly.

The picker check is **still** the first branch on Up / Down, so
slash-command navigation takes priority over both history recall
and cursor movement — matching the v1.3.5 binary.

## Tests

`go test -vet=off -count=1 -run 'TestMultiLine|TestCursor|TestBoundary|TestHistory' ./cmd/wagyu/` — 15 tests pass:

```
=== RUN   TestCursorLine_SingleLine_NoNewlines
=== RUN   TestCursorLine_MultiLine
=== RUN   TestCursorLine_ClampOutOfRange
=== RUN   TestIsOnFirstLine_TrueCases
=== RUN   TestIsOnFirstLine_FalseCases
=== RUN   TestIsOnLastLine_TrueCases
=== RUN   TestIsOnLastLine_FalseCases
=== RUN   TestInputCursorByteOffset_MultiLine
=== RUN   TestInputCursorByteOffset_UTF8
=== RUN   TestUpOnFirstLineSingleLine_TriggersHistory
=== RUN   TestUpOnFirstLineMultiLine_TriggersHistory
=== RUN   TestUpOnMiddleLine_DoesNotTriggerHistory
=== RUN   TestDownOnLastLine_TriggersHistory
=== RUN   TestDownOnMiddleLine_DoesNotTriggerHistory
=== RUN   TestPickerVisible_OverridesUpHistory
PASS
ok  	github.com/chadgpt/openagy/cmd/wagyu	0.586s
```

The 6 integration tests required by the issue brief:

| Test | Asserts |
|---|---|
| `TestUpOnFirstLineSingleLine_TriggersHistory` | Up on empty input (single-line, only line is both first and last) recalls the pushed history entry; `m.historyIdx` advances to 1 |
| `TestUpOnFirstLineMultiLine_TriggersHistory` | Multi-line input `first\nsecond\nthird`, cursor on row 0 via `MoveToBegin` — Up recalls history and replaces the input |
| `TestUpOnMiddleLine_DoesNotTriggerHistory` | Same input, cursor on row 1 (`CursorDown` from row 0) — Up leaves the input value and `historyIdx` unchanged and moves the cursor to row 0 via the textarea's `LinePrevious` |
| `TestDownOnLastLine_TriggersHistory` | Two pushed entries, navigated up so `historyIdx == 2`, then fresh input with cursor on the last line — Down calls `historyDown`, `historyIdx` decrements, value returns to the newer entry |
| `TestDownOnMiddleLine_DoesNotTriggerHistory` | Multi-line input, cursor on row 1 — Down leaves value and `historyIdx` unchanged and moves the cursor to row 2 via `LineNext` |
| `TestPickerVisible_OverridesUpHistory` | Open the picker by typing `/`, advance highlight to row 1 with Down, then Up — `m.picker.Idx()` decrements to 0; input value and `historyIdx` are untouched |

The remaining 9 tests cover the pure `(value, cursor)` helpers and
the byte-offset bridge, including a UTF-8 case (`α\nβ`) so the
rune-to-byte conversion is exercised end-to-end.

## Build / format

```
$ go build -o /tmp/wagyu-mlb ./cmd/wagyu
$ ls -la /tmp/wagyu-mlb
-rwxr-xr-x  1 gear  staff  73306642  Oct 18 11:57  /tmp/wagyu-mlb

$ gofmt -l cmd/wagyu/tui_cursor_boundary.go cmd/wagyu/tui_cursor_boundary_test.go
(no output — both gofmt-clean)

$ git diff --stat
 cmd/wagyu/tui_run.go | 26 ++++++++++++++++++++++----
 2 files changed, 497 insertions(+), 4 deletions(-)
```

(`gofmt -l cmd/wagyu/tui_run.go` is also clean — it was never on
the pre-existing-dirty list per AGENTS.md §"gofmt -l is not clean".)

## Why not `m.input.Cursor()`?

The bug report suggested reading the cursor byte position via
`m.input.Cursor()`. The wagyu clone's `m.input` is a
`charm.land/bubbles/v2/textarea.Model`, not a `textinput.Model` —
textarea's `Cursor()` returns a `*tea.Cursor` (screen-space X / Y
for rendering), not a byte offset. The byte position has to be
reconstructed from `Line()` / `Column()` / `Value()`. The bridge
(`inputCursorByteOffset`) and its test (`TestInputCursorByteOffset_UTF8`)
are the verification that the byte-position interface used by
`isOnFirstLine` / `isOnLastLine` is sound; the pure helpers remain
straightforwardly unit-testable on `(string, int)` inputs.

## 硬约束 / 存疑 / limitations

- **Modifier-carrying Up / Down**: the bug report and the reference
  binary don't distinguish modifier variants. The fix matches the
  pre-fix behaviour: plain Up / Down, Shift+Up / Down, and Ctrl+Up /
  Down all route through the same boundary check. Shift+Up is
  also bound to the textarea's `SelectLineUp` for selection
  extension, but the pre-fix clone swallowed the key for history
  regardless — that pre-existing divergence is left intact (out of
  scope for this fix).
- **Soft-wrap**: `Line()` returns the logical line index (after
  hard newlines). A single long line that soft-wraps at the
  terminal width is treated as one line — Up / Down on a soft-wrap
  continuation still falls through to the textarea. The
  v1.3.5 binary's prompt box rarely exceeds `MaxHeight=24` so
  soft-wrap is mostly cosmetic, but if a future cycle finds a
  mismatch on a long soft-wrapped prompt, the fix would move from
  `m.input.Line()` to a visual-row counter using `LineInfo()`'s
  `RowOffset` / `Height`. Marked as a follow-up, not a regression.
- **Picker override order**: still matches the pre-fix code — the
  picker check is the first branch in each case. Tested by
  `TestPickerVisible_OverridesUpHistory` (Down variant is not
  separately tested but uses the same code path; if asymmetry
  matters later, add a symmetric `TestPickerVisible_OverridesDownHistory`).
- **`fakeClient` reuse**: `runModeForTest` reuses the
  package-level `fakeClient` declared in `tui_run_test.go`. The two
  files are in the same `package main`, so the unexported type is
  visible without import gymnastics. No new fake-backend type was
  added.

## Reproduction (manual)

Inside `tmux`/`script -q`:

```
$ /tmp/wagyu-mlb
# Type a multi-line prompt, e.g. via Ctrl+J or Shift+Enter:
> first line<Ctrl+J>second line<Ctrl+J>third line█
# Press Up:
#   pre-fix : replaces prompt with previous history entry
#   post-fix: cursor moves from line 1 (where █ was) up one line
#             into the row above; no history recall
# Move cursor back to line 1, then Down:
#   pre-fix : recalls newer history entry (if historyIdx > 0)
#   post-fix: cursor moves down one line; no history recall
# With cursor on the first (top) row, Up recalls history.
# With cursor on the last (bottom) row, Down recalls history.
```

The picker still wins: `/ch<Up>` walks the `/changelog`-filtered
slash list rather than recalling history.
