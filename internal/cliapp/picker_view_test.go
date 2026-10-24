package cliapp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/configtest"
)

// Cycle 129: picker scroll offset. Before the fix, 5+ Down's lost
// the highlight because the renderer loop only walked i < visible.
// With the offset, the highlighted row always lands in the visible
// window. The "↓ N more" tail reports rows below the window, not
// just rows below row 0.

func key(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: mod})
}

func stripAnsi(s string) string { return ansi.Strip(s) }

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func TestPickerView_ScrollOffsetKeepsHighlightVisible(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("picker should be visible after typing '/'")
	}
	for i := 0; i < 8; i++ {
		m.Update(key(tea.KeyDown, 0))
	}
	if got := m.picker.Idx(); got < 8 {
		t.Fatalf("expected idx >= 8 after 8 Down's, got %d", got)
	}
	body := m.pickerView()
	hiName := m.picker.Selected()
	if hiName == nil {
		t.Fatalf("picker.Selected() should be non-nil")
	}
	want := "> " + hiName.Name
	found := false
	for _, l := range strings.Split(body, "\n") {
		if strings.Contains(l, want) && strings.HasPrefix(stripAnsi(l), "> ") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("highlighted row %q should be in the rendered window after 8 Down's, got:\n%s",
			want, body)
	}
	if m.pickerOffset < 1 {
		t.Errorf("expected pickerOffset >= 1 after scrolling, got %d", m.pickerOffset)
	}
}

func TestPickerView_ScrollOffsetRetreatsOnUp(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	for i := 0; i < 10; i++ {
		m.Update(key(tea.KeyDown, 0))
	}
	// PickerView() is what mutates pickerOffset as a side effect
	// of laying out the visible window. The runtime emits a View()
	// after every Update; tests should follow the same contract.
	_ = m.pickerView()
	offsetAfterDown := m.pickerOffset
	if offsetAfterDown < 1 {
		t.Fatalf("expected offset > 0 after 10 Down's, got %d", offsetAfterDown)
	}
	for i := 0; i < 10; i++ {
		m.Update(key(tea.KeyUp, 0))
	}
	// After 10 Up's, idx drops by 10; rendering retreats the offset.
	_ = m.pickerView()
	if m.pickerOffset > offsetAfterDown {
		t.Errorf("offset should not advance on Up, got %d (was %d)", m.pickerOffset, offsetAfterDown)
	}
}

func TestPickerView_MoreTailTracksWindow(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	body := m.pickerView()
	// With /teamwork, /boost, /accounts and /provider added, the picker shows 99 visible-filtered
	// entries. With 5 in the window, the tail is "94 more".
	if !strings.Contains(body, "94 more") {
		t.Errorf("default picker tail should be '94 more' (=99 - 5 visible), got tail: %q", tailOf(body))
	}
	for i := 0; i < 50; i++ {
		m.Update(key(tea.KeyDown, 0))
	}
	body = m.pickerView()
	cnt := extractTailCount(body)
	if cnt < 0 {
		t.Fatalf("could not parse tail count after scroll: %q", body)
	}
	if cnt >= 98 {
		t.Errorf("after scrolling, tail should be < 98, got %d", cnt)
	}
}

func extractTailCount(body string) int {
	for _, l := range strings.Split(body, "\n") {
		l = stripAnsi(l)
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "\u2193") {
			fields := strings.Fields(l)
			if len(fields) >= 2 {
				return atoiSafe(fields[1])
			}
		}
	}
	return -1
}

func tailOf(body string) string {
	for _, l := range strings.Split(body, "\n") {
		l = stripAnsi(l)
		if strings.HasPrefix(strings.TrimSpace(l), "\u2193") {
			return l
		}
	}
	return "<no tail>"
}

