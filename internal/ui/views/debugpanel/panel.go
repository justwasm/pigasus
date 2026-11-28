// Package debugpanel renders the full-viewport, live-updating /debug inspector.
package debugpanel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/clilog"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

const (
	// PollInterval controls how often the open /debug panel checks for new log lines.
	PollInterval = 500 * time.Millisecond

	// MaxTailLines bounds how many trailing log lines are loaded into the viewer.
	MaxTailLines = 2000

	defaultVisibleRows = 20
	headerRows         = 2
	footerRows         = 4 // counter + blank + hint + status line
)

// TickMsg triggers a live refresh of the /debug inspector while it is open.
type TickMsg struct{}

// Tick schedules the next live refresh tick for the /debug panel.
func Tick() tea.Cmd {
	return tea.Tick(PollInterval, func(time.Time) tea.Msg {
		return TickMsg{}
	})
}

// State holds the snapshot rendered by the Logs view.
type State struct {
	Path      string
	Lines     []string
	Follow    bool
	Width     int
	Height    int
	SizeBytes int64
}

// LoadState reads the active (or latest) session log file for display.
func LoadState(width, height int, follow bool) State {
	path := clilog.ResolveReadablePath()
	st := State{
		Path:   path,
		Follow: follow,
		Width:  width,
		Height: height,
	}
	if path == "" {
		return st
	}
	fi, err := os.Stat(path)
	if err != nil {
		return st
	}
	st.SizeBytes = fi.Size()
	st.Lines = tailFor(path, fi)
	return st
}

// tailMemo is the last tail read, keyed by the file's identity.
//
// The panel asks for the log state several times per repaint — the frame,
// the scroll bound and the row count all need it — and each call used to
// tail the file. A wheel notch therefore touched the filesystem a handful
// of times to draw the same 2 000 lines. The size and mtime are the
// key: a file that grew is re-read, one that did not costs a stat.
var tailMemo struct {
	path    string
	size    int64
	modTime time.Time
	lines   []string
	ok      bool
}

// InvalidateTail drops the memoised tail.
//
// The live refresh tick calls this, because appending to a file within the
// same clock tick can leave size and mtime unchanged while the content
// moved. Everything else can rely on the stat.
func InvalidateTail() {
	tailMemo.ok = false
	tailMemo.path = ""
	tailMemo.lines = nil
}

// tailFor returns the file's tail, reusing the memo when the file is the
// same file at the same size and mtime.
func tailFor(path string, fi os.FileInfo) []string {
	if tailMemo.ok && tailMemo.path == path &&
		tailMemo.size == fi.Size() && tailMemo.modTime.Equal(fi.ModTime()) {
		return tailMemo.lines
	}
	lines, err := clilog.Tail(path, MaxTailLines)
	if err != nil {
		return tailMemo.lines
	}
	tailMemo.path = path
	tailMemo.size = fi.Size()
	tailMemo.modTime = fi.ModTime()
	tailMemo.lines = lines
	tailMemo.ok = true
	return lines
}

// VisibleRows returns how many scrollable log rows fit in the terminal height.
func VisibleRows(height int) int {
	if height > 0 {
		vis := height - (headerRows + footerRows)
		if vis < 1 {
			vis = 1
		}
		return vis
	}
	return defaultVisibleRows
}

// Spec builds the viewer Spec for the current log state.
func Spec(st State) tuiviewer.Spec {
	doc := Document(st)
	return tuiviewer.Spec{
		Lines:   doc,
		Visible: VisibleRows(st.Height),
		Total:   len(doc),
	}
}

// Document wraps and styles the loaded log lines for the viewer width.
func Document(st State) []string {
	if st.Path == "" {
		return []string{
			palette.DimGrayOpen + "  No session log file yet (dir: " + clilog.Dir() + ")" + palette.DimGrayClose,
		}
	}
	if len(st.Lines) == 0 {
		return []string{
			palette.DimGrayOpen + "  (empty — waiting for routing, retry, or error events...)" + palette.DimGrayClose,
		}
	}

	budget := st.Width - 4
	if budget < 24 {
		budget = 76
	}
	out := make([]string, 0, len(st.Lines))
	for _, raw := range st.Lines {
		wrapped := wrapPlainLine(raw, budget)
		for i, seg := range wrapped {
			prefix := "  "
			if i > 0 {
				prefix = "    "
			}
			out = append(out, prefix+styleLogSegment(raw, seg))
		}
	}
	return out
}

