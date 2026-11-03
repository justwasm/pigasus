// Package cascades — types.go: the *Cascade struct, the
// *ClientInfo sidecar, the *Opts constructor argument, the
// ErrorEntry ring entry, and the MaxErrors cap. Pure file split;
// the field-level comments document the per-cycle history of each
// field. The methods that operate on these types live in
// cascade.go (simple getters), lifecycle.go (per-turn hooks) and
// turn.go (the SendStream pipeline).
package cascades

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// ClientInfo is the dashboard-supplied self-description on
// StartRemoteAgentCascade. Mirrors the MCP handshake's
// clientInfo (initialize) so a future log-dumper can see which
// dashboard started which cascade. Forward-compatible: the
// rc-handle-user-interaction cycle may add a fresh Metadata field
// here.
type ClientInfo struct {
	Name    string
	Version string
}

// Cascade is one dashboard-driven session. Constructed by
// New from a StartRemoteAgentCascade params object, then
// registered with a *Registry. Survives across multiple
// JSON-RPC calls (Send…Message, Stream…Updates, Handle…Interaction)
// because the registry keys it by ID, not by pointer.
//
// Fields are read by the dispatcher on each cascade-scoped call
// (future cycles) and snapshot at register time. The dashboard's
// clientInfo is captured once at construction and never updated;
// the model label is captured once at construction (the resolved
// choice, not the raw value) and replaced on later cycles that
// change model mid-turn.
//
// Per-cascade lifecycle (this cycle only: Created):
//
//	StartRemoteAgentCascade → Created (lifecycle: "started")
//	rc-cascade-message      → Working, Idle, Working, … (TBD)
//	rc-final-parity-verify  → Closed
//
// We don't model the lifecycle as a field yet — Start just
// registers the Cascade; subsequent cycles extend the struct.
type Cascade struct {
	// mu guards the per-cascade fields that registry.Send
	// mutates on a successful turn (ConversationID, LastReplyAt,
	// Closed) and that the dispatcher's cascade-not-found /
	// cascade-closed paths read. Set in New; never
	// expected to be held across Send (which itself takes mu
	// briefly inside RecordReply / IsClosed).
	mu sync.Mutex

	// dispatchDefault is the dispatcher-installed default
	// backend the lazy Backend() accessor installs when
	// SetBackend has not run. Held under mu (acquired
	// exclusively by Backend) so a racy SetBackend /
	// Backend pair cannot install the default concurrently
	// with a user-supplied backend.
	//
	// Cycle rc-piagent-bind adds this. New can
	// pre-populate it from the dispatcher's Config.MessageBackend.
	dispatchDefault Backend

	// backendExchanged records whether SetBackend has run
	// at least once. The lazy Backend() accessor uses the
	// flag to decide whether installing dispatchDefault
	// would clobber an explicit user choice — it would,
	// because tests can install a *StubBackend at the
	// cascade layer and rely on the constructor-seeded
	// one being replaced rather than overlaid with the
	// dispatcher's *PiagentBackend.
	//
	// Read/write under mu. Cycle rc-piagent-bind adds
	// this.
	backendExchanged bool

	// ID is the opaque cascade identifier the dashboard uses to
	// route every subsequent call back to this cascade. Format:
	// "cas_" + 16 hex chars (8 random bytes from crypto/rand,
	// 64 bits of entropy).
	ID string
	// ConversationID is the convstore conversation id the first
	// SendRemoteAgentCascadeMessage turn produced. Empty until
	// rc-cascade-message runs the first turn.
	ConversationID string
	// CreatedAt is when New ran. Dumped in the
	// StartRemoteAgentCascade response so a dashboard can
	// reason about ordering without a separate "list" call.
	CreatedAt time.Time
	// LastReplyAt is when the last successful Send call landed.
	// Zero until the first turn completes; updated by
	// (*Registry).Send under the per-cascade mu.
	LastReplyAt time.Time
	// LastReply is the latest assistant text. Cycle
	// rc-cascade-stream adds it so the streaming
	// ConnNotifierSink can accumulate mid-turn text via
	// AppendReplyText.
	LastReply string
	// Closed is the "closed sentinel" — when true, the
	// registry's Send returns ErrClosed.
	Closed bool
	// Model is the resolved model label. Empty when the
	// dashboard omitted model.
	Model string
	// BackendLabel is the backend the cascade should route to
	// ("gateway" | "shim"). Empty means "use the host's default".
	BackendLabel string
	// Agent is the agent name (default, explorer, …).
	Agent string
	// ClientInfo is the dashboard-supplied (name, version)
	// from the StartRemoteAgentCascade params.
	ClientInfo ClientInfo
	// Workspace is the workspace directory the cascade should
	// operate in.
	Workspace string
	// Effort is the reasoning effort the dashboard asked for
	// ("low|medium|high|xhigh|max" or "" for host default).
	Effort string
	// Sandbox mirrors --sandbox.
	Sandbox bool
	// Scaffold mirrors --scaffold.
	Scaffold bool
	// SessionMode is the opaque passthrough the dashboard
	// sends verbatim.
	SessionMode string
	// ProjectID is the project id, or "" for "current".
	ProjectID string

	// backend is the Backend installed on this cascade.
	// Nil-able at construction time; New seeds it with
	// a fresh *StubBackend so Backend() never returns nil
	// during the cascade's lifetime.
	backend Backend

	// terminalOutput is the buffered chan the cascade's
	// streaming sink writes text-delta chunks into. Cycle
	// rc-terminals adds this so a terminal subscribed to
	// the cascade can replay the cascade's stream of text
	// as terminal/output notifications. Buffered with
	// capacity 32.
	//
	// Lazy-init via (*Cascade).TerminalOutput().
	terminalOutputOnce sync.Once
	terminalOutput     chan []byte

	// pending is the per-cascade registry of in-flight
	// user-interaction prompts. nil until the first
	// AddInteraction call (lazy allocation — most turns
	// don't surface permission prompts, so a freshly-
	// started cascade carries no allocation overhead).
	//
	// The cascade's mu guards lazy-init of `pending`; the
	// registry has its own mutex for the inner map.
	pending *PendingInteractions

	// errors is the per-cascade bounded error ring surfaced
	// by RemoteAgentErrorStatus. Lazily allocated via
	// RecordError (most cascades never see a turn-level
	// error). MaxErrors is the cap; the oldest
	// entry is dropped when the cap fires.
	//
	// Cycle rc-status-tool adds this field. Lazily
	// allocated; nil until the first RecordError call.
	errors []ErrorEntry

	// cancelMu guards cancel and cancelReason. Held
	// briefly during SendStream setup (StartCancel
	// installs a fresh cancel func) and during Cancel
	// (consume the func). cancelReason is captured at
	// Cancel time so the cancel notification can
	// surface the reason verbatim. The atomic
	// `cancelled` flag is read on the hot path
	// (IsCancelled) without acquiring cancelMu.
	//
	// Cycle rc-cancel-cascade adds this.
	cancelMu sync.Mutex
	cancel   context.CancelFunc
	// cancelReason is the human-readable text Cancel
	// was called with.
	cancelReason string

	// cancelled is the atomic "Cancel has fired" flag
	// flipped by (*Cascade).Cancel. Read by IsCancelled
	// on the hot path; written once per Cancel call.
	cancelled atomic.Bool

	// focused is the atomic "this cascade is the
	// dashboard's currently-active conversation" flag.
	// Set to true by (*Cascade).Focus() when the
	// dispatcher's remoteControlDeepLink handler
	// receives a `focus-cascade` deep-link.
	//
	// Cycle rc-deep-link adds this.
	focused atomic.Bool

	// fastpush is the atomic "this cascade has
	// fast-push enabled" flag. Set to true by
	// (*Cascade).SetFastpush when the dispatcher's
	// SetRemoteAgentFastpushEnabled handler records
	// a successful toggle.
	//
	// Cycle rc-fastpush-toggle adds this.
	fastpush atomic.Bool
}