// Cycle 130: picker alias rendering. The first alias is shown
// inline between the command name and the description (e.g.
// `/clear (new)`), the rest are dropped from the picker overlay
// but still listed in /help COMMANDS tab.
func TestPickerView_FirstAliasInline(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	// Move highlight to /clear (idx 5 after 5× Down).
	for i := 0; i < 5; i++ {
		m.Update(key(tea.KeyDown, 0))
	}
	body := m.pickerView()
	if !strings.Contains(body, "/clear (new)") {
		t.Errorf("/clear row should render with '(new)' alias, got body fragment: %s",
			body[:min(len(body), 500)])
	}
	// /codesearch has 2 aliases; only the first (`cs`) should render.
	if !strings.Contains(body, "/codesearch (cs)") {
		t.Errorf("/codesearch should render only first alias '(cs)', not '(cs, search)'")
	}
	if strings.Contains(body, "(cs, search)") {
		t.Errorf("/codesearch multi-alias joined form is not what the ref shows")
	}
	// /config shows (settings).
	if !strings.Contains(body, "/config (settings)") {
		t.Errorf("/config should render with '(settings)' alias")
	}
	// Highlighted row's alias is part of the highlighted prefix.
	// The "> /clear (new)" string must be present in the rendered
	// output (sits inside the pan-lenient style segments).
	if !strings.Contains(body, "> /clear (new)") {
		t.Errorf("highlighted row should lead with '> /clear (new)'")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Cycle 131: descriptions longer than 48 chars are truncated to
// 45 chars + "...". Short descriptions pass through unchanged.
// Captured reference truncation widths:
//   /btw (57 chars) → "Ask a side question without interrupting the ..."
//   /codesearch (57 chars) → "Search code in the workspace (usage: /codesea..."
func TestPickerView_DescriptionTruncation(t *testing.T) {
	if got := truncatePickerDesc("short"); got != "short" {
		t.Errorf("short desc should pass through, got %q", got)
	}
	// 48-char description is the boundary: pass through unchanged.
	exact := strings.Repeat("a", pickerDescWidth)
	if got := truncatePickerDesc(exact); got != exact {
		t.Errorf("desc at exact width should not be truncated, got %q", got)
	}
	// 49-char description should be truncated.
	over := strings.Repeat("a", pickerDescWidth+1)
	if got := truncatePickerDesc(over); got != strings.Repeat("a", pickerDescWidth-3)+"..." {
		t.Errorf("49-char desc should truncate to 45+..., got len=%d (%q)", len(got), got)
	}
	// The recovered /btw case.
	btw := "Ask a side question without interrupting the current task"
	wantBtw := "Ask a side question without interrupting the ..."
	if got := truncatePickerDesc(btw); got != wantBtw {
		t.Errorf("btw desc truncation wrong:\n  got  %q\n  want %q", got, wantBtw)
	}
	// The recovered /codesearch case.
	cs := "Search code in the workspace (usage: /codesearch <query>)"
	wantCs := "Search code in the workspace (usage: /codesea..."
	if got := truncatePickerDesc(cs); got != wantCs {
		t.Errorf("codesearch desc truncation wrong:\n  got  %q\n  want %q", got, wantCs)
	}
}

// Cycle 131: rendering surfaces the truncated description in the
// picker output for /btw. This is the integration check that the
// truncation helper is actually wired into the picker overlay.
func TestPickerView_RenderTruncatesBtw(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	body := m.pickerView()
	if !strings.Contains(body, "Ask a side question without interrupting the ...") {
		t.Errorf("/btw should render with 45-char + ... truncation in picker overlay")
	}
	if strings.Contains(body, "current task") {
		t.Errorf("non-truncated /btw tail leaked into the rendered overlay")
	}
}

// Cycle 151: when the picker overlay is visible and the user presses
// Enter, the highlighted command is dispatched — not the literal
// buffer text. Typing "/ch" with "/changelog" highlighted and pressing
// Enter should open /changelog, not the literal /ch (which the cycle
// 149 implementation would have routed to "Unknown command: /ch").
// Captured against the v1.3.6 reference at /tmp/ourpk.txt.
func TestPickerView_EnterUsesHighlighted(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	// Type "/ch" — narrows the picker to /changelog.
	m.Update(runeKey('/'))
	m.Update(runeKey('c'))
	m.Update(runeKey('h'))
	sel := m.picker.Selected()
	if sel == nil || sel.Name != "/changelog" {
		t.Fatalf("setup: highlighted should be /changelog, got %v", sel)
	}
	// Press Enter — dispatches the picker-selected command, not the
	// typed "/ch".
	m.Update(enterKey())
	joined := strings.Join(m.response, "\n")
	// /changelog case sets helpMode=true (it would render the panel)
	// but cycle 149 dropped the slash-command echo, so transcript is
	// empty after dispatch. The picker substitution correctness is
	// verified by checking that the picker closed (handleSlashCommand
	// reset it via m.picker.Reset) and that the literal "/ch" was NOT
	// dispatched to the default case ("Unknown command: /ch").
	if m.picker.Visible() {
		t.Errorf("picker should be reset after Enter (cycle 150)")
	}
	if strings.Contains(joined, "Unknown command: /ch") {
		t.Errorf("Enter dispatched literal buffer text %q instead of picker selection %q",
			"/ch", "/changelog")
	}
}

// ---- cycle 232: picker mouse click + hover + wheel ------------------------

// TestPickerMouse_ClickOnOptionSelects opens the picker by typing "/",
// renders the view to populate the geometry, then clicks each picker
// list row in turn.
func TestPickerMouse_ClickOnOptionSelects(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("setup: picker should be visible after '/'")
	}
	// Refresh geom so handlePickerClick can map (x, y) to a row.
	_ = m.View()
	if !m.pickerGeom.Valid {
		t.Fatal("setup: pickerGeom should be valid after first render")
	}
	if !m.pickerGeom.HasList {
		t.Fatal("setup: pickerGeom.HasList should be true for the '/' list")
	}
	// Click the 3rd list row (visible-row offset 2).
	x := 5
	y := m.pickerGeom.ListTopY + 2
	startBuf := m.input.Value()
	want := m.picker.SelectedAt(int(m.pickerLayoutLocal.ListTop) + 2)
	if want == nil {
		t.Fatalf("setup: no command at list offset 2 (listTop=%d)",
			m.pickerLayoutLocal.ListTop)
	}
	m.Update(mouseClick(x, y, tea.MouseLeft))
	if startBuf != "/" {
		t.Errorf("pre-click buffer %q should be '/'", startBuf)
	}
	if m.input.Value() != want.Name {
		t.Errorf("buffer after click = %q, want %q (the clicked command's name)",
			m.input.Value(), want.Name)
	}
	if !m.picker.Visible() {
		t.Errorf("click on a list row should keep the picker visible (new buffer starts with '/'); got hidden")
	}
	if got := m.picker.Len(); got != 1 {
		t.Errorf("after click, picker.Len() = %d, want 1 (only /artifact etc. matches)", got)
	}
	if got := m.picker.Idx(); got != 0 {
		t.Errorf("after click, picker.Idx() = %d, want 0 (re-derived for the new buffer)", got)
	}
	_ = m.View()
	if !m.pickerGeom.Valid {
		t.Errorf("after re-render with the new picker, pickerGeom should be valid")
	}
	if !m.pickerGeom.HasList {
		t.Errorf("after re-render, pickerGeom.HasList should be true")
	}
}

// TestPickerMouse_ClickOnInputReopensPicker mirrors the AGY 1.2.13
// "clicks on the input field reopen the picker" rule.
func TestPickerMouse_ClickOnInputReopensPicker(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("setup: picker should be visible after '/'")
	}
	m.Update(key(tea.KeyEsc, 0))
	if m.picker.Visible() {
		t.Fatal("setup: Esc should have closed the picker")
	}
	if m.input.Value() == "" {
		t.Fatal("setup: Esc should NOT have cleared the input buffer")
	}
	_ = m.View()
	if m.promptScreenY < 0 {
		t.Fatal("setup: promptScreenY should be valid after first render")
	}
	m.Update(mouseClick(5, m.promptScreenY, tea.MouseLeft))
	if !m.picker.Visible() {
		t.Errorf("click on input row should reopen picker (buffer still starts with '/'); got hidden")
	}
}

