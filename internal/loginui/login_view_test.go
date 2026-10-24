// login_view_test.go — the login screen's layout.
//
// The regression this file guards: the renderer draws the view into a
// cell buffer the size of the terminal, so a line longer than the
// terminal is *clipped*, not soft wrapped. The authorization URL is
// ~700 characters long, so on a phone-sized terminal the visible part
// stopped after a few dozen characters and the link could not be
// copied at all.
//
// The reference wraps the URL into chunks exactly as wide as the
// terminal and makes every chunk a hyperlink to the *complete* URL;
// the clone does the same.
package loginui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/loginui"
)

// testAuthURL is a stand-in for the real authorization URL: it has
// the same shape (a long query string with no spaces) and the same
// length ballpark.
const testAuthURL = "https://accounts.google.com/o/oauth2/auth?access_type=offline&client_id=1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com&code_challenge=LBEcUCV24l4_MZLPME3p3QIJgETRf1YmC0t6q5vrh7cg&code_challenge_method=S256&prompt=consent&redirect_uri=https%3A%2F%2Fantigravity.google%2Foauth-callback&response_type=code&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fcloud-platform&state=HLzxDisAwi4u76yTuWVujA"

// oauthModel returns a model sitting on the OAuth step with a known
// URL and the given terminal size.
func oauthModel(width, height int) *loginui.Model {
	m := loginui.New()
	m.State = loginui.StateOAuth
	m.AuthURL = testAuthURL
	m.Width = width
	m.Height = height
	return m
}

// visibleURL joins the visible text of the URL block back together.
//
// The URL block is a sequence of OSC 8 hyperlinks: the link target is
// the complete URL, the visible text is one chunk of it. The chunks
// are what the user selects and copies.
//
// The "→ Click here to authenticate" line is an OSC 8 hyperlink too,
// but its visible text is a label and not a piece of the URL — it is
// skipped (it contains a space, the URL never does).
func visibleURL(view string) string {
	const open = "\x1b]8;;"
	var b strings.Builder
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(line, open) {
			continue
		}
		rest := line[strings.Index(line, open)+len(open):]
		// Handle both \x1b\ (ST) and \x07 (BEL)
		var chunk string
		var ok bool
		if strings.Contains(rest, "\x1b\\") {
			_, chunk, ok = strings.Cut(rest, "\x1b\\")
		} else {
			_, chunk, ok = strings.Cut(rest, "\x07")
		}
		if !ok {
			continue
		}
		chunk, _, _ = strings.Cut(chunk, "\x1b]8;;")
		chunk = ansi.Strip(chunk)
		if strings.ContainsAny(chunk, " ") {
			continue
		}
		b.WriteString(chunk)
	}
	return b.String()
}

// TestLoginView_NoLineExceedsTerminalWidth is the regression guard for
// the truncation: no rendered line may be wider than the terminal,
// because the renderer would clip it.
func TestLoginView_NoLineExceedsTerminalWidth(t *testing.T) {
	for _, width := range []int{20, 40, 60, 80, 120} {
		m := oauthModel(width, 60)
		view := m.View().Content
		for i, line := range strings.Split(view, "\n") {
			w := ansi.StringWidth(line)
			if w > width {
				t.Errorf("width %d: line %d is %d cells wide: %q", width, i+1, w, ansi.Strip(line))
			}
		}
	}
}

// TestLoginView_URLIsNotTruncated pins the fix itself: the whole
// authorization URL has to be on screen, wrapped over several lines.
func TestLoginView_URLIsNotTruncated(t *testing.T) {
	m := oauthModel(40, 60)
	view := m.View().Content

	if got := visibleURL(view); got != testAuthURL {
		t.Errorf("the URL on screen is not the whole URL\n got: %q\nwant: %q", got, testAuthURL)
	}
}

