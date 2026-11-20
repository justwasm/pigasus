package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestScrollPositionIndicator_1512(t *testing.T) {
	m := &RootModel{
		viewport: uiState{
			altScreen:      true,
			autoScroll:     false,
			width:          80,
			height:         24,
			scroll:         5,
			userPromptRows: []int{0, 10, 25},
			rowsView: composedRows{
				Rows: make([]string, 50),
			},
		},
	}
	// Populate dummy rows
	for i := range m.viewport.rowsView.Rows {
		m.viewport.rowsView.Rows[i] = "line"
	}

	// 1. autoScroll = true -> indicator should be empty
	m.viewport.autoScroll = true
	if got := m.scrollPositionIndicator(); got != "" {
		t.Errorf("autoScroll=true got %q, want empty", got)
	}
	m.viewport.autoScroll = false

	// 2. altScreen = false -> indicator should be empty
	m.viewport.altScreen = false
	if got := m.scrollPositionIndicator(); got != "" {
		t.Errorf("altScreen=false got %q, want empty", got)
	}
	m.viewport.altScreen = true

	// 3. scrolled up in altscreen:
	// total = 50 rows, height = 24.
	// scrollPositionIndicator() should contain the indicator and down arrow.
	ind := m.scrollPositionIndicator()
	if !strings.Contains(ind, "↓") {
		t.Errorf("indicator = %q, want containing '↓'", ind)
	}
	if !strings.Contains(ind, "lines") {
		t.Errorf("indicator = %q, want containing 'lines'", ind)
	}

	// 4. promptRule(true) and (false) remain clean separator rules
	topRule := m.promptRule(true)
	if strings.Contains(ansi.Strip(topRule), "↓") {
		t.Errorf("promptRule(true) should not contain indicator: %q", topRule)
	}
	bottomRule := m.promptRule(false)
	if strings.Contains(ansi.Strip(bottomRule), "↓") {
		t.Errorf("promptRule(false) should not contain indicator: %q", bottomRule)
	}
}

func TestScrollIndicator_StreamingWithoutTip_SameRowAboveQueue(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24
	m.viewport.altScreen = true
	m.viewport.autoScroll = false
	m.viewport.scroll = 5
	m.viewport.userPromptRows = []int{0, 10, 25}
	m.viewport.rowsView.Rows = make([]string, 50)
	for i := range m.viewport.rowsView.Rows {
		m.viewport.rowsView.Rows[i] = "line"
	}
	m.conversation.streaming = true
	m.conversation.spinFrame = 9
	m.viewport.activeTip = ""
	m.session.store.StreamQueue = []string{"queued follow-up message"}

	// 1. liveRows check: indicator is on the SAME row as spinner
	rows := m.liveRows(80)
	if len(rows) != 1 {
		t.Fatalf("expected 1 live row, got %d: %v", len(rows), rows)
	}
	spinnerLine := rows[0]
	cleanSpinner := ansi.Strip(spinnerLine)
	if !strings.Contains(cleanSpinner, "Generating") {
		t.Errorf("expected spinnerLine to contain 'Generating', got %q", spinnerLine)
	}
	if !strings.Contains(spinnerLine, "↓") || !strings.Contains(spinnerLine, "lines") {
		t.Errorf("expected spinnerLine to contain indicator ('↓' and 'lines'), got %q", spinnerLine)
	}
	if ansi.StringWidth(cleanSpinner) > 80 {
		t.Errorf("expected width <= 80, got %d: %q", ansi.StringWidth(cleanSpinner), cleanSpinner)
	}

	// 2. viewBody check: indicator line appears BEFORE queued message
	body, _ := m.viewBody(nil)
	indIdx := strings.Index(body, "↓")
	queuedIdx := strings.Index(body, "queued follow-up message")
	if indIdx < 0 {
		t.Fatalf("indicator not found in viewBody")
	}
	if queuedIdx < 0 {
		t.Fatalf("queued message not found in viewBody")
	}
	if indIdx >= queuedIdx {
		t.Errorf("expected indicator to appear ABOVE queued message: indIdx=%d, queuedIdx=%d", indIdx, queuedIdx)
	}
}

