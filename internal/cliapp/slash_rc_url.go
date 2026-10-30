// slash_rc_url.go — the /remote-control row's browser link.
//
// The /remote-control transcript row shows a URL the way the
// add-account screen shows one: the URL is cut into chunks, one chunk
// per row, and every chunk carries the *complete* URL as its OSC 8
// hyperlink target.
//
// The reason is the renderer. The TUI draws its view into a cell
// buffer the size of the terminal, so a line longer than the terminal
// is *clipped*, not soft wrapped: the tail of every long line simply
// disappears. The account chooser URL is ~170 characters — the deep
// link's instance id is at its tail — so a URL left on one line was a
// URL whose tail the user could not see, could not select and could
// not click.
//
// The chunking and the hyperlink frame live in internal/urlchunk,
// which the add-account screen shares: the two surfaces show a URL the
// same way, so they cut it the same way.
package cliapp

import (
	"net/url"
	"strings"

	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/rclink"
	"github.com/justwasm/pigasus/internal/urlchunk"
)

// rcURLIndent is the indent the /remote-control transcript rows carry.
//
// The reference's own /remote-control output indents its lines by five
// columns ("     Open on another device to take over:").
const rcURLIndent = "     "

// rcURLDefaultWidth is the width the URL rows are laid out at when the
// terminal has not reported its size yet.
//
// It is the same default the add-account screen uses (80 columns).
const rcURLDefaultWidth = 80

// rcURLChunkWidth is the number of characters of the URL that fit on
// one transcript row.
//
// The rows carry the transcript's five-column indent, and one column
// is left empty so that a row never touches the terminal's right edge
// — the same slack the add-account screen leaves.
func (m *runMode) rcURLChunkWidth() int {
	width := m.width
	if width <= 0 {
		width = rcURLDefaultWidth
	}
	w := width - len(rcURLIndent) - 1
	if w < 1 {
		w = 1
	}
	return w
}

// rcURLRows renders the browser link as the rows the transcript shows.
func (m *runMode) rcURLRows(url string) []string {
	return urlchunk.Rows(url, rcURLIndent, m.rcURLChunkWidth(), m.hyperlinkCaps)
}

// buildRemoteControlCard constructs the status message and extra transcript lines
// for the /remote-control card (used both by the /remote-control slash command
// and the startup --remote-control flag).
func buildRemoteControlCard(conversationID, uuid, hostname string, urlRows func(string) []string) (string, []string) {
	return buildRemoteControlCardForAccount("", conversationID, uuid, hostname, urlRows)
}

// buildRemoteControlCardForAccount constructs the /remote-control card with an explicit account email.
func buildRemoteControlCardForAccount(email, conversationID, uuid, hostname string, urlRows func(string) []string) (string, []string) {
	msg := "Remote control on for this session."
	instanceID := persistentrc.InstanceIDPrefix + uuid + persistentrc.InstanceIDSuffix
	lead := "Open on another device to take over:"
	rawQuery := ""
	if conversationID != "" {
		lead = "Open on another device to continue this conversation:"
		rawQuery = "p=" + url.QueryEscape("c/"+conversationID)
	}
	extra := []string{rcURLIndent + lead}
	if hostname != "" {
		extra = append(extra, rcURLIndent+"Hostname: "+hostname)
	}
	if strings.TrimSpace(email) == "" {
		email = rclink.ActiveAccount()
	}
	browserURL := rclink.BrowserURLForAccount(strings.TrimSpace(email), instanceID, rawQuery)
	if urlRows != nil {
		extra = append(extra, urlRows(browserURL)...)
	} else {
		extra = append(extra, rcURLIndent+browserURL)
	}
	return msg, extra
}

