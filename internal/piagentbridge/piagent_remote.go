// Package piagentbridge — piagent_remote.go: the production
// `SessionFactory` the remote-control daemon installs on
// every cascade.
//
// Cycle rc-piagent-bind ships the production wiring for
// `PiagentBackend` (internal/remotecontrol/piagentbackend.go).
// The dispatcher constructs a `*PiagentBackend` whose
// `SessionFactory` is a closure; this file is that closure
// for production. The factory mints a fresh
// `*pigrt.Runtime` per `SendStream` call (one runtime per
// turn; the runtime is `Close`d on turn exit) and adapts
// PiG's event callback vocabulary (EventFunc) into the
// cycle's channel-fed `Session` interface.
//
// Why a separate file:
//
//   - The print path's wiring already lives in
//     cmd/wagyu/print_pig.go and uses `pigrt.New` +
//     `rt.RunWith(ctx, prompt, onText, onEvent)` directly.
//     The remote-control surface needs the same model +
//     transport, but the seam `PiagentBackend` exposes is
//     `SessionFactory -> Session`. The adapter below is
//     the bridge.
//
//   - Keeping this factory here (rather than in
//     cmd/wagyu/remote_control.go) means cmd/wagyu has
//     zero direct pigrt imports — the daemon wires up
//     PiagentBackend with a single `SessionFactory: …`
//     closure, and the factory handles every build-time
//     choice (model, agent dir, MCP config path, …).
//
//   - The TUI's runtime wiring in
//     internal/agent/pig.go is unchanged: it goes through
//     its own piagent.Agent (not the cycle's Session
//     surface) and never sees the cycle's bridge. The
//     `--print` and `--remote-control` paths share the
//     underlying `pigrt.Runtime` constructor but route
//     through different seams; this file is the cycle's
//     third seam for the third consumer.
//
// How the channel→callback bridge works:
//
//	PiagentBackend.SendStream creates a buffered chan
//	agent.AgentEvent (cap 128) and starts the session
//	on a goroutine. The goroutine calls sess.Send
//	which is implemented here as a wrapper around
//	pigrt.Runtime.RunWith; the runtime invokes
//	onEvent(ev) for every agent.AgentEvent, which the
//	wrapper forwards into the channel. PiG's emit
//	blocks on a full channel — the cap=128 buffer
//	gives the dispatcher plenty of slack to drain.
//
// Stdlib-only imports + the existing internal/pigrt
// dependency. No new third-party code.
package piagentbridge

