// Package remotecontrol — daemon_meta.go: the DaemonMeta
// struct (the `daemon` block the rich-status tool emits) +
// the AgentLister / AgentInfo / StaticAgentLister /
// defaultAgentLister / DefaultAgentLister types + the
// OutputReader / DefaultOutputReader / ErrNotFound contract.
// Pure file split out of daemon.go; the daemon_meta.go
// comments explain the per-cycle history of each field.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"errors"
)

// DaemonMeta is the small block of "what transport / what
// lifecycle mode is this daemon running under" fields the
// rich-status tool emits. Populated at Start time by
// cmd/wagyu/remote_control.go (per-turn path:
// Mode="in-process") and remote_control_foreground.go
// (LaunchAgent path: Mode="persistent"). Surfaced as the
// `daemon` block on the wire.
//
// The fields match what an operator scanning a dashboard
// needs to know: which transport carried the JSON-RPC
// frames ("tcp+ws" / "unix") and where to point a new
// client at this daemon. Transport and Addr are
// intentionally not derivable from the SessionState —
// they're populated by the start-time wiring so a long-
// running daemon reflects the bind it actually used (a
// port=0 bind ends up with an OS-assigned port in Addr).
//
// Concurrency: written once at Start time, read often
// (every `status` poll). The session state is mutated
// throughout the daemon's lifetime; the DaemonMeta is
// fixed. We mirror it on the Dispatcher.Config rather
// than the SessionState for that reason — a status
// snapshot is a values copy under the lock, and reading
// the daemon meta is a values copy from a pre-populated
// Config field.
type DaemonMeta struct {
	// Transport is "tcp+ws" (the WS adapter on a TCP
	// socket — the LaunchAgent mode and the
	// --remote-control-tcp per-turn mode) or "unix"
	// (the --remote-control per-turn mode). A future
	// transport would add its own constant; the wire
	// envelope treats unknown values as opaque
	// strings.
	Transport string
	// Addr is the bind address the dispatcher
	// listens on (e.g. "0.0.0.0:8787" for tcp+ws, or
	// "/tmp/foo.sock" for unix). For TCP bindings
	// with port=0 the OS-assigned port is what's
	// recorded.
	Addr string
	// Mode is "persistent" (the LaunchAgent daemon
	// process; the addr is a stable LAN-bind) or
	// "in-process" (the per-turn CLI daemon; the
	// addr is whatever the host picked at
	// startup).
	Mode string
}

// Snapshot returns a value-type copy of the DaemonMeta.
// Nil-safe (a nil *DaemonMeta returns the zero value
// rather than panicking).
func (d *DaemonMeta) Snapshot() DaemonMeta {
	if d == nil {
		return DaemonMeta{}
	}
	return *d
}

// AgentLister is the subset of agent discovery the daemon needs.
// Both the production wiring and tests satisfy this interface.
//
//   - In the host binary the production implementation reads the
//     built-in agent list (default + named) and the user's custom
//     agents from ~/.gemini/config/agents.json. The clone does not
//     implement custom-agent loading yet, so the production impl
//     returns the same three names (default, Default, explorer) for
//     parity with `--agent` flag validation.
//
//   - In tests AgentLister is a hand-rolled slice.
type AgentLister interface {
	ListAgents() []AgentInfo
}

// AgentInfo is one row of the list_agents tool's output. Name is
// the canonical id; Label is the human-readable form (e.g. for
// `--agent NAME` validation messages and the `/model` picker).
type AgentInfo struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// StaticAgentLister returns an AgentLister that emits a fixed slice.
// Used by tests; the production wiring uses defaultAgentLister.
type StaticAgentLister struct{ Agents []AgentInfo }

// ListAgents returns the static slice verbatim.
func (s StaticAgentLister) ListAgents() []AgentInfo { return s.Agents }

// defaultAgentLister is the built-in AgentLister. It mirrors the
// names cmd/wagyu/main.go's knownAgent() accepts and adds the
// recovered "explorer" / "researcher" variants observed in the
// v1.3.4 binary's agent directory listing. The label format matches
// the recovered "Gemini 3.8 Flash (Medium)" style.
type defaultAgentLister struct{}

// DefaultAgentLister is the singleton production AgentLister.
var DefaultAgentLister AgentLister = defaultAgentLister{}

// ListAgents returns the built-in agent roster. Mirrors the names
// cmd/wagyu/main.go's knownAgent() accepts.
func (defaultAgentLister) ListAgents() []AgentInfo {
	return []AgentInfo{
		{Name: "default", Label: "Default"},
		{Name: "Default", Label: "Default"},
	}
}

// OutputReader is the subset of conversation store access the
// get_output tool needs. Returning "ok / not-found / err" keeps
// the tool honest about why a request failed.
type OutputReader interface {
	// ReadTurn returns the most-recent assistant turn text for the
	// given conversation id, or (nil, ErrNotFound) when no such
	// conversation exists. Other errors are returned verbatim.
	ReadTurn(ctx context.Context, conversationID string) (string, error)
}

// ErrNotFound is returned by OutputReader.ReadTurn when the
// conversation id is unknown. The daemon surfaces it as the MCP
// isError=true response with a clear message.
var ErrNotFound = errors.New("conversation not found")

// DefaultOutputReader is the production OutputReader. It is wired
// in Start to read from internal/convstore; tests substitute a
// stub. The daemon never calls ReadTurn on a nil receiver — the
// production wiring always provides a real reader, and tests
// inject a stub via the Reader field on Config.
type DefaultOutputReader struct{}

// ReadTurn returns "" / ErrNotFound. The production wiring
// replaces this with a convstore-backed reader; the default
// keeps the daemon runnable when Start is called without a
// Reader and the dashboard probes get_output on a stale id.
func (DefaultOutputReader) ReadTurn(ctx context.Context, id string) (string, error) {
	return "", ErrNotFound
}
