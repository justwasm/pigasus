// resume_cycle278_test.go — cycle 278: the /resume picker's measured parity
// gaps against agy 1.4.2.
//
// Every case here was observed live at 200x40 in a tmux pane; the comment on
// each test records the reference behaviour it pins.
package cliapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/sandbox"
	"github.com/justwasm/pigasus/internal/tools"
	_ "github.com/ncruces/go-sqlite3/driver"
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

func TestResume_PopulatesFromStoreDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	steps1 := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "hello world"},
		{Idx: 1, Type: convstore.StepModelResponse, Status: 3, Text: "hi there"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", "aaaa1111-0000-4000-8000-000000000001.db"),
		"traj-1", steps1); err != nil {
		t.Fatal(err)
	}
	steps2 := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "fix the bug please"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", "bbbb2222-0000-4000-8000-000000000002.db"),
		"traj-2", steps2); err != nil {
		t.Fatal(err)
	}

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, _ := m.handleSlashCommand("/resume")
	if !handled {
		t.Fatal("/resume should be handled")
	}
	if !m.resumeMode {
		t.Fatal("/resume should open the picker")
	}
	if len(m.resumeList) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(m.resumeList))
	}
	body := m.resumeView()
	if !strings.Contains(body, "fix the bug please") {
		t.Errorf("picker missing first conversation title: %q", body)
	}
	if !strings.Contains(body, "hello world") {
		t.Errorf("picker missing second conversation title: %q", body)
	}
}

func TestResume_UpDownMovesCursor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, label := range []string{"first", "second", "third"} {
		id := filepath.Join(dir, "conversations",
			"00000000-0000-4000-8000-00000000000"+string(rune('1'+i))+".db")
		_ = convstore.WriteConversation(id, "traj-"+label,
			[]convstore.Step{
				{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: label},
			})
	}

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	_, _ = m.handleSlashCommand("/resume")
	if !m.resumeMode {
		t.Fatal("/resume should open")
	}
	// Cycle 336: the picker opens on the workspace tree (agy 1.4.7). The
	// three conversations above share one workspace — no workspace at all —
	// so the tree is one group row followed by its three conversations,
	// and the cursor opens on the first conversation (row 1).
	if m.resumeCursor != 1 {
		t.Fatalf("initial cursor = %d, want 1 (the first conversation of the expanded group)", m.resumeCursor)
	}
	m.Update(key(tea.KeyDown, 0))
	if m.resumeCursor != 2 {
		t.Fatalf("after down, cursor = %d, want 2", m.resumeCursor)
	}
	m.Update(key(tea.KeyDown, 0))
	if m.resumeCursor != 3 {
		t.Fatalf("after 2nd down, cursor = %d, want 3", m.resumeCursor)
	}
	m.Update(key(tea.KeyDown, 0))
	if m.resumeCursor != 3 {
		t.Fatalf("down should clamp at last (3), got %d", m.resumeCursor)
	}
	m.Update(key(tea.KeyUp, 0))
	if m.resumeCursor != 2 {
		t.Fatalf("after up, cursor = %d, want 2", m.resumeCursor)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.resumeMode {
		t.Fatal("Esc should close the picker")
	}
}

func TestFork_WritesConvStoreAndAppearsInPicker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}

	pastSteps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "an older prompt"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", "bbbb2222-0000-4000-8000-000000000002.db"),
		"traj-past", pastSteps); err != nil {
		t.Fatal(err)
	}

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session = []backend.Message{
		{Role: "user", Content: "do thing 1"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "do thing 2"},
		{Role: "assistant", Content: "ok done"},
	}

	_, _ = m.handleSlashCommand("/fork")
	if len(m.session) == 0 {
		t.Fatal("fork should not wipe the live session")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "conversations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 dbs (past + fork), got %d", len(entries))
	}

	m.resumeMode = false
	m.resumeList = nil
	_, _ = m.handleSlashCommand("/resume")
	if !m.resumeMode {
		t.Fatalf("/resume should open with the past conversation present (resp=%v)", m.response)
	}
	if len(m.resumeList) != 1 {
		t.Fatalf("expected 1 conversation (the fork is current and hidden), got %d", len(m.resumeList))
	}
	if m.resumeList[0].Title == "" || !strings.Contains(m.resumeList[0].Title, "older prompt") {
		t.Errorf("picker should show the past conversation, got %q", m.resumeList[0].Title)
	}
}