import (
	"context"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// RemoteCascadeOptions captures the production wiring the
// `NewRemoteCascadeSession` factory needs. The host
// (cmd/wagyu/remote_control.go) builds this once at
// daemon start and closes over it in the factory closure;
// the per-call `MessagePrompt` arguments layer the
// per-turn knobs on top.
//
// Field semantics:
//
//   - Backend is the gateway (or shim) transport. Cycle
//     rc-piagent-bind passes the same backend
//     `pickBackendWithLog` returns to the print path,
//     so a `StartRemoteAgentCascade` →
//     `SendRemoteAgentCascadeMessage` round-trip sees
//     the same gateway as `--print`.
//
//   - Workspace is the cwd the session is anchored
//     to. Captured at daemon start so a reconnection
//     doesn't drift.
//
//   - AgentDir is the agy config home. Defaults to
//     pigrt.DefaultAgentHome() at factory time when
//     empty.
//
//   - Effort is the host's reasoning-effort level.
//     Empty leaves the model's default.
type RemoteCascadeOptions struct {
	Backend   backend.Client
	Workspace string
	AgentDir  string
	Effort    string
}

// NewRemoteCascadeSession returns the production
// SessionFactory the daemon wires into PiagentBackend.
// The factory:
//
//   - builds a fresh `*pigrt.Runtime` per call (one per
//     `SendStream` invocation; no state is shared across
//     turns);
//   - applies the host-supplied options to the runtime;
//   - on `Session.Send`, calls `Runtime.RunWith(ctx,
//     prompt.Text, onText, onEvent)` where onEvent
//     forwards into the `events` channel PiagentBackend
//     creates;
//   - on `Session.Close`, calls `Runtime.Close()`.
//
// The factory returns a `remotecontrol.Session` interface
// value (the cycle's own shape, not pigrt.Runtime
// directly) so the cycle's driver can drive a SendStream
// without importing pigrt.
//
// `prompt.BackendLabel` is honoured: "shim" forces a
// `backend.NewMock()` regardless of the supplied
// `Backend` so a dashboard's per-turn override sticks.
// An empty label uses `opts.Backend` verbatim. This is
// the cycle's "the dashboard's resolved backend wins"
// rule — same as cycle-3a's `MessageBackend` seam.
//
// `prompt.Effort` is honoured similarly: empty uses
// `opts.Effort`, non-empty overrides. PiG's
// `SetThinkingLevel` clamps to the active model's
// ThinkingLevelMap; we forward without re-validation.
//
// `prompt.Model` is honoured in the same shape: empty
// keeps the resolved model from `opts.Backend` (the
// `agent.NativeProvider` derivation); non-empty
// overrides.
func NewRemoteCascadeSession(opts RemoteCascadeOptions) remotecontrol.SessionFactory {
	return func(ctx context.Context, prompt remotecontrol.MessagePrompt, _ remotecontrol.MessageSendOptions) (remotecontrol.Session, error) {
		client := resolveRemoteBackend(opts.Backend, prompt.BackendLabel)
		modelName := resolveRemoteModel(client, prompt.Model)
		effort := resolveRemoteEffort(prompt.Effort, opts.Effort)
		return buildRemoteCascadeSession(ctx, opts, client, modelName, effort)
	}
}

// resolveRemoteBackend honours the per-prompt "shim" override: a label
// of "shim" forces a fresh backend.NewMock() regardless of what was
// supplied, and a nil supplied backend also gets the mock. Empty
// labels fall through to the supplied client.
func resolveRemoteBackend(supplied backend.Client, label string) backend.Client {
	if label == "shim" {
		return backend.NewMock()
	}
	if supplied == nil {
		return backend.NewMock()
	}
	return supplied
}

// resolveRemoteModel picks the wire model id the runtime will talk to.
// An explicit prompt.Model wins; otherwise we ask NativeProvider what
// it would put on the wire (the gateway-native path); failing that we
// fall back to the PiG default model.
func resolveRemoteModel(client backend.Client, label string) string {
	if label != "" {
		return label
	}
	if provider := NativeProvider(client); provider != nil {
		return provider.WireModel()
	}
	return "MiniMax-M3"
}

// resolveRemoteEffort merges the per-prompt and per-session effort.
// The empty-string rule mirrors how Prompt overrides cascade defaults.
func resolveRemoteEffort(prompt string, fallback string) string {
	if prompt != "" {
		return prompt
	}
	return fallback
}

// buildRemoteCascadeSession wires the resolved inputs into a pigrt
// runtime and returns the per-turn Session. The function lives
// separately so the factory closure stays a small dispatcher.
func buildRemoteCascadeSession(ctx context.Context, opts RemoteCascadeOptions, client backend.Client, modelName, effort string) (remotecontrol.Session, error) {
	model := buildRemoteCascadeModel(client, modelName)
	rtOpts := buildRemoteCascadeRtOpts(opts, model, effort)
	rt, err := pigrt.New(ctx, rtOpts)
	if err != nil {
		return nil, err
	}
	return &remoteCascadeSession{rt: rt}, nil
}

// buildRemoteCascadeRtOpts assembles the pigrt.Options the runtime
// needs. Mirrors cmd/wagyu/print_pig.go's pigrt.New call minus the
// print-only knobs (Handler, History, NoSession=true) and plus the
// per-turn Model + ErrorSlot.
func buildRemoteCascadeRtOpts(opts RemoteCascadeOptions, model *pi.Model, effort string) pigrt.Options {
	rtOpts := pigrt.Options{
		CWD:              opts.Workspace,
		AgentDir:         opts.AgentDir,
		Model:            model,
		SkipBuiltinTools: true,
		ErrorSlot:        &pigrt.ErrorSlot{},
		Effort:           effort,
	}
	if opts.AgentDir != "" {
		rtOpts.MCPConfigPath = opts.AgentDir + string("/mcp.json")
	}
	return rtOpts
}

// remoteCascadeSession is the per-turn `Session`
// implementation. It holds a `*pigrt.Runtime`; Close
// releases the runtime.
//
// The type is unexported because callers receive it via
// the `remotecontrol.Session` interface; production code
// never names the concrete type.
type remoteCascadeSession struct {
	rt *pigrt.Runtime
}

// Send drives one turn and forwards every agent event
// into the supplied channel. The runtime is called via
// `RunWith` so the channel-fed `EventFunc` callback
// receives every event. The agent loop's
// `emit` blocks on a full channel — the cap-128 buffer
// the cycle's `PiagentBackend` allocates is the buffer
// the runtime sees here.
//
// `events` may be nil; in that case the runtime is
// driven without forwarding (Send returns the messages
// the run produced). Today's
// `PiagentBackend.SendStream` always allocates a
// channel, but the nil-tolerance is cheap insurance
// for future callers.
func (s *remoteCascadeSession) Send(ctx context.Context, prompt string, events chan<- piagent.AgentEvent) ([]piagent.AgentMessage, error) {
	if s == nil || s.rt == nil {
		return nil, errNilSession
	}
	return s.rt.RunWith(ctx, prompt, nil, func(ev piagent.AgentEvent) {
		if events == nil {
			return
		}
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	})
}

// Close releases the runtime. Idempotent: a nil receiver
// or a nil runtime is a no-op. The runtime's own `Close`
// is the source of truth for the per-runtime teardown.
func (s *remoteCascadeSession) Close() error {
	if s == nil || s.rt == nil {
		return nil
	}
	return s.rt.Close()
}

// errNilSession is the typed sentinel a nil-receiver or
// zero-value `*remoteCascadeSession` returns from Send.
// The factory never produces one today (New always
// returns a runtime-backed instance or an error), but
// defensive coverage is cheap.
var errNilSession = &remoteClosedError{msg: "piagent_remote: nil session"}

// remoteClosedError is a private error type the cycle
// doesn't surface today; the dispatcher wraps it in
// ErrTurnFailed and -32603 lands. A future cycle that
// wants a distinct JSON-RPC code can switch on the
// concrete type without renaming the sentinel.
type remoteClosedError struct{ msg string }

func (e *remoteClosedError) Error() string { return e.msg }

// buildRemoteCascadeModel picks the PiG model the runtime
// drives. Mirrors cmd/wagyu/print_pig.go's pigPrintModel:
// a gateway backend yields the native provider; everything
// else is wrapped via NewBackendProvider. Today the
// remote-control surface has no offline-mock path; the
// shim fallback (prompt.BackendLabel == "shim") goes
// through NewMock which NewBackendProvider adapts.
func buildRemoteCascadeModel(be backend.Client, modelName string) *pi.Model {
	if provider := NativeProvider(be); provider != nil {
		return provider.Model(modelName)
	}
	return NewBackendProvider(be, modelName).Model(modelName)
}
