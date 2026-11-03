package compaction

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/palette"
)

// The reference paints the /compact completion as two *centred* rows
// (agy 1.4.5, live pty capture — docs/cycles/auto-compact-parity/captures.md):
//
//	──────…── Conversation compacted ──…──          #586E75
//	    3 steps summarized · context 13.1k → ~17.9k (271 summary) · took 7s
//
// Cycle 237's clone printed a "⎿  Compacted 12 messages (-87%)" row
// instead. The reference has no such row: the compaction is announced by a
// full-width rule with the label centred inside it, and the numbers live
// on the next line, also centred.
//
// Geometry, measured at three terminal widths (the rule is width−1
// wide and the stats line is centred on the *full* width):
//
//	width   rule dashes (before/after)   rule width   stats column
//	  100            37 / 38                 99            17
//	  120            47 / 48                119            27
//	  160            67 / 68                159            47
//
// The rule therefore spans width−1 columns with the label centred
// inside it (floor on the left half, ceil on the right), and the stats
// line starts at column (width−len)/2 + 1.
const (
	// DividerLabel is the reference's centred label, spaces
	// included — the rule reads "─── Conversation compacted ───".
	DividerLabel = " Conversation compacted "

	// DividerInset is the number of columns the reference leaves
	// off the right edge of the rule (width−1 at every measured
	// width).
	DividerInset = 1
)

// DividerRow renders the reference's "Conversation compacted"
// rule. A non-positive width yields the bare label (no rule), which
// keeps the row readable in narrow terminals and in the tests that do
// not set a width.
func DividerRow(width int) string {
	body := DividerLabel
	if width > 0 {
		dashes := width - DividerInset - len(DividerLabel)
		if dashes > 0 {
			before := dashes / 2
			after := dashes - before
			body = strings.Repeat("─", before) + DividerLabel + strings.Repeat("─", after)
		}
	}
	return palette.SubtleOpen + body + "\x1b[m"
}

// StatsText is the reference's stats line, unstyled.
//
// The shape is the reference's:
//
//	3 steps summarized · context 13.1k → ~17.9k (271 summary) · took 7s
//
// The summary size hangs off the context group (a space, not a "·"),
// and the groups are separated by "·".
//
// The four fields are optional so the two compaction paths can share
// the shape: the generic path (cycle 237) knows the step count, the
// summary size and the elapsed time; PiG's end event only carries the
// token counts. A negative value means "not measured" and drops the
// field; a step count of zero drops the step group.
//
// The meaning of the reference's "context A → ~B" pair is resolved by
// the agy 1.4.5 binary (decompiled from cli/render/render's
// compactionStatsParts and CompactionTokens):
//
//   - A is the *model-reported* prompt-token count of the compaction
//     request — the context size at the moment of the compaction. The
//     reference's checkpoint config sets use_model_reported_token_count.
//   - B is the *base context* estimate — the system prompt plus the
//     tool declarations at the reference's three bytes per token — plus
//     the summary's own size.
//
// The two numbers are therefore on different scales: A counts the
// model's real tokens, B counts the reference's three-bytes-per-token
// estimate of the base context. That is why B is larger than A in every
// reference capture ("context 13.1k → ~17.9k") — it is not a
// before/after of one quantity.
func StatsText(steps, beforeTokens, afterTokens, summaryTokens int, took time.Duration) string {
	parts := make([]string, 0, 3)
	if steps > 0 {
		parts = append(parts, fmt.Sprintf("%d steps summarized", steps))
	}
	context := fmt.Sprintf("context %s → ~%s",
		TokenMagnitude(beforeTokens), TokenMagnitude(afterTokens))
	if summaryTokens >= 0 {
		context += fmt.Sprintf(" (%d summary)", summaryTokens)
	}
	parts = append(parts, context)
	if took >= 0 {
		parts = append(parts, fmt.Sprintf("took %ds", int(took.Seconds())))
	}
	return strings.Join(parts, " · ")
}

// StatsRow renders the stats line centred in width.
//
// The centring is by *display cells*, not bytes: the stats text
// contains "·" (2 bytes, 1 cell) and "→" (3 bytes, 1 cell), so a
// byte count would push the line two columns to the left of where the
// reference puts it. The reference centres with a cursor move to
// column (width−cells)/2 + 1; padding with the same number of spaces
// lands the text in the same column.
func StatsRow(width int, steps, beforeTokens, afterTokens, summaryTokens int, took time.Duration) string {
	text := StatsText(steps, beforeTokens, afterTokens, summaryTokens, took)
	cells := ansi.StringWidth(text)
	pad := 0
	if width > cells {
		pad = (width - cells) / 2
	}
	return palette.DimGrayOpen + strings.Repeat(" ", pad) + text + "\x1b[m"
}

// TokenMagnitude formats a token count the way the reference's
// stats row does: thousands with one decimal and a "k" suffix
// ("13.1k"), plain integers below a thousand.
func TokenMagnitude(n int) string {
	if n < 0 {
		n = 0
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}