// TestLoginView_URLChunksAreLinks pins the mobile path: every chunk
// carries the complete URL as its OSC 8 target, so a tap on any chunk
// opens the whole thing.
func TestLoginView_URLChunksAreLinks(t *testing.T) {
	m := oauthModel(40, 60)
	view := m.View().Content

	links := 0
	for _, line := range strings.Split(view, "\n") {
		const open = "\x1b]8;;"
		if !strings.Contains(line, open) {
			continue
		}
		rest := line[strings.Index(line, open)+len(open):]
		var target string
		var ok bool
		if strings.Contains(rest, "\x1b\\") {
			target, _, ok = strings.Cut(rest, "\x1b\\")
		} else {
			target, _, ok = strings.Cut(rest, "\x07")
		}
		if !ok {
			t.Fatalf("unterminated OSC 8 sequence: %q", line)
		}
		if target != testAuthURL {
			t.Errorf("OSC 8 target = %q, want the complete URL", target)
		}
		links++
	}
	if links < 2 {
		t.Errorf("the URL is not wrapped into several links: %d link(s)", links)
	}
}

// TestLoginView_ClickHereBlock pins the fallback block the reference
// shows under the URL: the "copy and paste" hint, the two rules and
// the "→ Click here to authenticate" link, which is an OSC 8
// hyperlink to the complete URL (a cmd+click opens it).
func TestLoginView_ClickHereBlock(t *testing.T) {
	m := oauthModel(80, 60)
	view := m.View().Content

	stripped := ansi.Strip(view)
	for _, want := range []string{
		"Copy and paste the URL or click on the link below:",
		"→ Click here to authenticate",
	} {
		if !strings.Contains(stripped, want) {
			t.Errorf("the screen has no %q:\n%s", want, stripped)
		}
	}
	if !strings.Contains(stripped, strings.Repeat("─", 20)) {
		t.Errorf("the screen has no rule around the link:\n%s", stripped)
	}

	// The link label is a hyperlink to the complete URL.
	const open = "\x1b]8;;"
	found := false
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(ansi.Strip(line), "Click here to authenticate") {
			continue
		}
		rest := line[strings.Index(line, open)+len(open):]
		var target string
		var ok bool
		if strings.Contains(rest, "\x1b\\") {
			target, _, ok = strings.Cut(rest, "\x1b\\")
		} else {
			target, _, ok = strings.Cut(rest, "\x07")
		}
		if !ok {
			t.Fatalf("unterminated OSC 8 sequence: %q", line)
		}
		if target != testAuthURL {
			t.Errorf("the click-here target = %q, want the complete URL", target)
		}
		found = true
	}
	if !found {
		t.Fatalf("the click-here line is not a hyperlink:\n%s", stripped)
	}
}

// TestLoginView_ScrollsWhenTheContentOverflows pins the viewport: the
// screen is taller than a phone-sized terminal, so it has to be
// scrollable — the reference shows "(1–26 of 35 lines)" and scrolls
// with shift+up / shift+down.
func TestLoginView_ScrollsWhenTheContentOverflows(t *testing.T) {
	m := oauthModel(80, 20)
	view := m.View().Content
	if !strings.Contains(view, " lines)") {
		t.Fatalf("expected the line counter in the view, got:\n%s", view)
	}
	if !strings.Contains(view, "shift+") {
		t.Fatalf("expected the scroll hint in the footer, got:\n%s", view)
	}

	// The code prompt has to be reachable.
	for i := 0; i < 100; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	}
	scrolled := m.View().Content
	if !strings.Contains(ansi.Strip(scrolled), "authorization code...") {
		t.Fatalf("the code prompt is not reachable by scrolling, got:\n%s", ansi.Strip(scrolled))
	}
}

// TestLoginView_ArrowKeysAreNotText pins the input handler: the
// navigation keys carry a special key code (0x100000 and up), which
// must not end up in the pasted authorization code.
func TestLoginView_ArrowKeysAreNotText(t *testing.T) {
	m := oauthModel(40, 60)
	m.Update(tea.KeyPressMsg{Text: "4/0A"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.CodeInput != "4/0A" {
		t.Fatalf("codeInput = %q, want %q", m.CodeInput, "4/0A")
	}
}
