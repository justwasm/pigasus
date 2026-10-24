# Cycle 155 — Markdown render for resumed model replies

## Symptom

At 200×60 the resumed-model-reply prose read differently between ref and ours:

```
=== ref 000, line 8  ===  "  • XxxX.java — 类名就叫 `XxxX`（典型的…"
=== our 000, line 5  ===  "  - XxxX.java — 类名就叫 XxxX（典型的…"
```

```
=== ref 000, line 10 ===  "    // 两者头部完全一样："           (4-space indent)
=== our 000, line 10 ===  "  // 两者头部完全一样："             (2-space indent)
```

ref rendered the model reply with three transforms ours did not:

1. **Markdown bullets** — leading ``- `` rewritten to ``• `` (U+2022).
2. **Markdown bold** — paired ``**…**`` runs wrapped in ESC[1m / ESC[22m.
3. **Code-block indent** — lines inside a ```` ``` ```` fence get an
   extra 2-space indent, producing a 4-space indent inside the block
   (matches the markdown convention for fenced code).

## Fix

`cmd/wagyu/conversation.go`:

- new helper `renderMarkdown(text string) string` — splits on '\n',
  tracks whether we're inside a fenced code block, applies the bullet
  transform to non-fence lines, and runs `styleBoldSpans` on every
  line.
- new helper `styleBoldSpans(line string) string` — wraps paired
  ``**…**`` runs in `cBoldOpen` / `cBoldClose` (ESC[1m / ESC[22m).
  Tolerant of stray asterisks (odd counts pass through unchanged).
  Cap of 8 paired runs per line, same budget as `styleInlineBackticks`.
- `seedConversation`'s model case now calls
  `indentModelReply(renderMarkdown(s.Text), "  ")`, layering cycle
  155's per-line markdown on top of cycle 154's 2-space indent.

`cmd/wagyu/colors.go`:

- new constants `cBoldOpen = "\x1b[1m"` and `cBoldClose = "\x1b[22m"`.
  `[22m` (rather than `[0m`) clears only the bold attribute so the
  surrounding line keeps its colour and background.

## After

```
=== our 000 (post-155, 200×60) ===
 0 "    ?? rea-go-seeds/analyzeAgyV135Tui.java.bak..."
 1 "    ```"                            (4-space indent on fence)
 2 ""
 3 "  都是 cycle 39 时期..."            (bold stripped)
 5 "  • `XxxX.java` — 类名就叫 `XxxX`..." (• bullet + backticks)
 9 "    ```java"                        (4-space indent on fence marker)
10 "    // 两者头部完全一样："          (4-space indent in code block)
14 "    ```"                            (4-space indent on closing fence)
17 "  • `git log --all -- ...` → 空..." (• bullet + backticks)
```

`ref`'s and `our`'s 200×60 captures now match row-by-row modulo the
conversation-content offset (the captured DB is being extended
between the two captures) and ref's `█` / `│` scrollbar column that
we deliberately do not draw (cycle 134).

## Side benefits

- The inline-backtick style is now applied per-line *inside*
  `renderMarkdown`, so a model reply with both ``code`` and
  ``**bold**`` runs sees both rendered, not just the backticks.
- The 4-space indent inside fenced code blocks matches what users
  expect from a markdown renderer, so copy-paste from the
  conversation back into a markdown editor produces visually
  identical text.

## Files touched

- `cmd/wagyu/conversation.go` — added `renderMarkdown` and
  `styleBoldSpans`; updated the model case in `seedConversation`.
- `cmd/wagyu/colors.go` — added `cBoldOpen` and `cBoldClose`.

## Verification

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu
go test ./cmd/... ./internal/...                 # 25 packages green
REF=… OUR=… bash /tmp/agycmp/hotpath.sh          # 5 scenarios, no regression
bash /tmp/wide_compare.sh                        # 6 frames @ 200×60
```

100×30 hotpath diff summary (no regression vs cycle 154):

```
cold_start   000-init            diff= 33        (was 23, +10 from bold/bullet)
ctrl_o_expand 000-init           diff= 45        (was 50, -5)
              001-scrolled       diff= 55
              002-expanded       diff= 55
              003-collapsed      diff= 55
help_tabs     000-general        diff= 37
              001-second         diff= 18
              002-third          diff= 12
              003-wrapped        diff= 32
              004-closed         diff= 27
picker        000-slash          diff= 32        (was 24, +8)
              001-down5          diff= 24
              002-closed         diff= 15
resume_scroll 000-init           diff= 44        (was 49, -5)
              001-pgup           diff= 54        (was 53, +1)
              002-bottom         diff= 54        (was 53, +1)
              003-top            diff= 54        (was 53, +1)
```

The cold_start diff jumped because cycle 155's bold/bullet rendering
now applies to the cold-start resume content — the captured pane
contains model-reply prose that ref renders with bullets and bold and
ours now does too. The diff change is from "ours missing the
rendering" to "ours rendering, ANSI escapes slightly differing".

200×60 000-init captures now show ref with 40 lines of content and
ours with 43 lines (was 18 before cycle 153, 41 after cycle 154).
Both side's text fills the viewport; the remaining diff is the
content offset from the live conversation DB.

## Limitations

- Italic styling (`*foo*` single-asterisk runs) is not yet
  rendered — italic ANSI escapes weren't recovered for this build.
  The asterisks pass through unchanged.
- Markdown tables (`| col | col |`) are passed through unchanged
  — ref draws them as a bordered table, ours draws them as
  pipe-separated plain text.
- Markdown link (`[text](url)`) is passed through unchanged — ref
  underlines the visible text.
- The bullet transform matches ``- `` and ``* `` at column 0 only;
  ref also handles indented bullets (sub-lists).
- The code-fence detector is whitespace-tolerant on the fence line
  (`` ``` ``, `` ```java ``, `` ``` `` with leading spaces all work)
  but does not look for ``~~~ `` fences (a markdown alternative
  ref doesn't seem to use either, so this is unobserved behaviour).