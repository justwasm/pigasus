package loginui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/tuilib"
)

// View renders the login screen.
//
// The screen is taller than a phone-sized terminal — the logo, the
// authorization URL and the code prompt do not fit in one screen — so
// it is rendered into a scrollable window, the way the reference does
// (the reference's viewport shows "(1–26 of 35 lines)" and scrolls
// with shift+up / shift+down).
func (m *Model) View() tea.View {
	lines := m.screenLines()

	area := m.contentArea()
	overflow := len(lines) > area
	visible := area
	if overflow {
		visible = area - 1 // the last row carries the line counter
	}
	if visible < 1 {
		visible = 1
	}
	start := m.scrollStart(len(lines), visible)
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for _, line := range lines[start:end] {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if overflow {
		fmt.Fprintf(&b, "  (%d–%d of %d lines)\n\n", start+1, end, len(lines))
	}
	b.WriteString(m.footer(overflow))

	v := tea.NewView(b.String())
	v.AltScreen = m.AltScreen
	return v
}

// screenLines builds the screen's lines. Every line is already at
// most the terminal width wide, so the renderer cannot clip it.
func (m *Model) screenLines() []string {
	w := m.contentWidth()

	lines := make([]string, 0, 32)
	lines = append(lines, "")
	for _, l := range tuilib.RenderLogoLines() {
		lines = append(lines, l)
	}
	lines = append(lines, "")

	switch m.State {
	case StateSelectMethod:
		lines = append(lines, wrapProse(" Welcome to the \x1b[38;2;66;133;244mAntigravity CLI\x1b[m. You are currently not signed in.", w)...)
		lines = append(lines, "")
		lines = append(lines, m.selectMethodLines()...)
	case StateOAuth:
		lines = append(lines, m.oauthLines()...)
	case StateExchanging:
		lines = append(lines, m.exchangingLines()...)
	case StateCloudProject:
		lines = append(lines, m.cloudProjectLines()...)
	case StateSuccess:
		lines = append(lines, "\x1b[32m Authentication successful!\x1b[m")
	case StateError:
		lines = append(lines, m.errorLines()...)
	}

	return lines
}

// contentArea is the number of rows the scrolling area gets: the
// terminal height minus the line counter's row, the blank below it and
// the footer.
func (m *Model) contentArea() int {
	h := m.Height
	if h <= 0 {
		h = 24
	}
	area := h - 3
	if area < 1 {
		area = 1
	}
	return area
}

// scrollStart clamps the scroll offset so the window stays inside the
// content.
func (m *Model) scrollStart(total, height int) int {
	start := m.Scroll
	if start > total-height {
		start = total - height
	}
	if start < 0 {
		start = 0
	}
	return start
}

// footer is the key hint line at the bottom of the screen. The hints
// that do not fit the terminal are left out rather than clipped: the
// full line first, then the important hints alone.
func (m *Model) footer(scrollable bool) string {
	esc := "\x1b[94;1mesc\x1b[90;22m Back"
	enter := "\x1b[94;1menter\x1b[90;22m Submit"
	scroll := "\x1b[94;1mshift+↑/↓\x1b[90;22m Scroll"

	candidates := [][]string{{esc, enter}}
	if scrollable {
		candidates = [][]string{
			{esc, enter, scroll},
			{enter, scroll},
			{scroll},
		}
	}

	width := m.contentWidth()
	for _, parts := range candidates {
		line := strings.Join(parts, " · ")
		if ansi.StringWidth(line)+2 <= width {
			return "  " + line + "\n"
		}
	}
	return "  " + scroll + "\n"
}

// selectMethodLines renders the login-method chooser.
func (m *Model) selectMethodLines() []string {
	glyph := brailleFrames[m.SpinFrame%len(brailleFrames)]
	lines := []string{fmt.Sprintf(" %s  Signing in... Select login method:", glyph)}
	if m.Cursor == 0 {
		lines = append(lines, " > 1. Google OAuth", "   2. Use a Google Cloud project")
	} else {
		lines = append(lines, "   1. Google OAuth", " > 2. Use a Google Cloud project")
	}
	lines = append(lines, "", "   \x1b[94;1m↑/↓\x1b[90;22m Navigate · \x1b[94;1menter\x1b[90;22m Select\x1b[m")
	return lines
}

// oauthLines renders the OAuth step.
//
// The reference's 1.4.x screen is:
//
//	Your browser should open automatically. If not:
//
//	<the authorization URL, wrapped to the terminal width>
//
//	Copy and paste the URL or click on the link below:
//	<rule>
//	→ Click here to authenticate
//	<rule>
//
//	If you aren't automatically redirected, paste the authorization code below:
//
//	 authorization code...
//
// Both the URL chunks and the "click here" line are OSC 8 hyperlinks
// to the *complete* URL, so a cmd+click on either one opens it.
func (m *Model) oauthLines() []string {
	w := m.contentWidth()

	lines := make([]string, 0, 32)
	lines = append(lines, wrapProse(" Your browser should open automatically. If not:", w)...)
	lines = append(lines, "")
	lines = append(lines, m.authURLLines()...)
	lines = append(lines, "")
	lines = append(lines, wrapProse(" Copy and paste the URL or click on the link below:", w)...)
	lines = append(lines, m.rule())
	lines = append(lines, m.clickHereLine())
	lines = append(lines, m.rule())
	lines = append(lines, "")
	lines = append(lines, wrapProse(" If you aren't automatically redirected, paste the authorization code below:", w)...)
	lines = append(lines, "")
	if m.CodeInput != "" {
		lines = append(lines, m.clip(fmt.Sprintf(" %s\x1b[7m \x1b[m", m.CodeInput)))
	} else {
		lines = append(lines, m.clip(fmt.Sprintf(" \x1b[90mauthorization code...\x1b[m")))
	}
	lines = append(lines, "", "")
	return lines
}

// rule is the horizontal rule that frames the "click here" link. It
// spans the terminal, the way the reference's does.
func (m *Model) rule() string {
	w := m.contentWidth() - 1
	if w < 1 {
		w = 1
	}
	return " " + strings.Repeat("─", w)
}

// clickHereLine is the "→ Click here to authenticate" link. It is an
// OSC 8 hyperlink to the complete URL, so a cmd+click on it opens the
// whole thing — the same target the URL chunks carry.
//
// The label is clipped, never wrapped: a wrapped link label would
// split the OSC 8 target away from the text it belongs to.
func (m *Model) clickHereLine() string {
	const label = "→ Click here to authenticate"
	w := m.contentWidth() - 1
	if w < 1 {
		w = 1
	}
	text := label
	if ansi.StringWidth(text) > w {
		text = ansi.Truncate(text, w, "")
	}
	return fmt.Sprintf(" \x1b]8;;%s\x1b\\\x1b[34;4m%s\x1b[m\x1b]8;;\x1b\\", m.AuthURL, text)
}

// clip trims a rendered line to the terminal width.
func (m *Model) clip(line string) string {
	w := m.contentWidth()
	if ansi.StringWidth(line) <= w {
		return line
	}
	return ansi.Truncate(line, w, "")
}

// authURLLines writes the authorization URL, wrapped to the terminal
// width.
//
// Every chunk carries the *complete* URL as its OSC 8 target and is
// styled with underline and blue colour (matching clickHereLine and the
// reference binary's URL styling), so:
//
//   - modern terminals render it visibly as a clickable hyperlink;
//   - cmd+click or tap on any chunk opens the complete URL;
//   - the whole URL is on screen and can be selected and copied.
func (m *Model) authURLLines() []string {
	width := m.contentWidth() - 2
	if width < 1 {
		width = 1
	}
	lines := make([]string, 0, 32)
	for _, chunk := range urlChunks(m.AuthURL, width) {
		lines = append(lines, fmt.Sprintf(" \x1b]8;;%s\x1b\\\x1b[34;4m%s\x1b[m\x1b]8;;\x1b\\", m.AuthURL, chunk))
	}
	return lines
}

// exchangingLines renders the code exchange step.
func (m *Model) exchangingLines() []string {
	glyph := brailleFrames[m.SpinFrame%len(brailleFrames)]
	return []string{fmt.Sprintf(" %s  Exchanging authorization code...", glyph)}
}

// cloudProjectLines renders the Cloud project chooser.
func (m *Model) cloudProjectLines() []string {
	lines := []string{"Google Cloud sign-in method:"}
	if m.CloudCursor == 0 {
		lines = append(lines, " > 1. Continue with Google Cloud", "   2. Advanced SSO config")
	} else {
		lines = append(lines, "   1. Continue with Google Cloud", " > 2. Advanced SSO config")
	}
	lines = append(lines, "", "   · \x1b[94;1mesc\x1b[90;22m Back · \x1b[94;1menter\x1b[90;22m Select\x1b[m")
	return lines
}

// errorLines renders the failure step.
func (m *Model) errorLines() []string {
	if m.Err != nil {
		return []string{
			fmt.Sprintf("\x1b[31m Authentication failed: %v\x1b[m", m.Err),
			"",
			"  \x1b[94;1menter/esc\x1b[90;22m Try again\x1b[m",
		}
	}
	return []string{
		"\x1b[31m Authentication failed.\x1b[m",
		"",
		"  \x1b[94;1menter/esc\x1b[90;22m Try again\x1b[m",
	}
}
