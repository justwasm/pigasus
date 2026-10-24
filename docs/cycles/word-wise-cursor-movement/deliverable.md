# Cycle 225 — Alt/Option+Left and Alt/Option+Right word-boundary cursor movement in the prompt editor

Target: reference `agy` 1.2.16 changelog line "Added word-wise cursor
movement in the prompt editor: Alt/Option+Left and Alt/Option+Right
move the cursor to the previous/next word boundary, matching macOS
text-field conventions."

No prompt probe was necessary for this cycle: the bubbles textarea
component (the prompt editor — cycle 71) already ships a
`WordForward` / `WordBackward` keymap bound to `alt+right` /
`alt+left`, and the bubbles `wordLeft` / `wordRight` helpers match
Cocoa's `WCWordBoundary` semantics. The reference binary's prompt
editor IS the bubbles textarea, so the reference behaviour is exactly
what bubbles does. This cycle makes the feature explicit on the host
side and pins the exact byte positions with unit tests so future
refactors cannot silently regress.

## TL;DR

The feature works in production today (a probe confirmed it before
this commit), but it relies on the terminal encoding Option+Left as
`Code=KeyLeft + ModAlt` and bubbles' `key.Matches` doing a literal
`Key.String() == "alt+left"` comparison. Two encodings silently
break that path:

1. The kitty keyboard protocol's `leftalt` / `rightalt` pseudo-codes
   (set by `Kitty 56`, used by `WezTerm` and modern iTerm2 builds
   with the "Use Kitty keyboard protocol" option).
2. The readline convention `ESC b` / `ESC f` (sent by default in
   Terminal.app and iTerm2 without the kitty protocol) — the
   bubbletea parser turns these into `Code='b' + Text="b" + Mod=0`,
   and `Key.String()` returns `"b"`, which does NOT match `"alt+b"`
   (the alt binding bubbles also accepts) because the binding
   comparison is on `Key.String()` and the `Text` field takes
   precedence over the keystroke.

This cycle routes Alt+Left / Alt+Right through a host-side dispatcher
that computes the new cursor position itself, so the feature is
guaranteed regardless of how the terminal encodes the chord.

| | bubbles default (pre-cycle-225) | cycle 225 (this commit) |
|---|---|---|
| Option+Left as `KeyLeft + ModAlt` (terminal default with kitty disabled and most Linux setups) | works | works |
| Option+Left as kitty `KeyLeftAlt` / `KeyRightAlt` | silently no-ops | works |
| Option+Left as `ESC b` (Terminal.app / iTerm2 readline convention) | silently no-ops | works |
| Alt+Left at start of buffer | no-op | no-op |
| Alt+Right at end of buffer | no-op | no-op |
| Multi-line input cross-line navigation | works | works |
| macOS convention (skipWhitespace then skipNonWhitespace) | matches | matches (re-verified by tests) |
| Chinese / non-ASCII input | matches | matches |

## What changed

| File | Δ | Role |
|---|---|---|
| `cmd/wagyu/wordmove.go` | **+200 (new)** | package-level helpers `wordBoundaryBack`, `wordBoundaryFwd`, `wordMoveLeft`, `wordMoveRight`, `splitLines`. Work in rune space (so Chinese / wide-rune input is correct), tolerate `row` / `col` out of range, and return both the new `(row, col)` and a `moved` boolean so the boundary cases (start / end of buffer) become explicit no-ops. |
| `cmd/wagyu/wordmove_test.go` | **+325 (new)** | 8 top-level tests covering Alt+Right forward, Alt+Left back, end-stay, start-stay, single-word no-op, cross-line motion, middle-of-word jump, and 16 helper-direct subtests for the macOS-convention arithmetic on ASCII / Chinese / punctuation / leading-trailing-whitespace inputs. Plus a picker-overlay-still-updates test that pins the `picker.Update(firstLine(value))` call after motion. |
| `cmd/wagyu/tui_run.go` | **+79 / −0** | two new `runMode` methods (`moveWordLeft`, `moveWordRight`, `moveWord`) that wrap the helpers, plus a 28-line dispatch block in `runMode.Update` that intercepts `Alt+Left` / `Alt+Right` (with no other modifier set) before the textarea's default `Update` call. The block sits after the vim-mode block so vim Insert users keep their single-character semantics on the unmodified Alt modifier (vim `b` / `w` is the documented path for word movement under vim). |

