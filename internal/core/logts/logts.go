// Package logts renders the timestamp suffix of the CLI's log file name.
//
// The suffix is a fact about the CLI's own log file, not about the view
// that shows the path: the /help overlay prints the file's path and the
// status line's error card points at the file it names. Two surfaces that
// have nothing to do with each other need the same string, so it cannot
// live in either one of them — the view that renders it would have to
// import a host to ask.
//
// The timestamp therefore lives in tier 1, next to the other facts the
// whole tree shares: `internal/ui/tuilib` renders the /help overlay's
// rows, and `internal/cliapp` collects the status line's payload — the
// error card itself is drawn by `internal/ui/views/statusline` — and both
// read the suffix from here.
package logts

import "time"

// FormatLogTS is the timestamp suffix of the CLI's log file name.
//
// The shape is `YYYYMMDD_HHMMSS` — the reference's own log file names
// carry a suffix of that form.
func FormatLogTS() string {
	return time.Now().Format("20060102_150405")
}
