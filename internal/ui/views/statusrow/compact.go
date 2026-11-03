// compact.go — the status row's compaction label.
//
// The label is the reference's own: a Braille spinner, the phrase and
// the token figures, all in the row's dim grey.
//
//	⠿  Compacting context…  184 K → 1.2 M (12s)
//	⠿  Compacting context… (12s)
//	Compacted 184 K → 1.2 M (12s)
//
// A running compaction carries the spinner and no "Compacted"; a
// finished one drops the spinner and says "Compacted".

package statusrow

import (
	"time"

	"github.com/justwasm/pigasus/internal/core/tokenfmt"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
)

// compactLabel is the row's compaction label.
//
// The elapsed time is measured against the wall clock while the
// compaction runs, so the label is drawn with the clock it has.
func compactLabel(c Compact) string {
	frame := usagepanel.SpinnerFrame(c.SpinnerFrame)
	if c.End.IsZero() {
		if c.TokensAfter > 0 {
			return frame + "  Compacting context…  " +
				tokenfmt.K(c.TokensBefore) + " → " +
				tokenfmt.K(c.TokensAfter) + " (" +
				compactDuration(c.Start, time.Now()) + ")"
		}
		return frame + "  Compacting context… (" +
			compactDuration(c.Start, time.Now()) + ")"
	}
	return "Compacted " +
		tokenfmt.K(c.TokensBefore) + " → " +
		tokenfmt.K(c.TokensAfter) + " (" +
		compactDuration(c.Start, c.End) + ")"
}

// compactDuration is the elapsed time of a compaction.
//
// A zero start or a zero end means "no measurement"; a clock that went
// backwards is clamped to zero.
func compactDuration(start, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "0s"
	}
	d := end.Sub(start)
	if d < 0 {
		d = 0
	}
	return d.Truncate(time.Second).String()
}