func TestScrollIndicator_StreamingWithTip_SameRowAsTipAboveQueue(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24
	m.viewport.altScreen = true
	m.viewport.autoScroll = false
	m.viewport.scroll = 5
	m.viewport.userPromptRows = []int{0, 10, 25}
	m.viewport.rowsView.Rows = make([]string, 50)
	for i := range m.viewport.rowsView.Rows {
		m.viewport.rowsView.Rows[i] = "line"
	}
	m.conversation.streaming = true
	m.viewport.activeTip = "Press esc to interrupt generation."
	m.session.store.StreamQueue = []string{"queued follow-up message"}

	// 1. liveRows check: 2 rows. Row 0 has spinner, Row 1 has Tip AND indicator on the same row!
	rows := m.liveRows(80)
	if len(rows) != 2 {
		t.Fatalf("expected 2 live rows (spinner + tip with indicator), got %d: %v", len(rows), rows)
	}
	if strings.Contains(rows[0], "↓") {
		t.Errorf("expected spinner row 0 to NOT contain indicator when tip is present: %q", rows[0])
	}
	tipLine := rows[1]
	if !strings.Contains(tipLine, "Tip:") {
		t.Errorf("expected row 1 to contain 'Tip:', got %q", tipLine)
	}
	if !strings.Contains(tipLine, "↓") || !strings.Contains(tipLine, "lines") {
		t.Errorf("expected row 1 to contain indicator ('↓' and 'lines'), got %q", tipLine)
	}
	cleanTip := ansi.Strip(tipLine)
	if ansi.StringWidth(cleanTip) > 80 {
		t.Errorf("expected width <= 80, got %d: %q", ansi.StringWidth(cleanTip), cleanTip)
	}

	// 2. viewBody check: indicator appears BEFORE queued message
	body, _ := m.viewBody(nil)
	indIdx := strings.Index(body, "↓")
	queuedIdx := strings.Index(body, "queued follow-up message")
	if indIdx < 0 {
		t.Fatalf("indicator not found in viewBody")
	}
	if queuedIdx < 0 {
		t.Fatalf("queued message not found in viewBody")
	}
	if indIdx >= queuedIdx {
		t.Errorf("expected indicator to appear ABOVE queued message: indIdx=%d, queuedIdx=%d", indIdx, queuedIdx)
	}
}

func TestScrollIndicator_Idle_DedicatedRowAboveQueue(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24
	m.viewport.altScreen = true
	m.viewport.autoScroll = false
	m.viewport.scroll = 5
	m.viewport.userPromptRows = []int{0, 10, 25}
	m.viewport.rowsView.Rows = make([]string, 50)
	for i := range m.viewport.rowsView.Rows {
		m.viewport.rowsView.Rows[i] = "line"
	}
	m.conversation.streaming = false
	m.session.store.StreamQueue = []string{"queued follow-up message"}

	// 1. liveRows is nil when idle
	rows := m.liveRows(80)
	if len(rows) != 0 {
		t.Fatalf("expected 0 live rows when idle, got %d: %v", len(rows), rows)
	}

	// 2. viewBody check: indicator is rendered on its own row, ABOVE queued message
	body, _ := m.viewBody(nil)
	indIdx := strings.Index(body, "↓")
	queuedIdx := strings.Index(body, "queued follow-up message")
	if indIdx < 0 {
		t.Fatalf("indicator not found in viewBody")
	}
	if queuedIdx < 0 {
		t.Fatalf("queued message not found in viewBody")
	}
	if indIdx >= queuedIdx {
		t.Errorf("expected indicator to appear ABOVE queued message: indIdx=%d, queuedIdx=%d", indIdx, queuedIdx)
	}
}