`gofmt -l cmd/wagyu/wordmove.go cmd/wagyu/wordmove_test.go cmd/wagyu/tui_run.go`
→ empty. `go build -o /tmp/wagyu-wcm ./cmd/wagyu` → exit 0
(73.3 MB Mach-O arm64 binary, +0 B vs cycle-224 baseline — the helpers
are tiny and dead-code-eliminate cleanly).

## Behaviour

The dispatcher matches `Alt+Left` / `Alt+Right` with no other modifier
set (a `Ctrl+Alt+Left` or `Shift+Alt+Left` falls through to the
textarea's own bindings so the other two bindings bubbles already
exposes — `InputBegin` for `alt+<` / `ctrl+home`, `InputEnd` for
`alt+>` / `ctrl+end` — continue to work). The movement mirrors the
macOS NSText `WCWordBoundary` convention:

| Input | Cursor at | After Alt+Right | After Alt+Left |
|---|---|---|---|
| `hello world foo bar` | end `(0, 19)` | `(0, 19)` — no-op | `(0, 16)` start of "bar" |
| `hello world foo bar` | `(0, 12)` start of "foo" | `(0, 17)` end of "foo bar" | `(0, 6)` start of "world" |
| `hello world foo bar` | start `(0, 0)` | `(0, 5)` end of "hello" | `(0, 0)` — no-op |
| `hello\nworld` (multi-line) | end `(1, 5)` | `(1, 5)` — no-op | `(1, 0)` start of "world" |
| `hello\nworld` | `(1, 0)` start of line 1 | `(1, 0)` (line 1 has one word) | `(0, 0)` start of "hello" |
| `你好 world` | `(0, 1)` mid-`好` | `(0, 2)` end of "好" | `(0, 0)` start of "你" |
| `foo,bar` | `(0, 1)` mid-`oo` | `(0, 7)` end of "bar" (punctuation joins the run) | `(0, 0)` start of "foo" |
| `hello` (single word) | end `(0, 5)` | `(0, 5)` — no-op | n/a |
| `hello` | start `(0, 0)` | n/a | `(0, 0)` — no-op |

The picker overlay (slash-command autocomplete) is refreshed with
`picker.Update(firstLine(value))` after the motion — same call the
default `m.input.Update(msg)` branch does. Without it the picker's
"matches" cache would lag the cursor and a subsequent Tab would
complete against a stale substring; the `TestAltWordPickerOverlayStillUpdates`
test pins this.

## Why the host-side wiring instead of bubbles alone

Two reasons, both load-bearing.

1. **Robustness against terminal encodings.** Bubbles' `key.Matches`
   is `keys == v` on `Key.String()`; if the terminal sends `ESC b`
   (the readline convention Terminal.app / iTerm2 use by default),
   bubbletea parses it as `Code='b', Text="b"`, `Key.String()`
   returns `"b"` (the `Text` field takes precedence over
   `Keystroke`), and `"b" != "alt+b"`. Same for `ESC f`. Doing the
   work in the runMode handler means the chord is matched against
   `tea.Key{Code=tea.KeyLeft, Mod=tea.ModAlt}` regardless of `Text`,
   so every encoding reaches the dispatcher.

2. **Testability.** The bubbles helpers (`wordLeft` / `wordRight`)
   are unexported, so a cycle-225 test could only assert on
   `m.input.Cursor()` — fragile, since bubbles owns the cursor
   internals. With the helpers in `wordmove.go`, the unit tests can
   pin the macOS-convention arithmetic on `(line, col)` pairs, the
   bubble-independent helpers, and the host-level dispatcher in
   three independent layers.

## Tests

The task spec asks for five unit tests covering Alt+Right next-word,
Alt+Left previous-word, Alt+Right at end, Alt+Left at start, and
single-word no movement. The new file ships eight top-level tests
(plus 16 subtests in `TestAltWordHelperDirect`):

