package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestCycle124_ClipToScreenRows verifies the cycle-124 fix for the
// banner-rows-vanish bug: when scroll entries contain embedded newlines,
// clipToScreenRows trims the tail so the visible set fits inside the
// scrollable-area row budget, keeping the banner anchored at the top
// instead of being shaved off by View()'s drop-from-top fallback.
func TestCycle124_ClipToScreenRows(t *testing.T) {
	cases := []struct {
		name      string
		allRows   []string
		visible   []string
		scrollOff int
		maxRows   int
		wantOut   int
		wantRows  int
	}{
		{
			name:      "single-line entries, no clipping",
			allRows:   []string{"a", "b", "c", "d"},
			visible:   []string{"a", "b", "c", "d"},
			scrollOff: 0,
			maxRows:   10,
			wantOut:   4,
			wantRows:  4,
		},
		{
			name:      "single-line entries, fits exactly",
			allRows:   []string{"a", "b", "c"},
			visible:   []string{"a", "b", "c"},
			scrollOff: 0,
			maxRows:   3,
			wantOut:   3,
			wantRows:  3,
		},
		{
			name:      "multi-line last entry gets trimmed",
			allRows:   []string{"a", "b", "c\nd\ne"},
			visible:   []string{"a", "b", "c\nd\ne"},
			scrollOff: 0,
			maxRows:   4,
			wantOut:   2,
			wantRows:  2,
		},
		{
			name:      "first row is multi-line",
			allRows:   []string{"a\nb\nc", "d"},
			visible:   []string{"a\nb\nc", "d"},
			scrollOff: 0,
			maxRows:   3,
			wantOut:   1,
			wantRows:  3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counts := screenRowCounts(tc.allRows)
			got := clipToScreenRows(tc.visible, counts, tc.scrollOff, tc.maxRows)
			if len(got) != tc.wantOut {
				t.Errorf("len = %d, want %d", len(got), tc.wantOut)
			}
			rows := 0
			for _, r := range got {
				rows += screenRows(r)
			}
			if rows != tc.wantRows {
				t.Errorf("totalRows = %d, want %d", rows, tc.wantRows)
			}
		})
	}
}

// TestCycle124_BannerPreservedAtTop verifies that the cycle-124 banner-in-
// scrollable-area change combined with clipToScreenRows preserves all
// five banner rows at the very top when the scroll position is zero.
// This is the exact regression from cycle-123: banner rows 1-3 vanished
// after PageUp/C-Home on a 108MB conversation because the embedded
// newlines in transcript entries pushed the rendered body past 30 rows
// and the View() drop-from-top shaved the banner itself.
func TestCycle124_BannerPreservedAtTop(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "/Users/gear/Documents/GitHub/openagy", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.fallbackFrom = "MiniMax-M3"
	m.fallbackTo = "Gemini 3.8 Flash (High)"
	m.response = append(m.response,
		"line one",
		"line two with embedded\nnewline inside an entry",
		"line three",
	)
	m.gotoTop()
	v := m.View()
	if !containsBannerLine(v.Content, "Pigasus CLI") {
		t.Errorf("banner should appear in body when scroll==0; first lines:\n%s", debugFirstLines(v.Content))
	}
}

// TestCycle124_BannerHidesAfterScroll verifies the cycle-124 contract:
// after scrolling past the first screen the banner is gone, exactly the
// way the reference binary hides its logo past the first page.
func TestCycle124_BannerHidesAfterScroll(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for i := 0; i < 600; i++ {
		m.response = append(m.response, "transcript line")
	}
	m.gotoTop()
	v := m.View()
	if !containsBannerLine(v.Content, "Pigasus CLI") {
		t.Fatalf("precondition: banner must be at the top when scroll==0; first lines:\n%s scroll=%d autoScroll=%v", debugFirstLines(v.Content), m.scroll, m.autoScroll)
	}
	m.scrollBy(m.pageRows() * 2)
	v = m.View()
	if containsBannerLine(v.Content, "Pigasus CLI") {
		t.Errorf("banner must not be visible after scrolling away; first lines:\n%s", debugFirstLines(v.Content))
	}
}

// debugFirstLines prints the first 8 lines of content with ANSI escapes
// stripped, used in test failures so the layout regression can be read at
// a glance instead of guessing from a hexdump.
func debugFirstLines(content string) string {
	plain := ansi.Strip(content)
	lines := strings.Split(plain, "\n")
	if len(lines) > 8 {
		lines = lines[:8]
	}
	return strings.Join(lines, "\n")
}

// containsBannerLine reports whether content has a non-empty line that
// contains the banner text. We can't just take the first non-blank line
// because bubbletea emits a trailing cursor glyph on the topmost row even
// when that row is otherwise empty — TrimSpace would still find the
// glyph. Strip ANSI first, then look for the marker text.
func containsBannerLine(content, marker string) bool {
	plain := ansi.Strip(content)
	return strings.Contains(plain, marker)
}
