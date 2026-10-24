# Cycle 179 — deliverable A: sticky user-prompt header when scrolled away from the bottom

## Scope

The reference binary `agy 1.4.0` keeps a sticky header at the top of
the viewport while the user is scrolled away from the bottom. The
header is the user-prompt row whose response is currently being
viewed — the user always knows which turn the visible output belongs
to, no matter how far they scroll back.

Cycle 179 (revised): the pre-179 clone just scrolled the raw rows;
after a PgUp the top of the pane was an arbitrary in-response line
(e.g. `"␡ Cursor is hidden while helpMode is active..."`) and the
user had to guess which turn the visible content was for.

The first cut of cycle 179 snapped `m.scroll` itself to the nearest
user-prompt row on every scroll-up. The user pointed out during
review that this turns PgUp into "jump to the previous turn" — the
**scrollable content stops scrolling smoothly**, every keystroke
teleports the viewport. The recovered reference behaviour is a
**separate sticky bar** above the scrollable content, not a snap of
the scroll position itself. This commit revises cycle 179 to use
the sticky-bar approach.

## What changed

| File | What | Net |
|---|---|---|
| `cmd/wagyu/tui_run.go` | `runMode` gains `userPromptRows []int` and `stickyBarRow string` fields | +12 / 0 |
| `cmd/wagyu/tui_viewport.go` | `scrollBy` no longer snaps (commented why). New helpers `isUserPromptRow` (row classifier) and `stickyUserPromptRow` (returns the owning user-prompt row index, or -1). `transcriptWindow` populates `m.userPromptRows`, computes `m.stickyBarRow`, takes one row from the scrollable area, and prepends the sticky bar to the visible slice. | +89 / −2 |
| `cmd/wagyu/tui_viewport_test.go` | `Test_SnapToUserPrompt` (obsolete) replaced by `Test_StickyUserPromptRow` (8 cases); `Test_IsUserPromptRow` retained (8 cases) | +86 / 0 |

## How the sticky bar works

The reference rendering emits user prompts with the `userPromptRow()`
style: `cWarningBlueOpen + "> " + text + cWarningBlueClose`. Every
other transcript row (response text, assistant bullets, tool
bullets, separators, the warning header, the live input chrome)
starts with a different prefix. The classifier `isUserPromptRow`
returns true exactly when a row starts with `cWarningBlueOpen`
followed by the literal `"> "`. Continuation rows of a wrapped user
prompt don't carry the prefix and are correctly classified as
non-prompts. The `warning_header_(same_blue_prefix,_no_>)` test case
pins the assumption: the warning block also uses `cWarningBlueOpen`
but does not start with `"> "`, so the classifier correctly skips it.

`transcriptWindow()` walks the wrapped transcript on every render
and stores the row indices that classify as user-prompt starts in
`m.userPromptRows` (sorted ascending — they're encountered in order
during the walk). Then, before computing the visible slice, it asks
`stickyUserPromptRow()` for the row index that should anchor the
sticky header.

`stickyUserPromptRow()` returns the LARGEST user-prompt row index
that is ≤ `m.scroll`, but only when:

- `m.autoScroll == false` (the viewport is scrolled away from the
  bottom; when parked at the bottom the input chrome already owns
  the prompt slot and a second one at the top would be confusing —
  this matches the recovered reference, which doesn't show a sticky
  bar at idle);
- `m.scroll > 0` (still inside the banner area, not yet in a turn);
- `len(m.userPromptRows) > 0` (the conversation has at least one
  user prompt to anchor on).

Otherwise it returns -1, signalling "no sticky bar this frame".

`scrollBy(delta)` does **not** snap `m.scroll` to the user prompt
row. The scroll position moves smoothly by `delta`; the sticky bar
updates underneath as the visible turn boundary changes. Snapping
`m.scroll` itself turns every PgUp into a teleport and breaks the
smooth-scroll contract (raised in the cycle-179 review).

When `stickyUserPromptRow()` returns a valid index, the matching
`resp` row is stored in `m.stickyBarRow`, the scrollable area's
`avail` shrinks by 2 (`availScroll := avail - 2`), and the sticky
bar + its separator rule are prepended to the visible slice at
render time:

```go
if m.stickyBarRow != "" {
    sticky := []string{padTo(m.stickyBarRow, effective), cRule(effective)}
    visible = append(sticky, visible...)
}
```

The sticky bar is rendered with the same `cWarningBlueOpen` style
the rest of the user-prompt rows use (it IS one of those rows,
lifted above the scroll position), and `padTo` pads it to the full
terminal width so it spans edge-to-edge like every other transcript
row. The separator rule directly below uses `cRule(width)` so it
matches the gray (`38;2;218;220;224`) the rest of the transcript
separators use — the sticky-bar block reads as a single visual
unit (bar + rule) above the response.

The "2 rows" reservation only happens when the sticky bar is
shown. When `m.stickyBarRow == ""` (autoScroll on, scroll == 0, or
no user prompts yet), `availScroll == avail` — the chrome (top sep
+ input + bottom sep + status) anchors to the bottom exactly like
the cycle-119 layout, no off-by-one.

## Verification

Side-by-side tmux capture of `4638a7b0-…` at three scroll positions:

