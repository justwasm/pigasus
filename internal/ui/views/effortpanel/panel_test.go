package effortpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// plainLines strips the SGR escapes from the panel's frame.
func plainLines(p *Panel) []string {
	var out []string
	for _, l := range p.Lines() {
		out = append(out, stripSGR(l))
	}
	return out
}

// The effort slider geometry was recovered by driving REF's /effort
// panel with ←/→: three 15-cell groups, the active one collapsing to a
// single ◉, and the bar coloured only up to and including the active
// marker. The label row's plain text is the same at every level — the
// group split changes, not the glyphs.
//
// The blurbs carry the reference's own em dash (U+2014). Cycle 383's
// survey read the reference's `strings` dump as a newline there — but
// `strings` splits a printable run at every non-ASCII byte, and the em
// dash is one. The binary settles it (agy 1.4.12, offset 0x3740275):
// "Faster responses, lighter reasoning \xe2\x80\x94 great for simpler
// tasks". The reference's rendered panel agrees: the cycle-208 capture
// and the cycle `model-picker-search` byte diff against the clone both
// show the blurb on one line with the em dash.
func TestPanelLinesLayout(t *testing.T) {
	const labels = "          low          medium          high      "
	tests := []struct {
		level  int
		bar    string
		blurb  string
		active string
		head   string // the coloured head, right after the level colour
	}{
		{
			level:  EffortHigh,
			bar:    "  ◂        ●━━━━━━━━━━━━━━●━━━━━━━━━━━━━━◉        ▸",
			blurb:  "    Deepest reasoning for complex problems — slower but strongest  (current)",
			active: PanelEffHighOpen,
			head:   "       ●━━━━━━━━━━━━━━●━━━━━━━━━━━━━━◉",
		},
		{
			level:  EffortMedium,
			bar:    "  ◂        ●━━━━━━━━━━━━━━◉──────────────○        ▸",
			blurb:  "    Balanced speed and reasoning quality for most tasks",
			active: PanelEffMedOpen,
			head:   "       ●━━━━━━━━━━━━━━◉",
		},
		{
			level:  EffortLow,
			bar:    "  ◂        ◉──────────────○──────────────○        ▸",
			blurb:  "    Faster responses, lighter reasoning — great for simpler tasks",
			active: PanelEffLowOpen,
			head:   "       ◉",
		},
	}
	for _, tc := range tests {
		p := New(Seams{Current: func() string { return "high" }})
		p.SetCursor(tc.level)
		got := plainLines(p)
		if got[0] != "Set Effort" {
			t.Errorf("level %d title = %q", tc.level, got[0])
		}
		if got[2] != tc.bar {
			t.Errorf("level %d bar:\n got: %q\nwant: %q", tc.level, got[2], tc.bar)
		}
		if got[3] != labels {
			t.Errorf("level %d labels:\n got: %q\nwant: %q", tc.level, got[3], labels)
		}
		if got[4] != tc.blurb {
			t.Errorf("level %d blurb:\n got: %q\nwant: %q", tc.level, got[4], tc.blurb)
		}
		if got[6] != "Keyboard: ←/→ Effort  enter Select  esc Go Back" {
			t.Errorf("level %d legend = %q", tc.level, got[6])
		}
		// The bar's head carries the level colour; the tail is 238.
		raw := p.Lines()[2]
		if !strings.Contains(raw, tc.active+tc.head) {
			t.Errorf("level %d head should be %q: %q", tc.level, tc.active+tc.head, raw)
		}
		if tail := raw[strings.Index(raw, tc.head)+len(tc.head):]; tc.level != EffortHigh {
			if !strings.Contains(tail, PanelGray238) || !strings.Contains(tail, "○") {
				t.Errorf("level %d tail should be a dim empty run: %q", tc.level, tail)
			}
		} else if !strings.Contains(tail, PanelGray238) {
			t.Errorf("level %d should still close its tail in 238: %q", tc.level, tail)
		}
	}
	// Exactly one level carries "(current)" — the one matching the
	// session's effort (high).
	for _, level := range []int{EffortLow, EffortMedium, EffortHigh} {
		p := New(Seams{Current: func() string { return "high" }})
		p.SetCursor(level)
		n := strings.Count(plainLines(p)[4], "(current)")
		if level == EffortHigh && n != 1 {
			t.Errorf("high should be marked current, got %d", n)
		}
		if level != EffortHigh && n != 0 {
			t.Errorf("level %d should not be marked current", level)
		}
	}
}

