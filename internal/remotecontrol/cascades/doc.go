// Package cascades is the cascade domain: the unit of work a
// dashboard drives end-to-end.
//
// A cascade is created via StartRemoteAgentCascade, sent user
// messages via SendRemoteAgentCascadeMessage, observed via
// StreamRemoteAgentStateUpdates, and resolved via
// HandleRemoteAgentCascadeUserInteraction (and its cousins in
// later cycles). The remote-control surface is *one ws connection
// may hold zero or more cascades in parallel* — a single dashboard
// can fan out multiple independent sessions.
//
// The domain owns the cascade's own vocabulary and nothing else:
//
//   - types.go   — the Cascade struct, the ClientInfo sidecar, the
//     Opts constructor argument, the ErrorEntry ring entry, the
//     MaxErrors cap.
//   - cascade.go — the struct's immediate getters and simple
//     mutators (Pending, AddInteraction, New, Backend, SetBackend,
//     IsClosed, MarkClosed, ErrorsSnapshot, RecordError,
//     RecordCancel, ConvID) and the mintCascadeID helper.
//   - lifecycle.go — the per-turn cancel / focus / fastpush hooks
//     and the TerminalOutput chan.
//   - turn.go    — the reply-state helpers and the Send /
//     SendStream pipeline.
//   - registry.go — the Registry type and its accessors.
//   - pending.go — the per-cascade registry of in-flight user
//     interactions.
//   - backend.go — the Backend seam: the dispatcher-agnostic
//     interface a turn's backend implements.
//   - stub.go    — the StubBackend fixture.
//   - events.go  — the streaming seam: StateFrame, the phases, the
//     notification names, EventSink, StreamingBackend and the
//     InteractionHostInstaller.
//   - image.go   — the pictures a user attached to a message.
//
// The domain is stdlib-only and knows nothing about the
// dispatcher, the transport or the dashboard. What it needs from
// the outside it asks for through a narrow seam: the Backend
// interface, the EventSink interface and the
// InteractionHostInstaller capability.
//
// This file is the per-cycle history the package accumulated
// across rc-cascade-start / rc-cascade-message / rc-cascade-stream
// / rc-handle-user-interaction / rc-status-tool /
// rc-cancel-cascade / rc-deep-link / rc-fastpush-toggle. Pure
// documentation.
package cascades

// ensureCascadeDocKept is a no-op marker so Go accepts the
// file as a valid source file. The file is a pure documentation
// companion to cascade.go (which carries the actual package
// declaration + types + simple getters).
var _ = ensureCascadeDocKept

func ensureCascadeDocKept() {}
