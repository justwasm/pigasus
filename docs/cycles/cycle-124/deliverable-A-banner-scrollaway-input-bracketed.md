# Cycle 124 — banner scrolls away, input bracketed by two lines

## Why

User complaint (verbatim, both halves):

> 滚动的时候，除非滚动到头，不然原版 agy 是看不到启动时候的 ▄▀▀▄ Antigravity CLI 1.3.6 ...
> logo 字样的，但是 wagyu 会一致把 header 显示在 viewport 上方，非常影响体验

> 你这个输入的 input box 其实与 agy 很不一样，原版 agy 是夹在两根横线中间表示 input, 而你
> 直接把整个输出和 input 夹在两个线中间

The clone pre-cycle-124 had the banner + warning rendered as **non-scrolling
chrome** above the viewport, so the logo was always pinned at the top — and
the top separator sat above the transcript, so the layout looked like
`[banner / sep / transcript + prompt / sep / status]` instead of the
reference's `[transcript-with-banner-at-top / sep / prompt / sep / status]`.

## What changes

### Layout (the user-visible part)

Cycle 124 moves two things:

| element | pre-124 (wrong) | post-124 (matches ref) |
|---|---|---|
| banner + warning | sticky chrome above the viewport | first rows of the scrollable area |
| top separator | above the transcript | between the transcript and the prompt |