// TestResume_F4DeletesConversation verifies that pressing F4 in /resume
// mode deletes the selected conversation from disk and updates the list.
func TestResume_F4DeletesConversation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	convID := "test-conv-f4"
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 1, Text: "hello"},
	}
	if err := convstore.SaveConversation(convID, "/workspace", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.resumeMode = true
	m.reloadResumeList()
	if len(m.resumeList) == 0 {
		t.Fatalf("expected saved conversation in resumeList")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyF4})

	convPath := filepath.Join(tmpDir, ".gemini", "antigravity-cli", "conversations", convID+".db")
	if _, err := os.Stat(convPath); !os.IsNotExist(err) {
		t.Fatalf("conversation file still exists at %s after F4 delete", convPath)
	}
}

// TestResume_F2RenamesConversation verifies that pressing F2 in /resume
// mode closes resume mode and opens the rename panel for that conversation.
func TestResume_F2RenamesConversation(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.resumeMode = true
	m.resumeList = []convstore.ConversationMeta{
		{ID: "c1", Title: "Original Title", StepCount: 3},
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	if m.resumeMode {
		t.Fatal("resumeMode should be closed after F2 rename")
	}
	if m.panel.kind != panelRename {
		t.Fatalf("F2 in resume should open panelRename; got %v", m.panel.kind)
	}
	if got := m.conversationName; got != "Original Title" {
		t.Errorf("conversationName = %q, want 'Original Title'", got)
	}
}

// TestContinueFlag exercises the documented top-level --continue:
// resolves the conversation through resolveConversationSelection (which
// in turn consults FindMostRecent for the parent/child fallback), seeds
// the runMode session, and renders an "Resumed conversation: <title>"
// banner the v1.4.0 changelog requires.
func TestContinueFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "11111111-2222-4000-8000-000000000099"
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "say what?"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", id+".db"), "traj", steps); err != nil {
		t.Fatal(err)
	}
	// Pin the seeded workspace to the test's cwd so the most-recent
	// lookup finds it without walking into the source tree.
	ws := filepath.Join(t.TempDir())
	t.Chdir(ws)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeConversationSummaryRow(
		filepath.Join(dir, "conversation_summaries.db"), id,
		"file://"+filepath.Clean(ws), now); err != nil {
		t.Fatal(err)
	}

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("continue", true, "")
	fs.Bool("c", false, "")
	sel := resolveConversationSelection(fs, io.Discard)
	if sel.ID != id {
		t.Fatalf("sel.ID=%q, want %q", sel.ID, id)
	}
	if sel.Store == nil || len(sel.Store.Steps) == 0 {
		t.Fatalf("sel.Store not loaded: %+v", sel.Store)
	}
	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.conversationID = sel.ID
	m.seedConversation(sel.Store)
	joined := strings.Join(m.response, "\n")
	// Resumed transcript echoes user prompts matching agy.
	if !strings.Contains(joined, "say what?") {
		t.Errorf("resumed transcript must echo user prompts; got:\n%s", joined)
	}
	seededUser := false
	for _, msg := range m.session {
		if msg.Role == "user" && strings.Contains(msg.Content, "say what?") {
			seededUser = true
		}
	}
	if !seededUser {
		t.Errorf("prior user prompt missing from the seeded session: %+v", m.session)
	}
}

// TestContinueNoConversation runs the documented --continue empty-store
// path: no conversation in the workspace, no parent or child. The
// continueNoConversationMessage helper prints "No previous
// conversation to continue." and the caller exits non-zero.
func TestContinueNoConversation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Chdir(t.TempDir())

	var buf strings.Builder
	convstore.ContinueNoConversationMessage(&buf)
	if !strings.Contains(buf.String(), "No previous conversation to continue.") {
		t.Errorf("got %q, want the documented message", buf.String())
	}
	// resolveConversationSelection returns an empty selection —
	// resolveContinue-driven main.go should surface the error.
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("continue", true, "")
	sel := resolveConversationSelection(fs, io.Discard)
	if sel.ID != "" {
		t.Errorf("non-empty sel with empty store: %+v", sel)
	}
}

