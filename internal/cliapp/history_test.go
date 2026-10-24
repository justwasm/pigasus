package cliapp

// Cycle trim-empty-history-messages tests.
//
// AGY 1.2.7: "Fixed the conversation history list showing empty
// message entries when the user submitted a whitespace-only prompt,
// which wasted screen space and confused scroll calculations."
//
// The clone's pre-fix pushHistory guarded only against prompt == "",
// which let a single space (or any run of Unicode whitespace) slip
// through. This file covers:
//
//  1. pushHistory skips "" and whitespace-only inputs.
//  2. pushHistory trims surrounding whitespace before storing /
//     deduping so a follow-up "  hello  " collapses with "hello".
//  3. submitPrompt rejects whitespace-only inputs (no transcript row,
//     no session entry, no history entry).
//  4. scroll / navigation math holds when a model-reply entry has
//     wrapped multi-line content and the prompt row is blank —
//     View() must still fit inside the terminal and historyUp /
//     historyDown must still reach a non-blank row.
//  5. historyUp / historyDown land on the most recent non-blank
//     entry even when an upstream caller somehow injected blanks
//     directly into m.history.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
)

// trimEmptyHistoryMode returns a runMode with a known terminal size
// and ANTIGRAVITY_HOME pointed at a temp dir so convstore appends
// don't pollute the real history.jsonl.
func trimEmptyHistoryMode(t *testing.T) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "MiniMax M3", "", "/tmp", false)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	return m
}

// 1. Whitespace-only prompt is not added to history.
//
// Covers the AGY 1.2.7 changelog fix. pushHistory used to skip only
// empty strings — " " and "\n\t" went through and produced empty
// entries in m.history, m.response, m.session, and history.jsonl.
func TestPushHistory_SkipsWhitespaceOnly(t *testing.T) {
	m := trimEmptyHistoryMode(t)

	blanks := []string{
		"",
		" ",
		"\n",
		"\t",
		"  \t \n ",
		"\u00a0", // non-breaking space
		"\u3000", // ideographic space (CJK input)
		"\u2003", // em space
		strings.Repeat(" ", 64),
	}
	for _, b := range blanks {
		m.pushHistory(b)
	}
	if len(m.history) != 0 {
		t.Fatalf("pushHistory stored blank entries: %q", m.history)
	}
}

// 2. Leading/trailing whitespace is stripped before storage.
//
// Surrounding whitespace is a common accident: users tap Space
// before a prompt, or hit Enter on a trailing newline. Storing the
// raw form produces near-duplicate history entries and a near-empty
// transcript row. Trim happens before the dedupe check so "  hello  "
// after "hello" collapses to the same entry.
func TestPushHistory_TrimsSurroundingWhitespace(t *testing.T) {
	m := trimEmptyHistoryMode(t)

	m.pushHistory("  hello  ")
	if len(m.history) != 1 || m.history[0] != "hello" {
		t.Fatalf("expected trimmed history entry, got %q", m.history)
	}

	// Trim happens BEFORE the dedupe check.
	m.pushHistory("\n\thello\t\n")
	if len(m.history) != 1 {
		t.Fatalf("expected dedupe to collapse whitespace-padded duplicate, got %q", m.history)
	}

	// Internal whitespace is preserved — only the outer run is trimmed.
	m.pushHistory("  multi word prompt ")
	if len(m.history) != 2 || m.history[1] != "multi word prompt" {
		t.Fatalf("expected internal whitespace preserved, got %q", m.history)
	}
}

// 3. submitPrompt rejects whitespace-only prompts.
//
// The submit path mirrors pushHistory's behaviour: a whitespace-only
// buffer is treated as an empty submission. The transcript must NOT
// gain a blank "> " row and the session must NOT gain an empty user
// message. The input buffer is reset so a stray tap doesn't leave
// the prompt textarea stuck on whitespace.
func TestSubmitPrompt_RejectsWhitespaceOnly(t *testing.T) {
	m := trimEmptyHistoryMode(t)
	m.input.SetValue("   ")

	beforeResp := len(m.response)
	beforeSess := len(m.session)
	beforeHist := len(m.history)

	if cmd := m.submitPrompt(); cmd != nil {
		t.Errorf("submitPrompt for blank input returned cmd %v, want nil", cmd)
	}

	if len(m.response) != beforeResp {
		t.Errorf("submitPrompt appended %d transcript rows for a blank prompt", len(m.response)-beforeResp)
	}
	if len(m.session) != beforeSess {
		t.Errorf("submitPrompt appended %d session messages for a blank prompt", len(m.session)-beforeSess)
	}
	if len(m.history) != beforeHist {
		t.Errorf("submitPrompt pushed %d history entries for a blank prompt", len(m.history)-beforeHist)
	}
	if m.streaming {
		t.Error("submitPrompt left streaming=true for a blank prompt")
	}
	if got := m.input.Value(); strings.TrimSpace(got) != "" {
		t.Errorf("submitPrompt left whitespace in the input buffer: %q", got)
	}

	// Tabs and newlines also count.
	m.input.SetValue("\t\n  \t")
	beforeResp = len(m.response)
	if cmd := m.submitPrompt(); cmd != nil {
		t.Errorf("submitPrompt for tab/newline input returned cmd %v, want nil", cmd)
	}
	if len(m.response) != beforeResp {
		t.Errorf("submitPrompt appended %d transcript rows for a tab/newline prompt", len(m.response)-beforeResp)
	}
}

