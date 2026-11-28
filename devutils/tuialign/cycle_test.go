// cycle_test.go — the mode-cycle table's two checks, on their own.
//
// The live walk in 49_mode_cycle_order proves the clone's cycle matches the
// table. These tests prove the *checks* can fail: a walker whose assertions
// pass on anything is a photograph with a timeout, and the whole reason the
// scene moved from ASSERT_NOT_TEXT to a table is that the old form could not
// distinguish a correct lap from a lap that skipped a stop.
package main

import (
	"strings"
	"testing"
)

// row builds a status-row line: the hint, the padding, then the segment.
func row(hint, segment string) string {
	pad := 80 - len(hint) - len(segment)
	if pad < 2 {
		pad = 2
	}
	return hint + strings.Repeat(" ", pad) + segment
}

// screen assembles a frame with a composer line and a status row.
func screen(composer, statusRow string) string {
	return strings.Join([]string{
		"  ▀▀▄      ▄▀▀    Pigasus CLI 1.5.18",
		"",
		"────────────────────────────────────",
		composer,
		"────────────────────────────────────",
		statusRow,
		"",
	}, "\n")
}

func TestCheckStatusRowTail_AcceptsEachMode(t *testing.T) {
	for _, tc := range []struct{ name, segment string }{
		{"plan", "plan · Gemini 3.8 Flash · high"},
		{"accept-edits", "accept-edits · Gemini 3.8 Flash · high"},
		{"auto", "auto · Gemini 3.8 Flash · high"},
		{"default", "Gemini 3.8 Flash · high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := screen("> ", row("? for shortcuts", tc.segment))
			if err := checkStatusRowTail(s, tc.segment); err != nil {
				t.Errorf("checkStatusRowTail(%q) = %v, want nil", tc.segment, err)
			}
		})
	}
}

// TestCheckStatusRowTail_RejectsAnotherMode — the assertion names the mode it
// expects, so a row that is a different mode must fail.
func TestCheckStatusRowTail_RejectsAnotherMode(t *testing.T) {
	s := screen("> ", row("? for shortcuts", "accept-edits · Gemini 3.8 Flash · high"))
	err := checkStatusRowTail(s, "auto · Gemini 3.8 Flash · high")
	if err == nil {
		t.Fatal("the row is accept-edits but the check accepted auto")
	}
	if !strings.Contains(err.Error(), "does not end with") {
		t.Errorf("error = %v, want it to name the missing segment", err)
	}
}

// TestCheckStatusRowTail_RejectsAPrefixedMode — the case the padding rule
// exists for.
//
// `auto · plan · Gemini 3.8 Flash · high` ends with the plan segment, so a
// bare suffix test would report the auto stop as a correct plan stop. The
// padding check is what makes the assertion about the whole segment.
func TestCheckStatusRowTail_RejectsAPrefixedMode(t *testing.T) {
	s := screen("> ", row("? for shortcuts", "auto · plan · Gemini 3.8 Flash · high"))
	err := checkStatusRowTail(s, "plan · Gemini 3.8 Flash · high")
	if err == nil {
		t.Fatal("a row carrying two mode words satisfied the single-mode check")
	}
	if !strings.Contains(err.Error(), "padding") {
		t.Errorf("error = %v, want it to explain the segment must start at the padding", err)
	}
}

// TestCheckStatusRowTail_RejectsTheDefaultStopWhenAModeIsNamed — the default
// stop's segment is the model alone, which is a suffix of every named mode's
// segment. Without the padding rule the default row's check would pass in
// plan, auto and accept-edits, and the stop that matters most — the one that
// is easy to fold onto auto — would never fail.
func TestCheckStatusRowTail_RejectsTheDefaultStopWhenAModeIsNamed(t *testing.T) {
	for _, mode := range []string{"auto", "plan", "accept-edits"} {
		s := screen("> ", row("? for shortcuts", mode+" · Gemini 3.8 Flash · high"))
		if err := checkStatusRowTail(s, "Gemini 3.8 Flash · high"); err == nil {
			t.Errorf("the default stop's check passed on the %q row", mode)
		}
	}
}

// TestCheckStatusRowTail_RejectsAMissingRow — an empty or truncated frame
// must fail rather than pass by finding nothing to contradict it.
func TestCheckStatusRowTail_RejectsAMissingRow(t *testing.T) {
	if err := checkStatusRowTail("", "Gemini 3.8 Flash · high"); err == nil {
		t.Error("an empty screen satisfied the status-row check")
	}
	if err := checkStatusRowTail("just a banner\n\n", "Gemini 3.8 Flash · high"); err == nil {
		t.Error("a screen with no status row satisfied the check")
	}
}

func TestCheckComposerBanner(t *testing.T) {
	const banner = "Auto mode: edits & safe commands auto-approved (shift+tab to cycle)"
	s := screen("> "+banner, row("? for shortcuts", "auto · Gemini 3.8 Flash · high"))
	if err := checkComposerBanner(s, banner); err != nil {
		t.Errorf("checkComposerBanner = %v, want nil", err)
	}

	// A bare composer is the default stop's, and NoBanner is how the table
	// spells it.
	bare := screen(">", row("? for shortcuts", "Gemini 3.8 Flash · high"))
	if err := checkComposerBanner(bare, NoBanner); err != nil {
		t.Errorf("checkComposerBanner(NoBanner) on a bare composer = %v, want nil", err)
	}
	if err := checkComposerBanner(s, NoBanner); err == nil {
		t.Error("NoBanner accepted a composer that was showing a banner")
	}
	if err := checkComposerBanner(bare, banner); err == nil {
		t.Error("a bare composer satisfied a banner expectation")
	}
}

// TestParseScenario_ModeCycleTable pins that the table survives parsing with
// its spaces intact: the row tail and the banner both hold spaces, and a
// parser that split them would make every stop unfindable.
func TestParseScenario_ModeCycleTable(t *testing.T) {
	sc, err := ParseScenario("scenarios/49_mode_cycle_order.align")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sc.ModeCycle) != 5 {
		t.Fatalf("parsed %d rows, want 5", len(sc.ModeCycle))
	}
	want := []CycleStop{
		{"plan", "plan · Gemini 3.8 Flash · high", "Plan mode: research & plan only (shift+tab to cycle)"},
		{"default", "Gemini 3.8 Flash · high", NoBanner},
		{"accept-edits", "accept-edits · Gemini 3.8 Flash · high", "Accept-edits mode: file edits auto-approved (shift+tab to cycle)"},
		{"auto", "auto · Gemini 3.8 Flash · high", "Auto mode: edits & safe commands auto-approved (shift+tab to cycle)"},
		{"plan", "plan · Gemini 3.8 Flash · high", "Plan mode: research & plan only (shift+tab to cycle)"},
	}
	for i := range want {
		if sc.ModeCycle[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, sc.ModeCycle[i], want[i])
		}
	}

	// The lap must close where it opened, or it is a list and the scene
	// would pass while the cycle walked off its end.
	if sc.ModeCycle[0].Mode != sc.ModeCycle[len(sc.ModeCycle)-1].Mode {
		t.Error("the table does not close the lap: its last stop is not its first")
	}
	// And every stop must be distinct in the middle, or the scene would
	// accept a cycle that visits one stop twice and never reaches another.
	seen := map[string]bool{}
	for _, s := range sc.ModeCycle[:len(sc.ModeCycle)-1] {
		if seen[s.Mode] {
			t.Errorf("mode %q appears twice before the lap closes", s.Mode)
		}
		seen[s.Mode] = true
	}
}