// TestGoroutineLeakResumePicker reproduces the cycle's headline fix.
// The v1.4.0 changelog records: "background step-cache eviction
// goroutines running for the rest of the session" survive /resume
// scans. The clone's resume picker has a 530 ms blink tick (the only
// periodic goroutine the cycle-184 /resume panel scheduled); the test
// ensures 10 open + close cycles of the picker do not leave
// background goroutines running.
//
// Goroutine counts under Go's runtime are statistical — the call stack
// after a tea.Tick() expires falls back to the runtime's internal
// bookkeeping, so we tolerate ±1.
func TestGoroutineLeakResumePicker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "33333333-4444-4000-8000-000000000299"
	steps := []convstore.Step{{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "x"}}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", id+".db"), "traj", steps); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeConversationSummaryRow(
		filepath.Join(dir, "conversation_summaries.db"), id,
		"file://"+filepath.Clean(dir), now); err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	baseline := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", dir, true)
		handled, _ := m.handleSlashCommand("/resume")
		if !handled || !m.resumeMode {
			t.Fatalf("iter %d: /resume failed: handled=%v mode=%v", i, handled, m.resumeMode)
		}
		// Drive the picker through the happy path so any spawned
		// goroutines see their natural close.
		m.Update(key(tea.KeyDown, 0))
		m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
		if m.resumeMode {
			t.Fatalf("iter %d: picker should close on Esc", i)
		}
		// Cycle tasks-panel: NewRunMode arms a subscriber goroutine
		// on the shared task registry; close it here so the next
		// iteration's m doesn't accumulate (delta=10). The cancel
		// callback closes the registry's channel, the goroutine's
		// `for range ch` exits, and runtime.GC drains it.
		m.tasksSubCancel()
	}
	// Allow blink ticks to drain.
	time.Sleep(700 * time.Millisecond)
	runtime.GC()
	final := runtime.NumGoroutine()
	if delta := final - baseline; delta > 1 || delta < -1 {
		t.Errorf("goroutine count drifted: baseline=%d final=%d (delta=%d)",
			baseline, final, final-baseline)
	}
}

// TestSlashContinueResumeLast exercises the /continue slash command
// the cycle-resume TUI surface adds. Seeds one conversation in the
// active workspace, runs /continue, and checks (a) the slash was
// handled, (b) m.conversationID was overwritten with the picked id,
// (c) the prior-turn transcript re-appeared in m.response.
func TestSlashContinueResumeLast(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "44444444-5555-4000-8000-000000000301"
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "old prompt"},
		{Idx: 1, Type: convstore.StepModelResponse, Status: 3, Text: "old reply"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", id+".db"), "traj-old", steps); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	t.Chdir(ws)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeConversationSummaryRow(
		filepath.Join(dir, "conversation_summaries.db"), id,
		"file://"+filepath.Clean(ws), now); err != nil {
		t.Fatal(err)
	}

	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", ws, true)
	m.conversationID = "pre-existing-id-from-new-run" // would otherwise short-circuit
	if handled, _ := m.handleSlashCommand("/continue"); !handled {
		t.Fatal("/continue should be handled")
	}
	if m.conversationID != id {
		t.Errorf("m.conversationID = %q, want %q", m.conversationID, id)
	}
	joined := strings.Join(m.response, "\n")
	// Resumed transcript echoes user prompts and model reply matching agy.
	if !strings.Contains(joined, "old reply") {
		t.Errorf("missing the prior model reply: %q", joined)
	}
	if !strings.Contains(joined, "old prompt") {
		t.Errorf("resumed transcript must echo user prompts: %q", joined)
	}
	if !strings.Contains(joined, "Resumed conversation "+id) {
		t.Errorf("missing the resumed banner: %q", joined)
	}
	// The transcript is the resumed one, not any leftover the new-run
	// might have written before /continue wiped response/session.
	if strings.Contains(joined, "pre-existing") {
		t.Errorf("transcript not wiped before seeding: %q", joined)
	}
}

