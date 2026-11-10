package panelregistry

import (
	tea "charm.land/bubbletea/v2"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
)

// Kind identifies which reference panel is open on the host.
type Kind int

const (
	None Kind = iota
	MCP
	Tasks
	Effort
	Hooks
	Permissions
	Agents
	Credits
	CodeSearch
	Artifact
	Feedback
	Rewind
	Context
	Model
	Config
	Plugin
	Skills
	Usage
	Changelog
	Diff
	AddDir
	Rename
	FeatDiff
	Provider
	Accounts
	Projects
)

// ExitNotice reports whether closing the panel displays an exit notice.
func (k Kind) ExitNotice() bool {
	switch k {
	case None, MCP, Permissions, CodeSearch, Feedback,
		Rewind, Context, Plugin, Changelog,
		Usage, AddDir, Rename, FeatDiff, Projects:
		return false
	}
	return true
}

// KeepStatusHint reports whether this panel retains the status hint.
func (k Kind) KeepStatusHint() bool {
	return k == CodeSearch
}

// IsViewer reports whether the panel is a scrollable document viewer.
func (k Kind) IsViewer() bool {
	return k == Usage || k == Changelog || k == FeatDiff
}

// TrailingBlank reports whether the panel's legend row is followed by an
// empty row before the status row.
func (k Kind) TrailingBlank() bool {
	return k != Hooks && k != Context && k != Usage && k != Changelog && k != Diff
}

// OwnsEarlyKeys reports whether the panel intercepts keyboard navigation
// before the generic list handlers.
func (k Kind) OwnsEarlyKeys() bool {
	switch k {
	case Permissions, Hooks, Agents, Provider, Accounts, Plugin, Skills, Effort, Projects:
		return true
	default:
		return false
	}
}

// Handler defines the contract for an individual panel.
type Handler interface {
	Lines() []string
	HandleKey(key tea.Key) (bool, tea.Cmd)
	ViewerSpec() (tuiviewer.Spec, bool)
}

// StandardPanel defines the common interface for standard interactive panels.
type StandardPanel interface {
	Lines() []string
	HandleKey(key tea.Key) (bool, tea.Cmd)
}

// RowCounter is implemented by panels that report their row count for cursor navigation.
type RowCounter interface {
	RowCount() int
}

// FuncHandler adapts functions to the Handler interface.
type FuncHandler struct {
	OnLines      func() []string
	OnHandleKey  func(key tea.Key) (bool, tea.Cmd)
	OnViewerSpec func() (tuiviewer.Spec, bool)
	OnRowCount   func() int
}

func (h FuncHandler) Lines() []string {
	if h.OnLines != nil {
		return h.OnLines()
	}
	return nil
}

func (h FuncHandler) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if h.OnHandleKey != nil {
		return h.OnHandleKey(key)
	}
	return false, nil
}

func (h FuncHandler) ViewerSpec() (tuiviewer.Spec, bool) {
	if h.OnViewerSpec != nil {
		return h.OnViewerSpec()
	}
	return tuiviewer.Spec{}, false
}

func (h FuncHandler) RowCount() int {
	if h.OnRowCount != nil {
		return h.OnRowCount()
	}
	return 0
}

// FromPanel adapts any StandardPanel to the Handler interface.
func FromPanel(p StandardPanel) Handler {
	if p == nil {
		return nil
	}
	fh := FuncHandler{
		OnLines:     p.Lines,
		OnHandleKey: p.HandleKey,
	}
	if rc, ok := p.(RowCounter); ok {
		fh.OnRowCount = rc.RowCount
	}
	return fh
}

// Seams are the host's answers the registry cannot give itself.
//
// Every one of them is optional: a registry without a seam behaves as if
// the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running host.
type Seams struct {
	// Close leaves the panel. The list rule's Esc goes through it.
	Close func() tea.Cmd

	// ViewerKey hands a key to the document viewer panel. The panel
	// owns the viewport's scroll state; the host owns the shared window
	// offset, so the offset goes in and the clamped one comes back.
	ViewerKey func(key tea.Key) (handled bool, cmd tea.Cmd)
}

// Wiring is the host's facts the registry cannot answer itself.
//
// The registry owns the fan-out over the panels; the host owns the
// facts: how many rows each panel has, which panel claims a key it
// does not own, and how a panel is left.
type Wiring struct {
	// Rows maps a kind onto the panel that counts its rows. The count
	// is the cursor bound of a vertical list panel.
	Rows map[Kind]func() int

	// Claims maps a kind onto a panel that answers whether it claimed
	// the key it was given. A panel that owns only *some* of the keys
	// it receives — the /codesearch panel, whose result list owns the
	// navigation while the host's list rule owns the rest — registers
	// its handler here, so the registry can tell "the panel did not own
	// this key" from "the panel swallowed it".
	Claims map[Kind]func(key tea.Key) (handled bool, cmd tea.Cmd, claimed bool)

	// Seams are the host's answers.
	Seams Seams
}

// Registry maps Kind to Handler instances without importing the host.
type Registry struct {
	handlers map[Kind]Handler
	rows     map[Kind]func() int
	claims   map[Kind]func(key tea.Key) (handled bool, cmd tea.Cmd, claimed bool)
	seams    Seams
	wired    bool
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{
		handlers: make(map[Kind]Handler),
	}
}