// TestPickerMouse_ClickOutsideDismissesPicker verifies the
// close-on-outside-click behaviour.
func TestPickerMouse_ClickOutsideDismissesPicker(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("setup: picker should be visible after '/'")
	}
	_ = m.View()
	if !m.pickerGeom.Valid {
		t.Fatal("setup: pickerGeom should be valid")
	}
	m.Update(mouseClick(2, 0, tea.MouseLeft))
	if m.picker.Visible() {
		t.Errorf("click outside the picker should dismiss it; picker still visible (idx=%d)",
			m.picker.Idx())
	}
}

// TestPickerMouse_WheelOverPickerScrollsPicker pins a wheel event on
// the picker's first list row.
func TestPickerMouse_WheelOverPickerScrollsPicker(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("setup: picker should be visible after '/'")
	}
	_ = m.View()
	if !m.pickerGeom.HasList {
		t.Fatal("setup: pickerGeom.HasList must be true to drive MoveDown")
	}
	startIdx := m.picker.Idx()
	wheel := tea.MouseWheelMsg(tea.Mouse{X: 5, Y: m.pickerGeom.ListTopY, Button: tea.MouseWheelDown})
	m.Update(wheel)
	if got := m.picker.Idx(); got != startIdx+1 {
		t.Errorf("wheel-down on picker should advance idx by 1; got %d, want %d",
			got, startIdx+1)
	}
	up := tea.MouseWheelMsg(tea.Mouse{X: 5, Y: m.pickerGeom.ListTopY, Button: tea.MouseWheelUp})
	m.Update(up)
	if got := m.picker.Idx(); got != startIdx {
		t.Errorf("wheel-up on picker should drop idx back; got %d, want %d",
			got, startIdx)
	}
	abovePicker := tea.MouseWheelMsg(tea.Mouse{X: 5, Y: m.pickerGeom.TopY - 5, Button: tea.MouseWheelDown})
	m.Update(abovePicker)
	if got := m.picker.Idx(); got != startIdx {
		t.Errorf("wheel event outside picker should not move idx; got %d, want %d",
			got, startIdx)
	}
}