// Opts is the Start-time wiring for New. Every
// field is optional (New applies defaults: empty model
// stays empty, etc.). The host's main() builds a Opts
// from the resolved CLI flags, but tests construct one directly.
type Opts struct {
	// ID is an explicit cascade id. Empty means "mint one" — the
	// clone's own "cas_<hex>" format. The IDE's Web UI mints the
	// uuid itself and sends it as StartCascade's `cascadeId`, then
	// uses the same uuid as the conversation id; a non-empty ID
	// makes the clone keep the client's id so the follow-up
	// SendUserCascadeMessage finds the cascade.
	ID           string
	ClientInfo   ClientInfo
	ProjectID    string
	Model        string
	Effort       string
	Agent        string
	Sandbox      bool
	Scaffold     bool
	SessionMode  string
	Workspace    string
	BackendLabel string
	// DispatchDefault is the dispatcher-supplied default
	// backend that the cascade's lazy Backend() accessor
	// installs on first Send. Nil keeps the cascade on
	// *StubBackend for its lifetime.
	DispatchDefault Backend
	// Now is injected for tests; production callers leave it
	// zero and New uses time.Now().UTC().
	Now time.Time
}

// ErrorEntry is one entry in the per-cascade bounded error
// ring. The Method is the dispatcher's failing RPC name; the
// Message is the error's text (verbatim); the Code is the
// JSON-RPC envelope code that surfaced (-32603 / -32010 /
// -32006). At is the wall-clock stamp the dispatcher stamped
// when the entry was added.
//
// Cycle rc-status-tool adds this type. Stdlib-only.
type ErrorEntry struct {
	At      time.Time `json:"at"`
	Method  string    `json:"method"`
	Message string    `json:"message"`
	Code    int       `json:"code"`
}

// MaxErrors is the per-cascade error ring cap. 32
// entries × ~120 bytes ≈ 4 KiB per cascade in the worst case
// — well below the 1 MiB dashboard poll interval's expectation.
const MaxErrors = 32
