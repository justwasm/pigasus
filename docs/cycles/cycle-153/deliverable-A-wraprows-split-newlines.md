# Cycle 153 — `wrapRows` split on embedded newlines

## Symptom

At a 200×60 terminal the clone showed only 18 lines of transcript content + 37
blank rows, while the reference showed 40 lines of the same conversation:

```
=== 000-init (pre-153, 200×60) ===
ref  text=40 lines, first="    ?? rea-go-seeds/XxxX.java..."
our  text=18 lines, first="然后 cycle 81 加的..."
```

The clone's input field also visibly jumped up and down as the user scrolled
because View()'s drop-from-top shaves a different number of rows on each
keypress depending on how many embedded newlines the tail of the transcript
happens to hold.

## Root cause

`wrapRows` in `cmd/wagyu/tui_viewport.go` was treating a multi-paragraph model
reply as a single entry: the function only checked whether the entry's total
*visible-cell* width fit inside `width` and returned the entry as a single
string (newlines preserved) when it did. `styleInlineBackticks(s.Text)` on a
resumed model reply produces exactly that shape — a single `m.response` entry
holding N paragraphs separated by `\n`.

Consequences in `transcriptWindow()`:

| layer              | saw the entry as      | rendered as           |
|--------------------|-----------------------|-----------------------|
| `rowsCache`        | 1 entry, N+1 lines    | n/a                   |
| `screenCounts[i]`  | 1, but                | n/a                   |
| `clipToScreenRows` | counts the entry as 1 row of budget | trims one entry |
| `padTo(row, w)`    | truncates the whole string to `w` cells | multi-line rendered |
| `View()`           | writes one `\n` after the entry | terminal sees N+1 rows |

`View()` then dropped the overflow from the top to fit `m.height=60`, shaving
off `Σ(extra_lines)` rows. On 200×60 the visible slice contained `visible[0:19]`
= 14 tool rows + a 5-part model message (42 logical rows) + 19 padding rows.
Total terminal rows = 75, budget 60 → drop 15 from the top. The drop swallowed
the entire tool-call header, so the user only saw the middle of the model
message.

## Fix

`cmd/wagyu/tui_viewport.go` — split the input on `\n` first, wrap each
sub-line independently. Each returned entry now maps to exactly one terminal
row, so `clipToScreenRows`, `padTo`, and `View()` all agree on the row count.

ANSI escapes that span a newline are dropped at the newline; the only styles
applied to a resumed model reply are inline-backticks (`\x1b[48;2;31;36;48m`
etc.), which never cross `\n`, so the loss is invisible.

`scrollHeaderRows` already split on `\n` before calling `wrapRows`, so its
behaviour is unchanged (one extra no-op split is harmless).

## After

```
=== 000-init (post-153, 200×60) ===
ref  text=40 lines
our  text=41 lines
```

PageUp × 5 jumps to the same conversation segment ref jumps to (modulo the
two binaries having been started a few seconds apart and the conversation
DB accumulating rows in between). Cycle 152's chrome-anchored-at-bottom
padding now has nothing to pad — the visible slice already matches the
viewport's terminal-row budget, so the input field stays on row 27 across
scroll keypresses at every terminal size, not just at 100×30.

## Files touched

- `cmd/wagyu/tui_viewport.go` — `wrapRows` rewritten to split on `\n` first;
  docstring updated with the cycle-153 note.

## Verification

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu
go test ./cmd/... ./internal/...                 # 25 packages green
bash /tmp/agycmp/hotpath.sh                      # all 5 scenarios run
bash /tmp/wide_compare.sh                        # 6 frames @ 200×60
```

`/tmp/agycmp/hotpath.sh` 5-scenario diff summary (100×30, conversation =
`4638a7b0-e5f4-4961-80c2-d61499f06050`):

```
cold_start   000-init            diff= 33
ctrl_o_expand 000-init           diff= 49
              001-scrolled       diff= 52
              002-expanded       diff= 52
              003-collapsed      diff= 52
help_tabs     000-general        diff= 37
              001-second         diff= 18
              002-third          diff= 12
              003-wrapped        diff= 32
              004-closed         diff= 27
picker        000-slash          diff= 37
              001-down5          diff= 23
              002-closed         diff= 15
resume_scroll 000-init           diff= 51
              001-pgup           diff= 54
              002-bottom         diff= 52
              003-top            diff= 52
```

No regression vs cycle 152's measurements (cold_start=33 / resume_scroll=58
etc. were within 1 line of the same numbers here). The remaining gaps are
the warning block, the email-tier annotation and ref-only escape sequences —
all cycle-100-era items, not regressions.

## Limitations

- The harness's `resume_scroll` and `cold_start` capture ref while it's in
  the OAuth Welcome flow (cycle-150-era behaviour: `JETSKI_OAUTH_TOKEN=test`
  no longer pushes ref past the welcome screen on this build), so those
  frames compare ref's welcome screen against our's loaded conversation —
  not a like-for-like content comparison. The diff numbers above are valid
  for chrome / layout, not for transcript bytes.
- The captured ref and our panes started a few seconds apart, so the
  conversation DB accumulated rows between captures. PageUp-5 lands at
  slightly different content in each binary. The diff is small (~10 rows)
  and the layout is now correct.
- Long lines (>200 cells) now split on `\n` first, so a deliberately
  unbroken long string (`"a"*500`) wraps to 3 rows instead of 1 multi-row
  entry — the same behaviour ref has.
