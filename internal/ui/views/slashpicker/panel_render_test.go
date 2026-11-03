// panel_render_test.go — the popup's rows and its mouse handling.
//
// The tests drive the panel the way the host does: the popup's state
// is derived from the buffer, the rows are written, and the rectangle
// is clipped into place before the mouse events arrive.

package slashpicker

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// host is a minimal stand-in for the composer's host: it holds the
// buffer, answers the terminal's width and knows which screen row is
// the composer.
type host struct {
	buffer string
	width  int
	inputY int
	alt    bool
}

func (h *host) seams() Seams {
	return Seams{
		AltScreen: func() bool { return h.alt },
		Width:     func() int { return h.width },
		IsInputY:  func(y int) bool { return y == h.inputY },
		Buffer:    func() string { return h.buffer },
		SetBuffer: func(s string) { h.buffer = s },
	}
}

// newPanel builds a popup over a host that holds the given buffer.
func newPanel(buffer string) (*Panel, *host) {
	h := &host{buffer: buffer, width: 80, alt: true}
	p := New(h.seams())
	p.Update([]rune(buffer))
	return p, h
}

// paint writes the popup's rows at the given screen row and clips the
// body, exactly as the host does around a frame.
func paint(p *Panel, bodyStart int) string {
	p.SetBodyStart(bodyStart)
	rows := p.Rows()
	p.ClipBody(0)
	return rows
}

func stripAnsi(s string) string { return ansi.Strip(s) }