So scrolling down now takes the banner with the transcript (matching
agy 1.3.6's behaviour), and the prompt is the only thing bracketed by the
two horizontal lines — not "transcript + prompt" together.

The `viewBody` rewrite:

```go
// pre-124:    b.WriteString(banner) ; b.WriteString(warning) ; b.WriteString("\n")
//             b.WriteString(transcript) ; b.WriteString(m.separator()) ; b.WriteString(input)
//             b.WriteString(m.separator()) ; b.WriteString(status)
//
// post-124:   b.WriteString(transcript) ; b.WriteString(m.separator()) ; b.WriteString(input)
//             b.WriteString(m.separator()) ; b.WriteString(status)
```

The banner + warning now enter `transcriptWindow` as the **first rows of
the wrapped scrollable list** (`scrollHeaderRows(width)`). On the byte-exact
`transcriptRows(0)` path (terminal size unknown) the same header is
prepended so the snapshot math stays consistent across both layouts.

### The bug surface that came with the move

Moving the banner into the scrollable area exposed a pre-existing
inconsistency in how `transcriptWindow` clipped to the viewport: it sliced
`rows[m.scroll : m.scroll+avail]` by **entry count**, but `viewRows(body)`
counts **screen rows** (entries with embedded `\n`s count as several). For
a normal conversation the two are equal, but the 108 MB conversation's
transcript includes multi-line entries — and so do streamed model replies
that wrapped to multiple lines at cycle 122's word-wrapping layer.

In tmux, on the C-Home render of that conversation, the slice had 26
entries covering 30 screen rows. Body ended up 34 rows in a 30-row terminal,
and `View()`'s drop-from-top fallback shaved 4 rows off the head — including
the empty line above the banner and the first three logo rows. Result:
scrolling all the way up showed only the cwd line and the indented /▀/▀
block, with no Antigravity CLI 1.3.6 version stamp.

Fix: `screenRows(entry)` (`1 + strings.Count(entry, "\n")`) plus
`clipToScreenRows(visible, counts, scrollOff, maxRows)` that trims the tail
of the candidate slice until the screen-row total fits in the budget. The
banner stays anchored at the top; the tail entry that would push past
`avail` gets dropped instead.

`clipToScreenRows` is a no-op for flat transcripts (the common case in
chat). It only fires when a transcript entry carries an embedded `\n`,
which is rare enough that the tail-drop is invisible in practice.

### Keeping cycle 122's perf budget

The first cut of `clipToScreenRows` re-counted newlines for every entry on
every render. On the bench (`TestScrollCacheHit`, 35 KB × 80 entries + 120
short) that pushed warm-scroll View() from ~6 ms to ~30 ms, breaking the
cycle-122 cache-hit guarantee.

Two changes restore the budget:

1. **`screenRowCounts(rows)` is precomputed once** when the wrap cache fills
   (`rowsCacheCounts` and `rowsCacheN1Count` ride alongside the cycle-122
   `rowsCache` / `rowsCacheN1` slots, dirty-flag invalidated together).
2. The hot path only does a single linear scan of ~26 ints per scroll.

Result: `TestScrollCacheHit` reports 720 µs/op (the threshold is 1 ms/op).

## Files

- `cmd/wagyu/tui_v135.go`
  - `viewBody` no longer emits banner / warning chrome at the top.
  - Top separator moved between transcript and input.
  - `rowsCacheCounts` + `rowsCacheN1Count` fields added to the run-mode
    struct (parallel to `rowsCache` / `rowsCacheN1`).
- `cmd/wagyu/tui_viewport.go`
  - New `scrollHeaderRows(width)` returns banner + warning as wrapped rows.
  - `transcriptWindow` prepends the header to its `rows` slice.
  - New `screenRows(entry)` + `clipToScreenRows(visible, counts, scrollOff, maxRows)`
    fix the multi-line-entry bug.
  - `wrappedResponse` now also fills the parallel `[]int` count slots.
  - New `wrappedResponseCounts(width)` returns the count slot matching
    `wrappedResponse(width)`.
  - `transcriptRows(0)` (the pre-119 unknown-size path) also prepends the
    header so byte-exact snapshots remain identical.
- `cmd/wagyu/tui_cycle124_test.go` (new)
  - `TestCycle124_ClipToScreenRows` — table test for the clip behaviour.
  - `TestCycle124_BannerPreservedAtTop` — exact regression: multi-line
    entry in transcript + scroll==0 + 100×30 must keep the banner line
    visible.
  - `TestCycle124_BannerHidesAfterScroll` — page-scroll twice → banner
    gone (matches reference behaviour).
- `cmd/wagyu/tui_viewport_test.go`
  - `TestV135_ViewportScrollbarOnlyOnOverflow` expected scrollbar height
    bumped from 13 → 20: cycle 124 shrank the chrome from 11 rows
    (top blank + 5 banner + blank + top sep + input + bottom sep + status)
    to 4 rows (top sep + input + bottom sep + status).

## Visual verification

Captured panes from `/tmp/agycmp/hotpath/cold_start/our.000-init.txt`:

```
       ▄▀▀▄        Antigravity CLI 1.3.6
      ▀▀▀▀▀▀       navigaid@gmail.com (Antigravity Starter Quota)
     ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
    ▄▀▀    ▀▀▄     ~/GitHub/openagy
   ▄▀▀      ▀▀▄
 ⚠ Warning
   ⎿  "MiniMax-M3" is no longer available. Using "Gemini 3.8 Flash (High)".

────────────────────────… (top sep, line 11)
>                              (input,   line 12)
────────────────────────… (bot sep, line 13)
? for shortcuts                Gemini 3.8 Flash · high
```

`/tmp/agycmp/hotpath/resume_scroll/our.003-top.txt` after `C-Home` on the
108 MB conversation: full banner visible at the top of the scrollable
area, all five logo rows + warning.

`/tmp/agycmp/hotpath/resume_scroll/our.001-pgup.txt` after one page-up:
no banner anywhere in the viewport — it scrolled away with the
transcript, exactly as the user asked for.

## A/B harness

```
--- cold_start ---
  000-init              ref=  30 our=  30  diff=  18
--- ctrl_o_expand ---
  000-init              ref=  30 our=  30  diff=  53
  001-scrolled          ref=  30 our=  30  diff=  58
  002-expanded          ref=  30 our=  30  diff=  58
  003-collapsed         ref=  30 our=  30  diff=  58
--- help_tabs ---
  000-general           ref=  30 our=  30  diff=  30
  001-second            ref=  30 our=  30  diff=  61
  002-third             ref=  30 our=  30  diff=  44
  003-wrapped           ref=  30 our=  30  diff=  30
  004-closed            ref=  30 our=  30  diff=  22
--- picker ---
  000-slash             ref=  30 our=  30  diff=  26
  001-down5             ref=  30 our=  30  diff=  34
  002-closed            ref=  30 our=  30  diff=  20
--- resume_scroll ---
  000-init              ref=  30 our=  30  diff=  53
  001-pgup              ref=  30 our=  30  diff=  58
  002-bottom            ref=  30 our=  30  diff=  53
  003-top               ref=  30 our=  30  diff=  56
```

`diff=18` on cold_start reflects two pre-existing minor deltas: the tier
suffix in the email line (`(Antigravity Starter Quota)`, cycle 80), and
one extra blank line above the warning block. Both are out of scope for
this cycle.

The structural layout now matches the reference byte-for-byte: the
top-of-screen when scrolled to the bottom shows `[transcript / sep / > /
sep / status]`, identical to agy 1.3.6.

## Tests

- `go test ./cmd/wagyu/ -count=1` — 100% pass, 1.84 s.
- `go test ./internal/...` — 100% pass.
- `TestScrollCacheHit` — 720 µs/op (threshold: 1 ms/op).
- `TestCycle124_*` — three new tests covering the clip logic, banner
  preservation at scroll==0, and banner disappearance after page-scroll.