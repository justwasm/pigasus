package effortpanel

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
		{3, "xhigh"},
		{4, "max"},
		{-1, "high"},
		{5, "high"},
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
		{"xhigh", 3},
		{"max", 4},
		{"", EffortHigh},
		{"unknown", EffortHigh},
	}
	for _, tc := range cases {
		if got := EffortIndexForName(tc.name); got != tc.want {
			t.Errorf("EffortIndexForName(%q) = %d, want %d", tc.name, got, tc.want)
		}
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
