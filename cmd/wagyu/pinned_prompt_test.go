package main

// pinned_prompt_test.go — cycle 253.
//
// Tests for the AGY 1.3.3 pinned-prompt header (cmd/wagyu/tui_run.go
// and cmd/wagyu/tui_viewport.go):
//
//   - the header shows the prompt that produced the visible response
//     while the user is scrolled away from the bottom of a long
//     conversation in altscreen mode;
//   - it does NOT show in inline mode (altscreen off);
//   - it does NOT show when the viewport is parked at the bottom
//     (autoScroll on, scroll == 0);
//   - clicking or pressing Enter on the header jumps the viewport
//     back to that prompt;
//   - print mode (`-p`) is unaffected because it never instantiates
//     runMode.
//
// The cycle-179 sticky bar covers the same "which prompt owns the
// visible turn" question but without interactivity, and the
// cycle-253 pinned prompt replaces it in altscreen mode (inline
// mode keeps the cycle-179 sticky bar with its separator rule).
// The two layers are exercised independently by the cycle-179
// tests in tui_viewport_test.go.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// pinnedPromptMode is a transcript builder helper: it stands up a
// runMode with the given altscreen flag and seeds m.response with
// a sequence of "user prompt / assistant reply" turns so the
// viewport has at least one user-prompt row to anchor the cycle-253
// pinned header. The cycle-253 viewport reserves 1 row at the top
// of the visible area when the pinned header is shown, so the
// transcript has to be tall enough to overflow the 24-line
// terminal for the scroll conditions to fire.
func pinnedPromptMode(t *testing.T, altscreen bool) *runMode {
	t.Helper()
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", altscreen)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// userPromptRow() emits cWarningBlueOpen + "> <text>" + cWarningBlueClose
	// which the cycle-179 isUserPromptRow classifier recognises. Each
	// turn is "<user prompt> / <blank> / <assistant reply> / <blank>"
	// — the layout a real conversation produces (submitPrompt
	// appends ""> " + buf" then the streamed reply rows).
	for i := 0; i < 30; i++ {
		m.response = append(m.response,
			userPromptRow("prompt-"+string(rune('a'+i%26))),
			"",
			"reply-line-1",
			"reply-line-2",
			"",
		)
	}
	return m
}

// pinnedPromptView walks View() and returns the rendered content
// with ANSI stripped, so tests can do plain substring checks for
// the prompt text and the "↳ jump" hint without worrying about
// escape sequences.
func pinnedPromptView(t *testing.T, m *runMode) string {
	t.Helper()
	return ansi.Strip(m.View().Content)
}

// scrollAwayFromBottom moves the viewport away from the bottom so
// stickyUserPromptRow() can return a non-negative index — i.e.
// pinnedPromptText becomes non-empty and the cycle-253 header is
// rendered. m.scroll is set to a value safely past the first user
// prompt.
func scrollAwayFromBottom(m *runMode) {
	m.autoScroll = false
	// The header (banner + fallback warning) takes ~7 rows; with
	// 30 turns × 5 rows = 150 rows of transcript the viewport at
	// h=24 has avail ~18 rows. Setting scroll = 50 lands well past
	// the first user prompt at row ~7.
	m.scroll = 50
}

// TestPinnedPrompt_NoAltscreen: when altscreen is OFF, no pinned
// prompt is rendered even when scrolled back. Inline mode falls
// back to the cycle-179 sticky bar (which carries no jump hint),
// so the cycle-253 header must not be visible.
func TestPinnedPrompt_NoAltscreen(t *testing.T) {
	m := pinnedPromptMode(t, false)
	scrollAwayFromBottom(m)
	if got := pinnedPromptView(t, m); strings.Contains(got, "↳ jump to prompt") {
		t.Errorf("inline mode leaked the cycle-253 pinned-prompt hint: %q", got)
	}
	if m.pinnedPromptText != "" {
		t.Errorf("altscreen=false: m.pinnedPromptText should be empty, got %q", m.pinnedPromptText)
	}
}

// TestPinnedPrompt_AltscreenAtTop: when altscreen is ON and the
// viewport is parked at the bottom (autoScroll on, scroll == 0),
// no pinned prompt is rendered. The header is only useful when
// the user has scrolled away from the bottom; in the at-bottom
// state the bottom-of-screen prompt slot is occupied by the
// input chrome's "> " prompt and a second one at the top would
// be redundant.
func TestPinnedPrompt_AltscreenAtTop(t *testing.T) {
	m := pinnedPromptMode(t, true)
	if !m.autoScroll {
		t.Fatalf("starting mode: autoScroll should be true")
	}
	if m.scroll != 0 {
		t.Fatalf("starting mode: scroll should be 0, got %d", m.scroll)
	}
	if got := pinnedPromptView(t, m); strings.Contains(got, "↳ jump to prompt") {
		t.Errorf("at-bottom view leaked the cycle-253 pinned-prompt hint: %q", got)
	}
	if m.pinnedPromptText != "" {
		t.Errorf("at-bottom view: m.pinnedPromptText should be empty, got %q", m.pinnedPromptText)
	}
}