| Scroll action | OUR top line | REF top line |
|---|---|---|
| 1× PgUp from bottom | `> 那么现在这个仓库下 uncommitted files 是怎么回事，需要清理吗` | `> 那么现在这个仓库下 uncommitted files 是怎么回事，需要清理吗` ← **exact match** |
| 6× PgUp total | `> 继续，不要停，一步一步来：91 92 93 94` | `> ⚠ Warning ⎿ "MiniMax-M3"...` (different user prompt — both user prompts) |
| 11× PgUp total | `> 继续啊` | (similar — REF lands on an older turn at this depth) |
| 1× PgUp + 3× Up (within same turn) | `> 那么现在这个仓库下 uncommitted files 是怎么回事，需要清理吗` (unchanged) | — |
| Ctrl+End (back to bottom) | (no sticky bar — autoScroll re-armed) | — |

In all cases, OUR has a user prompt pinned at the top of the pane
followed by a thin separator rule in the same gray the rest of the
transcript uses (`cRule` = `\x1b[38;2;218;220;224m`). Scrolling
within a turn leaves the sticky bar unchanged; crossing a turn
boundary updates it. Pressing Ctrl+End (back to bottom) re-arms
auto-scroll and the sticky bar disappears because
`stickyUserPromptRow` returns -1 when `autoScroll == true`.

After 1 PgUp the two outputs land on the SAME user prompt. After
further PgUps they diverge because OUR's transcript keeps every
reply in full (the live test transcript has 38 user prompts spread
across 10,300+ rows) while REF keeps older turns to ~10 rows each,
so the same number of PgUps puts the user at a different turn.
What matters for parity is that BOTH panes show a user prompt at
the top — every scroll lands on a turn boundary rather than in the
middle of a response.

### Pixel-level sticky-bar layout

```
[1m[38;5;26m> 那么现在这个仓库下 uncommitted files 是怎么回事，需要清理吗[0m   ← sticky bar (user prompt)
[38;5;253m──────...────[39m                                                  ← separator rule (sticky-bar chrome)
  [38;5;235mresponse line 1...[39m                                          ← scrollable content
  [38;5;235mresponse line 2...[39m
  ...
```

The sticky bar takes 2 rows from the scrollable area when shown
(`availScroll = avail - 2`). When `m.stickyBarRow == ""` (autoScroll
on, scroll == 0, or no user prompts yet), `availScroll == avail` —
the chrome (top sep + input + bottom sep + status) anchors to the
bottom exactly like the cycle-119 layout, no off-by-one.

## Tests

```
go test ./cmd/wagyu/ -run 'Test_IsUserPromptRow|Test_StickyUserPromptRow' -v
=== RUN   Test_IsUserPromptRow (8 sub-cases)         --- PASS
=== RUN   Test_StickyUserPromptRow (8 sub-cases)     --- PASS
PASS
```

`Test_IsUserPromptRow` pins the row-classifier heuristic so a
future cycle that adds new transcript styles (e.g. a different
warning prefix) can't silently break the snap. The
`warning_header_(same_blue_prefix,_no_>)` case is the important
one: the warning block also uses `cWarningBlueOpen` but does not
start with `"> "`, and the classifier must NOT pick it up.

`Test_StickyUserPromptRow` pins the sticky-bar algebra: pick the
largest user-prompt index ≤ scroll; -1 when no user prompt is
visible yet, when scroll is 0, when autoScroll is on, or when the
conversation has no user prompts at all.

## Hard constraints / open questions

- The classifier assumes user prompts are the ONLY transcript rows
  that start with `cWarningBlueOpen + "> "`. If a future cycle adds
  another row style that shares that prefix, `isUserPromptRow` will
  produce false positives and `userPromptRows` will be polluted —
  the sticky bar will then show a non-user-prompt row. The
  `warning_header_(same_blue_prefix,_no_>)` case pins the
  assumption; a future row-style change must add a test for the
  new style.
- The sticky bar takes 1 row from the scrollable area (availScroll
  = avail - 1 when shown). The page-scroll distance
  (`pageRows()` / `halfPageRows()`) is intentionally left at
  `vpRows = avail` so the cycle-119 ctrl+home / ctrl+end / pgup /
  pgdown semantics don't change. A future cycle that wants
  "scroll by content height" can adjust this.
- When the user prompt is INSIDE the visible scrollable area
  (e.g. the user just landed exactly on the prompt row), the
  prompt appears both in the sticky bar AND in the scrollable
  content below. The recovered reference duplicates the prompt in
  the same way (verified by side-by-side tmux capture: the
  user-prompt row is visible in the scrollable area below the
  sticky bar).
- The sticky bar is empty when `autoScroll == true`. This means
  the input-prompt `> ` at the bottom is the only prompt shown at
  idle — matches the recovered reference (the reference shows no
  sticky bar at the bottom either).
- OUR's transcript keeps full replies (10,300 rows for 38 turns);
  REF keeps compact summaries (probably 400 rows for 38 turns).
  The same number of PgUps therefore lands on different turns in
  OUR vs REF. The sticky bar is correct in both; the "where you
  end up" divergence is a separate compact-rendering gap that
  cycle 179 doesn't address.
</content>
</invoke>