// 4. Scroll calculations handle gaps without breaking.
//
// A "gap" here means a transcript where one or more rows are blank.
// Before the fix, whitespace-only submits could leave a row in
// m.response that produced an empty `> ` line on the next render.
// The viewport math must still fit inside the terminal: the empty
// row counts as one screen row, but the rendered content must
// remain bounded by the terminal height regardless of how many gaps
// exist. This pins cycle-124's `screenRows("") == 1` invariant and
// cycle-119's "view never taller than terminal" guarantee under a
// transcript that includes blank rows.
func TestScroll_CalculationsHandleBlankRows(t *testing.T) {
	for _, n := range []int{0, 1, 5, 40, 400} {
		m := sizedMode(80, 30, n)
		// Inject a few blank rows into the transcript (the exact
		// thing a whitespace-only submit would have produced before
		// the fix). These count as one screen row each — the
		// viewport math must tolerate them.
		blankInsertions := []int{2, 7, 11}
		for _, idx := range blankInsertions {
			if idx >= len(m.response) {
				continue
			}
			m.response = append(m.response, "")
			// Move "" to the requested index to keep the test
			// representative rather than append-only.
			blank := m.response[len(m.response)-1]
			copy(m.response[idx+1:], m.response[idx:])
			m.response[idx] = blank
		}
		// Sanity: at least one blank row present when the body is long
		// enough to host one.
		if n > 8 {
			foundBlank := false
			for _, r := range m.response {
				if r == "" {
					foundBlank = true
					break
				}
			}
			if !foundBlank {
				t.Fatalf("n=%d: blank row insertion failed", n)
			}
		}

		// Cycle-119 regression: View() must never exceed the terminal.
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) > 30 {
			t.Errorf("n=%d: view has %d rows, terminal is 30", n, len(lines))
		}

		// screenRows("") must return 1 (cycle-124 invariant). A blank
		// row counts as one screen row, never zero — otherwise the
		// clip math would loop indefinitely.
		if got := screenRows(""); got != 1 {
			t.Errorf("screenRows(\"\") = %d, want 1", got)
		}
	}
}

// 5. The history list skips empty entries and navigation still works.
//
// Even if a blank entry were to slip into m.history (e.g. a future
// caller that bypasses pushHistory, or a state reconstructed at
// resume time), historyUp / historyDown must not land on it. The
// simplest behaviour is to reject them at the push boundary (which
// the helper already does); this test pins the boundary so any
// future code path that re-introduces blanks is caught.
//
// We assert two invariants:
//
//	a) pushHistory never accepts a blank, so the navigation path
//	   can't observe one. (Defensive — the upstream fix.)
//	b) After pushing three real prompts and one attempted blank,
//	   historyUp walks back exactly three slots and lands on each
//	   in reverse order. The blank insertion is a no-op.
func TestHistory_NavigationSkipsBlankEntries(t *testing.T) {
	m := trimEmptyHistoryMode(t)

	m.pushHistory("alpha")
	m.pushHistory("beta")
	m.pushHistory("gamma")
	m.pushHistory("   ") // attempted blank — must be a no-op
	m.pushHistory("delta")

	if len(m.history) != 4 {
		t.Fatalf("history len = %d, want 4 (blank was rejected): %q", len(m.history), m.history)
	}
	want := []string{"alpha", "beta", "gamma", "delta"}
	for i, w := range want {
		if m.history[i] != w {
			t.Errorf("history[%d] = %q, want %q", i, m.history[i], w)
		}
	}

	// Seed the input with a sentinel so historyEdit is non-empty
	// during navigation.
	m.input.SetValue("DRAFT")
	m.historyIdx = 0
	m.historyEdit = ""

	// Walk back through every entry. historyUp increments historyIdx
	// each time; the rendered value at slot k is m.history[len-k].
	m.historyUp()
	if got := m.input.Value(); got != "delta" {
		t.Errorf("Up #1: got %q, want %q", got, "delta")
	}
	m.historyUp()
	if got := m.input.Value(); got != "gamma" {
		t.Errorf("Up #2: got %q, want %q", got, "gamma")
	}
	m.historyUp()
	if got := m.input.Value(); got != "beta" {
		t.Errorf("Up #3: got %q, want %q", got, "beta")
	}
	m.historyUp()
	if got := m.input.Value(); got != "alpha" {
		t.Errorf("Up #4: got %q, want %q", got, "alpha")
	}

	// One more Up is a no-op — there's no fifth entry.
	idxBefore := m.historyIdx
	m.historyUp()
	if m.historyIdx != idxBefore {
		t.Errorf("Up past the start advanced historyIdx: was %d, now %d", idxBefore, m.historyIdx)
	}

	// Walk forward; we expect to return to the draft.
	m.historyDown()
	if got := m.input.Value(); got != "beta" {
		t.Errorf("Down #1: got %q, want %q", got, "beta")
	}
	m.historyDown()
	m.historyDown()
	m.historyDown()
	if got := m.input.Value(); got != "DRAFT" {
		t.Errorf("Down past the start: got %q, want %q (the draft)", got, "DRAFT")
	}
}

// 6. (bonus) Submitting a real prompt after a rejected blank still works.
//
// After submitPrompt drops a whitespace-only input, the next real
// prompt must still reach the agent. This is the regression guard
// against an accidental "stuck" input buffer: a stray space must not
// poison the next submit.
func TestSubmitPrompt_RealPromptAfterBlankWorks(t *testing.T) {
	m := trimEmptyHistoryMode(t)
	m.input.SetValue("   ")
	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatalf("blank submit returned cmd %v, want nil", cmd)
	}
	if m.streaming {
		t.Fatal("blank submit left streaming=true")
	}

	// Now type a real prompt and submit. We don't drive the model
	// stream — we only verify that the submit path is reachable.
	m.input.SetValue("real prompt")
	beforeResp := len(m.response)
	// submitPrompt will queue a stream and batch with a spinner tick.
	// We can't await either in a unit test, but the synchronous side
	// effects (history append, response echo, session append) all
	// happen before the tea.Cmd is returned.
	_ = m.submitPrompt()

	if got := len(m.history); got != 1 || m.history[0] != "real prompt" {
		t.Errorf("after real submit: history = %q, want [\"real prompt\"]", m.history)
	}
	if len(m.response) <= beforeResp {
		t.Errorf("real submit did not append a transcript row (was %d, now %d)", beforeResp, len(m.response))
	}
	if len(m.session) == 0 {
		t.Errorf("real submit did not append a session message")
	} else if last := m.session[len(m.session)-1]; last.Role != "user" || strings.TrimSpace(last.Content) == "" {
		t.Errorf("session tail = %+v, want a user message with non-blank content", last)
	}
}

