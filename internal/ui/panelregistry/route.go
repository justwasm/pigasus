// route.go — the panel kinds' key routes.
//
// The registry is the one fan-out over the panels: the dispatcher keeps
// the fan-out, the domain keeps the table (invariant 75). The table
// below is the domain's — which route claims a key for which kind.
//
// A route is a *claim*, not a decision: a route that claims a key says
// the key belongs to the panel, whether or not the panel acted on it. A
// key no route claims reaches the host's own handlers.

package panelregistry

// Route tells the registry which route claims a key for a kind.
type Route int

const (
	// RouteNone claims no key: the kind's keys reach the host's own
	// handlers.
	RouteNone Route = iota
	// RoutePanel is the panel's own handler: it owns every key it
	// receives.
	RoutePanel
	// RouteSearch is a panel that owns the keys of its result list and
	// lets the rest through to the list rule.
	RouteSearch
	// RouteList is the list rule: every key is swallowed, except Ctrl+C
	// — the global "leave" key.
	RouteList
	// RouteViewer is the scrollable document viewer.
	RouteViewer
)

// Route tells which route claims a key for the kind.
//
// The four routes are the reference's own:
//
//   - the panel's own handler (/tasks, /rewind, /permissions, /hooks,
//     /agents, /provider, /accounts, /plugin, /skills, /effort,
//     /feedback, /credits, /diff, /add-dir and /rename): the panel owns
//     every key it receives;
//   - the search route (/codesearch): the panel owns the keys of its
//     result list, and the keys it does not own belong to the list rule;
//   - the list rule (/artifact, /context): every key but Ctrl+C;
//   - the document viewer (/usage, /changelog, /feature-diff).
func (k Kind) Route() Route {
	switch {
	case k == CodeSearch:
		// The /codesearch panel owns the keys of its result list; the
		// keys it does not own belong to the list rule.
		return RouteSearch
	case k == Tasks || k == Rewind || k == Feedback || k == Credits ||
		k == Diff || k == AddDir || k == Rename:
		// The panel owns every key it receives.
		return RoutePanel
	case k == Artifact:
		// The /artifact panel owns every key it receives: the picker's
		// cursor, the inline preview, the editor chord and the detail
		// viewer's scroll. It used to route to the list rule, which
		// swallowed ↑/↓ without moving the cursor and swallowed `p` and
		// `enter` without doing anything — a static list that looked
		// navigable.
		return RoutePanel
	case k.OwnsEarlyKeys():
		// The panel intercepts the keyboard navigation before the
		// generic list handlers.
		return RoutePanel
	case k == Debug:
		// The /debug surface owns its keys: the view bar, the fold list
		// and the log's follow toggle are its own vocabulary, and a
		// viewer route would swallow them.
		return RoutePanel
	case k.IsViewer():
		// The scrollable document viewer.
		return RouteViewer
	case k.IsList():
		// The list rule.
		return RouteList
	}
	return RouteNone
}

// IsList reports whether the kind is one of the list panels: /tasks,
// /codesearch, /context.
//
// /tasks and /artifact route to their own panel first (the panel owns the
// cursor, the detail view and the kill), but they stay on the list: the host's
// key pipeline hands a list panel's keys to the panel *before* the global
// handlers see them.
//
// /artifact is deliberately NOT here: it owns the whole keyboard while it is
// open, including the keys the list rule would otherwise swallow, so routing it
// through the list path would hand its keys to two owners.
func (k Kind) IsList() bool {
	switch k {
	case Tasks, CodeSearch, Context:
		return true
	}
	return false
}
