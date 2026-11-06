// Package agentturn is the seam between a turn's host and the code that
// runs the turn.
//
// A turn is a prompt in and a stream of events out. The events are
// agentlink frames — the same frames a remote peer sees — so a local
// host and a remote server speak one vocabulary, and the TUI that drives
// either of them renders a turn the same way.
//
// The package knows nothing about the TUI: the frames are the contract,
// and the TUI's own message vocabulary is built from them. That is what
// makes the split possible — the interactive session and the server
// share the turn, not the rendering.
package agentturn

import (
	"context"
	"errors"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
)

// Env is what one turn needs from its host.
//
// It is a value, not a live handle: a turn captures its environment when
// it starts, and the transport, the prompt and the tools of a turn do
// not change while it runs.
type Env struct {
	// Backend is the transport the turn streams from.
	Backend backend.Client
	// Model is the model the turn runs under.
	Model string
	// SystemPrompt is prepended to every request the turn makes.
	SystemPrompt string
	// Tools are the declarations the model may call.
	Tools []backend.ToolDecl
	// ToolRunner executes the model's tool calls. A nil runner leaves
	// the calls unexecuted — they are still reported, the model just
	// never learns their outcome.
	ToolRunner backend.ToolRunner
	// ExtraTools are the tools that drive a model turn of their own
	// and so do not fit the declaration/runner shape.
	ExtraTools []actor.Tool
	// Effort is the reasoning effort the turn asks for.
	Effort string
	// Policy is the optional permissions policy for tool execution.
	Policy *permissions.Policy
}

// EnvFunc builds the environment for one turn.
//
// The environment is built per turn on purpose: the transport, the
// prompt and the tool set belong to the host, and the host changes them
// between two turns — the user switches the model, a plugin installs a
// tool, the workspace moves.
//
// The model is the one the turn asked for; an empty one means the
// host's own default.
//
// The returned cleanup function releases whatever the environment holds
// (a subagent session, a browser) and runs when the turn ends.
type EnvFunc func(ctx context.Context, model string) (Env, func())

// Turn is one turn's request.
type Turn struct {
	// Model overrides the environment's model. Empty uses the
	// environment's.
	Model string
	// Effort overrides the environment's reasoning effort. Empty uses the
	// environment's default.
	Effort string
	// History is the transcript the turn replays, the user's prompt
	// last.
	History []agentlink.Message
}

// Engine answers turns.
//
// An engine is asked for one turn at a time: the caller reads the
// returned channel until it is closed, and the turn is over.
type Engine interface {
	// Turn runs one turn and reports its events on the returned
	// channel. The channel is closed when the turn ends.
	Turn(ctx context.Context, t Turn) (<-chan agentlink.Frame, error)
	// Close releases the engine. It is safe to call more than once.
	Close() error
}

// ModelInfo represents a model offered by an agent backend.
type ModelInfo struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description,omitempty"`
	Tiers       []string `json:"tiers,omitempty"`
	DefaultTier string   `json:"defaultTier,omitempty"`
}

// ModelLister is an optional interface an Engine may implement to provide its model catalogue.
type ModelLister interface {
	ListModels(ctx context.Context) ([]ModelInfo, error)
}

// Steerer is an optional interface an Engine may implement to steer an in-flight turn.
type Steerer interface {
	Steer(instruction string) error
}

// SteerController provides inspection and queue manipulation for in-flight steering instructions.
type SteerController interface {
	Steerer
	PendingSteer() []string
	PopSteer() (string, bool)
	ClearSteer() []string
}

// DefaultModeler is an optional interface an Engine may implement to advertise its default model.
type DefaultModeler interface {
	DefaultModel() string
}

// CommandInfo represents a slash command offered dynamically by an agent backend.
type CommandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputHint   string `json:"inputHint,omitempty"`
}

// CommandLister is an optional interface an Engine may implement to provide its dynamic slash commands.
type CommandLister interface {
	AvailableCommands() []CommandInfo
}

// ActionHandler is an optional interface an Engine may implement to execute
// backend-level actions (e.g. model switching, reasoning effort, compaction).
type ActionHandler interface {
	ExecuteAction(ctx context.Context, action string, args string) (ActionResult, error)
}