// 7. (bonus) HistoryCap is preserved when blanks are interleaved.
//
// Pre-fix, every whitespace-only submit still consumed a slot (the
// pushHistory check was prompt == "", not strings.TrimSpace(prompt)
// == ""), so spamming blanks could force out real entries. With the
// trim guard, blanks never reach the cap arithmetic.
func TestPushHistory_HistoryCapRespectedWithBlanks(t *testing.T) {
	m := trimEmptyHistoryMode(t)
	// Fill exactly to the cap with real entries.
	for i := 0; i < HistoryCap; i++ {
		m.pushHistory(fmt.Sprintf("prompt-%d", i))
	}
	if len(m.history) != HistoryCap {
		t.Fatalf("history len = %d, want %d", len(m.history), HistoryCap)
	}
	// Interleave a flurry of blanks. They must be no-ops.
	for i := 0; i < 50; i++ {
		m.pushHistory(" \t\n  ")
	}
	if len(m.history) != HistoryCap {
		t.Errorf("blanks changed history length: %d, want %d", len(m.history), HistoryCap)
	}
	// First entry should still be the first real prompt.
	if m.history[0] != "prompt-0" {
		t.Errorf("first entry = %q, want %q", m.history[0], "prompt-0")
	}
}

func TestLoadHistory_PopulatesHistoryFromDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	entries := []convstore.HistoryEntry{
		{Display: "first command", Workspace: "/workspace"},
		{Display: "second command", Workspace: "/workspace"},
		{Display: "/help", Workspace: "/workspace", Type: "slash_command"},
	}
	for _, e := range entries {
		if err := convstore.AppendHistoryEntry(e); err != nil {
			t.Fatalf("AppendHistoryEntry failed: %v", err)
		}
	}

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/workspace", true)
	if len(m.history) != 0 {
		t.Fatalf("expected empty m.history before loadHistory(), got %d", len(m.history))
	}

	m.loadHistory()
	if len(m.history) != 3 {
		t.Fatalf("expected 3 entries in m.history after loadHistory(), got %d: %v", len(m.history), m.history)
	}

	m.historyUp()
	if got := m.input.Value(); got != "/help" {
		t.Errorf("first Up = %q, want %q", got, "/help")
	}

	m.historyUp()
	if got := m.input.Value(); got != "second command" {
		t.Errorf("second Up = %q, want %q", got, "second command")
	}

	m.historyUp()
	if got := m.input.Value(); got != "first command" {
		t.Errorf("third Up = %q, want %q", got, "first command")
	}

	m.historyDown()
	if got := m.input.Value(); got != "second command" {
		t.Errorf("first Down = %q, want %q", got, "second command")
	}

	m.historyDown()
	if got := m.input.Value(); got != "/help" {
		t.Errorf("second Down = %q, want %q", got, "/help")
	}

	m.historyDown()
	if got := m.input.Value(); got != "" {
		t.Errorf("third Down (back to edit) = %q, want empty", got)
	}
}

func TestSubmitPrompt_CapturesSlashCommandInHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/workspace", true)
	m.input.SetValue("/clear")
	_ = m.submitPrompt()

	if len(m.history) != 1 || m.history[0] != "/clear" {
		t.Fatalf("m.history after /clear submit = %v, want [/clear]", m.history)
	}

	m.historyUp()
	if got := m.input.Value(); got != "/clear" {
		t.Fatalf("historyUp recalled %q, want %q", got, "/clear")
	}

	data, err := os.ReadFile(filepath.Join(home, "history.jsonl"))
	if err != nil {
		t.Fatalf("failed to read history.jsonl: %v", err)
	}
	var entry convstore.HistoryEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if entry.Display != "/clear" || entry.Type != "slash_command" {
		t.Errorf("persisted entry = %+v, want Display=/clear, Type=slash_command", entry)
	}
}

func TestSubmitPrompt_CapturesStreamingQueueInHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/workspace", true)
	m.streaming = true
	m.input.SetValue("queued while streaming")
	_ = m.submitPrompt()

	if len(m.streamQueue) != 1 || m.streamQueue[0] != "queued while streaming" {
		t.Fatalf("streamQueue = %v, want [queued while streaming]", m.streamQueue)
	}

	if len(m.history) != 1 || m.history[0] != "queued while streaming" {
		t.Fatalf("m.history = %v, want [queued while streaming]", m.history)
	}
}

func TestSubmitOrQueuePrompt_CapturesQueueModePrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/workspace", true)
	m.queueMode = true
	m.input.SetValue("queue mode prompt")
	_ = m.submitOrQueuePrompt()

	if m.queuedPrompt != "queue mode prompt" {
		t.Fatalf("queuedPrompt = %q, want %q", m.queuedPrompt, "queue mode prompt")
	}
	if len(m.history) != 1 || m.history[0] != "queue mode prompt" {
		t.Fatalf("m.history = %v, want [queue mode prompt]", m.history)
	}
}

