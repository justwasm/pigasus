// tabs.go — the /debug surface's views.
//
// # Why /debug
//
// The command began as /logs, showing the session's log file. It then grew
// the views that answer the questions the log file provokes — what will the
// next turn put on the wire, what has this session done, what did it send —
// and "logs" stopped describing it: the log file is one of five views, and
// the surface is the session's diagnostic surface as a whole.
//
// So the command is /debug and /logs is an alias. A user's fingers and every
// document in the tree still reach for /logs, and neither should break.
//
// /context stays the reference's own single view. It is a usage breakdown
// and the reference draws it inline; giving it tabs made it the only panel
// in the tree whose tab bar was hand-rolled, which is the inconsistency this
// move removes. The Context *view* here is a different question — measured
// occupancy rather than modelled breakdown — see contextview.go.
package debugpanel

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// View identifies which of the surface's views is up.
type View int

const (
	// ViewSummary is the at-a-glance dashboard: model, endpoint, context
	// occupancy, exchange health, and recent errors.
	ViewSummary View = iota

	// ViewLogs is the session log file: what the command showed when it was
	// called /logs.
	ViewLogs

	// ViewWire is the request the next turn puts on the wire.
	ViewWire

	// ViewTrajectory is the conversation's steps.
	ViewTrajectory

	// ViewRequests is the HTTP exchanges this session made.
	ViewRequests

	// ViewContext is how big the context really is and how full the window
	// really is, measured rather than modelled.
	ViewContext
)

// viewNames are the bar's labels, in bar order.
var viewNames = []string{"Summary", "Logs", "Wire", "Trajectory", "Requests", "Context"}

// ViewCount is how many views the surface has.
func ViewCount() int { return len(viewNames) }

// Next returns the view after v, wrapping.
func (v View) Next() View { return View((int(v) + 1) % len(viewNames)) }

// Prev returns the view before v, wrapping.
func (v View) Prev() View { return View((int(v) + len(viewNames) - 1) % len(viewNames)) }

// Name returns the view's label.
func (v View) Name() string {
	if int(v) < 0 || int(v) >= len(viewNames) {
		return viewNames[0]
	}
	return viewNames[int(v)]
}

// ViewByName resolves a view name as the user types it after `/debug`
// ("wire", "traj"), ignoring case.
//
// The second value is false for a word that names no view, so the host
// can leave the panel on its default view rather than guessing.
//
// # Why the words do not overlap
//
// The Wire view used to answer to "request" and "req", which was accurate
// while it was the only view about a request. Once the Requests view
// existed, that made `/debug request` and `/debug requests` land on different
// views — a one-letter difference with no way to tell from the command
// which one you got. So the words are now disjoint: Wire answers to what it
// *is* (the next turn's payload), Requests to every spelling of "request",
// and Context to what it measures. TestViewWordsAreDisjoint fails if that
// stops being true.
func ViewByName(name string) (View, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "summary", "sum", "overview", "dashboard", "all":
		return ViewSummary, true
	case "logs", "log", "file":
		return ViewLogs, true
	case "wire", "payload", "next", "prompt":
		return ViewWire, true
	case "trajectory", "traj", "steps", "history":
		return ViewTrajectory, true
	case "requests", "request", "reqs", "req", "http", "exchanges":
		return ViewRequests, true
	case "context", "ctx", "window", "tokens":
		return ViewContext, true
	}
	return ViewLogs, false
}

// TabBarWidth renders the view bar for a terminal of the given width and
// reports where each label sits.
//
// The bar is tuilib's — the same one /help draws — so this surface's tabs
// look like every other tabbed panel in the tree: the active label is
// bold, underlined and accented, the others are the separator grey, and
// the row ends with the reference's own "(←/→ or tab to cycle)".
//
// tuilib answers the hit regions from the same walk that draws the line,
// so a click cannot land on a label the bar did not paint. The previous
// bar was hand-rolled here and closed the active label with a
// foreground-only reset, which let the bold and the underline bleed into
// the rest of the row.
//
// The width is an argument rather than optional because the terminal is
// the only renderer: the cycle hint is dropped when the labels alone fill
// the row, so the bar never spills past the edge and never loses a label
// to the clip. A width of 0 means "no limit".
//
// A label that a very narrow terminal cuts in half is not reported: a
// click on half a word should not be able to switch to a view whose name
// the user cannot read, so the span is dropped with the columns it lost.
func TabBarWidth(active View, width int) (string, []tuilib.TabSpan) {
	line, spans := tuilib.RenderTabBarTabsSpansWidth(int(active), viewNames, width)
	if width <= 0 {
		return line, spans
	}
	line = ansi.Truncate(line, width, "")
	kept := spans[:0]
	for _, sp := range spans {
		if sp.End <= width {
			kept = append(kept, sp)
		}
	}
	return line, kept
}