// Register adds or replaces a handler for Kind.
func (r *Registry) Register(k Kind, h Handler) {
	if r.handlers == nil {
		r.handlers = make(map[Kind]Handler)
	}
	r.handlers[k] = h
}

// Get returns the registered handler for Kind, if any.
func (r *Registry) Get(k Kind) (Handler, bool) {
	if r == nil || r.handlers == nil {
		return nil, false
	}
	h, ok := r.handlers[k]
	return h, ok
}

// Wire attaches the host's facts to the registry.
//
// It replaces every fact it is given, so wiring the same registry twice
// is harmless.
func (r *Registry) Wire(w Wiring) {
	if r == nil {
		return
	}
	r.rows = w.Rows
	r.claims = w.Claims
	r.seams = w.Seams
	r.wired = true
}

// Wired reports whether the registry has been wired with the host's
// facts.
func (r *Registry) Wired() bool {
	return r != nil && r.wired
}

// Lines renders the lines for Kind via its registered handler.
func (r *Registry) Lines(k Kind) []string {
	if r == nil || r.handlers == nil {
		return nil
	}
	if h, ok := r.handlers[k]; ok {
		return h.Lines()
	}
	return nil
}

// RowCount returns the number of rows the kind's panel has.
//
// The count is the cursor bound of a vertical list panel: 0..count-1. It
// is the panel's own fact, so the registry asks the panel that owns the
// rows. A kind with no rows counts zero.
//
// Effort is special: it is a horizontal slider (←/→ keys), not a row
// picker, but it still wants the cursor clamped against its three
// levels. The three viewer-only panels (/usage, /changelog,
// /feature-diff) scroll the body with ↑/↓ and never enter the table.
//
// The count is not a constant for most of the panels: the /model panel's
// rows are the reference's five static rows plus the models the user
// configured, and they follow the search filter, so the count shrinks
// with the query. A hard-coded count made every row past the sixth
// unreachable.
func (r *Registry) RowCount(k Kind) int {
	if r == nil {
		return 0
	}
	if r.rows != nil {
		if fn, ok := r.rows[k]; ok && fn != nil {
			return fn()
		}
	}
	if h, ok := r.handlers[k]; ok && h != nil {
		if rc, ok := h.(RowCounter); ok {
			return rc.RowCount()
		}
	}
	return 0
}

// HandleKey routes one key to the panel that owns it.
//
// It answers (handled, cmd, claimed): "claimed" says the key belongs to
// the kind's route, whether or not the panel acted on it. A key no
// route claims reaches the host's own handlers.
func (r *Registry) HandleKey(k Kind, key tea.Key) (bool, tea.Cmd, bool) {
	switch k.Route() {
	case RoutePanel:
		h, c := r.panelKey(k, key)
		return h, c, true
	case RouteSearch:
		h, c, claimed := r.claimKey(k, key)
		if claimed {
			return h, c, true
		}
		// The panel did not own the key: the list rule takes it.
		return r.listKey(key)
	case RouteList:
		return r.listKey(key)
	case RouteViewer:
		h, c := r.viewerKey(key)
		return h, c, true
	}
	return false, nil, false
}

// panelKey hands a key to the kind's own panel.
func (r *Registry) panelKey(k Kind, key tea.Key) (bool, tea.Cmd) {
	if r == nil || r.handlers == nil {
		return false, nil
	}
	if h, ok := r.handlers[k]; ok {
		return h.HandleKey(key)
	}
	return false, nil
}

// claimKey hands a key to the panel that answers whether it claimed it.
func (r *Registry) claimKey(k Kind, key tea.Key) (bool, tea.Cmd, bool) {
	if r == nil || r.claims == nil {
		return false, nil, false
	}
	if fn, ok := r.claims[k]; ok && fn != nil {
		return fn(key)
	}
	return false, nil, false
}

// listKey is the list rule: every key is swallowed, except Ctrl+C — the
// global "leave" key — and Esc closes the panel.
func (r *Registry) listKey(key tea.Key) (bool, tea.Cmd, bool) {
	if key.Code == tea.KeyEsc {
		return true, r.close(), true
	}
	if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
		return false, nil, true
	}
	return true, nil, true
}

// viewerKey hands a key to the document viewer panel.
func (r *Registry) viewerKey(key tea.Key) (bool, tea.Cmd) {
	if r == nil || r.seams.ViewerKey == nil {
		return false, nil
	}
	return r.seams.ViewerKey(key)
}

// close leaves the panel.
func (r *Registry) close() tea.Cmd {
	if r == nil || r.seams.Close == nil {
		return nil
	}
	return r.seams.Close()
}

// ViewerSpec retrieves the viewer specification for Kind if registered.
func (r *Registry) ViewerSpec(k Kind) (tuiviewer.Spec, bool) {
	if r == nil || r.handlers == nil {
		return tuiviewer.Spec{}, false
	}
	if h, ok := r.handlers[k]; ok {
		return h.ViewerSpec()
	}
	return tuiviewer.Spec{}, false
}