// TestPickerMouse_HoverChangesHighlight covers the hover-to-highlight
// behaviour.
func TestPickerMouse_HoverChangesHighlight(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	_ = m.View()
	startIdx := m.picker.Idx()
	if startIdx != 0 {
		t.Fatalf("setup: idx should start at 0, got %d", startIdx)
	}
	hoverY := m.pickerGeom.ListTopY + 3
	m.Update(mouseMotion(5, hoverY))
	if got := m.picker.Idx(); got != 3 {
		t.Errorf("hover over row 3 should set idx = 3, got %d", got)
	}
	m.Update(mouseMotion(5, m.pickerGeom.ListTopY))
	if got := m.picker.Idx(); got != 0 {
		t.Errorf("hover over row 0 should set idx = 0, got %d", got)
	}
	m.Update(mouseMotion(5, m.pickerGeom.TopY-1))
	if got := m.picker.Idx(); got != 0 {
		t.Errorf("hover outside picker should leave idx alone; got %d", got)
	}
}

// TestPickerMouse_PickerGeomTracksRender confirms m.pickerGeom is
// populated correctly from a render pass.
func TestPickerMouse_PickerGeomTracksRender(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.Update(runeKey('/'))
	if m.pickerGeom.Valid {
		t.Error("pickerGeom should start invalid before any render")
	}
	_ = m.View()
	if !m.pickerGeom.Valid {
		t.Fatal("pickerGeom should be valid after a View() with the picker open")
	}
	if m.pickerGeom.BottomY <= m.pickerGeom.TopY {
		t.Errorf("pickerGeom should span more than one row; top=%d bottom=%d",
			m.pickerGeom.TopY, m.pickerGeom.BottomY)
	}
	m.picker.Reset()
	_ = m.View()
	if m.pickerGeom.Valid {
		t.Errorf("pickerGeom should clear when picker is hidden; got %+v", m.pickerGeom)
	}
}

// runPigJetski executes one invocation through the standard v1.3.5 parser
// and returns (stdout, stderr, exitCode). Mirrors internal/cli/close_gap_test.go's
// runJetski helper so the print-mode contract lives next to the production
// binary instead of inside the cli package.
//
// RootRun is wired to runRoot exactly the way main.go wires it; without
// that wiring the parser would resolve to rc 0 with no work done (the
// real binary sets RootRun before calling ExecuteArgs).
func runPigJetski(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	p.Root().RootRun = func(args []string) error { return runRoot(args, p) }
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	_ = p.ExecuteArgs(args)
	return out.String(), errb.String(), p.ExitCode()
}

