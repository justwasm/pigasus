package compaction

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// TestCompactDividerGeometry pins the reference's rule geometry.
//
// Measured on agy 1.4.5 with live pty captures at three terminal
// widths (docs/cycles/auto-compact-parity/captures.md): the rule is
// width−1 columns wide and the label " Conversation compacted " is
// centred inside it, floor on the left half and ceil on the right.
func TestCompactDividerGeometry(t *testing.T) {
	cases := []struct {
		width  int
		before int
		after  int
	}{
		{100, 37, 38},
		{120, 47, 48},
		{160, 67, 68},
	}
	for _, c := range cases {
		plain := ansi.Strip(DividerRow(c.width))
		if got := strings.Count(plain, "─"); got != c.before+c.after {
			t.Errorf("width %d: rule has %d dashes, want %d (%d/%d)",
				c.width, got, c.before+c.after, c.before, c.after)
		}
		if !strings.Contains(plain, " Conversation compacted ") {
			t.Errorf("width %d: rule missing the label: %q", c.width, plain)
		}
		// The label sits between the two dash runs.
		idx := strings.Index(plain, " Conversation compacted ")
		if got := strings.Count(plain[:idx], "─"); got != c.before {
			t.Errorf("width %d: %d dashes before the label, want %d", c.width, got, c.before)
		}
		if got := strings.Count(plain[idx:], "─"); got != c.after {
			t.Errorf("width %d: %d dashes after the label, want %d", c.width, got, c.after)
		}
		// The whole rule spans width−1 columns.
		if len([]rune(plain)) != c.width-1 {
			t.Errorf("width %d: rule is %d columns wide, want %d",
				c.width, len([]rune(plain)), c.width-1)
		}
	}
}

// TestCompactDividerNarrowWidth pins the degenerate cases: a
// non-positive width yields the bare label, and a width too small to
// hold the rule does not emit a broken half-rule.
func TestCompactDividerNarrowWidth(t *testing.T) {
	if got := ansi.Strip(DividerRow(0)); got != " Conversation compacted " {
		t.Errorf("width 0: rule = %q, want the bare label", got)
	}
	if got := ansi.Strip(DividerRow(10)); got != " Conversation compacted " {
		t.Errorf("width 10: rule = %q, want the bare label", got)
	}
}

// TestCompactStatsRowCentring pins the stats row's display-cell
// centring against the reference capture at width 100.
//
// The reference's column is 17 (16 leading spaces) for:
//
//	"3 steps summarized · context 13.1k → ~17.9k (271 summary) · took 7s"
//
// (67 display cells wide; (100−67)/2 = 16 spaces).
func TestCompactStatsRowCentring(t *testing.T) {
	const width = 100
	text := StatsText(3, 13_100, 17_900, 271, 7*time.Second)
	if cells := ansi.StringWidth(text); cells != 67 {
		t.Fatalf("stats line is %d cells, want 67 (text: %q)", cells, text)
	}

	row := StatsRow(width, 3, 13_100, 17_900, 271, 7*time.Second)
	plain := ansi.Strip(row)
	leading := len(plain) - len(strings.TrimLeft(plain, " "))
	if leading != 16 {
		t.Errorf("width %d: %d leading spaces, want 16 (column 17)", width, leading)
	}
	if !strings.HasPrefix(row, "\x1b[38;2;101;123;131m") {
		t.Errorf("stats row missing Solarized base0 colour escape: %q", row)
	}
	if !strings.HasSuffix(row, "\x1b[m") {
		t.Errorf("stats row missing trailing reset: %q", row)
	}
}

// TestCompactStatsTextOmissions verifies optional fields drop
// cleanly without leaving trailing or duplicated "·" separators.
func TestCompactStatsTextOmissions(t *testing.T) {
	full := StatsText(4, 400, 5, 5, 0)
	if full != "4 steps summarized · context 400 → ~5 (5 summary) · took 0s" {
		t.Errorf("full row mismatch: %q", full)
	}

	// PiG path: no step count, no summary size, no duration.
	partial := StatsText(0, 184_000, 42_000, -1, -1)
	if partial != "context 184.0k → ~42.0k" {
		t.Errorf("partial row mismatch: %q", partial)
	}
	if strings.Contains(partial, "·") {
		t.Errorf("single-group partial row should not contain '·': %q", partial)
	}
}

func TestCompactTokenMagnitude(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1.0k"},
		{13_100, "13.1k"},
		{17_900, "17.9k"},
		{184_000, "184.0k"},
		{-5, "0"},
	}
	for _, c := range cases {
		if got := TokenMagnitude(c.in); got != c.want {
			t.Errorf("TokenMagnitude(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
