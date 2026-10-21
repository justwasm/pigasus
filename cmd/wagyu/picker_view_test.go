package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
	// With /teamwork and /boost added, the picker shows 97 visible-filtered
	// entries. With 5 in the window, the tail is "92 more".
	if !strings.Contains(body, "92 more") {
		t.Errorf("default picker tail should be '92 more' (=97 - 5 visible), got tail: %q", tailOf(body))
	}
	for i := 0; i < 50; i++ {
		m.Update(key(tea.KeyDown, 0))
	}
	body = m.pickerView()
	cnt := extractTailCount(body)
	if cnt < 0 {
		t.Fatalf("could not parse tail count after scroll: %q", body)
	}
	if cnt >= 96 {
		t.Errorf("after scrolling, tail should be < 96, got %d", cnt)
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