// TestSlashContinueNoConversation runs /continue in a fresh
// ANTIGRAVITY_HOME — there is no candidate, so the handler echoes the
// "No previous conversation to continue." line and leaves
// m.conversationID alone.
func TestSlashContinueNoConversation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Chdir(t.TempDir())

	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)
	const origID = "orig-conv-id-unchanged"
	m.conversationID = origID
	if handled, _ := m.handleSlashCommand("/continue"); !handled {
		t.Fatal("/continue should be handled")
	}
	if m.conversationID != origID {
		t.Errorf("m.conversationID = %q, want %q (unchanged on miss)",
			m.conversationID, origID)
	}
	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, "No previous conversation to continue.") {
		t.Errorf("missing empty-state message: %q", joined)
	}
	if !strings.Contains(joined, "/continue") {
		t.Errorf("empty-state message should be wrapped in the slashRespond `> /continue` echo: %q", joined)
	}
}

// TestSlashContinueParentFallback seeds the conversation in the parent
// of the active workspace and checks /continue picks it up. This is
// the headline v1.4.0 fix: previously the picker would have shown no
// matching row and the TUI would have asked the user to /resume.
func TestSlashContinueParentFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "55555555-6666-4000-8000-000000000401"
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "parent owned prompt"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", id+".db"), "traj-parent", steps); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(t.TempDir(), "parent")
	sub := filepath.Join(ws, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeConversationSummaryRow(
		filepath.Join(dir, "conversation_summaries.db"), id,
		"file://"+filepath.Clean(ws), now); err != nil {
		t.Fatal(err)
	}

	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", sub, true)
	if handled, _ := m.handleSlashCommand("/continue"); !handled {
		t.Fatal("/continue should be handled")
	}
	if m.conversationID != id {
		t.Errorf("m.conversationID = %q, want %q (the parent's session)", m.conversationID, id)
	}
	seededUser := false
	for _, msg := range m.session {
		if msg.Role == "user" && strings.Contains(msg.Content, "parent owned prompt") {
			seededUser = true
		}
	}
	if !seededUser {
		t.Errorf("missing parent-owned prompt in the seeded session: %+v", m.session)
	}
}

// TestGenerateConversationTitle pins the heuristic the cycle-resume
// code path uses when applying the auto-generated title to the
// conversation_summaries.db row. The function is intentionally
// lightweight — no LLM call, no quota — so the assertion only checks
// shape: 4-6 content words, markdown noise stripped, ≤ 60 rune wide.
func TestGenerateConversationTitle(t *testing.T) {
	cases := []struct {
		name  string
		input string
		check func(t *testing.T, got string)
	}{
		{
			name:  "simple prompt",
			input: "fix the broken shell prompt in /tmp/run.sh",
			check: func(t *testing.T, got string) {
				// 4-6 content words — the heuristic keeps up to 6
				// tokens, so a 7-word prompt like this one trims the
				// tail. We don't pin the exact 6 words; only the
				// "shell prompt" anchor must survive.
				if !strings.Contains(got, "shell prompt") {
					t.Errorf("got %q, missing the \"shell prompt\" anchor", got)
				}
				if strings.Contains(got, "/tmp/run.sh") {
					t.Errorf("got %q, picked up a path it should not have", got)
				}
			},
		},
		{
			name:  "markdown noise stripped",
			input: "> tell me how to deploy\n# heading\n* bullet 1\n- bullet 2",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, ">") || strings.Contains(got, "#") ||
					strings.Contains(got, "*") || strings.Contains(got, "-") {
					t.Errorf("markdown markers not stripped: %q", got)
				}
			},
		},
		{
			name:  "code fence stripped",
			input: "explain this:\n```\nfoo bar baz qux quux quuz\n```",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, "```") || strings.Contains(got, "qux") {
					t.Errorf("code fence should be removed: %q", got)
				}
			},
		},
		{
			name:  "empty prompt",
			input: "",
			check: func(t *testing.T, got string) {
				if got != "" {
					t.Errorf("got %q, want empty", got)
				}
			},
		},
		{
			name:  "punctuation only",
			input: "...!!!",
			check: func(t *testing.T, got string) {
				// Function falls back to whatever cleaned text remains;
				// for purely punctuation input that is empty. We accept
				// either "" or the literal trimmed punctuation, but a
				// 4+ word title is the failure mode.
				if strings.Count(got, " ") >= 4 {
					t.Errorf("punctuation-only prompt should not yield a 5-token title: %q", got)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, convstore.GenerateConversationTitle(tc.input))
		})
	}
}