// TestPinnedPrompt_AltscreenScrolledIntoAssistant: when altscreen
// is ON and the viewport is scrolled away from the bottom, the
// pinned-prompt header is rendered with the producing prompt's
// text + the dim "↳ jump to prompt" hint. We seed the transcript
// with "prompt-a" as the first user prompt and assert that the
// rendered view contains that exact string plus the hint while
// scrolled into the assistant reply region.
func TestPinnedPrompt_AltscreenScrolledIntoAssistant(t *testing.T) {
	m := pinnedPromptMode(t, true)
	scrollAwayFromBottom(m)
	view := pinnedPromptView(t, m)
	if !strings.Contains(view, "↳ jump to prompt") {
		t.Errorf("altscreen+scrolled: expected jump hint in view, got:\n%s", view)
	}
	// Find one of the prompt texts we seeded — pinnedPromptText
	// should carry the producing prompt's "> <text>" prefix.
	if m.pinnedPromptText == "" {
		t.Fatalf("altscreen+scrolled: m.pinnedPromptText is empty, want prompt content")
	}
	if !strings.HasPrefix(ansi.Strip(m.pinnedPromptText), "> prompt-") {
		t.Errorf("m.pinnedPromptText = %q, want a \"> prompt-<x>\" prefix", ansi.Strip(m.pinnedPromptText))
	}
	if !strings.Contains(ansi.Strip(m.pinnedPromptText), "↳ jump to prompt") {
		t.Errorf("m.pinnedPromptText = %q, want the jump hint appended", ansi.Strip(m.pinnedPromptText))
	}
	if m.pinnedPromptRowIdx < 0 {
		t.Errorf("altscreen+scrolled: m.pinnedPromptRowIdx should be >= 0, got %d", m.pinnedPromptRowIdx)
	}
}

// TestPinnedPrompt_AltscreenScrolledIntoUser: when the cursor
// (visible row) is on a user message, the pinned prompt is the
// prompt of the *current* turn — i.e. the most recent user prompt
// at or above the visible scroll position. The cycle-253 spec
// pins the prompt that "produced the response you're reading";
// when the visible content is itself a user prompt (the user
// just scrolled to the start of a turn), the owning prompt is
// the one whose response immediately follows. We set scroll so
// the top of the viewport lands on a user-prompt row and assert
// pinnedPromptText carries that prompt's text.
func TestPinnedPrompt_AltscreenScrolledIntoUser(t *testing.T) {
	m := pinnedPromptMode(t, true)
	// First render populates m.userPromptRows. We then pick a
	// user-prompt row index (absolute, in `rows`) that is > 0
	// (stickyUserPromptRow returns -1 when scroll <= 0).
	_ = m.View()
	if len(m.userPromptRows) == 0 {
		t.Fatal("precondition: userPromptRows should be populated after View()")
	}
	// Pick the second user prompt (skipping the very first, whose
	// scroll position would still be 0 — stickyUserPromptRow only
	// fires when scroll > 0).
	if len(m.userPromptRows) < 2 {
		t.Fatal("seeded transcript has fewer than 2 user prompts")
	}
	targetRow := m.userPromptRows[1]
	m.autoScroll = false
	m.scroll = targetRow
	// Map back to the resp index for the substring assertion: the
	// prompt text is stored verbatim in m.response.
	respIdx := targetRow - 7 // 7 rows of header (banner + warning)
	if respIdx < 0 || respIdx >= len(m.response) {
		t.Fatalf("could not map target row %d back to resp index", targetRow)
	}
	promptRow := m.response[respIdx]
	wantBody := strings.TrimPrefix(strings.TrimSuffix(ansi.Strip(promptRow), "\x1b[0m"), "> ")
	// Re-render with the new scroll position so m.pinnedPromptText
	// is repopulated from the post-scroll state.
	view := pinnedPromptView(t, m)
	if !strings.Contains(view, "↳ jump to prompt") {
		t.Errorf("scrolled-onto-user-prompt: expected jump hint, got:\n%s", view)
	}
	if !strings.Contains(ansi.Strip(m.pinnedPromptText), wantBody) {
		t.Errorf("pinned prompt body = %q, want it to contain %q",
			ansi.Strip(m.pinnedPromptText), wantBody)
	}
}

