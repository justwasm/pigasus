// Tests for the /remote-control row's browser link.
//
// The link is shown the way the add-account screen shows a URL: cut
// into chunks, one chunk per row, and every chunk a hyperlink to the
// *complete* URL.
//
// The reason is the renderer: the TUI draws its view into a cell
// buffer the size of the terminal, so a line wider than the terminal
// is *clipped*, not soft wrapped. The account chooser URL is ~164
// characters and its tail carries the instance id — the part that
// names the daemon — so a URL left on one line was a URL whose tail
// the user could not see, could not select and could not click.
package cliapp

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/internal/tuiviewport"
)

// chooserURL is the account chooser link the clone prints — the long
// form, ~164 characters.
const chooserURL = "https://accounts.google.com/AccountChooser?Email=naivgaid%40gmail.com&continue=https%3A%2F%2Fantigravity.google.com%2Fr%2F98bc1aa9-eb81-4c35-8c20-fde1e5b64300-v2"

// linkRows returns the transcript rows that carry a browser link.
//
// A link row is a row that mentions a URL — the OSC 8 frame carries
// the complete URL, so the frame's target is the row's URL.
func linkRows(rows []string) []string {
	var out []string
	for _, r := range rows {
		if strings.Contains(r, "https://") {
			out = append(out, r)
		}
	}
	return out
}

// linkText joins the visible text of the link rows — the URL as the
// user reads it on screen, with the escapes removed.
func linkText(rows []string) string {
	var b strings.Builder
	for _, r := range linkRows(rows) {
		b.WriteString(strings.TrimSpace(ansi.Strip(r)))
	}
	return b.String()
}

// osc8Target returns the OSC 8 target of a row, or "" when the row is
// not a hyperlink.
func osc8Target(row string) string {
	const open = "\x1b]8;;"
	i := strings.Index(row, open)
	if i < 0 {
		return ""
	}
	rest := row[i+len(open):]
	j := strings.Index(rest, "\x1b\\")
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// rcURLRunMode returns a run mode with the OSC 8 hyperlink support
// forced on.
//
// The detection consults os.Environ(), and a `go test` runner does not
// necessarily advertise OSC 8 — so the test that cares about the
// hyperlink frame turns it on explicitly, the way the eligibility
// notice's test does.
func rcURLRunMode(t *testing.T, width int) *runMode {
	t.Helper()
	m := slashRemoteControlRunMode(t)
	m.width = width
	m.hyperlinkCaps.OSC8 = true
	return m
}

// TestRemoteControlURL_WrapsIntoChunkRows is the regression guard for
// the clipping: the URL is cut into rows that fit the terminal, and
// the chunks are the URL — nothing is lost.
func TestRemoteControlURL_WrapsIntoChunkRows(t *testing.T) {
	m := rcURLRunMode(t, 80)

	rows := m.rcURLRows(chooserURL)
	if len(rows) < 2 {
		t.Fatalf("the URL was not wrapped into several rows: %d row(s)", len(rows))
	}
	var joined string
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > m.width {
			t.Errorf("a row is %d cells wide, wider than the terminal (%d): %q", w, m.width, ansi.Strip(row))
		}
		if !strings.HasPrefix(row, rcURLIndent) {
			t.Errorf("a row does not carry the transcript indent: %q", row)
		}
		joined += ansi.Strip(row)[len(rcURLIndent):]
	}
	if joined != chooserURL {
		t.Errorf("the visible chunks are not the URL\n got: %q\nwant: %q", joined, chooserURL)
	}
}

// TestRemoteControlURL_EveryChunkIsALinkToTheCompleteURL pins the
// mobile path: a tap (or a cmd+click) on any chunk opens the whole
// link, not the chunk.
func TestRemoteControlURL_EveryChunkIsALinkToTheCompleteURL(t *testing.T) {
	m := rcURLRunMode(t, 80)

	rows := m.rcURLRows(chooserURL)
	links := 0
	for _, row := range rows {
		target := osc8Target(row)
		if target == "" {
			t.Errorf("a chunk row is not a hyperlink: %q", row)
			continue
		}
		if target != chooserURL {
			t.Errorf("the OSC 8 target = %q, want the complete URL", target)
		}
		links++
	}
	if links < 2 {
		t.Errorf("the URL is not wrapped into several links: %d link(s)", links)
	}
}

// TestRemoteControlURL_PlainTextWithoutOSC8 pins the fallback: a
// terminal that does not speak OSC 8 must not see the escape
// sequences — and it must still see the whole URL, one row at a time.
func TestRemoteControlURL_PlainTextWithoutOSC8(t *testing.T) {
	m := rcURLRunMode(t, 80)
	m.hyperlinkCaps.OSC8 = false

	rows := m.rcURLRows(chooserURL)
	var joined string
	for _, row := range rows {
		if strings.Contains(row, "\x1b") {
			t.Errorf("a row carries an escape sequence: %q", row)
		}
		joined += strings.TrimPrefix(row, rcURLIndent)
	}
	if joined != chooserURL {
		t.Errorf("the visible chunks are not the URL\n got: %q\nwant: %q", joined, chooserURL)
	}
}

// TestRemoteControlURL_CopiesAsOneLine pins the "select it" half: the
// chunks are one logical string, so copying them yields the URL and
// not a stack of lines.
func TestRemoteControlURL_CopiesAsOneLine(t *testing.T) {
	m := rcURLRunMode(t, 80)

	rows := m.rcURLRows(chooserURL)
	copied := tuiviewport.SelectionText(rows, 0, 0, len(rows)-1, ansi.StringWidth(ansi.Strip(rows[len(rows)-1]))-1)
	if strings.Contains(copied, "\n") {
		t.Errorf("the copy carries a newline: %q", copied)
	}
	if !strings.Contains(copied, chooserURL) {
		t.Errorf("the copy does not carry the URL: %q", copied)
	}
}

// TestRemoteControlURL_HandlerPrintsTheWrappedLink is the integration
// guard: the /remote-control row prints the link through the same
// helper, so what the user sees is the wrapped, clickable form.
func TestRemoteControlURL_HandlerPrintsTheWrappedLink(t *testing.T) {
	m := rcURLRunMode(t, 60)

	if handled, _ := m.handleSlashCommand("/remote-control"); !handled {
		t.Fatal("expected handled=true")
	}
	rows := linkRows(m.response)
	if len(rows) < 2 {
		t.Fatalf("the link was not wrapped into several rows: %d row(s) in\n%s", len(rows), strings.Join(m.response, "\n"))
	}
	// Every chunk row is a link to the complete URL.
	want := osc8Target(rows[0])
	if want == "" {
		t.Fatalf("the first chunk row is not a hyperlink: %q", rows[0])
	}
	for _, row := range rows {
		if got := osc8Target(row); got != want {
			t.Errorf("the OSC 8 target = %q, want %q", got, want)
		}
	}
	// The visible text of the rows is the whole URL.
	if got := linkText(m.response); !strings.Contains(got, want) {
		t.Errorf("the visible link text %q does not carry the URL %q", got, want)
	}
	// The transcript's own wrap leaves the chunk rows alone: a row
	// that the viewport had to hard-wrap would lose its OSC 8 frame
	// (the frame would be split across two rows) and the link would
	// stop working.
	for _, row := range rows {
		if wrapped := tuilib.WrapRows(row, m.width); len(wrapped) != 1 {
			t.Errorf("the transcript re-wrapped a chunk row into %d rows: %q", len(wrapped), row)
		}
	}
}
