package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/transcript"
)

func TestHeaderJumpConversation(t *testing.T) {
	c, err := convstore.LoadConversation("/Users/gear/.gemini/antigravity-cli/conversations/b5c4f82a-2fae-4545-adba-005448568f7d.db")
	if err != nil {
		t.Skip("db not found: ", err)
	}
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width = 120
	m.height = 40
	m.seedConversation(c)
	rows, _, _ := m.syncRowsView()
	t.Logf("Total rows: %d, user prompt rows: %d", len(rows), len(m.userPromptRows))
	for i, p := range m.userPromptRows {
		start := tuiviewport.PromptTurnStart(rows, p)
		// Every turn (including first prompt after banner): start must be exactly 1 blank line before separator rule line
		if rows[start] != "" {
			t.Errorf("Prompt %d (at %d): expected 1 blank line at start=%d, got %q", i, p, start, rows[start])
		}
		if !tuiviewport.IsPromptTurnSeparator(rows[start+1]) {
			t.Errorf("Prompt %d (at %d): expected separator at start+1=%d, got %q", i, p, start+1, rows[start+1])
		}
		if start+2 != p {
			t.Errorf("Prompt %d: expected start+2 == p (%d), got start=%d (rows between separator and prompt: %d)", i, p, start, p-(start+1))
		}
	}
}

func TestHeaderClickJump_ExactOneBlankLineAtTop(t *testing.T) {
	c, err := convstore.LoadConversation("/Users/gear/.gemini/antigravity-cli/conversations/b5c4f82a-2fae-4545-adba-005448568f7d.db")
	if err != nil {
		t.Skip("db not found: ", err)
	}
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width = 120
	m.height = 30
	m.seedConversation(c)
	m.syncRowsView()

	// Pick prompt 2
	targetPromptIdx := m.userPromptRows[2]
	m.autoScroll = false
	m.scroll = targetPromptIdx + 10 // scroll deep into assistant response of prompt 2

	// Render view to populate pinned prompt
	_ = m.View()
	if m.pinnedPromptRowIdx != targetPromptIdx {
		t.Fatalf("expected pinnedPromptRowIdx=%d, got %d", targetPromptIdx, m.pinnedPromptRowIdx)
	}

	// Click row 0 (the pinned prompt header)
	m.Update(mouseClick(0, 0, tea.MouseLeft))

	// Re-render and inspect the top visible lines
	visible := m.transcriptWindow()
	if len(visible) < 3 {
		t.Fatalf("visible lines too short: %d", len(visible))
	}

	// Line 0 must be exactly a blank line
	if strings.TrimSpace(visible[0]) != "" {
		t.Errorf("expected visible[0] to be blank line, got %q", visible[0])
	}
	// Line 1 must be thin separator rule
	if !tuiviewport.IsPromptTurnSeparator(visible[1]) {
		t.Errorf("expected visible[1] to be separator, got %q", visible[1])
	}
	// Line 2 must be the prompt
	if !tuiviewport.IsUserPromptRow(visible[2]) {
		t.Errorf("expected visible[2] to be user prompt row, got %q", visible[2])
	}
}

func TestEveryPromptHeaderClickJump(t *testing.T) {
	c, err := convstore.LoadConversation("/Users/gear/.gemini/antigravity-cli/conversations/b5c4f82a-2fae-4545-adba-005448568f7d.db")
	if err != nil {
		t.Skip("db not found: ", err)
	}
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width = 120
	m.height = 30
	m.seedConversation(c)
	rows, _, _ := m.syncRowsView()

	// Test clicking header from EVERY possible scroll position in the conversation!
	for s := 0; s < len(rows); s++ {
		m.autoScroll = false
		m.scroll = s
		_ = m.View()

		if m.pinnedPromptText == "" {
			continue // No header visible at this scroll position
		}

		// Click header
		m.Update(mouseClick(0, 0, tea.MouseLeft))

		visible := m.transcriptWindow()
		var blankCount int
		for j := 0; j < len(visible) && strings.TrimSpace(visible[j]) == ""; j++ {
			blankCount++
		}
		if blankCount > 1 {
			t.Errorf("At scroll %d (pinned prompt %q): after click, visible starts with %d blank lines! Lines:\n[0]=%q\n[1]=%q\n[2]=%q\n[3]=%q",
				s, m.pinnedPromptText, blankCount, visible[0], visible[1], visible[2], visible[3])
			break
		}
	}
}