// TestPrintPromptInteractiveConflict_Exits2 is the headline test for the
// changelog fix. Every alias combination of --print/--prompt/-p with
// --prompt-interactive/-i must exit 2 (not 0, not 1) and the stderr must
// carry the canonical mutually-exclusive line.
func TestPrintPromptInteractiveConflict_Exits2(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"long_long", []string{"--print=hi", "--prompt-interactive=bye"}},
		{"short_long", []string{"-p=hi", "--prompt-interactive=bye"}},
		{"long_short", []string{"--print=hi", "-i=bye"}},
		{"short_short", []string{"-p=hi", "-i=bye"}},
		{"prompt_alias_long", []string{"--prompt=hi", "--prompt-interactive=bye"}},
		{"reversed_order", []string{"--prompt-interactive=bye", "--print=hi"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errb, code := runPigJetski(t, tc.args...)
			if code != 2 {
				t.Errorf("args %v: exit code = %d, want 2\nstderr=%q", tc.args, code, errb)
			}
			if !strings.Contains(errb, "mutually exclusive") {
				t.Errorf("args %v: stderr missing 'mutually exclusive': %q", tc.args, errb)
			}
			if out != "" {
				t.Errorf("args %v: stdout = %q, want empty (no model call should have happened)", tc.args, out)
			}
		})
	}
}

// TestPrintPromptInteractiveConflict_StderrMentionsBothFlags proves the
// recovered error line names both flags so the operator can tell which two
// collided without reading the man page.
func TestPrintPromptInteractiveConflict_StderrMentionsBothFlags(t *testing.T) {
	_, errb, code := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb, "--print") {
		t.Errorf("stderr = %q; missing '--print'", errb)
	}
	if !strings.Contains(errb, "--prompt-interactive") {
		t.Errorf("stderr = %q; missing '--prompt-interactive'", errb)
	}
	if !strings.Contains(errb, "mutually exclusive") {
		t.Errorf("stderr = %q; missing 'mutually exclusive'", errb)
	}
}

// TestPrintPromptInteractiveConflict_NotExit1 is the regression guard from
// the changelog text itself: the fix is *specifically* exit code 2
// (matching other print-mode usage errors), not the catch-all exit 1 the
// handler-failure path uses and not the silent exit 0 the bug produced
// before the fix.
func TestPrintPromptInteractiveConflict_NotExit1(t *testing.T) {
	_, _, code := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
	if code != 2 {
		t.Errorf("exit code = %d, want exactly 2 (the changelog fix)", code)
	}
	if code == 0 {
		t.Error("regression: the bug (silent exit 0 after reporting the conflict) is back")
	}
	if code == 1 {
		t.Error("regression: handler-failure exit 1 was used instead of usage-error exit 2")
	}
}

// TestPrintPromptInteractiveConflict_NoRegression pins the other print-mode
// usage errors that the parser surfaces with exit 2 — none of these must
// regress because of the new conflict-detection block in runRoot. (The
// empty-prompt case is rc 1 by design — it is a different error class —
// so it is not in this set.)
func TestPrintPromptInteractiveConflict_NoRegression(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"unknown_input_format", []string{"-p", "/help", "--input-format", "bogus"}},
		{"positional_after_prompt", []string{"-p", "hi", "extra"}},
		{"flag_shaped_prompt", []string{"-p", "--agent", "nosuchagent", "hi"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, code := runPigJetski(t, tc.args...)
			if code != 2 {
				t.Errorf("args %v: exit code = %d, want 2", tc.args, code)
			}
		})
	}
}