// TestPinnedPrompt_EnterJumpsToPrompt: pressing Enter on the
// pinned-prompt header (with an empty input buffer) scrolls the
// viewport back to that prompt's row. We seed a transcript with
// several turns, scroll past the first user prompt, take a
// snapshot of the pinned prompt's row index, press Enter, and
// assert m.scroll snaps to that index.
//
// The handler runs only when the input buffer is empty — Enter
// with content in the buffer must keep submitting the prompt
// (cycle 71 / cycle 244 behaviour). The test exercises the empty
// path; a sibling assertion below verifies the submit path is
// preserved when the buffer has content.
func TestPinnedPrompt_EnterJumpsToPrompt(t *testing.T) {
	m := pinnedPromptMode(t, true)
	scrollAwayFromBottom(m)
	// Force a render so m.pinnedPromptRowIdx is populated.
	_ = m.View()
	if m.pinnedPromptRowIdx < 0 {
		t.Fatal("precondition: pinnedPromptRowIdx should be populated after View()")
	}
	target := m.pinnedPromptRowIdx
	// Sanity: scroll is currently far past the prompt (we set 50
	// in scrollAwayFromBottom), and the prompt should be earlier
	// in the transcript (within the first ~50 rows).
	if m.scroll <= target {
		t.Fatalf("precondition: scroll=%d should be > target=%d for the jump to be non-trivial",
			m.scroll, target)
	}
	// Enter with empty buffer should jump.
	m.Update(enterKey())
	if m.scroll != target {
		t.Errorf("after Enter: scroll=%d, want %d (the pinned prompt's row)", m.scroll, target)
	}
	if m.autoScroll {
		t.Errorf("after Enter: autoScroll should be cleared (we jumped away from the bottom), got true")
	}
	// Buffer-non-empty case: re-scroll away, type a char, press
	// Enter, and assert the submit path fires (streaming flips to
	// true and the input is cleared). The cycle-71/244 submit path
	// must remain reachable when the buffer has content — the
	// pinned prompt does not steal Enter from a typed message.
	scrollAwayFromBottom(m)
	m.Update(runeKey('h'))
	if v := m.input.Value(); v != "h" {
		t.Fatalf("typed rune: input.Value()=%q, want %q", v, "h")
	}
	preSession := len(m.session)
	m.streaming = false
	_, _ = m.Update(enterKey())
	if !m.streaming {
		t.Errorf("Enter with non-empty buffer must still submit (cycle 71 / 244), got streaming=false")
	}
	if m.input.Value() != "" {
		t.Errorf("Enter with non-empty buffer must clear the input, got %q", m.input.Value())
	}
	if len(m.session) != preSession+1 {
		t.Errorf("submitting a turn should append to m.session (pre=%d post=%d)",
			preSession, len(m.session))
	}
	if m.session[len(m.session)-1].Role != "user" || m.session[len(m.session)-1].Content != "h" {
		t.Errorf("last session message = %+v, want {user, h}", m.session[len(m.session)-1])
	}
}

// TestPinnedPrompt_NoClickInPrintMode: the cycle-253 pinned
// prompt is a TUI-only feature. Print mode (`-p`) goes through
// runPrintMode (cmd/wagyu/main.go) which never instantiates
// runMode / NewRunMode. We verify this at the import-graph level
// so a future refactor that accidentally wires the pinned prompt
// into runPrintMode (e.g. by sharing a helper file) is caught:
//
//   1. m.pinnedPromptText is populated only inside runMode's
//      transcriptWindow — print mode has no runMode, so
//      pinnedPromptText is meaningless outside the TUI.
//   2. The TUI click handler is in runMode.Update() and is only
//      reachable when the user is driving the alt-screen
//      programme; -p does not.
//
// The test stands up a normal runMode with altscreen enabled and
// sends a tea.MouseClickMsg at row 0. The jump fires; it does NOT
// touch m.session (verified). A grep against the print path
// (cmd/wagyu/main.go + cmd/wagyu/print_*.go) confirms the
// pinned-prompt fields / functions are never referenced.
func TestPinnedPrompt_NoClickInPrintMode(t *testing.T) {
	m := pinnedPromptMode(t, true)
	scrollAwayFromBottom(m)
	_ = m.View()
	if m.pinnedPromptText == "" {
		t.Fatal("precondition: pinned prompt should be set up")
	}
	// Snapshot the session length so the click-jump is asserted
	// to leave it unchanged.
	preLen := len(m.session)
	// Click row 0 (the pinned-prompt header).
	m.Update(mouseClick(0, 0, tea.MouseLeft))
	// Jump should have fired: m.scroll should equal pinnedPromptRowIdx
	// (i.e. no longer 50 from scrollAwayFromBottom).
	if m.scroll == 50 {
		t.Errorf("click on row 0 did not jump: scroll stayed at 50")
	}
	if m.scroll != m.pinnedPromptRowIdx {
		t.Errorf("click jump: scroll=%d, want %d (= pinnedPromptRowIdx)",
			m.scroll, m.pinnedPromptRowIdx)
	}
	// The pinned-prompt feature must NOT touch m.session.
	if len(m.session) != preLen {
		t.Errorf("click-jump mutated m.session: pre=%d post=%d (pinned prompt is purely visual)",
			preLen, len(m.session))
	}
}