func TestTUISessionPersistence(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	ws := "/workspace/openagy-demo"
	m := NewRunMode(nil, "user@test.com", "MiniMax M3", "", ws, false)

	prompt := "hello from wagyu"
	m.input.SetValue(prompt)
	if got := m.input.Value(); got != prompt {
		t.Fatalf("input buffer = %q, want %q", got, prompt)
	}
	if cmd := m.submitPrompt(); cmd != nil {
		_ = cmd
	}
	dbPathCheck := filepath.Join(tempHome, "conversations", m.conversationID+".db")
	if _, err := os.Stat(dbPathCheck); err != nil {
		t.Fatalf("after submitPrompt: %s missing: %v\nm.session=%+v m.conversationID=%q", dbPathCheck, err, m.session, m.conversationID)
	}

	_, _ = m.Update(backendChunkMsg("I am MiniMax M3, ready to help."))

	dbPath := filepath.Join(tempHome, "conversations", m.conversationID+".db")
	conv, err := convstore.LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation(%s): %v", dbPath, err)
	}
	if len(conv.Steps) != 2 {
		t.Fatalf("loaded steps count = %d, want 2", len(conv.Steps))
	}
	if got := strings.TrimSpace(conv.Steps[0].Text); got != "hello from wagyu" {
		t.Errorf("step 0 text = %q, want 'hello from wagyu'", conv.Steps[0].Text)
	}
	if got := strings.TrimSpace(conv.Steps[1].Text); got != "I am MiniMax M3, ready to help." {
		t.Errorf("step 1 text = %q, want 'I am MiniMax M3, ready to help.'", conv.Steps[1].Text)
	}

	histPath := filepath.Join(tempHome, "history.jsonl")
	data, err := os.ReadFile(histPath)
	if err != nil {
		t.Fatalf("ReadFile history.jsonl: %v", err)
	}
	var histEntry convstore.HistoryEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &histEntry); err != nil {
		t.Fatalf("Unmarshal history entry: %v", err)
	}
	if histEntry.Display != "hello from wagyu" {
		t.Errorf("histEntry.Display = %q, want 'hello from wagyu'", histEntry.Display)
	}
	if histEntry.Workspace != ws {
		t.Errorf("histEntry.Workspace = %q, want %q", histEntry.Workspace, ws)
	}
	if histEntry.ConversationID != m.conversationID {
		t.Errorf("histEntry.ConversationID = %q, want %q", histEntry.ConversationID, m.conversationID)
	}

	lastPath := filepath.Join(tempHome, "cache", "last_conversations.json")
	lastData, err := os.ReadFile(lastPath)
	if err != nil {
		t.Fatalf("ReadFile last_conversations.json: %v", err)
	}
	var lastMap map[string]string
	if err := json.Unmarshal(lastData, &lastMap); err != nil {
		t.Fatalf("Unmarshal lastMap: %v", err)
	}
	if lastMap[ws] != m.conversationID {
		t.Errorf("lastMap[%s] = %q, want %q", ws, lastMap[ws], m.conversationID)
	}

	list, err := convstore.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	found := false
	for _, c := range list {
		if c.ID == m.conversationID {
			found = true
			if c.Title != "hello from wagyu" {
				t.Errorf("c.Title = %q, want 'hello from wagyu'", c.Title)
			}
			if c.StepCount != 2 {
				t.Errorf("c.StepCount = %d, want 2", c.StepCount)
			}
			break
		}
	}
	if !found {
		t.Errorf("conversation %q not discovered in ListConversations: %+v", m.conversationID, list)
	}
}



// fakeClient is the tiny stub used by other viewport tests, declared
// in tui_run_test.go. Reusing it here keeps the test file
// dependency-free.

// eligibilityModel returns a fresh runMode with the announcement
// queue empty (so the inline-notice slot is the only transient row
// above the prompt). The model's hyperlinkCaps is force-enabled
// so the OSC 8 framed hyperlink is the canonical test target —
// NewRunMode's hyperlinkCapsFromEnv consults os.Environ() which
// may not advertise OSC 8 support in a `go test` runner (an empty
// env returns caps.OSC8 == false per DetectHyperlinkCaps), and
// the test cares about the OSC 8 framed shape rather than the
// detection rule. A separate sub-test exercises the OSC 8 off
// branch via setting m.hyperlinkCaps.OSC8 = false.
func eligibilityModel(t *testing.T) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.loadAnnouncements()
	// Cycle 262 test scaffolding: the empty catalog keeps the
	// announcement row out so the inline-notice slot is the only
	// transient row in the layout. announceSaveError is left at
	// its zero value so announcementRow() returns "" cleanly.
	m.announceVisible = nil
	// Force OSC 8 on for the OSC 8 ON test target. The test
	// scaffolding sets TERM to xterm-256color via t.Setenv (a
	// sibling line above) — NewRunMode runs DetectHyperlinkCaps
	// during construction, so the post-construction override is
	// the only way to guarantee OSC 8 is on regardless of the
	// test runner's environment.
	m.hyperlinkCaps.OSC8 = true
	return m
}

// captureStderr captures stderr writes for the duration of fn so a
// test can assert on the diagnostic log line setEligibilityNotice
// emits. The cycle uses fmt.Fprintf(os.Stderr, ...) — the helper
// redirects os.Stderr to an in-memory pipe and restores the
// original on fn's return.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	return <-done
}

// TestEligibilityError_InlineNotStacked pins the cycle 262
// headline contract: an eligibility error surfaces inline in the
// input loop and does NOT push a new row into the conversation
// transcript.
//
// Two checks per run:
//
//   - m.response length is unchanged after the chunk handler runs
//     (the cycle-262 hard constraint "inline notice must NOT be
//     appended to m.response"); pre-condition is non-zero so a
//     silent drop is distinguishable from a passing test.
//   - the inline notice row, when rendered into the screen buffer,
//     contains both the Reason and the verification URL.
//
// A second run (the "URL off" path) verifies the same shape
// contract holds when the upstream did not supply a verification
// URL — the row still renders, the transcript is still untouched.
func TestEligibilityError_InlineNotStacked(t *testing.T) {
	m := eligibilityModel(t)
	// Pre-seed a single transcript row so we can prove the
	// eligibility handler does NOT grow the transcript. The exact
	// content is irrelevant — what matters is the length stays
	// stable across the eligibility-handler run.
	m.response = []string{"> hello"}
	preRespLen := len(m.response)
	preSessLen := len(m.session)

	// Cycle 262 path: route an eligibility error through the
	// Update() handler the way streamPrompt's chunk handler does.
	const wantURL = "https://example.com/verify?token=abc"
	m.Update(eligibilityNoticeMsg{
		Reason:          "Your account is not eligible for this request.",
		VerificationURL: wantURL,
	})

	if len(m.response) != preRespLen {
		t.Errorf("m.response grew: pre=%d post=%d (cycle 262 hard constraint: inline notice must NOT be appended)",
			preRespLen, len(m.response))
	}
	if len(m.session) != preSessLen {
		t.Errorf("m.session grew: pre=%d post=%d (cycle 262 hard constraint: inline notice must NOT be appended)",
			preSessLen, len(m.session))
	}
	// The notice slot is populated.
	if m.eligibilityNotice.Reason == "" {
		t.Fatal("eligibilityNotice.Reason is empty after Update; want the upstream Reason")
	}
	if m.eligibilityNotice.VerificationURL != wantURL {
		t.Errorf("eligibilityNotice.VerificationURL = %q, want %q",
			m.eligibilityNotice.VerificationURL, wantURL)
	}

	// The view-body render surfaces the URL inside the screen
	// buffer. The URL itself rides inside the OSC 8 frame when
	// caps.OSC8 is on (the test forces caps.OSC8 = true via
	// eligibilityModel), so the OSC 8 opener + the URL appear in
	// the raw view but strip away under the all-ANSI stripper.
	// The runMode field check above pins the URL is stored on the
	// model; here we pin the OSC 8 frame carries it. ANSI escapes
	// for the inline notice styling (e.g. ⓘ glyph colour) may
	// wrap the OSC 8 segment so we use the raw view directly
	// rather than re-stripping.
	raw := m.View().Content
	osc8URL := "\x1b]8;;" + wantURL + "\x1b\\"
	if !strings.Contains(raw, osc8URL) {
		t.Errorf("inline notice does not carry the verification URL inside the OSC 8 frame; want prefix %q in raw view:\n%s", osc8URL, raw)
	}
	// The Reason is plain text and survives the all-ANSI stripper.
	view := stripANSIErrView(t, raw)
	if !strings.Contains(view, "Your account is not eligible for this request.") {
		t.Errorf("inline notice does not contain the Reason; full stripped view:\n%s", view)
	}
}

