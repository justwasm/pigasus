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
	"encoding/base64"
	"log"
	"strings"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"

	"github.com/justwasm/pigasus/agentbrowser"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
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
	Backend backend.Client
	// BackendFactory builds the transport for one turn's model. When it
	// is set, the model the turn names decides the transport: a model
	// the user configured goes to its own endpoint, a model in the
	// reference's catalogue goes to CloudCode. Without it the fixed
	// Backend answers every turn.
	//
	// The remote-control surfaces need it because the Web UI picks the
	// model per turn, while the daemon is started long before the first
	// conversation exists.
	BackendFactory func(modelName string) backend.Client
	Workspace      string
	AgentDir       string
	Effort         string
	// Browser is the agent↔browser bridge the IDE turn's
	// `webview_browser` tool runs its actions in.
	//
	// nil → the turn advertises the reference's twelve captured
	// declarations and no browser tool; the reference's IDE turn
	// always has the tool, so production wiring passes the very
	// broker the dispatcher's three LanguageServerService RPCs serve.
	Browser *agentbrowser.Broker
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
		client := resolveTurnBackend(opts, prompt)
		modelName := resolveRemoteModel(client, prompt.Model)
		effort := resolveRemoteEffort(prompt.Effort, opts.Effort)
		return buildRemoteCascadeSession(ctx, opts, client, modelName, effort, prompt.ConversationID, prompt.History, prompt.Images)
	}
}