func TestNonAltScreen_NoStickyPrompt(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.altScreen = false
	m.width = 100
	m.height = 30
	m.response = []string{
		transcript.UserPromptRow("prompt 1"),
		"response 1",
		"",
		transcript.TurnSeparator(100),
		transcript.UserPromptRow("prompt 2"),
		"response 2",
	}
	m.autoScroll = false
	m.scroll = 5
	rows, header, resp := m.syncRowsView()
	avail := 20
	availScroll := m.updateStickyPrompt(rows, header, resp, avail)
	if availScroll != avail {
		t.Errorf("inline mode: expected availScroll == avail (%d), got %d", avail, availScroll)
	}
	if m.stickyBarRow != "" {
		t.Errorf("inline mode: expected empty stickyBarRow, got %q", m.stickyBarRow)
	}
	if m.pinnedPromptText != "" {
		t.Errorf("inline mode: expected empty pinnedPromptText, got %q", m.pinnedPromptText)
	}
	visible := m.transcriptWindow()
	for _, row := range visible {
		if strings.Contains(row, "jump to prompt") {
			t.Errorf("inline mode leaked jump to prompt: %q", row)
		}
	}
}

func TestBottomScrollDown_NoStickyHeaderFlicker(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.altScreen = true
	m.width = 100
	m.height = 20
	m.response = []string{
		transcript.UserPromptRow("prompt 1"),
		"response 1",
		"",
		transcript.TurnSeparator(100),
		transcript.UserPromptRow("prompt 2"),
	}
	for i := 0; i < 50; i++ {
		m.response = append(m.response, "line of response")
	}

	// Initial render at the bottom
	_ = m.View()

	if !m.autoScroll {
		t.Fatalf("expected autoScroll to be true at initial render at bottom")
	}
	if m.pinnedPromptText != "" {
		t.Fatalf("expected no sticky header when parked at bottom, got %q", m.pinnedPromptText)
	}

	// Continuing to scroll down at the bottom (e.g. mouse wheel down inertia)
	// MUST NOT disable autoScroll, and MUST NOT flash sticky header
	for i := 0; i < 10; i++ {
		m.scrollBy(3)
		_ = m.View()
		if !m.autoScroll {
			t.Errorf("iteration %d: autoScroll should remain true on downward scroll at bottom", i)
		}
		if m.pinnedPromptText != "" {
			t.Errorf("iteration %d: sticky header should not flash when scrolling down at bottom, got %q", i, m.pinnedPromptText)
		}
	}

	// Only when user starts scrolling UP does sticky header appear
	m.scrollBy(-10)
	_ = m.View()
	if m.autoScroll {
		t.Errorf("scrolling up should set autoScroll to false")
	}
	if m.pinnedPromptText == "" {
		t.Errorf("scrolling up should show sticky header")
	}

	// Scrolling back down to the bottom deactivates sticky header
	m.scrollBy(20)
	_ = m.View()
	if !m.autoScroll {
		t.Errorf("scrolling to bottom should set autoScroll to true")
	}
	if m.pinnedPromptText != "" {
		t.Errorf("scrolling back to bottom should deactivate sticky header, got %q", m.pinnedPromptText)
	}
}

func TestBottomScrollDown_ZeroAllocs(t *testing.T) {
	c, err := convstore.LoadConversation("/Users/gear/.gemini/antigravity-cli/conversations/b5c4f82a-2fae-4545-adba-005448568f7d.db")
	if err != nil {
		t.Skip("db not found: ", err)
	}
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width = 120
	m.height = 30
	m.seedConversation(c)

	// Initial render at the bottom
	_ = m.View()
	if !m.autoScroll {
		t.Fatalf("expected autoScroll to be true at initial render at bottom")
	}

	// Warm-up one scroll down at bottom
	m.scrollBy(3)
	_ = m.View()

	// Measure allocations over 100 frames of downward inertia scrolling at the bottom
	allocs := testing.AllocsPerRun(100, func() {
		m.scrollBy(3)
		_ = m.View()
	})

	if allocs > 0 {
		t.Errorf("expected 0 allocs per frame when scrolling down at page bottom, got %.2f", allocs)
	}
}

func BenchmarkBottomScrollDown(b *testing.B) {
	c, err := convstore.LoadConversation("/Users/gear/.gemini/antigravity-cli/conversations/b5c4f82a-2fae-4545-adba-005448568f7d.db")
	if err != nil {
		b.Skip("db not found: ", err)
	}
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width = 120
	m.height = 30
	m.seedConversation(c)
	_ = m.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.scrollBy(3)
		_ = m.View()
	}
}
