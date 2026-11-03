// panel.go — the /usage panel's own rows: the document it scrolls, the
// window over that document and the panel's chrome.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/usagepanel: it owns the
// document, the window arithmetic, the counter and the legend. The host
// supplies the seams — the account's email, the terminal's size, the
// quota state and the scroll offset.
//
// # The offset
//
// The scroll offset stays the host's: the host's panel struct carries one
// window offset that several panels share, so Lines takes the offset as
// an argument and hands the clamped one back.

package usagepanel

import (
	"time"

	"github.com/justwasm/pigasus/internal/features/auth"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// State is the /usage panel's facts.
//
// The host gathers them on every draw, so the panel never shows a stale
// count: the quota call may have answered, and the refresh countdown
// ticks while the panel is up.
type State struct {
	// Email is the account line's address. An empty email falls back to
	// the signed-in account (see Chrome).
	Email string

	// Width and Height are the terminal's size. The height decides how
	// many document rows one window shows (see Page).
	Width  int
	Height int

	// Loading says the quota call is still in flight: the panel shows
	// the one-row spinner instead of the document.
	Loading bool

	// Summary is the live retrieveUserQuotaSummary answer. A nil summary
	// with Loading false means the clone's frozen stub document.
	Summary *auth.QuotaSummary

	// RefreshG and RefreshO are the two buckets' refresh times. A zero
	// time falls back to the frozen countdown.
	RefreshG time.Time
	RefreshO time.Time

	// Frame is the spinner frame the loading row shows.
	Frame int
}

// The frozen countdowns the reference's capture recorded. They are the
// values the stub document shows when no refresh time is known. 【推测】
const (
	frozenRefreshG = "158h 18m"
	frozenRefreshO = "64h 47m"
)

// Doc is the /usage scrollable document.
//
// It is one of three: the loading row while the quota call is in
// flight, the live summary once it answered, and the frozen stub the
// clone shows when it has no answer at all.
func Doc(st State) []string {
	if st.Loading {
		return LoadingDoc(st.Frame)
	}
	if st.Summary != nil {
		return DocSummary(st.Summary, st.Width)
	}
	doc := Group("GEMINI MODELS", "Gemini Flash, Gemini Pro", 100, 50,
		"\x1b[32m", "Refreshes in "+refreshText(st.RefreshG, frozenRefreshG))
	doc = append(doc, "", "")
	doc = append(doc, Group("CLAUDE AND GPT MODELS", "Claude Opus, Claude Sonnet, GPT-OSS", 0, 50,
		"\x1b[31m", "Refreshes in "+refreshText(st.RefreshO, frozenRefreshO))...)
	doc = append(doc, "", "")
	for _, l := range wrapWords(NoteText, WrapWidth(st.Width)) {
		doc = append(doc, "  "+palette.SeparatorOpen+"│"+palette.DimGrayOpen+l+"\x1b[39m")
	}
	return doc
}

// refreshText is the countdown for one bucket, or the frozen value when
// the bucket carries no time.
func refreshText(t time.Time, frozen string) string {
	if t.IsZero() {
		return frozen
	}
	return QuotaCountdown(t)
}

// Spec is the document the viewer's pager scrolls: the document's lines,
// how many of them are visible at once, and the document's own line
// count.
func Spec(st State) tuiviewer.Spec {
	doc := Doc(st)
	return tuiviewer.Spec{Lines: doc, Visible: Page(st.Height), Total: len(doc)}
}

// Lines is the whole /usage panel: the title, the three chrome rows,
// the window over the document, the counter and the legend.
//
// The second return value is the clamped scroll offset: a document that
// shrank leaves the window nearer its end than the offset asked for.
func Lines(st State, window int) ([]string, int) {
	doc := Doc(st)
	total := len(doc)
	page := Page(st.Height)
	win := tuiviewport.ScrollWindow(total, window, page)
	lines := []string{Title()}
	lines = append(lines, Chrome(st.Email)...)
	lines = append(lines, win.SliceRows(doc)...)
	if page < total {
		lines = append(lines, tuiviewer.Counter(win.Start()+1, win.End(), total))
	}
	lines = append(lines, "",
		tuilib.PanelDotHint("  ", "↑/↓", "Scroll", "pgup/pgdown", "Page",
			"ctrl+end", "Bottom", "ctrl+home", "Top", "esc", "Close"))
	return lines, win.Start()
}