// HeaderLines renders the log view's pinned header: the badge with the
// follow state, and the file row.
//
// It is exported so the full-viewport surface's Logs view and the
// inline Logs frame draw the same two rows from one place. Two copies
// would drift the first time a badge changed, and the same panel would
// then describe itself differently depending on which frame drew it.
func HeaderLines(st State) []string {
	badge := "  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Pigasus Session Logs \x1b[0m"
	followTag := "  \x1b[38;2;250;204;21m[PAUSED]\x1b[39m"
	if st.Follow {
		followTag = "  \x1b[38;2;74;222;128m[LIVE FOLLOW]\x1b[39m"
	}
	meta := ""
	if st.Path != "" {
		meta = palette.DimGrayOpen + fmt.Sprintf("  %s · %d lines", formatBytes(st.SizeBytes), len(st.Lines)) + palette.DimGrayClose
	}
	titleRow := badge + followTag + meta

	fileLabel := st.Path
	if fileLabel == "" {
		fileLabel = clilog.Dir() + " (pending)"
	} else if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(fileLabel, home+string(filepath.Separator)) {
		fileLabel = "~" + fileLabel[len(home):]
	}
	fileRow := palette.DimGrayOpen + formatFileRow(fileLabel, st.Width) + palette.DimGrayClose
	if st.Width > 8 {
		titleRow = ansi.Truncate(titleRow, st.Width, "…")
	}
	return []string{titleRow, fileRow}
}

// PanelLines renders the full Logs view's frame: pinned header, scrollable window,
// line counter, and keybinding hint.
func PanelLines(st State, rows []string, start, end, total int) []string {
	lines := make([]string, 0, headerRows+len(rows)+footerRows)
	lines = append(lines, HeaderLines(st)...)
	lines = append(lines, rows...)

	if st.Height > 0 {
		vis := VisibleRows(st.Height)
		for i := len(rows); i < vis; i++ {
			lines = append(lines, "")
		}
	}

	counter := tuiviewer.Counter(start, end, total)
	if st.Follow {
		counter += palette.DimGrayOpen + " · auto-tail ON" + palette.DimGrayClose
	} else {
		counter += palette.DimGrayOpen + " · auto-tail PAUSED (press f or G to resume)" + palette.DimGrayClose
	}
	hint := tuilib.PanelDotHint("  ", "↑/↓", "Scroll", "f", "Follow", "g/G", "Top/End", "esc", "Close")
	lines = append(lines, counter, "", hint)
	return lines
}

func wrapPlainLine(s string, width int) []string {
	if width <= 0 || ansi.StringWidth(s) <= width {
		return []string{s}
	}
	wrapped := ansi.Wordwrap(s, width, "")
	parts := strings.Split(wrapped, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if ansi.StringWidth(p) <= width {
			out = append(out, p)
			continue
		}
		// Hard-wrap unbroken tokens (e.g. long URLs) so lines never overflow.
		runes := []rune(p)
		for len(runes) > 0 {
			cut := len(runes)
			for cut > 1 && ansi.StringWidth(string(runes[:cut])) > width {
				cut--
			}
			out = append(out, string(runes[:cut]))
			runes = runes[cut:]
		}
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// styleLogSegment styles one wrapped segment of a log line by what the
// line says.
//
// The three loud cases (failure, retry, success) are the reference's own
// recovered colours and do not follow the scheme. The ordinary case is a
// body line, and a body line wears the scheme's foreground — the slot
// mdrender's document body already reads (resolveColorSchemeColors), which
// is the empty string under the terminal scheme, so the body inherits the
// terminal's own colour there and follows /config everywhere else.
//
// It used to hard-code #e8eaed here, which is the *dark* scheme's
// foreground. That is right for exactly one of the eight schemes; under
// any light one it drew near-white text on a light terminal, and it
// ignored SetScheme entirely. The wire and trajectory bodies and the
// reader draw their text with no foreground at all, so on any scheme but
// dark the log body was the one surface that disagreed with them.
func styleLogSegment(fullLine, seg string) string {
	low := strings.ToLower(fullLine)
	switch {
	case strings.Contains(low, "err=") ||
		strings.Contains(low, "failed") ||
		strings.Contains(low, "exhausted") ||
		strings.Contains(low, "abort") ||
		strings.Contains(low, "quarantined") ||
		strings.Contains(low, "http 4") ||
		strings.Contains(low, "http 5"):
		return logErrorOpen + seg + logColourClose
	case strings.Contains(low, "retry_in=") ||
		strings.Contains(low, "trying another account") ||
		strings.Contains(low, "switching") ||
		strings.Contains(low, "sandbox mirror"):
		return logRetryOpen + seg + logColourClose
	case strings.Contains(low, "succeeded"):
		return logSuccessOpen + seg + logColourClose
	default:
		return palette.BodyOpen + seg + logColourClose
	}
}

// The log row's colour roles.
//
// Failure, retry and success are semantic and fixed: a line that says a
// request failed reads as a failure under every scheme. The ordinary body
// line is the scheme's foreground, which is what palette.BodyOpen holds.
const (
	logErrorOpen   = "\x1b[38;2;248;113;113m"
	logRetryOpen   = "\x1b[38;2;250;204;21m"
	logSuccessOpen = "\x1b[38;2;74;222;128m"
	logColourClose = "\x1b[39m"
)

func formatFileRow(label string, width int) string {
	prefix := "  File: "
	full := prefix + label
	if width <= 8 || ansi.StringWidth(full) <= width {
		return full
	}
	avail := width - ansi.StringWidth(prefix+"…")
	if avail <= 4 {
		return ansi.Truncate(full, width, "…")
	}
	runes := []rune(label)
	for len(runes) > 1 && ansi.StringWidth(string(runes)) > avail {
		runes = runes[1:]
	}
	return prefix + "…" + string(runes)
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