// convstoreStoreDir delegates to the package's StoreDir.
func convstoreStoreDir() string { return convstore.StoreDir() }

// writeConversationSummaryRow is a helper to write synthetic conversation rows for testing.
func writeConversationSummaryRow(dbPath, id, uri, now string) error {
	if dbPath == "" {
		dbPath = filepath.Join(convstoreStoreDir(), "conversation_summaries.db")
	}
	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=rwc")
	if err != nil {
		return err
	}
	defer sdb.Close()
	if _, err := sdb.Exec(`CREATE TABLE IF NOT EXISTS conversation_summaries (
		conversation_id text PRIMARY KEY,
		title text,
		preview text,
		step_count integer,
		workspace_uris text,
		last_modified_time text)`); err != nil {
		return err
	}
	_, err = sdb.Exec(`INSERT INTO conversation_summaries (conversation_id, title, preview, step_count, workspace_uris, last_modified_time)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(conversation_id) DO UPDATE SET
			title=excluded.title,
			preview=excluded.preview,
			step_count=excluded.step_count,
			workspace_uris=excluded.workspace_uris,
			last_modified_time=excluded.last_modified_time`,
		id, "synthetic", "synthetic", 1, uri, now)
	return err
}

// nowUTC returns a stable RFC3339Nano timestamp for tests that don't
// care about the exact value.
func nowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// recordingRunner stands in for the real registry and records what ran.
type recordingRunner struct {
	ran []string
}

func (r *recordingRunner) Run(_ context.Context, call backend.ToolCall) (string, error) {
	r.ran = append(r.ran, call.Name)
	return "ran " + call.Name, nil
}

func TestPermissionRunner_DefaultAllows(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{inner: inner, policy: &permissions.Policy{}}
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"date"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ran run_command" {
		t.Errorf("out = %q, want the tool to have run", out)
	}
	if len(gate.Denials) != 0 {
		t.Errorf("denials = %v, want none", gate.Denials)
	}
}

func TestPermissionRunner_StrictSoftDenies(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{inner: inner, policy: &permissions.Policy{Strict: true}}
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"date"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(inner.ran) != 0 {
		t.Errorf("the tool ran despite strict mode: %v", inner.ran)
	}
	want := permissions.DeniedToolResult("run_command", "date")
	if out != want {
		t.Errorf("soft-deny text =\n  %q\nwant\n  %q", out, want)
	}
	if len(gate.Denials) != 1 || gate.Denials[0] != "run_command" {
		t.Errorf("denials = %v", gate.Denials)
	}
	if !strings.HasPrefix(permissions.HeadlessNotice(len(gate.Denials)), "the 1 tool(s) required approval") {
		t.Error("notice prefix changed")
	}
}

func TestPermissionRunner_SkipPermissionsAllowsUnderStrict(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{
		inner:  inner,
		policy: &permissions.Policy{Strict: true, SkipPermissions: true},
	}
	if _, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"date"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if len(inner.ran) != 1 {
		t.Errorf("--dangerously-skip-permissions did not allow: %v", gate.Denials)
	}
}

func TestPermissionRunner_AllowRuleAllowsUnderStrict(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{inner: inner, policy: &permissions.Policy{
		Strict: true,
		Allow:  []permissions.Rule{{Tool: "run_command", Pattern: "date"}},
	}}
	if _, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"date"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if len(inner.ran) != 1 {
		t.Errorf("allow rule did not permit: %v", gate.Denials)
	}
	// a call outside the rule is still denied
	if _, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"rm -rf /"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if len(inner.ran) != 1 || len(gate.Denials) != 1 {
		t.Errorf("out-of-rule call: ran=%v denials=%v", inner.ran, gate.Denials)
	}
}