// The panel opens on the tier the session runs at.
//
// The seam hands over the *word*: the index is an index into the
// model's own list of tiers, and only the host knows which model the
// session runs at.
func TestPanelOpensOnTheCurrentTier(t *testing.T) {
	for _, tc := range []struct {
		word  string
		level int
	}{
		{"low", EffortLow},
		{"medium", EffortMedium},
		{"high", EffortHigh},
	} {
		p := New(Seams{Current: func() string { return tc.word }})
		if p.Cursor() != tc.level {
			t.Errorf("cursor for %q = %d, want %d", tc.word, p.Cursor(), tc.level)
		}
	}
}

// The slider is horizontal: ←/→ move the marker, ↑/↓ do nothing, and
// the marker never leaves the bar.
func TestPanelKeysMoveTheMarker(t *testing.T) {
	p := New(Seams{Current: func() string { return "high" }})
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if p.Cursor() != EffortHigh {
		t.Fatalf("down moved the marker to %d", p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyLeft})
	p.HandleKey(tea.Key{Code: tea.KeyLeft})
	if p.Cursor() != EffortLow {
		t.Errorf("cursor = %d, want low", p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyLeft}) // clamps
	if p.Cursor() != EffortLow {
		t.Errorf("cursor = %d, want low (clamped)", p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyRight})
	if p.Cursor() != EffortMedium {
		t.Errorf("cursor = %d, want medium", p.Cursor())
	}
}