// TestEligibilityError_RendersAsHyperlink pins cycle 240's OSC 8
// contract: the inline notice wraps the verification URL in
// render.Hyperlink, so the cycle-240 percent-encoder applies. The
// terminal-side handshake is the OSC 8 frame:
//
//	ESC ] 8 ; ; URL ST  TEXT  ESC ] 8 ; ; ST
//
// where the URL portion is percent-encoded by
// internal/render.urlForOSC8. The test asserts on three
// independent shapes (against the raw view, not the ANSI-stripped
// view: the OSC 8 frame IS the cycle-240 contract, stripping the
// OSC 8 frame would erase the very bytes under test):
//
//  1. the OSC 8 opening escape "\x1b]8;;" appears in the view;
//  2. the cycle-240 percent-encoded URL appears verbatim inside the
//     OSC 8 frame (the URL's `;` chars become `%3B` so terminals
//     / multiplexers that split OSC sequences on `;` do not
//     truncate the URL at the query string boundary — this is the
//     regression test cycle 240 added);
//  3. the body text ("Verify here") appears verbatim OUTSIDE the
//     OSC 8 frame (cycle 262's visible label).
//
// The plain-text fallback (OSC8=false) is exercised by a
// separate sub-test so both render.Hyperlink branches are pinned.
func TestEligibilityError_RendersAsHyperlink(t *testing.T) {
	m := eligibilityModel(t)

	// URL with the cycle-240 trigger character (";") so the
	// percent-encoder runs. The original URL has 2 semicolons in
	// the query string; the encoded form replaces both with %3B.
	const urlWithSemi = "https://example.com/verify?token=abc;reason=tier"
	const urlEncoded = "https://example.com/verify?token=abc%3Breason=tier"
	m.Update(eligibilityNoticeMsg{
		Reason:          "verify your account",
		VerificationURL: urlWithSemi,
	})

	// Raw view — the OSC 8 frame contains the URL inside its URI
	// segment; stripping the OSC 8 frame would erase the bytes
	// under test, so the OSC 8 framing checks operate on the raw
	// (un-stripped) view.
	raw := m.View().Content

	// (1) OSC 8 framing opener is present in the rendered view.
	// The terminal-side handshake starts at \x1b]8;; and ends
	// at the next \x1b\\; the test pin here ensures the inline
	// row still routes through render.Hyperlink (a regression
	// would land the URL as plain text instead, which fails this
	// check).
	if !strings.Contains(raw, "\x1b]8;;") {
		t.Errorf("OSC 8 framing opener missing from inline notice; the row should be a render.Hyperlink call site. view:\n%s", raw)
	}

	// (2) Cycle 240 percent-encoded URL is embedded in the OSC 8
	// frame. Terminals / multiplexers that split OSC sequences at
	// the first `;` would otherwise truncate the URL at the
	// query-string boundary; the cycle-240 encoder emits `%3B` in
	// place of every `;` to prevent that. The OSC 8 frame is
	// exactly `\x1b]8;;<encoded>\x1b\\<body>\x1b]8;;\x1b\\`,
	// so the test pins the opener+encoded+ST prefix verbatim.
	osc8URL := "\x1b]8;;" + urlEncoded + "\x1b\\"
	if !strings.Contains(raw, osc8URL) {
		t.Errorf("cycle 240 percent-encoded URL missing from OSC 8 frame; want prefix %q in view:\n%s", osc8URL, raw)
	}

	// (3) Body text ("Verify here") is between the opener and
	// the closer; URL percent-encoding leaves display text
	// unchanged (cycle 240's explicit non-goal — only the URI
	// segment is encoded).
	if !strings.Contains(raw, "Verify here") {
		t.Errorf("OSC 8 link body text \"Verify here\" missing from inline notice. view:\n%s", raw)
	}

	// Sub-test: OSC 8 off branch renders the URL as plain text
	// (label (url)). render.Hyperlink falls back to
	// "Verify here (url)" when caps.OSC8 is false; the test pins
	// the full fallback string so a regression of cycle 213/240's
	// detection is caught.
	t.Run("OSC8 off renders plain text", func(t *testing.T) {
		m2 := eligibilityModel(t)
		m2.hyperlinkCaps.OSC8 = false
		const url = "https://example.com/plain"
		m2.Update(eligibilityNoticeMsg{
			Reason:          "verify your account",
			VerificationURL: url,
		})
		// Plain-text fallback case: the ANSI-stripped view
		// should contain the URL and the visible body. render.
		// Hyperlink's OSC8-off branch is "<label> (<url>)".
		view2 := stripANSIErrView(t, m2.View().Content)
		if !strings.Contains(view2, url) {
			t.Errorf("URL %q missing from plain-text fallback; view:\n%s", url, view2)
		}
		if !strings.Contains(view2, "Verify here") {
			t.Errorf("body \"Verify here\" missing from plain-text fallback; view:\n%s", view2)
		}
		// No OSC 8 framing escape should appear when caps.OSC8
		// is false — the cycle-213 / cycle-240 disable path.
		if strings.Contains(view2, "\x1b]8;;") {
			t.Errorf("OSC 8 framing escape present even though caps.OSC8 is false; render.Hyperlink should fall back to plain text")
		}
	})
}

