// Package remotecontrol — daemon_session.go: the SessionState
// struct + its lock-protected mutators (SetTurnOutput,
// AppendTurnOutput, SetConversationID, SetModel, SetBackend) +
// the ChunkSink callback plumbing (SetChunkSink / ChunkSink /
// AppendTurnOutput's per-chunk invocation) + the read-only
// snapshot helpers (TurnOutputSnapshot, TurnOutputSnapshotHostConv,
// HasTurnOutput, Connected) + the JSON statusSnapshot
// renderer. Pure file split out of daemon.go; the package doc
// at the top of daemon.go explains the daemon.go /
// daemon_session.go / daemon_meta.go / daemon_config.go split.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// MaxTurnOutputBytes bounds TurnOutput. The clone's chunks are
// already line-buffered so this is mostly a safety belt; 1 MiB is
// generous for a single-turn reply.
const MaxTurnOutputBytes = 1 << 20

// SessionState is the snapshot of the host binary's state passed in
// at Start time. Every field is read-only after construction; the
// daemon copies them out under the snapshot's RWMutex so a slow
// caller cannot race with the host's writers.
//
// Empty zero values mean "unknown" — the daemon surfaces them as
// empty strings or zeros in the status tool output rather than
// panicking. This keeps the daemon runnable when the host has not
// yet populated a field (e.g. a print-mode run that exits before
// the model turn completes).
type SessionState struct {
	mu sync.RWMutex

	// PID is the host process ID. Always set (the daemon reads
	// os.Getpid itself if Snapshot is nil); kept here so a test
	// can pin it.
	PID int
	// Version is the CLI version string the host advertised
	// (the v1.3.4 binary prints exactly "1.3.4\n" — we mirror).
	Version string
	// Backend is "gateway", "shim" or "" when the host hasn't decided
	// (e.g. pre-flag-parse). Mirrors --backend / $OPENAGY_BACKEND.
	Backend string
	// Model is the resolved model label or id. Empty for
	// non-print mode or before --model parses.
	Model string
	// Workspace is the cwd at daemon Start time. The clone
	// doesn't follow chdir() during a turn; pinning this here
	// means a dashboard sees the same path the host printed.
	Workspace string
	// ConversationID is the conversation the host is currently
	// running (or "" if the host is between turns). get_output
	// keys on this when the caller does not pass one.
	ConversationID string
	// TurnOutput is the most recent model reply, accumulated by
	// the host's chunk handler. Truncated to MaxTurnOutputBytes
	// to keep the daemon's memory bounded.
	TurnOutput string
	// StartedAt is when the daemon accepted its first connection;
	// used as the uptime baseline in the status tool.
	StartedAt time.Time
	// Connected is the number of dashboard clients currently
	// attached; updated by the accept loop. Atomic so the status
	// tool can read it without grabbing the mutex.
	//
	// The accept loop touches this through
	// Dispatcher.NotifyConnected / NotifyDisconnected — both
	// transports route through the same counter so a dashboard
	// sees a single connected count regardless of which
	// transport a client dialed.
	connected atomic.Int64

	// chunkSink is the optional callback SessionState fires on
	// every AppendTurnOutput. Cycle rc-streaming-protocol wires
	// this in so a future turn-text-emitter can stream model
	// chunks to connected dashboards via dispatcher.Notify
	// without the daemon package importing the WebSocket
	// transport. nil is a no-op (the same behaviour the print
	// path had before this cycle).
	//
	// Captured under the mu write lock so the AppendTurnOutput
	// call site doesn't race with SetChunkSink. Invoked outside
	// the lock so a slow callback can't block the next
	// AppendTurnOutput call (the print path is hot).
	chunkSink ChunkSink
}

// NewSessionState returns an empty SessionState with PID, Version
// and StartedAt pre-populated. Version may be empty.
func NewSessionState(version string) *SessionState {
	s := &SessionState{
		PID:       os.Getpid(),
		Version:   version,
		StartedAt: time.Now(),
	}
	return s
}

// SetTurnOutput updates the most-recent-turn output buffer. Callers
// should call this from the chunk handler that already feeds the
// print path; the daemon then exposes it through get_output. The
// stored value is truncated to MaxTurnOutputBytes.
func (s *SessionState) SetTurnOutput(out string) {
	if s == nil {
		return
	}
	if len(out) > MaxTurnOutputBytes {
		out = out[len(out)-MaxTurnOutputBytes:]
	}
	s.mu.Lock()
	s.TurnOutput = out
	s.mu.Unlock()
}

// AppendTurnOutput concatenates the given text onto the existing
// turn output. Used by the host's chunk handler, which sees the
// streamed reply one chunk at a time and wants to mirror each
// piece into the daemon's session without re-marshalling the
// whole buffer. The result is truncated to MaxTurnOutputBytes.
//
// Cycle rc-streaming-protocol: if a ChunkSink was registered
// (SetChunkSink below), AppendTurnOutput invokes it with the
// freshly-appended text. The sink is captured under the write
// lock and invoked outside the lock so a slow callback does
// not block subsequent chunk writes (the print path is hot).
func (s *SessionState) AppendTurnOutput(text string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.TurnOutput = s.TurnOutput + text
	if len(s.TurnOutput) > MaxTurnOutputBytes {
		s.TurnOutput = s.TurnOutput[len(s.TurnOutput)-MaxTurnOutputBytes:]
	}
	sink := s.chunkSink
	s.mu.Unlock()
	if sink != nil {
		// Errors from the sink are dropped: the daemon treats
		// server-push as best-effort. A future cycle ("rc-
		// streaming-protocol-buffered") will surface them via
		// the dispatcher's notifier so the dashboard knows the
		// chunk wasn't delivered.
		_ = s.fireChunkSink(sink, text)
	}
}