// PermissionAware is an optional interface an Engine may implement to handle tool permission prompts.
type PermissionAware interface {
	SetPermissionHandler(PermissionHandler)
}

// ActionResult represents the outcome of an engine action.
type ActionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// Capability flags describing what features an agentturn.Engine supports.
type Capability uint32

const (
	CapNone Capability = 0

	// CapModelSwitch indicates the engine supports model discovery and switching (/model).
	CapModelSwitch Capability = 1 << iota

	// CapReasoningTier indicates the engine supports reasoning effort configuration (/effort).
	CapReasoningTier

	// CapContextInspect indicates the engine supports context inspection and compaction (/context, /compact).
	CapContextInspect

	// CapTasks indicates the engine supports background task viewing (/tasks).
	CapTasks

	// CapAgents indicates the engine supports custom agent switching and subagent inspection (/agents).
	CapAgents

	// CapNativeTools indicates the engine uses native OpenAGY tool runner / permissions (/permissions).
	CapNativeTools

	// CapGoogleAccounts indicates the engine uses Google CloudCode accounts & routing (/accounts, /pool, /credits, /usage).
	CapGoogleAccounts

	// CapPlugins indicates the engine uses OpenAGY plugins and skills system (/plugin, /skills).
	CapPlugins

	// CapHooks indicates the engine executes local hooks.json runtime (/hooks).
	CapHooks

	// CapWorkflows indicates the engine runs local orchestrations (/goal, /schedule, /browser, /plan, etc.).
	CapWorkflows

	// CapAll represents all capabilities supported by the native LocalEngine.
	CapAll Capability = 0xFFFFFFFF
)

// CapabilityProvider is an optional interface an Engine may implement to advertise its capabilities and engine name.
type CapabilityProvider interface {
	EngineName() string
	Capabilities() Capability
}

// CapabilitiesOf returns the engine name and capabilities of the given engine,
// or ("pigasus", CapAll) if engine is nil or does not implement CapabilityProvider.
func CapabilitiesOf(eng Engine) (string, Capability) {
	if eng == nil {
		return "pigasus", CapAll
	}
	if cp, ok := eng.(CapabilityProvider); ok {
		return cp.EngineName(), cp.Capabilities()
	}
	return "pigasus", CapAll
}

// PlanLister is an optional interface an Engine may implement to provide its execution plan.
type PlanLister interface {
	Plan() []PlanEntry
}

// ErrNoTransport is the error a turn reports when its environment has
// no transport to stream from.
var ErrNoTransport = errors.New("agentturn: the turn has no transport")

// ToWireMessages converts the model's messages into the wire's.
//
// The wire carries the transcript the turn replays, so a message that
// the model would receive keeps its role, its text and its tool calls.
// The tool results do not travel: they live in the trajectory the turn
// builds, and the model reads them from the transport's own stream.
func ToWireMessages(msgs []backend.Message) []agentlink.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]agentlink.Message, 0, len(msgs))
	for _, m := range msgs {
		w := agentlink.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		for _, c := range m.ToolCalls {
			w.ToolCalls = append(w.ToolCalls, agentlink.ToolCall{
				ID:        c.ID,
				Name:      c.Name,
				Arguments: c.Arguments,
			})
		}
		for _, img := range m.Images {
			w.Images = append(w.Images, agentlink.Image{
				MIMEType: img.MIMEType,
				Data:     img.Data,
			})
		}
		out = append(out, w)
	}
	return out
}

// ToBackendMessages converts the wire's messages into the model's.
func ToBackendMessages(msgs []agentlink.Message) []backend.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]backend.Message, 0, len(msgs))
	for _, m := range msgs {
		b := backend.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		for _, c := range m.ToolCalls {
			b.ToolCalls = append(b.ToolCalls, backend.ToolCall{
				ID:        c.ID,
				Name:      c.Name,
				Arguments: c.Arguments,
			})
		}
		for _, img := range m.Images {
			b.Images = append(b.Images, backend.ImagePart{
				MIMEType: img.MIMEType,
				Data:     img.Data,
			})
		}
		out = append(out, b)
	}
	return out
}