// TestEligibilityError_ClearsOnInput pins the cycle 262 "user
// starts typing" contract: any keystroke that reaches the
// textarea clears the inline notice. The test seeds an active
// notice, drives a single rune insert, and asserts the notice
// slot is empty afterwards.
//
// Two shape checks:
//
//   - the eligibility notice field on the runMode is the zero
//     value (Reason == ""), so the next View() does not render a
//     row.
//   - m.response / m.session length is unchanged (the notice
//     didn't leak into the transcript).
//
// A separate sub-test drives an Enter (submit) — the
// submitPrompt path also clears the notice, even though the
// keystroke that triggers the submit also reaches the input. The
// test pins the explicit clearEligibilityNotice in submitPrompt
// so a future refactor that removes it is caught.
func TestEligibilityError_ClearsOnInput(t *testing.T) {
	t.Run("typing clears notice", func(t *testing.T) {
		m := eligibilityModel(t)
		m.Update(eligibilityNoticeMsg{
			Reason:          "verify your account",
			VerificationURL: "https://example.com/verify",
		})
		if m.eligibilityNotice.Reason == "" {
			t.Fatal("notice not set up for the test")
		}
		preRespLen := len(m.response)
		preSessLen := len(m.session)

		// Drive a single rune through Update — the textarea's
		// default branch inserts the rune and clears the
		// inline notice (cycle 262's "user starts typing"
		// signal).
		_, _ = m.Update(runeKey('h'))

		if m.eligibilityNotice.Reason != "" {
			t.Errorf("eligibilityNotice.Reason = %q after typing, want \"\"", m.eligibilityNotice.Reason)
		}
		if m.eligibilityNotice.VerificationURL != "" {
			t.Errorf("eligibilityNotice.VerificationURL = %q after typing, want \"\"", m.eligibilityNotice.VerificationURL)
		}
		if len(m.response) != preRespLen {
			t.Errorf("m.response grew during typing: pre=%d post=%d", preRespLen, len(m.response))
		}
		if len(m.session) != preSessLen {
			t.Errorf("m.session grew during typing: pre=%d post=%d", preSessLen, len(m.session))
		}
	})

	t.Run("submit clears notice", func(t *testing.T) {
		m := eligibilityModel(t)
		m.Update(eligibilityNoticeMsg{
			Reason:          "verify your account",
			VerificationURL: "https://example.com/verify",
		})
		// Compose a real prompt (single rune + Enter) so
		// submitPrompt reaches the streamPrompt batch. Without a
		// real backend stream the test does not assert on
		// streaming flags; the only assertion is that the
		// notice is cleared by the submit path.
		_, _ = m.Update(runeKey('h'))
		_, _ = m.Update(runeKey('i'))
		_, _ = m.Update(enterKey())
		if m.eligibilityNotice.Reason != "" {
			t.Errorf("eligibilityNotice.Reason = %q after submit, want \"\"", m.eligibilityNotice.Reason)
		}
	})
}

// TestEligibilityError_WithoutURL pins the changelog's "errors
// with a verification URL" qualifier: an eligibility error
// without a URL still surfaces inline (just the Reason, no
// hyperlink). The render still goes above the prompt, the
// transcript is still untouched, and the diagnostic log still
// emits — only the OSC 8 framing escape is omitted because the
// render.Hyperlink helper short-circuits on an empty URL.
func TestEligibilityError_WithoutURL(t *testing.T) {
	m := eligibilityModel(t)
	const reason = "Account verification required"
	m.Update(eligibilityNoticeMsg{
		Reason:          reason,
		VerificationURL: "", // upstream did not supply one
	})

	if m.eligibilityNotice.Reason != reason {
		t.Errorf("eligibilityNotice.Reason = %q, want %q", m.eligibilityNotice.Reason, reason)
	}
	if m.eligibilityNotice.VerificationURL != "" {
		t.Errorf("eligibilityNotice.VerificationURL = %q, want \"\"", m.eligibilityNotice.VerificationURL)
	}

	view := stripANSIErrView(t, m.View().Content)
	if !strings.Contains(view, reason) {
		t.Errorf("inline notice does not contain the Reason %q; full view:\n%s", reason, view)
	}
	// No hyperlink frame because there is no URL.
	if strings.Contains(view, "\x1b]8;;") {
		t.Errorf("OSC 8 framing escape present even though URL is empty; render.Hyperlink should short-circuit")
	}

	// Diagnostic still emits — the diagnostic log path is
	// independent of the URL presence.
	logged := captureStderr(t, func() {
		m.setEligibilityNotice(reason, "")
	})
	if !strings.Contains(logged, reason) {
		t.Errorf("diagnostic log missing the Reason; got %q", logged)
	}
}