// resolveTurnBackend picks the transport one turn runs on.
//
// The "shim" label always wins: the dashboard can force the stand-in
// for a turn. Otherwise the turn's model decides, through the
// factory, and the fixed backend answers only when the turn names no
// model.
func resolveTurnBackend(opts RemoteCascadeOptions, prompt remotecontrol.MessagePrompt) backend.Client {
	if prompt.BackendLabel == "shim" {
		return backend.NewMock()
	}
	if opts.BackendFactory != nil {
		if m := strings.TrimSpace(prompt.Model); m != "" {
			return opts.BackendFactory(m)
		}
	}
	return resolveRemoteBackend(opts.Backend, prompt.BackendLabel)
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
//
// The conversation's earlier turns are replayed into the runtime's
// session log before the turn runs: the Web UI's composer sends one
// message at a time, and the session this factory mints is a fresh
// one, so without the replay every message would be its own context.
//
// The replay is best-effort, the way the print path's implicit
// trajectory is: a history that cannot be read leaves the session as
// it was, and the turn runs.
func buildRemoteCascadeSession(ctx context.Context, opts RemoteCascadeOptions, client backend.Client, modelName, effort, conversationID string, history []remotecontrol.MessageHistoryTurn, images []remotecontrol.MessageImage) (remotecontrol.Session, error) {
	model := buildRemoteCascadeModel(client, modelName)
	rtOpts, closeTools, err := buildRemoteCascadeRtOpts(opts, model, effort, conversationID)
	if err != nil {
		return nil, err
	}
	rt, err := pigrt.New(ctx, rtOpts)
	if err != nil {
		closeTools()
		return nil, err
	}
	if err := rt.SeedHistory(remoteHistoryTurns(history)); err != nil {
		log.Printf("piagent_remote: seed the conversation's history: %v", err)
	}
	return &remoteCascadeSession{rt: rt, closeTools: closeTools, images: remoteImageContent(images)}, nil
}

// remoteImageContent converts the pictures a turn carried into PiG's
// own image blocks.
//
// PiG's block holds the base64 payload (ai.ImageContent.Data) and the
// media type beside it; the clone's neutral shape holds the bytes.
func remoteImageContent(images []remotecontrol.MessageImage) []pi.ImageContent {
	if len(images) == 0 {
		return nil
	}
	out := make([]pi.ImageContent, 0, len(images))
	for _, img := range images {
		if len(img.Data) == 0 {
			continue
		}
		out = append(out, pi.ImageContent{
			Data:     base64.StdEncoding.EncodeToString(img.Data),
			MimeType: img.MIMEType,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// remoteHistoryTurns converts the prompt's history into the session
// seed's shape.
func remoteHistoryTurns(history []remotecontrol.MessageHistoryTurn) []pigrt.HistoryTurn {
	if len(history) == 0 {
		return nil
	}
	out := make([]pigrt.HistoryTurn, 0, len(history))
	for _, turn := range history {
		out = append(out, pigrt.HistoryTurn{Role: turn.Role, Text: turn.Text})
	}
	return out
}

// buildRemoteCascadeRtOpts assembles the pigrt.Options the runtime
// needs. Mirrors cmd/wagyu/print_pig.go's pigrt.New call minus the
// print-only knobs (Handler, History, NoSession=true) and plus the
// per-turn Model + ErrorSlot.
//
// The tool surface is the reference's own twelve declarations, each
// wired to an executor from the clone's catalogue — the same surface
// the print path advertises. The remote-control turn used to run with
// *no* tools at all: `SkipBuiltinTools` removed PiG's own tools and no
// `ExtraTools` replaced them, so the request that reached the model
// carried no function declarations.
//
// 【一手】 2026-10-26: the request the remote-control path sent to the
// model carried an empty `tools` array, while the system prompt — the
// reference's own, byte for byte — describes `run_command`,
// `search_web` and the file tools. A model that reads the prompt and
// finds no declaration for them has no way to act: it thinks about the
// tools it does not have and ends the turn with nothing but the
// reasoning. That is the "Thought for Ns" panel with no answer under
// it.
//
// The returned closer releases the tool surface's own resources; the
// caller runs it when the turn's session closes.
func buildRemoteCascadeRtOpts(opts RemoteCascadeOptions, model *pi.Model, effort, conversationID string) (pigrt.Options, func(), error) {
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
	extraTools, closeTools, err := remoteCascadeTools(opts, model, conversationID)
	if err != nil {
		return pigrt.Options{}, func() {}, err
	}
	rtOpts.ExtraTools = extraTools
	return rtOpts, closeTools, nil
}

// remoteCascadeTools builds the tool surface the remote-control turn
// advertises.
//
// It is the print path's surface (internal/printmode/print_pig.go:
// `piagentbridge.SurfaceTools(cfg.Catalog, subTools)`), rebuilt from
// the same two pieces:
//
//   - the clone's catalogue — `tools.RuntimeCatalog` is the catalogue
//     plus the 1.4.4-only executors (read_file, write_file,
//     edit_file), so every declaration but the three subagent ones has
//     an executor;
//   - the subagent tools, which need PiG's session services.
//
// The subagent services are optional: when they cannot be created the
// turn keeps the rest of the surface and the three subagent
// declarations report the missing implementation the way a pending
// catalogue entry does. A turn without any tools at all is worse than
// a turn without subagents.
func remoteCascadeTools(opts RemoteCascadeOptions, model *pi.Model, conversationID string) ([]piagent.AgentTool, func(), error) {
	reg := tools.RuntimeCatalog(opts.Workspace)
	subServices, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      opts.Workspace,
		AgentDir: opts.AgentDir,
	})
	if err != nil {
		log.Printf("piagent_remote: subagent services: %v", err)
		return SurfaceTools(reg, nil), func() {}, nil
	}
	subTools := subagenttools.Tools(subagenttools.Deps{
		Services: subServices,
		Model:    model,
		CWD:      opts.Workspace,
		AgentDir: opts.AgentDir,
	})
	decls, extras := remoteCascadeBrowserSurface(opts, conversationID, subTools)
	return SurfaceToolsFor(reg, extras, decls), func() { subServices.Close() }, nil
}

// remoteCascadeBrowserSurface adds the reference's `webview_browser`
// tool to the turn's surface.
//
// 【一手】 2026-10-27, the reference's IDE turn advertises the browser
// tool next to its other declarations; the agent half of the bridge
// and the IDE half meet in one broker, so the declaration and its
// executor have to be the same pair the dispatcher's RPCs serve.
func remoteCascadeBrowserSurface(opts RemoteCascadeOptions, conversationID string, subTools []piagent.AgentTool) ([]tools.ModelToolDecl, []piagent.AgentTool) {
	decls := tools.ModelDeclarations()
	if opts.Browser == nil {
		return decls, subTools
	}
	root := conversationID
	decls = append(decls, BrowserDeclaration())
	extras := append(subTools, BrowserTool(opts.Browser, agentbrowser.Options{
		RootConversationID: func() string { return root },
		CascadeID:          func() string { return root },
		OwnerID:            root,
	}))
	return decls, extras
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
	// closeTools releases the tool surface's own resources (the
	// subagent session services). It runs after the runtime is closed.
	closeTools func()
	// images are the pictures the turn's message carried. The turn
	// sends them with the text when there are any.
	images []pi.ImageContent
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
	observe := func(ev piagent.AgentEvent) {
		if events == nil {
			return
		}
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	}
	if len(s.images) == 0 {
		return s.rt.RunWith(ctx, prompt, nil, observe)
	}
	// The turn carries pictures: the user message is a text block
	// followed by one block per image — the shape PiG's own
	// BuildUserContent builds (coding/session.go).
	content := coding.BuildUserContent(prompt, s.images)
	return s.rt.RunWithContent(ctx, content, nil, observe)
}

// Close releases the runtime. Idempotent: a nil receiver
// or a nil runtime is a no-op. The runtime's own `Close`
// is the source of truth for the per-runtime teardown.
func (s *remoteCascadeSession) Close() error {
	if s == nil || s.rt == nil {
		return nil
	}
	err := s.rt.Close()
	if s.closeTools != nil {
		s.closeTools()
	}
	return err
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
