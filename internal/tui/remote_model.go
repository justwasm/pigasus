package tui

// remote_model.go — the remote-runtime sub-model.
//
// RemoteModel owns the state that belongs to an attached or remote
// engine: whether the engine is external (ACP/AHP/Driver), the @ picker's
// remote file cache, the last /terminal id, and the in-process Remote
// Control server (its stop function, dispatcher and turn lock).
//
// None of it is turn transport: the engine field still runs the turn. This
// model is the runtime around that engine, so it left the transport bucket.
//
// Upstream: the reference's RootModel keeps its remote session beside the
// engine; the clone gives that runtime a named owner.

import (
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// RemoteModel owns the attached-engine and remote-control runtime.
type RemoteModel struct {
	// attachedEngine marks whether the engine is an external/attached
	// engine (ACP/AHP/Driver).
	attachedEngine bool
	// files backs the @ picker in a --remote-agent session.
	files *remoteFileCache
	// terminalID is the last terminal /terminal created.
	terminalID string
	// controlStop stops the in-process remote-control server if one was
	// started.
	controlStop func()
	// inprocessDisp is the in-process remote control dispatcher if active.
	inprocessDisp *remotecontrol.Dispatcher
	// turnLock arbitrates turn execution between the local TUI and an
	// in-process Remote Control turn.
	turnLock *rchost.SimpleTurnLock
}

// Attached reports whether the engine is an attached/external engine.
func (r *RemoteModel) Attached() bool { return r != nil && r.attachedEngine }

// SetAttached records whether the engine is attached.
func (r *RemoteModel) SetAttached(attached bool) {
	if r != nil {
		r.attachedEngine = attached
	}
}

// SetFiles installs the remote @ picker cache.
func (r *RemoteModel) SetFiles(files *remoteFileCache) {
	if r != nil {
		r.files = files
	}
}

// Files returns the remote @ picker cache.
func (r *RemoteModel) Files() *remoteFileCache {
	if r == nil {
		return nil
	}
	return r.files
}

// TerminalID returns the last terminal /terminal created.
func (r *RemoteModel) TerminalID() string {
	if r == nil {
		return ""
	}
	return r.terminalID
}

// SetTerminalID records the last terminal /terminal created.
func (r *RemoteModel) SetTerminalID(id string) {
	if r != nil {
		r.terminalID = id
	}
}

// SetControl installs the in-process remote-control server.
func (r *RemoteModel) SetControl(stop func(), disp *remotecontrol.Dispatcher) {
	if r == nil {
		return
	}
	r.controlStop = stop
	r.inprocessDisp = disp
}

// StopControl stops the in-process remote-control server and clears it.
func (r *RemoteModel) StopControl() {
	if r == nil || r.controlStop == nil {
		return
	}
	r.controlStop()
	r.controlStop = nil
	r.inprocessDisp = nil
}

// Dispatcher returns the in-process remote-control dispatcher.
func (r *RemoteModel) Dispatcher() *remotecontrol.Dispatcher {
	if r == nil {
		return nil
	}
	return r.inprocessDisp
}

// EnsureTurnLock returns the remote-control turn lock, creating it on
// first use.
func (r *RemoteModel) EnsureTurnLock() *rchost.SimpleTurnLock {
	if r == nil {
		return nil
	}
	if r.turnLock == nil {
		r.turnLock = &rchost.SimpleTurnLock{}
	}
	return r.turnLock
}

// TurnLock returns the remote-control turn lock, or nil.
func (r *RemoteModel) TurnLock() *rchost.SimpleTurnLock {
	if r == nil {
		return nil
	}
	return r.turnLock
}