// TestPrintPromptInteractiveConflict_BeforeModelCall is the "no model call"
// requirement: the conflict must short-circuit before runPrintMode spins
// up a backend. We stand up a stub gateway server that 500s on every request
// — if the conflict check fails to fire, the run hits the gateway and
// fails with the gateway's HTTP 500 instead of the conflict line.
func TestPrintPromptInteractiveConflict_BeforeModelCall(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "this should never be reached on a conflict", http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "gateway")
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "`+srv.URL+`/v1",
		"models": [{"id": "test-model", "name": "Test Model", "model": "test-model"}]}}}`)

	out, errb, code := runPigJetski(t, "--print=hi", "--prompt-interactive=bye", "--model=test-model")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if hits != 0 {
		t.Errorf("model gateway was hit %d times; conflict must short-circuit before any backend call", hits)
	}
	if !strings.Contains(errb, "mutually exclusive") {
		t.Errorf("stderr = %q; want the conflict message, not a model error", errb)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
}

// TestPrintPromptInteractiveConflict_RunRootGuard is the defense-in-depth
// test for the new runRoot-level check. The parser catches the conflict
// first in production, but runRoot can also be called from paths that
// skip the parser (a future dispatcher, a regression in Parse, this
// test rig). When that happens the new block at the top of runRoot must
// still return the same exit-2 / mutually-exclusive line.
//
// We build a real Parser, run a clean Parse so the FlagSet is populated
// with the standard flags, then call fs.Set on prompt-interactive to
// mark it as if the user had typed it on the command line — fs.Visit
// (which flagWasSet uses) only reports flags that have been touched by
// Set / Parse. The print flag is already set from the Parse call, so
// runRoot sees both flags set and short-circuits on the new guard.
func TestPrintPromptInteractiveConflict_RunRootGuard(t *testing.T) {
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	root := p.Root()
	// Parse a non-conflicting argument set so the parser returns 0 and
	// leaves us with a populated FlagSet. --print is set as a side
	// effect of this Parse — exactly the state we want.
	if code := root.Parse([]string{"--print=hi"}); code != 0 {
		t.Fatalf("setup Parse(--print=hi) = %d", code)
	}
	runfs := root.FlagSet()
	// Mark prompt-interactive as set without re-registering it (the
	// parser already did that during Parse). fs.Set updates the
	// flag's Value and the internal "actually set" tracker that
	// fs.Visit reads — exactly the behaviour flagWasSet relies on.
	if err := runfs.Set("prompt-interactive", "bye"); err != nil {
		t.Fatalf("runfs.Set(prompt-interactive, bye): %v", err)
	}
	// Belt-and-braces: set the short alias too. The runRoot guard
	// already covers both names via `||`, so setting the long form
	// alone is sufficient, but a future refactor that drops the
	// short-form check would still be caught here.
	if err := runfs.Set("i", "bye"); err != nil {
		t.Fatalf("runfs.Set(i, bye): %v", err)
	}
	// Now invoke runRoot directly. It will see printMode=true (--print
	// is set on the FlagSet) and promptInteractive=true (the long
	// form we just Set), and short-circuit with the conflict error
	// before any model call.
	root.Out = &out
	root.ErrOut = &errb
	err := runRoot(nil, p)
	if err == nil {
		t.Fatalf("runRoot returned nil, want conflict error")
	}
	if code, ok := cli.ExitCodeOf(err); !ok {
		t.Fatalf("runRoot error %v is not an ExitCodeError", err)
	} else if code != 2 {
		t.Errorf("runRoot exit code = %d, want 2", code)
	}
	// The error message itself carries the conflict text. (In
	// production, jetski.handleRunErr prints this to ErrOut. We are
	// calling runRoot directly so we read the message off the error
	// directly — the parser-level test above already pins the
	// stderr rendering.)
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err.Error() = %q; want the conflict line from the runRoot guard", err.Error())
	}
}

// TestPrintPromptInteractiveConflict_RunRootGuardNoModelCall is the
// companion to the parser-level "before model call" test, but at the
// runRoot level. If the runRoot guard regresses (e.g. a future refactor
// moves the check after the model call), this test fails because the
// gateway would record a hit.
func TestPrintPromptInteractiveConflict_RunRootGuardNoModelCall(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "should never be reached", http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "gateway")
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "`+srv.URL+`/v1",
		"models": [{"id": "test-model", "name": "Test Model", "model": "test-model"}]}}}`)

	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	root := p.Root()
	if code := root.Parse([]string{"--print=hi"}); code != 0 {
		t.Fatalf("setup Parse(--print=hi) = %d", code)
	}
	runfs := root.FlagSet()
	if err := runfs.Set("prompt-interactive", "bye"); err != nil {
		t.Fatalf("runfs.Set: %v", err)
	}
	if err := runRoot(nil, p); err == nil {
		t.Fatalf("runRoot returned nil, want conflict error")
	}
	if hits != 0 {
		t.Errorf("model gateway hit %d times; runRoot guard must short-circuit before backend dispatch", hits)
	}
}