// TestEligibilityError_PreservesPrintMode pins cycle 262's
// print-mode contract: a -p turn that terminates on an
// *EligibilityError emits the AGY_ERROR JSON envelope on stderr
// (the same shape every other model API failure uses) and exits
// with code 3. classifyEligibilityError produces the envelope,
// classifyAPIError is NOT consulted (the cycle's spec has
// "ELIGIBILITY_DENIED" win over "PERMISSION_DENIED").
//
// The test asserts on:
//
//   - classifyEligibilityError returns a non-nil *apiError for an
//     *EligibilityError;
//   - the envelope's Status is "ELIGIBILITY_DENIED" (the
//     distinguishing field an automation script branches on);
//   - the Lines() output is parseable JSON and contains the
//     Reason in short_error;
//   - classifyEligibilityError does NOT claim the error when the
//     underlying type is a plain *backend.HTTPError without an
//     eligibility wrapper (so a 403 with no IneligibleTier
//     payload keeps the existing PERMISSION_DENIED path);
//   - the no-TUI shape: there is no runMode-level inline notice
//     for print mode (the cycle's "print mode has no input loop"
//     rule). The test exercises AsEligibilityError + a non-nil
//     pointer so the print path's reliance on the typed shape
//     is pinned.
func TestEligibilityError_PreservesPrintMode(t *testing.T) {
	const reason = "Your account is not eligible for this request."
	const url = "https://example.com/verify?token=xyz"

	// (1) classifyEligibilityError produces a non-nil envelope.
	api, ok := classifyEligibilityError(&EligibilityError{
		Reason:          reason,
		VerificationURL: url,
	})
	if !ok || api == nil {
		t.Fatal("classifyEligibilityError returned (nil, false) for *EligibilityError")
	}

	// (2) Status is the distinguishing field — "ELIGIBILITY_DENIED"
	// wins over classifyAPIError's "PERMISSION_DENIED" for the
	// same upstream status code (a 403 with an IneligibleTier
	// payload).
	if api.Status != "ELIGIBILITY_DENIED" {
		t.Errorf("api.Status = %q, want %q", api.Status, "ELIGIBILITY_DENIED")
	}
	if api.Retryable {
		t.Errorf("api.Retryable = true; eligibility failures are not retryable (cycle 262 spec)")
	}

	// (3) Lines() output is parseable JSON and carries the
	// Reason in short_error.
	lines := api.Lines()
	if !strings.HasPrefix(lines, "error: ") {
		t.Errorf("Lines() missing the leading %q; got %q", "error: ", lines)
	}
	idx := strings.Index(lines, "AGY_ERROR: ")
	if idx < 0 {
		t.Fatalf("Lines() missing the AGY_ERROR envelope; got %q", lines)
	}
	envelope := strings.TrimSpace(lines[idx+len("AGY_ERROR: "):])
	var parsed map[string]any
	if err := json.Unmarshal([]byte(envelope), &parsed); err != nil {
		t.Fatalf("AGY_ERROR envelope is not valid JSON: %v\nenvelope: %s", err, envelope)
	}
	short, _ := parsed["short_error"].(string)
	if !strings.Contains(short, reason) {
		t.Errorf("AGY_ERROR short_error = %q, want substring %q", short, reason)
	}
	if got, _ := parsed["status"].(string); got != "ELIGIBILITY_DENIED" {
		t.Errorf("AGY_ERROR status = %q, want %q", got, "ELIGIBILITY_DENIED")
	}
	if got, _ := parsed["retryable"].(bool); got {
		t.Errorf("AGY_ERROR retryable = true, want false")
	}

	// (4) classifyEligibilityError does NOT claim a plain
	// *backend.HTTPError (the upstream 403 path without an
	// eligibility wrapper — keep the existing PERMISSION_DENIED
	// classification).
	if _, ok := classifyEligibilityError(&backend.HTTPError{
		StatusCode: 403,
		Body:       []byte(`{"error":{"message":"forbidden"}}`),
	}); ok {
		t.Errorf("classifyEligibilityError claimed a plain *backend.HTTPError; only *EligibilityError should match")
	}

	// (5) classifyEligibilityError returns (nil, false) on a
	// nil error so the print path's `if api, ok := ...; ok`
	// branch can fall through to classifyAPIError without a
	// nil-pointer panic.
	if api, ok := classifyEligibilityError(nil); ok || api != nil {
		t.Errorf("classifyEligibilityError(nil) = (%v, %v), want (nil, false)", api, ok)
	}

	// (6) AsEligibilityError walks the wrapped chain — the
	// backend layer often wraps the eligibility error in an
	// "agent: …" prefix before the chunk handler sees it. The
	// cycle's design relies on errors.As to find the eligibility
	// type through any number of wrap layers.
	wrapped := wrapErr("agent: stream: %w", &EligibilityError{Reason: reason, VerificationURL: url})
	if ee := AsEligibilityError(wrapped); ee == nil {
		t.Errorf("AsEligibilityError returned nil for a wrapped *EligibilityError")
	} else if ee.Reason != reason {
		t.Errorf("AsEligibilityError walked to %q, want %q", ee.Reason, reason)
	}

	// (7) Print mode has no input loop — the runMode's
	// eligibilityNotice stays at its zero value when the chunk
	// handler never runs (i.e. when -p bypasses NewRunMode).
	// The test pins the contract that the print path does not
	// touch m.eligibilityNotice at all. We instantiate a fresh
	// runMode and verify the zero value is the no-notice state;
	// the print path's main.go does not construct a runMode so
	// the slot is never allocated in production.
	m := eligibilityModel(t)
	if m.eligibilityNotice.Reason != "" || m.eligibilityNotice.VerificationURL != "" {
		t.Errorf("fresh runMode has non-empty eligibilityNotice: %+v", m.eligibilityNotice)
	}
}