// fireChunkSink invokes the per-chunk callback. Split out of
// AppendTurnOutput so the hot-path lock/release pair stays
// readable; the callback body is the cycle's only place where
// the sink's signature is asserted.
func (s *SessionState) fireChunkSink(sink ChunkSink, text string) error {
	return sink("turn/chunk", map[string]any{"text": text})
}

// ChunkSink is the optional callback SessionState fires on every
// AppendTurnOutput. The signature mirrors what
// dispatcher.Notify needs: (method, params) → error. The host
// code wires this up in main.go to bridge the print-path chunk
// handler to dispatcher.Notify:
//
//	rcSess.SetChunkSink(func(method string, p any) error {
//	    return rcDisp.Notify(ctx, connID, method, p)
//	})
//
// (or, for the broadcast case, an internal helper that iterates
// every open ws connection — that's the path rc-cascade-stream
// will take, not this cycle).
//
// ChunkSink is safe to invoke from any goroutine; the daemon
// invokes it synchronously from inside AppendTurnOutput, which
// is itself called from the print path's chunk handler.
type ChunkSink func(method string, params any) error

// SetChunkSink registers (or clears, when sink is nil) the
// callback AppendTurnOutput invokes per chunk. Pass nil to
// disable. Safe to call concurrently with AppendTurnOutput;
// the next AppendTurnOutput call uses the most-recently-set
// sink.
func (s *SessionState) SetChunkSink(sink ChunkSink) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.chunkSink = sink
	s.mu.Unlock()
}

// ChunkSink returns the currently-registered sink, or nil if
// none was set. Exposed so tests can assert the wiring without
// poking at the SessionState's internals.
func (s *SessionState) ChunkSink() ChunkSink {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chunkSink
}

// TurnOutputSnapshot returns the current accumulated turn output.
// Read-only; useful for callers that want to inspect the buffer
// without copying it under a lock (the dashboard tools always
// read the field through statusSnapshot, which copies the value
// out under the lock).
func (s *SessionState) TurnOutputSnapshot() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TurnOutput
}

// TurnOutputSnapshotHostConv returns the host's currently-running
// conversation id. A separate getter so the daemon can compare it
// against the caller's requested id without holding the lock.
func (s *SessionState) TurnOutputSnapshotHostConv() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ConversationID
}

// HasTurnOutput reports whether the host has pushed any turn text
// since the daemon started. A "yes" answer is what flips the
// get_output tool into the in-memory snapshot path; a "no"
// falls through to the OutputReader (which may itself return
// ErrNotFound if the convstore doesn't carry that conversation).
func (s *SessionState) HasTurnOutput() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TurnOutput != ""
}

// SetConversationID records the conversation the host is working on
// (or "" to clear). Called by the host's resume/conversation code.
func (s *SessionState) SetConversationID(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.ConversationID = id
	s.mu.Unlock()
}

// SetModel records the resolved model label so the status tool can
// report which model the host is currently routed to.
func (s *SessionState) SetModel(model string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.Model = model
	s.mu.Unlock()
}

// SetBackend records the resolved backend ("gateway" / "shim") so the
// status tool can report what the host is actually talking to.
// Mirrors SetModel — same locking discipline, same no-op-on-nil.
func (s *SessionState) SetBackend(backend string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.Backend = backend
	s.mu.Unlock()
}

// Connected returns the number of dashboard clients currently
// attached to the daemon. Updated by the accept loop (Unix socket
// or WebSocket — both call into the same Dispatcher counter).
func (s *SessionState) Connected() int64 {
	if s == nil {
		return 0
	}
	return s.connected.Load()
}

// statusSnapshot returns the JSON-able view the status tool emits.
// Reading is racy with writers (SetTurnOutput, SetConversationID,
// …); the daemon tolerates a torn read because the wire format is
// best-effort and dashboards re-poll.
//
// Cycle rc-status-tool: the snapshot returns the rich shape the
// brief pins. The shape is built by statusRichShape below; this
// method is kept as a thin shim so callers that want the
// SessionState-only view (tests in cycle 1) can keep working
// until they migrate.
func (s *SessionState) statusSnapshot() map[string]any {
	if s == nil {
		return map[string]any{}
	}
	pid, version, backend, model, workspace, conversationID, startedAt, connected := s.snapshotFields()
	return map[string]any{
		"pid":             pid,
		"version":         version,
		"backend":         backend,
		"model":           model,
		"workspace":       workspace,
		"conversation_id": conversationID,
		"started_at":      startedAt.UTC().Format(time.RFC3339Nano),
		"connected":       connected,
		"uptime_seconds":  int(time.Since(startedAt).Seconds()),
	}
}

// snapshotFields takes the read-lock and copies every field the
// status block carries out so statusSnapshot can build the map
// outside the lock. Split out so the helper stays under the
// 50-LOC ceiling this cycle pins.
func (s *SessionState) snapshotFields() (int, string, string, string, string, string, time.Time, int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	startedAt := s.StartedAt
	return s.PID, s.Version, s.Backend, s.Model, s.Workspace, s.ConversationID, startedAt, s.connected.Load()
}