// TestPrintPromptInteractiveConflict_PrintAloneDoesNotTrigger is the
// negative side of the headline test: --print on its own must not be
// misclassified as a conflict. We point at a model-free backend and
// confirm the run proceeds to the print path (no conflict line, exit
// 0 or normal-print failure exit 3, never the mutually-exclusive rc 2).
func TestPrintPromptInteractiveConflict_PrintAloneDoesNotTrigger(t *testing.T) {
	// Use the shim backend (mock) so the run can complete locally
	// without OAuth or a network. NewMock returns a synthetic reply,
	// so the run finishes with rc 0 and stdout containing the reply.
	t.Setenv("OPENAGY_BACKEND", "shim")
	out, errb, code := runPigJetski(t, "--print=hello world")
	if code == 2 && strings.Contains(errb, "mutually exclusive") {
		t.Fatalf("regression: --print alone was misclassified as a conflict: rc=%d stderr=%q", code, errb)
	}
	if strings.Contains(errb, "mutually exclusive") {
		t.Errorf("stderr = %q; --print alone must not emit the conflict message", errb)
	}
	// shim succeeds, so we expect rc 0 + a non-empty stdout (the mock
	// reply). If this fails the test rig is broken, not the changelog
	// fix; the assertion is here so future regressions in the mock or
	// parser are caught separately.
	if code != 0 {
		t.Errorf("exit code = %d, want 0 for --print alone on the shim backend", code)
	}
	if out == "" {
		t.Errorf("stdout = %q, want non-empty (the mock should have produced a reply)", out)
	}
}

// TestPrintPromptInteractiveConflict_Exit2Exact is a paranoid guard: the
// changelog text says "exits with status 2 like other print-mode usage
// errors" — verify the integer is exactly 2, with no off-by-one (1 or 3)
// that could be a copy-paste from a neighbouring exit-code line.
func TestPrintPromptInteractiveConflict_Exit2Exact(t *testing.T) {
	_, _, code := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
	if code != 2 {
		t.Fatalf("exit code = %d, want exactly 2", code)
	}
	// Belt-and-braces: run the same combo three times in a row and
	// confirm the code is stable. A flaky parser path (e.g. one that
	// only fires on first invocation) would show up here as a
	// different code on a later call.
	for i := 0; i < 3; i++ {
		_, _, c := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
		if c != 2 {
			t.Errorf("run %d: exit code = %d, want 2", i, c)
		}
	}
}

// TestPrintPromptInteractiveConflict_ErrorHasBothFlags is the strict
// substring check for the recovered message. The line is recovered from
// the reference binary (see cmd/wagyu/testdata/print_changelog.md and the
// internal/cli/jetski.go mutual-exclusion branch) so the exact wording is
// load-bearing — if a future refactor rewords it, this test fires.
func TestPrintPromptInteractiveConflict_ErrorHasBothFlags(t *testing.T) {
	_, errb, code := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	want := "error: --print and --prompt-interactive are mutually exclusive"
	if !strings.Contains(errb, want) {
		t.Errorf("stderr = %q, want substring %q", errb, want)
	}
}

// Sanity check: the helper itself produces a Parser.ExitCode we can read.
// Wrapped in a test so the test file builds cleanly even if the helper
// drifts.
func TestPrintPromptInteractiveConflict_HelperExposesExitCode(t *testing.T) {
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	_ = p.ExecuteArgs([]string{"--print=hi", "--prompt-interactive=bye"})
	if p.ExitCode() != 2 {
		t.Errorf("Parser.ExitCode() = %d, want 2", p.ExitCode())
	}
	if !strings.Contains(errb.String(), "mutually exclusive") {
		t.Errorf("errb = %q, want mutually-exclusive line", errb.String())
	}
}

// Compile-time guard: confirm the backend.Client interface is what
// runRoot would dispatch to. Keeps the test file self-contained: any
// future interface drift surfaces here, not in the production binary.
var _ backend.Client = (*recordingPigBackend)(nil)

// recordingPigBackend is a no-op backend used by future regression
// checks. It is intentionally unused by the active tests but exported
// via a compile-time assertion so the package builds.
type recordingPigBackend struct {
	calls []backend.StreamRequest
}

func (r *recordingPigBackend) Stream(ctx context.Context, _ backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	ch := make(chan backend.StreamChunk, 1)
	ch <- backend.StreamChunk{Done: true}
	close(ch)
	return ch, fmt.Errorf("recordingPigBackend: would have called the model here")
}
