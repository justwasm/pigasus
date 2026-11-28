// status_close_test.go — the mode word's close restores the row's baseline.
//
// The bug this pins: auto's word is opened with the dim foreground *and* bold,
// and the row closed it with DimGrayOpen, which sets a colour and leaves the
// intensity alone. The bold therefore ran on through the " · " separator and
// the model behind it, so the row rendered the model bold in auto mode and
// plain in plan and accept-edits — switching mode appeared to restyle the
// model, which never moved. The row also ended with the terminal still bold.
//
// These tests are about the escape pair, not the row: they apply a mode's open
// and its close to a running SGR state and check that the state afterwards is
// the row's baseline. The row's own rendering is pinned in
// internal/ui/views/statusrow.
package palette_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// sgrState is the attributes an SGR stream leaves set.
//
// Only the attributes the palette can emit are modelled; anything else is
// ignored rather than guessed at, so a new attribute has to be added here
// before it can be asserted — which is the point.
type sgrState struct {
	fg                             string
	bold, faint, italic, underline bool
}

// String renders the state so a failure names what leaked.
func (s sgrState) String() string {
	var parts []string
	if s.fg != "" {
		parts = append(parts, "fg="+s.fg)
	}
	for _, a := range []struct {
		name string
		on   bool
	}{
		{"bold", s.bold}, {"faint", s.faint},
		{"italic", s.italic}, {"underline", s.underline},
	} {
		if a.on {
			parts = append(parts, a.name)
		}
	}
	if len(parts) == 0 {
		return "(baseline)"
	}
	return strings.Join(parts, " ")
}

// replay applies every SGR escape in s to the state.
func (s sgrState) replay(seq string) sgrState {
	for i := 0; i < len(seq); i++ {
		if seq[i] != 0x1b || i+1 >= len(seq) || seq[i+1] != '[' {
			continue
		}
		j := i + 2
		start := j
		for j < len(seq) && seq[j] != 'm' {
			j++
		}
		if j >= len(seq) {
			break
		}
		params := strings.Split(seq[start:j], ";")
		i = j
		for k := 0; k < len(params); k++ {
			n, err := strconv.Atoi(params[k])
			if err != nil {
				continue
			}
			switch {
			case n == 0:
				s = sgrState{}
			case n == 1:
				s.bold = true
			case n == 2:
				s.faint = true
			case n == 3:
				s.italic = true
			case n == 4:
				s.underline = true
			case n == 22:
				s.bold, s.faint = false, false
			case n == 23:
				s.italic = false
			case n == 24:
				s.underline = false
			case n == 39:
				s.fg = ""
			case n == 38 && k+1 < len(params) && params[k+1] == "2" && k+4 < len(params):
				s.fg = "rgb(" + strings.Join(params[k+2:k+5], ",") + ")"
				k += 4
			case n == 38 && k+1 < len(params) && params[k+1] == "5" && k+2 < len(params):
				s.fg = "ansi(" + params[k+2] + ")"
				k += 2
			}
		}
	}
	return s
}

// baseline is the state the status row is drawn in before any mode word: the
// dim foreground and no intensity.
func baseline() sgrState {
	var s sgrState
	return s.replay(palette.DimGrayOpen)
}

// TestStatusModeCloseRestoresTheBaseline — a mode's open followed by its close
// leaves the row exactly as it found it.
//
// This is the invariant, stated without naming an escape: whatever a mode's
// word switches on, its close must switch off. The test walks every scheme so
// a close that only works for one scheme's slot values fails too.
func TestStatusModeCloseRestoresTheBaseline(t *testing.T) {
	// The scheme is package state; a test that moves it puts it back.
	prev, _ := palette.CurrentScheme()
	defer palette.SetScheme(prev)

	for _, name := range []string{"light", "solarized light", "dark", "terminal"} {
		palette.SetScheme(name)
		want := baseline()
		for _, mode := range []string{"auto", "plan", "accept-edits"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				open := palette.StatusModeColor(mode)
				if open == "" {
					t.Fatalf("StatusModeColor(%q) is empty under %q: the mode draws nothing", mode, name)
				}
				closeSeq := palette.StatusModeClose(mode)
				if closeSeq == "" {
					t.Fatalf("StatusModeClose(%q) is empty under %q: the open is never closed", mode, name)
				}
				got := want.replay(open).replay(closeSeq)
				if got != want {
					t.Errorf("open+close for %q under %q leaves %s, want the baseline %s: an attribute the open sets survives the close",
						mode, name, got, want)
				}
			})
		}
	}
}

// TestStatusModeCloseLeavesTheCallersState — the close undoes the mode's own
// attributes and nothing else.
//
// The row is a view: it may only undo what it did. A close written as a full
// reset (SGR 0) would also clear a background the caller had set around the
// row, so the close has to be the mode's own inverse rather than a hammer.
func TestStatusModeCloseLeavesTheCallersState(t *testing.T) {
	// A caller that set a background before drawing the row.
	const callerBg = "\x1b[48;2;10;20;30m"
	start := baseline().replay(callerBg)
	if !strings.Contains(callerBg, "48;2") {
		t.Fatal("the fixture's background is not a background escape")
	}

	for _, mode := range []string{"auto", "plan", "accept-edits"} {
		t.Run(mode, func(t *testing.T) {
			got := start.replay(palette.StatusModeColor(mode)).replay(palette.StatusModeClose(mode))
			// The state's foreground and intensity must be the baseline's.
			want := baseline()
			if got.fg != want.fg || got.bold || got.faint || got.italic || got.underline {
				t.Errorf("after open+close the row is %s, want the baseline %s", got, want)
			}
			// And the caller's background must have survived: a full reset
			// would have cleared it. The close must not contain SGR 0.
			if strings.Contains(palette.StatusModeClose(mode), "\x1b[0m") {
				t.Errorf("StatusModeClose(%q) = %q contains a full reset, which would clear the caller's background",
					mode, palette.StatusModeClose(mode))
			}
		})
	}
}

// TestStatusModeCloseIsEmptyForNoMode — the stored-empty mode opens nothing,
// so it must close nothing either.
//
// A close without an open would reset the dim foreground the row is drawn in
// and drop the rest of the line to the terminal's default colour.
func TestStatusModeCloseIsEmptyForNoMode(t *testing.T) {
	for _, mode := range []string{"", "default", "not-a-mode"} {
		if got := palette.StatusModeClose(mode); got != "" {
			t.Errorf("StatusModeClose(%q) = %q, want empty", mode, got)
		}
	}
}