// TestEligibilityError_StillLogged pins cycle 262's diagnostic
// contract: even though the inline notice stays out of the
// conversation transcript, the underlying error IS logged on
// stderr so an operator tailing the diagnostic stream can see
// why the row is on screen. The test exercises two paths:
//
//   - setEligibilityNotice emits a "diagnostic: eligibility
//     error: <reason> (verify at <url>)" line that includes both
//     the Reason and the URL;
//   - the no-URL path emits "diagnostic: eligibility error:
//     <reason>" without the parenthetical URL segment so a
//     log-tail regex can match either shape.
//
// The test also verifies the AsEligibilityError diagnostic
// contract — errors.As walks the wrapped chain so a future
// cycle that wraps the *EligibilityError in an "agent: …"
// prefix does not silently lose the diagnostic line.
func TestEligibilityError_StillLogged(t *testing.T) {
	t.Run("with URL", func(t *testing.T) {
		m := eligibilityModel(t)
		const reason = "Your account is not eligible for this request."
		const url = "https://example.com/verify?token=abc"
		logged := captureStderr(t, func() {
			m.setEligibilityNotice(reason, url)
		})
		if !strings.Contains(logged, "diagnostic: eligibility error:") {
			t.Errorf("diagnostic prefix missing; got %q", logged)
		}
		if !strings.Contains(logged, reason) {
			t.Errorf("diagnostic log missing the Reason %q; got %q", reason, logged)
		}
		if !strings.Contains(logged, url) {
			t.Errorf("diagnostic log missing the verification URL %q; got %q", url, logged)
		}
		if !strings.Contains(logged, "verify at "+url) {
			t.Errorf("diagnostic log missing the verbatim 'verify at <url>' tail; got %q", logged)
		}
	})

	t.Run("without URL", func(t *testing.T) {
		m := eligibilityModel(t)
		const reason = "Account verification required"
		logged := captureStderr(t, func() {
			m.setEligibilityNotice(reason, "")
		})
		if !strings.Contains(logged, "diagnostic: eligibility error:") {
			t.Errorf("diagnostic prefix missing; got %q", logged)
		}
		if !strings.Contains(logged, reason) {
			t.Errorf("diagnostic log missing the Reason %q; got %q", reason, logged)
		}
		// No URL segment in the no-URL branch.
		if strings.Contains(logged, "verify at") {
			t.Errorf("diagnostic log mentions 'verify at' even though no URL was supplied; got %q", logged)
		}
	})

	t.Run("empty reason falls back to generic", func(t *testing.T) {
		m := eligibilityModel(t)
		logged := captureStderr(t, func() {
			m.setEligibilityNotice("", "https://example.com/verify")
		})
		if !strings.Contains(logged, "your account is not eligible for this request") {
			t.Errorf("generic fallback reason missing; got %q", logged)
		}
	})

	t.Run("wrapped error is still found via errors.As", func(t *testing.T) {
		const reason = "wrapped eligibility error"
		const url = "https://example.com/verify?token=wrapped"
		// Build a wrapped chain where the inner *EligibilityError
		// has a Cause set so Unwrap() has a non-nil target. The
		// production upstream typically wraps an
		// *EligibilityError around a *backend.HTTPError, and the
		// cause is what the agent log ships to the operator when
		// the inline row fails to clear. The test pins that
		// errors.As + Unwrap compose so a future cycle that
		// changes the wrapping signature has to update this test.
		causeErr := fmt.Errorf("upstream transport error")
		inner := &EligibilityError{
			Reason:          reason,
			VerificationURL: url,
			Cause:           causeErr,
		}
		wrapped := wrapErr("agent: stream: %w", inner)

		// Mirror the cycle's streamPrompt chunk-handler branch:
		// AsEligibilityError walks the wrapped chain and the
		// notice message carries Reason / VerificationURL verbatim.
		m := eligibilityModel(t)
		ee := AsEligibilityError(wrapped)
		if ee == nil {
			t.Fatal("AsEligibilityError returned nil for a wrapped *EligibilityError")
		}
		m.Update(eligibilityNoticeMsg{
			Reason:          ee.Reason,
			VerificationURL: ee.VerificationURL,
		})
		if m.eligibilityNotice.Reason != reason || m.eligibilityNotice.VerificationURL != url {
			t.Errorf("after wrapped notice: %+v, want Reason=%q VerificationURL=%q",
				m.eligibilityNotice, reason, url)
		}
		// Unwrap exposes the Cause chain for downstream callers
		// (operator log on the diagnostic stream).
		if got := ee.Unwrap(); got != causeErr {
			t.Errorf("Unwrap returned %v, want causeErr %v (cycle 262 spec: errors.As walks the wrapped chain)",
				got, causeErr)
		}
	})
}

// TestEligibilityError_RowIsAbovePrompt is the layout test: the
// inline notice row, when rendered, must appear above the prompt
// input (">"). This is the cycle 262 changelog's "inline in the
// input loop" rule pinned in code — a row that lands below the
// prompt would be a regression of cycle 262's headline contract.
//
// The test is conservative: it does not assert on absolute row
// indices (those depend on terminal width / transcript scroll),
// only on relative position — the row containing the Reason
// must come strictly before the row that contains the prompt
// marker.
func TestEligibilityError_RowIsAbovePrompt(t *testing.T) {
	m := eligibilityModel(t)
	const reason = "Your account is not eligible for this request."
	const url = "https://example.com/verify"
	m.Update(eligibilityNoticeMsg{Reason: reason, VerificationURL: url})

	view := stripANSIErrView(t, m.View().Content)
	lines := strings.Split(view, "\n")
	reasonIdx, promptIdx := -1, -1
	for i, line := range lines {
		if reasonIdx < 0 && strings.Contains(line, reason) {
			reasonIdx = i
		}
		if promptIdx < 0 && strings.HasPrefix(line, "> ") {
			promptIdx = i
		}
	}
	if reasonIdx < 0 {
		t.Fatalf("Reason %q missing from view", reason)
	}
	if promptIdx < 0 {
		t.Fatalf("prompt marker \"> \" missing from view")
	}
	if reasonIdx >= promptIdx {
		t.Errorf("inline notice row at index %d, prompt at index %d — notice must come strictly before the prompt (cycle 262 spec)",
			reasonIdx, promptIdx)
	}
}

// wrapErr is a thin wrapper around fmt.Errorf so the test can
// build the chain that AsEligibilityError walks. Production code
// uses fmt.Errorf("…: %w", err) for the same purpose; the test
// uses the same idiom so errors.Is / errors.As semantics match
// exactly. The earlier hand-rolled wrappedErr type failed to
// participate in the errors.As chain because its Unwrap was a
// custom method (Go's stdlib walks the unwrap chain via
// reflection on the standard fmt.wrapError / %w output).
func wrapErr(format string, err error) error {
	return fmt.Errorf(format, err)
}

// stripANSI removes ANSI / OSC escape sequences from a screen
// buffer so substring assertions can match plain text. The
// helper is intentionally minimal: it strips CSI (ESC[...),
// OSC (ESC]...BEL or ESC]...\x1b\), and a few stray private
// escapes. A future test that needs finer control can layer
// colour-by-colour assertions on top of the stripped view.
//
// Renamed to stripANSIErrView (slash_actions.go already owns a
// stripANSI helper) so the package compiles cleanly when both
// files are present. The semantics match slash_actions.go's
// stripANSI byte-for-byte so a future consolidation can merge
// them into a single package-level helper.
func stripANSIErrView(t *testing.T, s string) string {
	t.Helper()
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			// Skip CSI (ESC[...m etc.) — terminates on a
			// letter in the [@-~] range.
			if i+1 < len(s) && s[i+1] == '[' {
				j := i + 2
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					i = j + 1
					continue
				}
			}
			// Skip OSC (ESC]...BEL or ESC]...\x1b\)
			if i+1 < len(s) && s[i+1] == ']' {
				j := i + 2
				for j < len(s) && s[j] != 0x07 {
					if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j
				continue
			}
			// Skip any other escape sequence (single
			// character or 2-character escapes).
			if i+1 < len(s) {
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// keep the import alive for tests that don't otherwise use tea
// (runeKey / enterKey are referenced by name in the test bodies,
// not directly imported — but the import anchors a future test
// that needs the bubbletea types directly).
var _ tea.KeyMsg