// Cycle 129: the popup's scroll offset. Before the fix, 5+ Down's
// lost the highlight because the renderer loop only walked
// i < visible. With the offset, the highlighted row always lands in
// the visible window. The "↓ N more" tail reports rows below the
// window, not just rows below row 0.
func TestPickerView_ScrollOffsetKeepsHighlightVisible(t *testing.T) {
	p, _ := newPanel("/")
	if !p.Visible() {
		t.Fatal("the popup should be visible after '/'")
	}
	for i := 0; i < 8; i++ {
		p.MoveDown()
	}
	if got := p.Idx(); got < 8 {
		t.Fatalf("expected idx >= 8 after 8 Down's, got %d", got)
	}
	body := paint(p, 0)
	hiName := p.Selected()
	if hiName == nil {
		t.Fatalf("the popup should have a highlighted row")
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
	if p.offset < 1 {
		t.Errorf("expected the scroll offset >= 1 after scrolling, got %d", p.offset)
	}
}

func TestPickerView_ScrollOffsetRetreatsOnUp(t *testing.T) {
	p, _ := newPanel("/")
	for i := 0; i < 10; i++ {
		p.MoveDown()
	}
	// Rows() is what mutates the offset as a side effect of laying out
	// the visible window. The runtime emits a View() after every
	// Update; tests should follow the same contract.
	_ = paint(p, 0)
	offsetAfterDown := p.offset
	if offsetAfterDown < 1 {
		t.Fatalf("expected offset > 0 after 10 Down's, got %d", offsetAfterDown)
	}
	for i := 0; i < 10; i++ {
		p.MoveUp()
	}
	// After 10 Up's, idx drops by 10; rendering retreats the offset.
	_ = paint(p, 0)
	if p.offset > offsetAfterDown {
		t.Errorf("offset should not advance on Up, got %d (was %d)", p.offset, offsetAfterDown)
	}
}

func TestPickerView_MoreTailTracksWindow(t *testing.T) {
	p, _ := newPanel("/")
	body := paint(p, 0)
	// Cycle 351: the popup now holds every catalogue row — the
	// chrome-devtools-plugin:* filter is gone — so the 109 entries
	// (the reference's 105 plus /feature-diff, /accounts, /provider,
	// /pool) give a "104 more" tail with 5 in the window.
	if !strings.Contains(body, "104 more") {
		t.Errorf("the default popup's tail should be '104 more' (=109 - 5 visible), got tail: %q", tailOf(body))
	}
	for i := 0; i < 50; i++ {
		p.MoveDown()
	}
	body = paint(p, 0)
	cnt := extractTailCount(body)
	if cnt < 0 {
		t.Fatalf("could not parse tail count after scroll: %q", body)
	}
	if cnt >= 108 {
		t.Errorf("after scrolling, tail should be < 108, got %d", cnt)
	}
}

func extractTailCount(body string) int {
	for _, l := range strings.Split(body, "\n") {
		l = stripAnsi(l)
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "↓") {
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
		if strings.HasPrefix(strings.TrimSpace(l), "↓") {
			return l
		}
	}
	return "<no tail>"
}

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

// Cycle 130: the popup's alias rendering. The first alias is shown
// inline between the command name and the description (e.g.
// `/clear (new)`), the rest are dropped from the popup but still
// listed in /help COMMANDS tab.
func TestPickerView_FirstAliasInline(t *testing.T) {
	p, _ := newPanel("/")
	// Move the highlight to /clear (idx 5 after 5× Down).
	for i := 0; i < 5; i++ {
		p.MoveDown()
	}
	body := paint(p, 0)
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
	// The highlighted row's alias is part of the highlighted prefix.
	// The "> /clear (new)" string must be present in the rendered
	// output (it sits inside the pan-lenient style segments).
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
//
//	/btw (57 chars) → "Ask a side question without interrupting the ..."
//	/codesearch (57 chars) → "Search code in the workspace (usage: /codesea..."
func TestPickerView_DescriptionTruncation(t *testing.T) {
	if got := truncateDesc("short"); got != "short" {
		t.Errorf("short desc should pass through, got %q", got)
	}
	// 48-char description is the boundary: pass through unchanged.
	exact := strings.Repeat("a", descWidth)
	if got := truncateDesc(exact); got != exact {
		t.Errorf("desc at exact width should not be truncated, got %q", got)
	}
	// 49-char description should be truncated.
	over := strings.Repeat("a", descWidth+1)
	if got := truncateDesc(over); got != strings.Repeat("a", descWidth-3)+"..." {
		t.Errorf("49-char desc should truncate to 45+..., got len=%d (%q)", len(got), got)
	}
	// The recovered /btw case.
	btw := "Ask a side question without interrupting the current task"
	wantBtw := "Ask a side question without interrupting the ..."
	if got := truncateDesc(btw); got != wantBtw {
		t.Errorf("btw desc truncation wrong:\n  got  %q\n  want %q", got, wantBtw)
	}
	// The recovered /codesearch case.
	cs := "Search code in the workspace (usage: /codesearch <query>)"
	wantCs := "Search code in the workspace (usage: /codesea..."
	if got := truncateDesc(cs); got != wantCs {
		t.Errorf("codesearch desc truncation wrong:\n  got  %q\n  want %q", got, wantCs)
	}
}

// Cycle 131: rendering surfaces the truncated description in the
// popup's output for /btw. This is the integration check that the
// truncation helper is actually wired into the popup.
func TestPickerView_RenderTruncatesBtw(t *testing.T) {
	p, _ := newPanel("/")
	body := paint(p, 0)
	if !strings.Contains(body, "Ask a side question without interrupting the ...") {
		t.Errorf("/btw should render with 45-char + ... truncation in the popup")
	}
	if strings.Contains(body, "current task") {
		t.Errorf("non-truncated /btw tail leaked into the rendered overlay")
	}
}

// ---- cycle 232: the popup's mouse click + hover + wheel --------------

// TestPickerMouse_ClickOnOptionSelects opens the popup, renders the
// rows to populate the rectangle, then clicks a list row.
func TestPickerMouse_ClickOnOptionSelects(t *testing.T) {
	p, h := newPanel("/")
	if !p.Visible() {
		t.Fatal("setup: the popup should be visible after '/'")
	}
	// Paint so HandleClick can map (x, y) to a row.
	paint(p, 10)
	if !p.geom.Valid {
		t.Fatal("setup: the popup's rectangle should be valid after the first render")
	}
	if !p.geom.HasList {
		t.Fatal("setup: the popup's rectangle should carry a list for the '/' list")
	}
	// Click the 3rd list row (visible-row offset 2).
	x := 5
	y := p.geom.ListTopY + 2
	startBuf := h.buffer
	want := p.SelectedAt(p.layout.ListTop + 2)
	if want == nil {
		t.Fatalf("setup: no command at list offset 2 (listTop=%d)", p.layout.ListTop)
	}
	if !p.HandleClick(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}) {
		t.Fatal("setup: the click should be consumed by the popup")
	}
	if startBuf != "/" {
		t.Errorf("pre-click buffer %q should be '/'", startBuf)
	}
	if h.buffer != want.Name {
		t.Errorf("buffer after click = %q, want %q (the clicked command's name)",
			h.buffer, want.Name)
	}
	if !p.Visible() {
		t.Errorf("click on a list row should keep the popup visible (new buffer starts with '/'); got hidden")
	}
	if got := p.Len(); got != 1 {
		t.Errorf("after click, the popup holds %d rows, want 1 (only /artifact etc. matches)", got)
	}
	if got := p.Idx(); got != 0 {
		t.Errorf("after click, the highlight is at %d, want 0 (re-derived for the new buffer)", got)
	}
	paint(p, 10)
	if !p.geom.Valid {
		t.Errorf("after re-render with the new popup, the rectangle should be valid")
	}
	if !p.geom.HasList {
		t.Errorf("after re-render, the rectangle should carry a list")
	}
}

// TestPickerMouse_ClickOnInputReopensPicker mirrors the agy 1.2.13
// "clicks on the input field reopen the popup" rule.
func TestPickerMouse_ClickOnInputReopensPicker(t *testing.T) {
	p, h := newPanel("/")
	if !p.Visible() {
		t.Fatal("setup: the popup should be visible after '/'")
	}
	p.Cancel()
	if p.Visible() {
		t.Fatal("setup: Esc should have closed the popup")
	}
	if h.buffer == "" {
		t.Fatal("setup: Esc should NOT have cleared the composer's buffer")
	}
	paint(p, 10)
	h.inputY = 12
	p.HandleClick(tea.Mouse{X: 5, Y: h.inputY, Button: tea.MouseLeft})
	if !p.Visible() {
		t.Errorf("click on the input row should reopen the popup (the buffer still starts with '/'); got hidden")
	}
}

// TestPickerMouse_ClickOutsideDismissesPicker verifies the
// close-on-outside-click behaviour.
func TestPickerMouse_ClickOutsideDismissesPicker(t *testing.T) {
	p, _ := newPanel("/")
	if !p.Visible() {
		t.Fatal("setup: the popup should be visible after '/'")
	}
	paint(p, 10)
	if !p.geom.Valid {
		t.Fatal("setup: the popup's rectangle should be valid")
	}
	if p.HandleClick(tea.Mouse{X: 2, Y: 0, Button: tea.MouseLeft}) {
		t.Errorf("a click outside the popup should not be consumed by it")
	}
}

// TestPickerMouse_WheelOverPickerScrollsPicker pins a wheel event on
// the popup's first list row.
func TestPickerMouse_WheelOverPickerScrollsPicker(t *testing.T) {
	p, _ := newPanel("/")
	if !p.Visible() {
		t.Fatal("setup: the popup should be visible after '/'")
	}
	paint(p, 10)
	if !p.geom.HasList {
		t.Fatal("setup: the popup's rectangle must carry a list to drive MoveDown")
	}
	startIdx := p.Idx()
	if !p.HandleWheel(tea.Mouse{X: 5, Y: p.geom.ListTopY, Button: tea.MouseWheelDown}) {
		t.Fatal("setup: the wheel event should be consumed by the popup")
	}
	if got := p.Idx(); got != startIdx+1 {
		t.Errorf("wheel-down on the popup should advance the highlight by 1; got %d, want %d",
			got, startIdx+1)
	}
	p.HandleWheel(tea.Mouse{X: 5, Y: p.geom.ListTopY, Button: tea.MouseWheelUp})
	if got := p.Idx(); got != startIdx {
		t.Errorf("wheel-up on the popup should drop the highlight back; got %d, want %d",
			got, startIdx)
	}
	p.HandleWheel(tea.Mouse{X: 5, Y: p.geom.TopY - 5, Button: tea.MouseWheelDown})
	if got := p.Idx(); got != startIdx {
		t.Errorf("wheel event outside the popup should not move the highlight; got %d, want %d",
			got, startIdx)
	}
}

// TestPickerMouse_HoverChangesHighlight covers the hover-to-highlight
// behaviour.
func TestPickerMouse_HoverChangesHighlight(t *testing.T) {
	p, _ := newPanel("/")
	paint(p, 10)
	startIdx := p.Idx()
	if startIdx != 0 {
		t.Fatalf("setup: the highlight should start at 0, got %d", startIdx)
	}
	hoverY := p.geom.ListTopY + 3
	p.HandleHover(tea.Mouse{X: 5, Y: hoverY})
	if got := p.Idx(); got != 3 {
		t.Errorf("hover over row 3 should set the highlight to 3, got %d", got)
	}
	p.HandleHover(tea.Mouse{X: 5, Y: p.geom.ListTopY})
	if got := p.Idx(); got != 0 {
		t.Errorf("hover over row 0 should set the highlight to 0, got %d", got)
	}
	p.HandleHover(tea.Mouse{X: 5, Y: p.geom.TopY - 1})
	if got := p.Idx(); got != 0 {
		t.Errorf("hover outside the popup should leave the highlight alone; got %d", got)
	}
}

// TestPickerMouse_PickerGeomTracksRender confirms the popup's
// rectangle is populated correctly from a render pass.
func TestPickerMouse_PickerGeomTracksRender(t *testing.T) {
	p, _ := newPanel("/")
	if p.geom.Valid {
		t.Error("the rectangle should start invalid before any render")
	}
	paint(p, 10)
	if !p.geom.Valid {
		t.Fatal("the rectangle should be valid after a render with the popup open")
	}
	if p.geom.BottomY <= p.geom.TopY {
		t.Errorf("the rectangle should span more than one row; top=%d bottom=%d",
			p.geom.TopY, p.geom.BottomY)
	}
	p.Reset()
	paint(p, 10)
	if p.geom.Valid {
		t.Errorf("the rectangle should clear when the popup is hidden; got %+v", p.geom)
	}
}
