// Package piagentbridge drives the openagy clone's provider-neutral backend
// through PiG's agent loop (github.com/MichaelKinsy/PiG/agent).
//
// Why this package exists
// -----------------------
// The clone ships its own agent loop in internal/agent. That loop's only
// provider dependency is the one-method backend.Client interface:
//
//	Stream(ctx, backend.StreamRequest) (<-chan backend.StreamChunk, error)
//
// PiG's loop has the mirror-image seam: an agent.StreamFn that turns an
// ai.TranscriptContext into an ai.AssistantMessageEventStream. Everything
// else — the tool-call round trip, stop-reason handling, usage accounting,
// retry/queue semantics, event delivery — lives inside PiG.
//
// This package is the thin adapter between the two surfaces. It is purely
// additive: nothing in internal/agent, internal/backend or cmd/wagyu changes,
// so the feasibility of the swap can be measured without perturbing the
// existing TUI. Cycle 182 spike; see docs/cycles/cycle-182/.
//
// Fidelity notes
// --------------
//   - The clone's tools.Tool carries no description, so toolAdapter.Schema
//     uses a synthetic one. This is the one place the adapter visibly loses
//     information relative to a native PiG tool.
//   - backend.StreamRequest is text-only, so image content blocks are
//     dropped during transcript conversion (the clone's transport has no
//     multimodal path either).
//   - PiG's default tool-execution strategy is parallel; the clone's loop is
//     strictly serial. Options.ToolExecution defaults to sequential here.
package piagentbridge

import (
	"context"
	"errors"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/tools"
)

// ProviderID is the provider name stamped on every assistant message the
// bridge produces, whichever path answered the request.
const ProviderID = "agw"

// BridgeAPI tags assistant messages produced by the StreamFn adapter, which
// re-wraps the clone's own transport rather than speaking the wire itself.
const BridgeAPI pi.API = "openagy-backend"

// ProviderAPI tags assistant messages produced by Provider, which speaks the
// OpenAI Chat-Completions dialect directly.
const ProviderAPI pi.API = "agw-chat-completions"

// Options configures a PiG agent that talks to the clone's backend.
type Options struct {
	// Client is the clone's provider transport. It is used with
	// BackendStreamFn. Exactly one of Client and Provider must be set.
	Client backend.Client
	// Provider is the native agw-backed PiG provider. When set it takes
	// precedence over Client and the loop reaches agw through
	// ProviderStreamFn, which dispatches to the model's own provider.
	Provider *Provider
	// ModelID is the Antigravity model id (e.g. "claude-sonnet-4-6") sent
	// upstream. Required: both transports reject an empty model.
	ModelID string
	// SystemPrompt is the agent's initial instruction block.
	SystemPrompt string
	// Registry, when non-nil, is exposed to the model as the tool set. The
	// clone's registry is a flat name -> Tool map; every entry is wrapped as
	// an agent.AgentTool.
	Registry *tools.Registry
	// Events, when non-nil, receives every agent.AgentEvent. EventDone should
	// be closed when the receiver stops reading.
	Events    chan<- piagent.AgentEvent
	EventDone <-chan struct{}
	// MaxTurns caps the model -> tool -> model round trips inside one Send
	// (0 = PiG's default, which is unlimited).
	MaxTurns int
	// ToolExecution is the batch strategy for one assistant turn with several
	// tool calls. Empty means ToolModeSequential, matching the clone's loop.
	ToolExecution piagent.ToolExecutionMode
}

// NewAgent builds a PiG agent loop wired to the clone's backend and tools.
//
// The returned agent owns no goroutines until Send is called; it is safe to
// hold for the lifetime of a conversation and to call Send on repeatedly.
//
// PiG 0.5.1 made agent.NewAgent return an error: it fails with
// agent.ErrNoDefaultStreamFunction when the options carry no stream function,
// because a model's Provider no longer stands in for one. Every path here
// installs a stream function, so the only error this can return is that one,
// and an empty Options (no Client and no Provider) is what provokes it.
func NewAgent(opts Options) (*piagent.Agent, error) {
	execution := opts.ToolExecution
	if execution == "" {
		execution = piagent.ToolModeSequential
	}
	agentOptions := piagent.AgentOptions{
		Tools:         Tools(opts.Registry),
		SystemPrompt:  opts.SystemPrompt,
		EventCh:       opts.Events,
		EventDone:     opts.EventDone,
		MaxTurns:      opts.MaxTurns,
		ToolExecution: execution,
	}
	if opts.Provider != nil {
		agentOptions.Model = opts.Provider.Model(opts.ModelID)
		agentOptions.StreamFn = ProviderStreamFn()
	} else {
		agentOptions.Model = Model(opts.ModelID)
		agentOptions.StreamFn = BackendStreamFn(opts.Client, opts.ModelID)
	}
	return piagent.NewAgent(agentOptions)
}

// ProviderStreamFn is the native-provider stream function. PiG 0.4.x installed
// streamWithProvider as the default (agent_loop.go:412), which was nothing but
// model.Provider.Stream; 0.5.1 dropped it, so a caller that drives a native
// ai.Provider must supply the same one line itself. This is that line.
//
// ai.Provider.Stream does not receive the model, so the pin has to come from
// the handle: the loop hands back the *ai.Model it was constructed with, which
// for this bridge is Provider.Model(label) / BackendProvider.Model(label).
func ProviderStreamFn() piagent.StreamFn {
	return func(ctx context.Context, model *pi.Model, transcript pi.TranscriptContext, opts pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
		if model == nil || model.Provider == nil {
			return nil, errors.New("piagentbridge: model has no provider")
		}
		return model.Provider.Stream(ctx, transcript, opts)
	}
}

// Model builds the minimal ai.Model PiG needs when the request is answered by
// an explicit StreamFn rather than a native provider. PiG reads ID for
// bookkeeping only; the wire model is supplied to BackendStreamFn directly.
func Model(id string) *pi.Model {
	if id == "" {
		id = "unknown"
	}
	return &pi.Model{
		Type:        pi.ModelTypeChat,
		ID:          id,
		DisplayName: id,
		ProviderMeta: pi.ProviderMetadata{
			ProviderID: ProviderID,
			API:        BridgeAPI,
		},
		Input: []string{"text"},
	}
}