// Enter applies the tier the marker sits on; Esc leaves the panel.
func TestPanelKeysApplyAndClose(t *testing.T) {
	applied := -1
	closed := false
	p := New(Seams{
		Current: func() string { return "low" },
		Apply:   func(level int) tea.Cmd { applied = level; return nil },
		Close:   func() tea.Cmd { closed = true; return nil },
	})
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("right not handled")
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	if applied != EffortMedium {
		t.Errorf("applied level = %d, want medium", applied)
	}
	if closed {
		t.Error("enter must not close the panel on its own")
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if !closed {
		t.Error("esc did not close the panel")
	}
}

// The lit run stops at the active ◉; the 14 cells after it are dim.
func TestEffortBarColourBoundary(t *testing.T) {
	on, off := EffortBarSegments(EffortLow)
	if on != strings.Repeat(" ", 7)+"◉" {
		t.Errorf("low on-run = %q", on)
	}
	if off != strings.Repeat("─", 14)+"○"+strings.Repeat("─", 14)+"○"+strings.Repeat(" ", 7) {
		t.Errorf("low off-run = %q", off)
	}

	on, off = EffortBarSegments(EffortMedium)
	if !strings.HasSuffix(on, "◉") || strings.Contains(on, "─") {
		t.Errorf("medium on-run = %q", on)
	}
	if !strings.HasPrefix(off, strings.Repeat("─", 14)) {
		t.Errorf("medium off-run = %q", off)
	}

	on, off = EffortBarSegments(EffortHigh)
	if off != strings.Repeat(" ", 7) {
		t.Errorf("high off-run = %q", off)
	}
	if !strings.HasSuffix(on, "◉") {
		t.Errorf("high on-run = %q", on)
	}
}

// A model with a fixed reasoning budget has no adjustable effort, and
// the reference's answer is the panel's whole view.
//
// The reference's own string — 【strings】 agy 1.4.12, file offset
// 0x36f64d5, and the binary stores the length 45 beside it — carries no
// trailing period and names no model. The command's own answer for the
// same situation is a *different* string ("Effort isn't adjustable for
// %s."), and the two are not interchangeable.
func TestPanelNotAdjustable(t *testing.T) {
	applied := false
	p := New(Seams{
		// A model that publishes no level at all.
		Tiers:   func() []string { return nil },
		Apply:   func(level int) tea.Cmd { applied = true; return nil },
		Close:   func() tea.Cmd { return nil },
		Current: func() string { return "high" },
	})
	got := plainLines(p)
	if len(got) != 1 {
		t.Fatalf("lines = %d, want 1: %q", len(got), got)
	}
	if got[0] != "Effort isn't adjustable for the current model" {
		t.Errorf("line = %q", got[0])
	}
	// Enter applies nothing: there is no tier to apply.
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	if applied {
		t.Error("a model with a fixed reasoning budget must not apply a tier")
	}
}

// A model that publishes one level has a fixed reasoning budget too:
// the reference's rule is "more than one level"
// (【反编译】 backend.CurrentEffortSelection, 0x10274e9b0 — `cmp x1, 1;
// cset x7, gt`).
func TestPanelSingleLevelIsNotAdjustable(t *testing.T) {
	p := New(Seams{
		Tiers:   func() []string { return []string{"medium"} },
		Current: func() string { return "medium" },
	})
	got := plainLines(p)
	if len(got) != 1 || got[0] != NotAdjustableMessage {
		t.Errorf("lines = %q, want the not-adjustable answer", got)
	}
}

// A model that publishes five levels gets a five-place slider, and the
// marker can reach the heaviest of them.
//
// The bar's field is the reference's own 45 cells, split into five
// groups of nine (45 / 5 = 9, odd, so the margins stay symmetric at
// four cells each). The labels are centred in their own group.
func TestPanelFiveTiers(t *testing.T) {
	tiers := []string{"low", "medium", "high", "xhigh", "max"}
	applied := -1
	p := New(Seams{
		Tiers:   func() []string { return tiers },
		Current: func() string { return "high" },
		Apply:   func(level int) tea.Cmd { applied = level; return nil },
	})
	if p.RowCount() != 5 {
		t.Fatalf("row count = %d, want 5", p.RowCount())
	}
	// The marker opens on the tier the session runs at — index 2.
	if p.Cursor() != 2 {
		t.Fatalf("cursor = %d, want 2", p.Cursor())
	}
	got := plainLines(p)
	// The panel opens on "high" — the third of the five tiers — so the
	// lit run reaches the third marker and the rest is dim.
	wantBar := "  ◂     ●━━━━━━━━●━━━━━━━━◉────────○────────○     ▸"
	if got[2] != wantBar {
		t.Errorf("bar:\n got: %q\nwant: %q", got[2], wantBar)
	}
	wantLabels := "    " + "   low   " + " medium  " + "  high   " + "  xhigh  " + "   max   "
	if got[3] != wantLabels {
		t.Errorf("labels:\n got: %q\nwant: %q", got[3], wantLabels)
	}
	wantBlurb := "    Deepest reasoning for complex problems — slower but strongest  (current)"
	if got[4] != wantBlurb {
		t.Errorf("blurb:\n got: %q\nwant: %q", got[4], wantBlurb)
	}
	// The marker walks to the heaviest tier and Enter applies *its*
	// index — four, not two.
	for i := 0; i < 2; i++ {
		if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyRight}); !handled {
			t.Fatal("right not handled")
		}
	}
	if p.Cursor() != 4 {
		t.Fatalf("cursor = %d, want 4", p.Cursor())
	}
	got = plainLines(p)
	wantBar = "  ◂     ●━━━━━━━━●━━━━━━━━●━━━━━━━━●━━━━━━━━◉     ▸"
	if got[2] != wantBar {
		t.Errorf("max bar:\n got: %q\nwant: %q", got[2], wantBar)
	}
	wantBlurb = "    Maximum reasoning depth — slowest, strongest results"
	if got[4] != wantBlurb {
		t.Errorf("max blurb:\n got: %q\nwant: %q", got[4], wantBlurb)
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	if applied != 4 {
		t.Errorf("applied level = %d, want 4", applied)
	}
	// The bottom of the bar is the model's heaviest tier, which is not
	// the reference's own three.
	p.SetCursor(0)
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnd}); !handled {
		t.Fatal("end not handled")
	}
	if p.Cursor() != 4 {
		t.Errorf("cursor after End = %d, want 4", p.Cursor())
	}
}