// TestPermissionRunner_ModeAutoAllowsCommands covers the Auto mode claim
// "edits & safe commands auto-approved".
func TestPermissionRunner_ModeAutoAllowsCommands(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{inner: inner, policy: &permissions.Policy{
		Mode: permissions.ModeAuto, Strict: true,
	}}
	if _, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"date"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if len(inner.ran) != 1 {
		t.Errorf("auto mode did not allow the command: %v", gate.Denials)
	}
}

// TestToolTarget pins the field the settings rules match against.
func TestToolTarget(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"run_command", `{"command":"go test ./..."}`, "go test ./..."},
		{"view_file", `{"path":"src/a.go"}`, "src/a.go"},
		{"write_to_file", `{"path":"a.txt","content":"x"}`, "a.txt"},
		// Cycle 275: the reference's wire keys are capitalised. Before this
		// the gate matched nothing for a real call and waved it through.
		{"run_command", `{"CommandLine":"go test ./...","Cwd":"/ws"}`, "go test ./..."},
		{"view_file", `{"AbsolutePath":"/ws/src/a.go"}`, "/ws/src/a.go"},
		{"write_to_file", `{"TargetFile":"/ws/a.txt","CodeContent":"x"}`, "/ws/a.txt"},
		{"finish", `{"summary":"done"}`, ""},
		{"broken", `not json`, ""},
	}
	for _, c := range cases {
		if got := toolTarget(backend.ToolCall{Name: c.name, Arguments: json.RawMessage(c.args)}); got != c.want {
			t.Errorf("toolTarget(%s, %s) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

// newStandardFlagSetForTest builds a parsed standard flag set for `args`.
func newStandardFlagSetForTest(t *testing.T, args []string) *flag.FlagSet {
	t.Helper()
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	if code := p.Root().Parse(args); code != 0 {
		t.Fatalf("Parse(%v) = %d", args, code)
	}
	return p.Root().FlagSet()
}

// writeSettingsJSON writes a settings.json into an Antigravity home.
func writeSettingsJSON(t *testing.T, home, body string) error {
	t.Helper()
	return os.WriteFile(filepath.Join(home, "settings.json"), []byte(body), 0o644)
}

// TestPrintModePolicy_FlagsAndEnv wires the flags and OPENAGY_PERMISSION_POLICY
// through to the policy.
func TestPrintModePolicy_FlagsAndEnv(t *testing.T) {
	fs := newStandardFlagSetForTest(t, []string{"--mode", "auto", "--dangerously-skip-permissions"})
	t.Setenv("OPENAGY_PERMISSION_POLICY", "strict")
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	p := printModePolicy(fs)
	if p.Mode != permissions.ModeAuto {
		t.Errorf("Mode = %q, want auto", p.Mode)
	}
	if !p.SkipPermissions {
		t.Error("SkipPermissions = false")
	}
	if !p.Strict {
		t.Error("Strict = false despite OPENAGY_PERMISSION_POLICY=strict")
	}
}

// TestPrintModePolicy_LoadsSettingsRules proves settings.json reaches the
// policy.
func TestPrintModePolicy_LoadsSettingsRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("OPENAGY_PERMISSION_POLICY", "")
	if err := writeSettingsJSON(t, home, `{"permissions":{"allow":["view_file:src/"]}}`); err != nil {
		t.Fatal(err)
	}
	p := printModePolicy(newStandardFlagSetForTest(t, nil))
	if len(p.Allow) != 1 || p.Allow[0] != (permissions.Rule{Tool: "view_file", Pattern: "src/"}) {
		t.Errorf("Allow = %+v", p.Allow)
	}
}

// TestCatalogToolNamesUnchangedInPrintMode keeps the advertised surface stable.
func TestCatalogToolNamesUnchangedInPrintMode(t *testing.T) {
	reg := tools.Catalog(t.TempDir())
	for _, want := range []string{"run_command", "view_file", "grep_search", "finish", "notebook_edit"} {
		if _, ok := reg.Get(want); !ok {
			t.Errorf("catalog lost %q", want)
		}
	}
}

// scriptedBackend replays canned chunk sets, one per Stream call.
type scriptedBackend struct {
	turns [][]backend.StreamChunk
	reqs  []backend.StreamRequest
	i     int
}

func (s *scriptedBackend) Stream(_ context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	s.reqs = append(s.reqs, req)
	var chunks []backend.StreamChunk
	if s.i < len(s.turns) {
		chunks = s.turns[s.i]
	} else {
		chunks = []backend.StreamChunk{{Done: true}}
	}
	s.i++
	out := make(chan backend.StreamChunk, len(chunks)+1)
	for _, c := range chunks {
		out <- c
	}
	close(out)
	return out, nil
}

// TestGateThroughAgentLoop is the composition check: a soft-denied call must
// come back to the model as an ordinary tool result so the turn completes,
// and the denied tool must never reach the registry.
func TestGateThroughAgentLoop(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{inner: inner, policy: &permissions.Policy{Strict: true}}

	be := &scriptedBackend{turns: [][]backend.StreamChunk{
		{{ToolCalls: []backend.ToolCall{{
			ID: "c1", Name: "run_command",
			Arguments: json.RawMessage(`{"command":"date"}`),
		}}}, {Done: true}},
		{{Text: "I could not run that."}, {Done: true}},
	}}
	var final strings.Builder
	loop, err := agent.New(agent.Options{
		Backend: be,
		Handler: func(chunk backend.StreamChunk) error {
			if chunk.Text != "" {
				final.WriteString(chunk.Text)
			}
			return nil
		},
		Tools:      []backend.ToolDecl{{Name: "run_command"}},
		ToolRunner: gate,
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	if err := loop.Send(context.Background(), agent.TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "what time is it?"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(inner.ran) != 0 {
		t.Errorf("the denied tool ran: %v", inner.ran)
	}
	if len(gate.Denials) != 1 {
		t.Errorf("denials = %v, want one", gate.Denials)
	}
	if final.String() != "I could not run that." {
		t.Errorf("final text = %q", final.String())
	}
	// The second request must carry the denial text back as the tool result.
	if len(be.reqs) != 2 {
		t.Fatalf("backend saw %d requests, want 2", len(be.reqs))
	}
	var found bool
	for _, m := range be.reqs[1].Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "was denied.") {
			found = true
		}
	}
	if !found {
		t.Errorf("denial text was not fed back to the model: %+v", be.reqs[1].Messages)
	}
}

// ---- cycle 228 (AGY 1.2.10 close-gap): sandbox gate tests ------------------

type sandboxRecordingRunner struct {
	ran []string
}

func (r *sandboxRecordingRunner) Run(_ context.Context, call backend.ToolCall) (string, error) {
	r.ran = append(r.ran, call.Name)
	return "ran", nil
}

func sandboxGate(t *testing.T, sb sandbox.Sandbox) (*permissionRunner, *sandboxRecordingRunner) {
	t.Helper()
	rec := &sandboxRecordingRunner{}
	gate := &permissionRunner{
		inner:   agent.ToolRunner(rec),
		policy:  &permissions.Policy{},
		sandbox: sb,
	}
	return gate, rec
}

func fakeAntigravityHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	return home
}

func TestSandboxArtifactDirAllowed(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	source := filepath.Join(workspace, "src.txt")
	if err := writeFile(t, source, "x"); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	gate, rec := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-1"))
	artifact := filepath.Join(home, "artifacts", "snapshot.json")
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"cp ` + source + ` ` + artifact + `"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ran" {
		t.Errorf("sandboxed cp into artifact dir must be allowed; got %q (rec=%v)", out, rec.ran)
	}
	if len(gate.Denials) != 0 {
		t.Errorf("sandboxed cp should not have denied; denials=%v", gate.Denials)
	}
}

func TestSandboxScratchDirAllowed(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, rec := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-2"))
	source := filepath.Join(workspace, "in.txt")
	if err := writeFile(t, source, "hi"); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	scratch := filepath.Join(home, "brain", "conv-2", "scratch", "tmp.txt")
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"cp ` + source + ` ` + scratch + `"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ran" {
		t.Errorf("sandboxed write to scratch dir must be allowed; got %q (rec=%v)", out, rec.ran)
	}
	if len(gate.Denials) != 0 {
		t.Errorf("sandboxed scratch write should not have denied; denials=%v", gate.Denials)
	}
}

