// reader_frame_test.go — the reader's furniture column and its legend's
// degradation order.
//
// # What these pin
//
// Two defects that survived the reader's first round of tests because
// neither was asserted directly:
//
//  1. a part's label sat two cells *right* of the body it named, instead
//     of one level out from it
//  2. the key hint dropped "esc close" first on a narrow terminal, so the
//     60-column frame advertised four keys and none of them was the way
//     out
//
// Both are the kind of thing a frame renders without complaint, which is
// why they are pinned by measurement rather than by eye.
package debugpanel

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// labelledPanel is a panel with a labelled part open, so the label row
// and the body rows both exist.
func labelledPanel(t *testing.T, width, height int) *Panel {
	t.Helper()
	p := New(Seams{
		Width:  func() int { return width },
		Height: func() int { return height },
	})
	p.SetView(ViewTrajectory)
	p.reader = newReader("traj/1", Body{
		Title: "traj/1 · tool",
		Parts: []ReaderPart{
			{Label: "args", Text: "CommandLine: go build"},
			{Label: "result", Text: "ok"},
		},
	})
	return p
}

// leadOf returns how many cells precede the row's first visible glyph.
func leadOf(row string) int {
	plain := ansi.Strip(row)
	return len(plain) - len(strings.TrimLeft(plain, " >0123456789"))
}

// TestReaderLabelSitsLeftOfItsBody — the label is one level *out* from
// the text it names, so its text starts left of the body's, not right.
//
// It started right when the furniture branch trimmed readerBodyIndent
// from a row indented by readerLabelIndent: the TrimPrefix found nothing
// to remove, so the label kept its own indent on top of the gutter and
// landed two cells past its own body.
func TestReaderLabelSitsLeftOfItsBody(t *testing.T) {
	p := labelledPanel(t, 100, 24)
	body := readerBodyOf(p)

	var labelLead, textLead int
	for _, row := range body {
		plain := ansi.Strip(row)
		switch {
		case strings.Contains(plain, "args"), strings.Contains(plain, "result"):
			labelLead = leadOf(row)
		case strings.TrimSpace(plain) == "":
		case textLead == 0:
			textLead = leadOf(row)
		}
	}
	if labelLead == 0 || textLead == 0 {
		t.Fatalf("did not find both a label and a body row: label=%d text=%d", labelLead, textLead)
	}
	if labelLead >= textLead {
		t.Errorf("the label starts at cell %d and its body at %d; the label must sit left of the text it names",
			labelLead, textLead)
	}
	if want := textLead - (readerBodyIndent - readerLabelIndent); labelLead != want {
		t.Errorf("the label starts at cell %d, want %d (one level out from the body)", labelLead, want)
	}
}

// TestReaderLegendKeepsTheWayOut — every width down to the narrowest the
// panel supports still names the key that leaves the reader.
//
// legendRow drops whole entries from the tail, so the order of the
// entries is the degradation policy. "esc close" was last, which meant a
// 60-column terminal lost the exit before the conveniences.
func TestReaderLegendKeepsTheWayOut(t *testing.T) {
	for _, structured := range []bool{true, false} {
		legend := ReaderLegend(structured)
		for _, width := range []int{120, 100, 80, 60, 50, 40, 32} {
			row := ansi.Strip(legendRow(legend, width))
			if !strings.Contains(row, "esc close") {
				t.Errorf("structured=%v at %d columns the hint reads %q, which omits the way out",
					structured, width, strings.TrimSpace(row))
			}
			if w := ansi.StringWidth(row); w > width {
				t.Errorf("structured=%v at %d columns the hint is %d cells wide", structured, width, w)
			}
		}
	}
}

// TestReaderLegendDropsWholeEntries — the hint never ends in a fragment
// of a word, which is what a plain truncation produced ("esc c…").
func TestReaderLegendDropsWholeEntries(t *testing.T) {
	legend := ReaderLegend(true)
	entries := strings.Split(legend, "  ·  ")
	for _, width := range []int{120, 90, 70, 55, 45, 36} {
		row := strings.TrimSpace(ansi.Strip(legendRow(legend, width)))
		if strings.HasSuffix(row, "…") {
			t.Errorf("at %d columns the hint was truncated mid-entry: %q", width, row)
			continue
		}
		body := strings.TrimPrefix(row, "↑/↓ line  ·  ")
		if body == row {
			continue // only the first entry survived, which is fine
		}
		// Whatever survived must be a whole entry from the legend.
		for _, kept := range strings.Split(body, "  ·  ") {
			found := false
			for _, e := range entries {
				if e == kept {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("at %d columns the hint keeps %q, which is not a whole entry", width, kept)
			}
		}
	}
}
