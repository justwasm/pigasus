# cycle 269 — ANSI variant trims glamour's trailing newline

## TL;DR

After cycle 268 plumbed the real terminal width through to
glamour, the chat transcript picked up an extra empty row at the
end of every model reply. Cause: `mdrender.ANSI(reply, width)` —
the final-render path the turn-end handler calls — did not trim
the lone trailing `\n` glamour always appends, so
`strings.Split(reply, "\n")` produced one phantom empty row the
streaming path never had. The streaming path's per-line
`LineANSI` already trimmed (stream_glamour.go:190), so the bug
only appeared at final render, not while the reply was still
streaming in — the asymmetry the user reported.

Fix: in `renderWithFallthrough`, the ANSI variant (the only
variant postProcessPlain didn't already cover) gets the same
`strings.TrimRight(out, "\n")` the Plain / Quiet / User variants
have had since cycle 175. Total diff: a 12-line `else` branch in
one function and a 100-line pin test. No regex, no per-line
post-process pass, no `postProcessTrim` rewrite. The fix matches
what crush does (crush's streaming markdown renderer trims the
trailing newline at the same place) without needing any of crush's
prefix-cache machinery.

## Root cause

Glamour v2.0.1's ANSI renderer always appends a single trailing
`\n` to its `Render(...)` output — verifiable by reading any
glamour-based programme, or by running the diagnostic at
`internal/mdrender/cycle269_trailing_trim_test.go`. Pre-cycle-268
this was hidden because glamour always wrapped at 80 cols (the
hard default in `glamour.go:25 defaultWidth`) so the transcript
terminal column was also 80, and `postProcessPlain` already
trimmed the trailing `\n` for the Plain / Quiet / User variants.
The ANSI variant — the chat-transcript palette the user sees —
was the only variant without the trim, and it was rendering into
a transcript whose downstream split-on-`\n` already assumed no
trailing newline.

Once cycle 268 wired the actual terminal width through
(`internal/mdrender/glamour_adapter.go:486 currentWidth`),
glamour's trailing `\n` was still present in the ANSI output but
the wider terminal made the resulting phantom row visually
significant in a way it wasn't at 80 cols. A reply with N content
lines — say `first\n\nsecond`, three rows after a proper split —
came out as four rows: `[first, blank, second, ""]`. The last
row is `""`, gets `padTo`'d to `m.width` cells of dim pad, and
appears on screen as one full-width blank row below the last
paragraph.

The streaming path was unaffected: each `backendChunkMsg` calls
`LineANSI(line)` (stream_glamour.go:160), and `LineANSI` already
ends with `return strings.TrimRight(restored, "\n"), nil`. That
asymmetry between per-line trim and whole-doc no-trim is exactly
the bug the user diagnosed: "the bug only appears at final
render, not during streaming."

## What changed

```diff
--- a/internal/mdrender/glamour_adapter.go
+++ b/internal/mdrender/glamour_adapter.go
@@ -432,6 +132,8 @@ func renderWithFallthrough(v mdVariant, src string, width int) (string, error) {
 	if v == variantPlain || v == variantQuiet || v == variantUser {
 		out = postProcessPlain(out)
+	} else {
+		// Cycle 269: ANSI variant also trims the lone trailing
+		// newline glamour appends so a reply with N content
+		// lines renders to N rows, not N+1.
+		out = strings.TrimRight(out, "\n")
 	}
 	return out, nil
 }
```

That's the entire production change. The pin test
`internal/mdrender/cycle269_trailing_trim_test.go` covers the
five cases that matter — empty input, single paragraph, two
paragraphs, three paragraphs, and heading-then-paragraph — at
three widths (80, 120, 250). The second test in the file,
`TestCycle269_AnsiNoPhantomTrailingRow`, is the streaming-vs-final
contract pin: the ANSI path's `len(strings.Split(out, "\n"))`
must equal the row count a user would get from manually walking
the same content.

## Why this is the right shape

The cycle-269 first attempt (commit `888fd1f`, since reverted
before merge) took a different route: a `postProcessTrim` regex
that stripped the trailing pad bytes from every glamour-rendered
line, on the theory that glamour's per-line right-pad was
inflating the row height. Two reasons that was the wrong
direction:

1. **It contradicted the user's own observation.** The user
   reported the bug only at final render, but the regex would
   have affected every per-line glamour call — including the
   streaming path that the user explicitly said was fine. If
   the per-line pad was the problem, the streaming path would
   have shown it too.

2. **It broke mouse selection.** Cycle 174's
   `applySelectionHighlight` (cmd/wagyu/tui_selection.go:168)
   uses `ansi.StringWidth(row)` as the upper bound for the
   selection rectangle. With cycle 269-r's regex stripping the
   dim-pad bytes from each line, `rowWidth` collapsed to the
   actual content width (~50 cells), and `to = ec + 1` clamped
   to `rowWidth` so the visual highlight stopped at the
   content edge instead of extending to the right wall. The
   user reported this as "每行只能选中行首的一些部分，行末无法
   选中"; reverting cycle 269-r's regex restores full-width
   selection because the rows stay full-width (padTo, not
   postProcessTrim, owns the dim pad).

The fix in this cycle is the one the user actually asked for:
"其实我不相信这个 markdown 问题解决起来这么麻烦，你完全可以
参考 ../crush 的做法". Crush does the same `TrimRight("\n")` at
the same boundary (`../crush/internal/ui/chat/streaming_markdown.go`
around the equivalent render-and-trim step). The fix is also
exactly what cycle 175 already did for the Plain / Quiet / User
variants — the only reason ANSI didn't get the same treatment
was that, pre-cycle-268, glamour always wrapped at 80 cols so
the bug was invisible in the only variant that didn't trim.

## Streaming path sanity check

The streaming path (`LineANSI` → `LineANSIBoundedWithCache` →
`replaceRange`) was already producing tight rows. With this fix
the final-render path's `replaceRange(m.replyStart,
len(m.response), indented)` (tui_run.go:3535) now produces the
same number of rows as the streaming path had at the moment the
final glamour pass took over, so the replaceRange swap is
1-to-1 instead of N-to-N+1. No mid-stream raw row gets dropped
on the floor at final render.

## Mouse selection

No code change was needed beyond reverting cycle 269-r's
postProcessTrim. With the fix in place, `rowWidth` is
`ansi.StringWidth(row)` of the untrimmed row — which is
`m.width` for any padded row (because `padTo`
cmd/wagyu/tui_viewport.go:773 keeps rows at `m.width` cells) —
and selection extends to the right wall as cycle 174 designed.

## Files

| File | Lines | Purpose |
|---|---|---|
| `internal/mdrender/glamour_adapter.go` | +12 | The fix |
| `internal/mdrender/cycle269_trailing_trim_test.go` | +106 | Pin tests |

No changes to `cmd/wagyu/*.go`, no changes to the streaming
machinery, no changes to the cycle-268 width plumbing. The fix
is scoped to the single ANSI-render function the user reported.

## Verification

```
$ go test -count=1 ./internal/mdrender/
ok      github.com/chadgpt/openagy/internal/mdrender  0.477s

$ go test -count=1 ./internal/...
ok      github.com/chadgpt/openagy/internal/mdrender       0.477s
ok      github.com/chadgpt/openagy/internal/convstore      0.703s
...
ok      github.com/chadgpt/openagy/internal/pigrt          1.375s
...
```

(The `signal: broken pipe` line that appears under
`./cmd/wagyu/`'s run is from the help-subprocess test running
with `head` truncated output; it pre-dates this cycle and is not
a regression.)

## Repro

The diagnostic at `internal/mdrender/cycle269_trailing_trim_test.go`
runs in any `go test` invocation. For a manual smoke test:

```go
import mdrender "github.com/chadgpt/openagy/internal/mdrender"

mdrender.SetCurrentWidth(250)
out, _ := mdrender.ANSI("first paragraph\n\nsecond paragraph", 250)
// pre-cycle-269:  out ends with "\n"   → strings.Split gives 4 rows
// post-cycle-269: out does NOT end with "\n" → strings.Split gives 3 rows
```

## 硬约束 / limitations

- The fix only addresses the trailing-newline case. The
  per-line right-pad glamour still emits is left alone — cycle 268
  shipped it intentionally (so the transcript rows flush to the
  right wall) and the mouse-selection code depends on it. Do
  not "improve" this by trimming the per-line pad; cycle 269-r's
  first attempt did and it broke selection.
- Glamour may in some configurations emit `\n\n` at the end
  rather than `\n` (e.g. trailing blockquote at the end of a
  reply). `TrimRight` collapses both. If a future glamour
  version starts emitting content after the trailing `\n`
  (e.g. a foot divider), the fix will need a more targeted
  trim. Out of scope for this cycle.
- The diag-vs-pin split (TestCycle269_TrimTrailingNewline for
  the basic count, TestCycle269_AnsiNoPhantomTrailingRow for
  the streaming contract) is the minimum test surface that
  catches a regression without being brittle to glamour's
  internal line-counting quirks (multiple consecutive `\n`
  collapsing into one blank row, etc.).