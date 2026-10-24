package modelpicker

import (
	"strings"
	"testing"
)

func TestEffortLevelName(t *testing.T) {
	cases := []struct {
		level int
		want  string
	}{
		{EffortLow, "low"},
		{EffortMedium, "medium"},
		{EffortHigh, "high"},
		{-1, "high"},
		{3, "high"},
		{100, "high"},
	}
	for _, tc := range cases {
		if got := EffortLevelName(tc.level); got != tc.want {
			t.Errorf("EffortLevelName(%d) = %q, want %q", tc.level, got, tc.want)
		}
	}
}

func TestEffortIndexForName(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"low", EffortLow},
		{"medium", EffortMedium},
		{"high", EffortHigh},
		{"", EffortHigh},
		{"unknown", EffortHigh},
	}
	for _, tc := range cases {
		if got := EffortIndexForName(tc.name); got != tc.want {
			t.Errorf("EffortIndexForName(%q) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestHasEffortTiers(t *testing.T) {
	// Empty name defaults to true.
	if !HasEffortTiers("") {
		t.Errorf("HasEffortTiers(\"\") = false, want true")
	}
	if !HasEffortTiers("   ") {
		t.Errorf("HasEffortTiers(\"   \") = false, want true")
	}

	// Reference models carry tiers.
	if !HasEffortTiers("Gemini 3.8 Flash (High)") {
		t.Errorf("HasEffortTiers(Gemini 3.8 Flash (High)) = false, want true")
	}
	if !HasEffortTiers("gemini-3.8-flash-high") {
		t.Errorf("HasEffortTiers(gemini-3.8-flash-high) = false, want true")
	}

	// Unknown model not in catalogue keeps tiers.
	if !HasEffortTiers("some-completely-unknown-model") {
		t.Errorf("HasEffortTiers(unknown) = false, want true")
	}
}

func TestEffortBarSegmentsReused(t *testing.T) {
	on, off := EffortBarSegments(EffortLow)
	if !strings.Contains(on, "◉") {
		t.Errorf("EffortBarSegments(low) lit run missing ◉: %q", on)
	}
	if !strings.Contains(off, "○") {
		t.Errorf("EffortBarSegments(low) dim run missing ○: %q", off)
	}
}
