// resume_cycle278_test.go — cycle 278: the /resume picker's measured parity
// gaps against agy 1.4.2.
//
// Every case here was observed live at 200x40 in a tmux pane; the comment on
// each test records the reference behaviour it pins.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/charmbracelet/x/ansi"
)

// TestResumeSearchAcceptsShiftChars pins the search-field input rule.
//
// Terminals deliver an uppercase letter (and shifted symbols such as "_")
// with ModShift set. The old guard required `key.Mod == 0`, so typing
// "Fixing" into the reference's own search box produced "ixing" in the clone
// while agy 1.4.2 kept every character (verified side by side at 200x40:
// "Fixing", "A_B", "Hi").
func TestResumeSearchAcceptsShiftChars(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width, m.height = 200, 40
	m.resumeMode = true

	for _, k := range []tea.Key{
		{Code: 'F', Text: "F", Mod: tea.ModShift},
		{Code: 'i', Text: "i"},
		{Code: 'x', Text: "x"},
		{Code: 'i', Text: "i"},
		{Code: 'n', Text: "n"},
		{Code: 'g', Text: "g"},
	} {
		m.Update(tea.KeyPressMsg(k))
	}
	if m.resumeQuery != "Fixing" {
		t.Fatalf("resumeQuery = %q, want %q (shifted characters must be inserted)", m.resumeQuery, "Fixing")
	}

	// Ctrl-modified keys are commands, not text, and must stay out of the
	// query even though some terminals attach Text to them.
	m.resumeQuery = ""
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'v', Text: "v", Mod: tea.ModCtrl}))
	if m.resumeQuery != "" {
		t.Fatalf("resumeQuery = %q after ctrl+v, want empty", m.resumeQuery)
	}
}

// TestResumeEmptyStoreOpensPicker pins the empty-state screen.
//
// agy 1.4.2 with an empty conversations/ directory opens the overlay and
// renders exactly:
//
//	Conversations
//	No conversations available.
//	Press Enter to return.
//	<blank>
//	Keyboard: enter Select  tab Switch Tab  esc Go back / Clear search
//
// The search row ("Type to search conversations...") is absent, and the
// clone used to print one transcript line instead of opening anything.
func TestResumeEmptyStoreOpensPicker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width, m.height = 200, 40
	if handled, _ := m.handleSlashCommand("/resume"); !handled {
		t.Fatal("/resume should be handled")
	}
	if !m.resumeMode {
		t.Fatal("/resume on an empty store must still open the picker")
	}
	body := ansi.Strip(m.resumeView())
	if !strings.Contains(body, "No conversations available.") {
		t.Errorf("missing empty-state line: %q", body)
	}
	if !strings.Contains(body, "Press Enter to return.") {
		t.Errorf("missing return hint: %q", body)
	}
	if strings.Contains(body, "Type to search conversations...") {
		t.Errorf("empty store must not show the search row: %q", body)
	}
	if strings.Contains(body, "Grouped View") {
		t.Errorf("empty store must not advertise grouped view: %q", body)
	}

	// Enter returns to the prompt without echoing anything.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.resumeMode {
		t.Fatal("Enter on the empty screen should close the picker")
	}
	if len(m.response) != 0 {
		t.Fatalf("Enter on the empty screen echoed %q, want nothing", m.response)
	}
}

// TestResumeRowDropsBlankAgeCell pins the row geometry when the age is blank.
//
// Cache entries whose summary.UpdatedAt is 0001-01-01 have no age in the
// reference. agy 1.4.2 then omits the 12-cell age field entirely and lets the
// title grow by the 14 columns it frees, so the steps cell sits flush against
// the last column. The clone kept a blank 12-cell field and lost those
// columns (rows measured 186 cells wide against the reference's 200).
func TestResumeRowDropsBlankAgeCell(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width, m.height = 200, 40
	m.resumeMode = true
	m.resumeList = []convstore.ConversationMeta{{
		ID:        "0495d3b3-fb08-b3c0-9290-41ec9c5f0251",
		Title:     "0495d3b3-fb08-b3c0-9290-41ec9c5f0251",
		StepCount: 2,
		// ModTime zero: no age column.
	}}

	var row string
	for _, l := range strings.Split(m.resumeView(), "\n") {
		if strings.Contains(l, "steps") {
			row = l
		}
	}
	if row == "" {
		t.Fatal("no row rendered")
	}
	if got := ansi.StringWidth(row); got != 200 {
		t.Errorf("row width = %d cells, want 200 (the blank age field must be dropped)", got)
	}
	if !strings.HasSuffix(ansi.Strip(row), "     2 steps") {
		t.Errorf("steps cell is not flush against the right edge: %q", ansi.Strip(row))
	}
}

// TestResumeHintHidesGroupedView pins the contextual keyboard hint.
//
// agy 1.4.2 shows "ctrl+f Grouped View" over a store whose rows carry project
// names and omits it when no visible row has one (both observed at 200x40).
func TestResumeHintHidesGroupedView(t *testing.T) {
	without := ansi.Strip(resumeKeyboardHint(true, false))
	if strings.Contains(without, "Grouped View") {
		t.Errorf("hint with no project groups advertises grouped view: %q", without)
	}
	for _, want := range []string{"Navigate", "Page", "enter", "Select", "f2", "f4", "tab", "esc"} {
		if !strings.Contains(without, want) {
			t.Errorf("hint missing %q: %q", want, without)
		}
	}
	with := ansi.Strip(resumeKeyboardHint(true, true))
	if !strings.Contains(with, "ctrl+f") || !strings.Contains(with, "Grouped View") {
		t.Errorf("hint with project groups must advertise grouped view: %q", with)
	}
	// The empty/other-tab hint keeps its reduced form.
	empty := ansi.Strip(resumeKeyboardHint(false, false))
	for _, bad := range []string{"Navigate", "Page", "f2", "f4", "Grouped View"} {
		if strings.Contains(empty, bad) {
			t.Errorf("reduced hint contains %q: %q", bad, empty)
		}
	}
}

// TestResumeProjectGroups pins the count the hint decision reads.
func TestResumeProjectGroups(t *testing.T) {
	rows := []convstore.ConversationMeta{
		{ID: "a", Project: "openagy"},
		{ID: "b", Project: "openagy"},
		{ID: "c", Project: ""},
	}
	if got := resumeProjectGroups(rows); got != 1 {
		t.Errorf("resumeProjectGroups = %d, want 1", got)
	}
	rows = append(rows, convstore.ConversationMeta{ID: "d", Project: "opencode"})
	if got := resumeProjectGroups(rows); got != 2 {
		t.Errorf("resumeProjectGroups = %d, want 2", got)
	}
}

// TestResumeCacheRowWithoutFileIsSkipped is the UI-side guarantee behind the
// cycle-278 convstore change: a cache entry whose .db was removed must not be
// offered, because picking it only produced "Failed to load <id>".
func TestResumeCacheRowWithoutFileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"conversations":{"gone":{"summary":{"Title":"Gone","NumSteps":2,"UpdatedAt":"2026-10-19T18:00:00Z"},"last_modified_time":"2026-10-19T18:00:00Z"}}}`
	if err := os.WriteFile(filepath.Join(cacheDir, "conversation_metadata.json"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := convstore.ListConversations()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("ListConversations = %+v, want empty (no .db on disk)", list)
	}
}