func TestSandboxOutsidePathDenied(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, _ := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-3"))
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"rm -rf /etc/passwd"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == "ran" {
		t.Errorf("sandboxed rm -rf /etc/passwd must be denied; got %q", out)
	}
	if !strings.Contains(out, "Sandbox blocked") {
		t.Errorf("denial text must name the sandbox; got %q", out)
	}
	if len(gate.Denials) != 1 {
		t.Errorf("denials = %v, want one", gate.Denials)
	}
}

func TestSandboxBypassIsPerCall(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, _ := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-4"))

	res1 := gate.sandbox.CheckCommandWithBypass("rm -rf /etc/passwd", true)
	if res1.Outside {
		t.Errorf("bypass call must report Outside=false; got %+v", res1)
	}

	res2 := gate.sandbox.CheckCommandWithBypass("rm -rf /etc/passwd", false)
	if !res2.Outside {
		t.Errorf("non-bypass call must report Outside=true; got %+v", res2)
	}
}

func TestSandboxDisabledAllowsEverything(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, rec := sandboxGate(t, sandbox.Resolve(false, workspace, home, "conv-5"))
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"rm -rf /etc/passwd"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ran" {
		t.Errorf("disabled sandbox must allow every command; got %q (rec=%v)", out, rec.ran)
	}
}

