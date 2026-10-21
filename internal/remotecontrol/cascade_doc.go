// Package remotecontrol — cascade_doc.go: the per-cycle history
// the cascade package accumulated across rc-cascade-start /
// rc-cascade-message / rc-cascade-stream / rc-handle-user-
// interaction / rc-status-tool / rc-cancel-cascade /
// rc-deep-link / rc-fastpush-toggle. Pure documentation; every
// field / method referenced here lives in cascade.go (types),
// cascade_lifecycle.go (per-turn hooks), or cascade_io.go
// (SendStream pipeline).
//
// The cascade is the unit of work a dashboard drives
// end-to-end: it is created via StartRemoteAgentCascade, sent
// user messages via SendRemoteAgentCascadeMessage, observed via
// StreamRemoteAgentStateUpdates, and resolved via
// HandleRemoteAgentCascadeUserInteraction (and its cousins in
// later cycles). The remote-control surface is *one ws
// connection may hold zero or more cascades in parallel* — a
// single dashboard can fan out multiple independent sessions.
//
// Cycle rc-filesize-internal-remotecontrol splits cascade.go
// into three cohesive files:
//
//   - cascade.go           — types + simple getters
//   - cascade_lifecycle.go — Cancel / Focus / Fastpush /
//     per-turn ctx / record-reply mutation
//   - cascade_io.go        — LastReply / Send / SendStream
//   - per-stream helpers
//   - cascade_doc.go       — this file
//
// Cycle rc-cascade-start ships only the metadata shell: a
// *Cascade holds the dashboard-visible fields (id, conversation
// id, model, backend, agent, clientInfo) and a CascadeRegistry
// hands them out / looks them up.
//
// Cycle rc-cascade-message adds:
//
//   - Closed / LastReplyAt on *Cascade.
//   - a MessageBackend field (default StubBackend) plus the
//     Backend / SetBackend accessors.
//   - (*CascadeRegistry).Send, which looks the cascade up,
//     resolves per-turn overrides, calls into the installed
//     backend with a context-bounded timeout, and updates
//     ConversationID + LastReplyAt on success.
//
// Cycle rc-cascade-stream adds:
//
//   - LastReply on *Cascade — the latest accumulated assistant
//     text.
//   - appendReplyText and setReply accessors (mutex-protected,
//     nil-safe).
//
// Cycle rc-handle-user-interaction adds:
//
//   - pending *PendingInteractions on *Cascade — the per-
//     cascade registry of in-flight permission / question
//     prompts.
//
// Cycle rc-status-tool adds:
//
//   - Errors []CascadeError on *Cascade — a bounded
//     FIFO (default cap MaxCascadeErrors=32) of recent
//     errors the dispatcher's SendRemoteAgentCascadeMessage
//     path dropped into.
//
// Cycle rc-cancel-cascade adds:
//
//   - cancel / cancelMu / cancelled on *Cascade — the per-turn
//     cancel func + atomic "Cancel has fired" flag.
//
// Cycle rc-deep-link adds:
//
//   - focused atomic.Bool on *Cascade — the dashboard's
//     currently-active conversation flag.
//
// Cycle rc-fastpush-toggle adds:
//
//   - fastpush atomic.Bool on *Cascade — the per-cascade
//     fast-push flag.
package remotecontrol

// ensureCascadeDocKept is a no-op marker so Go accepts the
// file as a valid source file. The file is a pure documentation
// companion to cascade.go (which carries the actual package
// declaration + types + simple getters).
var _ = ensureCascadeDocKept

func ensureCascadeDocKept() {}