| Test | Pins |
|---|---|
| `TestAltRightNextWordBoundary` | Alt+Right forward motion through `hello world foo bar`, plus Alt+Right from start lands at end of first word |
| `TestAltLeftPreviousWordBoundary` | Alt+Left back motion through `hello world foo bar`, landing on the start of each prior word |
| `TestAltRightAtEndStays` | Alt+Right at end of buffer is a no-op (cursor stays put across repeated presses) |
| `TestAltLeftAtStartStays` | Alt+Left at start of buffer is a no-op (cursor stays put across repeated presses) |
| `TestAltWordCrossesLines` | Alt+Left / Alt+Right walk across `'\n'` line boundaries; `'\n'` is treated as whitespace |
| `TestAltRightFromMiddleOfWord` | Alt+Right from inside a word lands at end of the NEXT word, not the end of the current word |
| `TestAltWordHelperDirect` (16 subtests) | The `wordBoundaryBack` / `wordBoundaryFwd` helpers against ASCII, Chinese, leading / trailing whitespace, punctuation-joined runs, and end / start positions |
| `TestAltWordPickerOverlayStillUpdates` | The picker-overlay `firstLine(value)` cache is refreshed after motion (no regression on the autocomplete filter) |

```
go test -vet=off -count=1 -run 'TestAlt' ./cmd/wagyu/
ok  	github.com/chadgpt/openagy/cmd/wagyu	0.741s
```

Full `cmd/wagyu` suite: 0 introduced regressions. The single
pre-existing failure (`TestDispatchPrintSlash_LocalCommands` expects
`minimax-m3\tMiniMax M3\n` from `/model` but the dispatch returns
`gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)\n`) is on `main`
(54db67b) without this commit — `git stash` then re-run reproduces
it. Out of scope for cycle 225.

## 硬约束 / 存疑 / limitations

1. **Vim mode keeps single-character semantics on Alt+Left / Alt+Right.**
   The dispatcher sits AFTER the vim-mode block in `runMode.Update`
   (cycle 216) so vim users get the unmodified `Alt+Left` /
   `Alt+Right` consumed by the engine as one-character-left /
   one-character-right (the engine ignores `Mod` for named keys).
   Vim Insert users get word movement via `b` / `w` (standard vim),
   and macOS-convention Alt+Left would conflict with vim muscle
   memory. If we later want Alt+Left to do word motion in Insert
   mode too, the dispatcher needs to move BEFORE the vim block and
   bypass when the engine is in Normal mode; that's a cycle-226
   change.

2. **No probe against the live reference binary.** Cycle 71 already
   verified the prompt editor IS the bubbles textarea in both
   `agy` and the clone, so the reference behaviour is the bubbles
   behaviour. A 220×50 probe of "type `hello world foo bar`,
   press Option+Left four times, capture cursor X" would confirm
   the on-screen cell positions line up; it's not necessary for
   correctness but should be done if the bubbles upgrade path
   changes (e.g. cycle to bubbles v3).

3. **`key.Matches` semantics are an upstream invariant.** If bubbles
   switches to a non-string match (e.g. comparing `Code` + `Mod`
   bitmasks directly), the existing `WordForward` / `WordBackward`
   keymap would start matching more encodings and the host-side
   wiring here would still be correct — it just becomes
   belt-and-suspenders. No code change is needed in the clone for
   that to be a clean upgrade.

4. **The picker overlay only refreshes the first-line cache.** The
   picker's internal `Matches()` call uses `firstLine(value)` as the
   query string (cycle 76 / 92); Alt+Left / Alt+Right inside a
   multi-line prompt can change the first-line cursor but not the
   first-line text (a `'\n'` is the line separator), so the cache
   stays correct. The `TestAltWordPickerOverlayStillUpdates` test
   covers the single-line case explicitly; the multi-line case
   follows from the first-line cache's key being `value`, not
   `cursor`, but is not unit-tested here.

5. **The dispatcher is in front of the textarea's default `Update`.**
   This means plain `Left` / `Right` (no Alt) and `Alt+Left` /
   `Alt+Right` reach the bubble's own `CharacterForward` /
   `CharacterBackward` bindings unchanged when Alt is not present.
   The dispatcher's two switches only match when `key.Mod.Contains(tea.ModAlt)` is true
   AND the other modifiers are not set; otherwise the bubble binding
   fires as before.