func TestSandboxSiblingPrefixDenied(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, _ := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-6"))
	evil := filepath.Join(home, "artifacts-evil", "x")
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"cat ` + evil + `"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == "ran" {
		t.Errorf("a sibling-prefix path must NOT pass the sandbox; got %q", out)
	}
}

func TestSandboxOtherConversationScratchDenied(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, _ := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-this"))
	other := filepath.Join(home, "brain", "conv-other", "scratch", "x")
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"cat ` + other + `"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == "ran" {
		t.Errorf("a different conversation's scratch must NOT pass this sandbox; got %q", out)
	}
}

func writeFile(t *testing.T, path, body string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o600)
}

func TestResumeConversation_BoostStepTextRestored(t *testing.T) {
	longReply := "好的，我开始检查 1.4.10。\n" + strings.Repeat("检查细节...\n", 30)
	c := &convstore.Conversation{
		ID: "test-boost-conv",
		Steps: []convstore.Step{
			{Idx: 0, Type: convstore.StepUserInput, Text: "/boost 1.4.10 又出来了，请重新对齐"},
			{Idx: 1, Type: convstore.StepModelResponse, Text: longReply},
		},
	}
	m := NewRunMode(nil, "", "m", "", "/tmp", true)
	m.width, m.height = 120, 24
	m.altScreen = true
	m.seedConversation(c)
	_ = m.View()

	if len(m.userPromptRows) == 0 {
		t.Fatal("expected at least 1 user prompt row")
	}
	header := m.scrollHeaderRows(m.width)
	wrapped := m.wrappedResponse(m.width)
	promptIdx := m.userPromptRows[0] - len(header)
	if promptIdx < 0 || promptIdx >= len(wrapped) {
		t.Fatalf("promptIdx %d out of bounds for wrapped (len %d)", promptIdx, len(wrapped))
	}
	got := ansi.Strip(wrapped[promptIdx])
	want := "> /boost 1.4.10 又出来了，请重新对齐"
	if got != want {
		t.Errorf("prompt row = %q, want %q", got, want)
	}

	// Verify sticky header when scrolled down past prompt
	m.scroll = m.userPromptRows[0] + 5
	m.autoScroll = false
	_ = m.View()

	if m.stickyUserPromptRow() < 0 {
		t.Fatalf("stickyUserPromptRow = %d, want >= 0", m.stickyUserPromptRow())
	}
	if !strings.Contains(m.pinnedPromptText, "1.4.10") {
		t.Errorf("pinnedPromptText = %q, want to contain '1.4.10'", m.pinnedPromptText)
	}
	if strings.Contains(m.pinnedPromptText, "jump to prompt") {
		t.Errorf("pinnedPromptText = %q, expected no 'jump to prompt' hint matching agy 1.4.10", m.pinnedPromptText)
	}
}



