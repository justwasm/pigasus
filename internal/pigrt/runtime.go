// Package pigrt runs the clone's turns on PiG's coding runtime.
//
// Cycle 209 starts the migration AGENTS.md calls "PiG as the default agent
// runtime, jetski sunset gradually". Until now the clone owned its turn
// engine: internal/agent wrapped PiG's agent.Agent and internal/tools supplied
// a hand-recovered tool catalogue. PiG's coding.Runtime is a superset — it owns
// tools, the extension runner, the built-in MCP extension (a real MCP client
// with stdio/SSE/streamable-HTTP transports) and the resource loader that
// discovers skills. Delegating to it is how the clone gets MCP and skills
// without reimplementing either.
//
// What this package deliberately keeps from the clone:
//
//   - the transport. Model is a *ai.Model whose Provider is the clone's
//     piagentbridge.Provider (the gateway OpenAI-SSE client), so the gateway, the
//     fingerprint and the *backend.HTTPError classification are unchanged;
//   - the config. AgentDir is the agy config home
//     (~/.gemini/antigravity-cli), and the MCP extension is pointed at the
//     agy mcp.json through LoadAgyMCP, not PiG's own file;
//   - the tool surface, when the caller passes ExtraTools.
//
// PiG's hot path never sees a backend.StreamChunk: the session emits
// agent.AgentEvent and ai.AssistantMessageEvent. Callers that need the clone's
// chunk vocabulary (the print and TUI emitters) translate at their own seam;
// see docs/cycles/cycle-209/.
package pigrt

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"

	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/retrybudget"
)

// Runtime is one configured session. It is not safe for concurrent Run calls:
// a Session owns a single turn queue.
type Runtime struct {
	opts     Options
	services *coding.AgentSessionServices
	rt       *coding.Runtime
	session  *coding.Session
	loader   *coding.DefaultResourceLoader

	// alwaysLoad is the merged always-load hint list the runtime parsed at New() time.
	alwaysLoad []AlwaysLoadHint
	// retryBudget enforces the per-turn auto-retry wait cap.
	retryBudget *retrybudget.Budget

	mu      sync.Mutex
	onText  func(string)
	onEvent EventFunc

	compactMu     sync.Mutex
	lastCompact   CompactionRecord
	compactActive bool

	drainDone chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// initServicesAndRuntime instantiates the coding session services and extension runtime.
func initServicesAndRuntime(ctx context.Context, opts Options) (*coding.AgentSessionServices, *coding.Runtime, error) {
	settingsManager := buildDefaultSettingsManager(opts)
	services, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:             opts.CWD,
		AgentDir:        opts.AgentDir,
		SettingsManager: settingsManager,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("pigrt: services: %w", err)
	}

	exts, extRuntime, err := builtinExtensions(opts)
	if err != nil {
		services.Close()
		return nil, nil, err
	}

	rt, err := coding.NewRuntime(coding.RuntimeOptions{
		Services:         services,
		NewExtensions:    exts,
		ExtensionRuntime: extRuntime,
		AbortContext:     ctx,
	})
	if err != nil {
		services.Close()
		return nil, nil, fmt.Errorf("pigrt: runtime: %w", err)
	}
	return services, rt, nil
}

// initResourceLoader initializes the skill and context resource loader.
func initResourceLoader(opts Options, services *coding.AgentSessionServices) (*coding.DefaultResourceLoader, []string, error) {
	skillPaths := opts.SkillPaths
	if skillPaths == nil {
		skillPaths = AgySkillPaths(opts.AgentDir, opts.CWD)
	}
	loader := coding.NewDefaultResourceLoader(coding.DefaultResourceLoaderOptions{
		CWD:                  opts.CWD,
		AgentDir:             opts.AgentDir,
		SettingsManager:      services.SettingsManager(),
		NoSkills:             true,
		AdditionalSkillPaths: skillPaths,
		NoContextFiles:       opts.NoContextFiles,
	})
	if err := loader.Reload(); err != nil {
		return nil, nil, fmt.Errorf("pigrt: resource loader: %w", err)
	}
	return loader, skillPaths, nil
}

// resolveSystemPrompt computes the system prompt to apply.
func resolveSystemPrompt(opts *Options, skillPaths []string) string {
	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" && !opts.PigPrompt {
		conversationID := opts.ConversationID
		if conversationID == "" {
			conversationID = agyprompt.NewConversationID()
		}
		promptOpts := agyprompt.DefaultOptions(opts.CWD, opts.AgentDir, conversationID)
		promptOpts.Skills = agyprompt.DiscoverSkills(skillPaths)
		promptOpts.Subagents = append(promptOpts.Subagents, opts.CustomSubagents...)
		if opts.AgentBody != "" {
			systemPrompt = agyprompt.BuildAgent(opts.AgentBody, promptOpts)
		} else {
			systemPrompt = agyprompt.Build(promptOpts)
		}
		opts.ConversationID = conversationID
	}
	return systemPrompt
}

// SystemPromptFor computes the system prompt a runtime would use for
// these options — the same text pigrt.New sends, without building a
// runtime.
//
// The print mode needs it before its session exists: its subagent tool
// set is built first, and a `self` delegation runs on the parent's own
// prompt.
func SystemPromptFor(opts Options) string {
	skillPaths := opts.SkillPaths
	if skillPaths == nil {
		skillPaths = AgySkillPaths(opts.AgentDir, opts.CWD)
	}
	return resolveSystemPrompt(&opts, skillPaths)
}

// initRetryBudget constructs the budget observer or returns nil if disabled.
func initRetryBudget(cap time.Duration) *retrybudget.Budget {
	switch {
	case cap < 0:
		return nil
	case cap == 0:
		return retrybudget.New(retrybudget.DefaultPerTurnCap)
	default:
		return retrybudget.New(cap)
	}
}

// New builds the services, the extension runner and one session, and issues
// session_start so the MCP servers connect before the first prompt.
func New(ctx context.Context, opts Options) (*Runtime, error) {
	if err := normalizeOptions(&opts); err != nil {
		return nil, err
	}
	services, rt, err := initServicesAndRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}
	loader, skillPaths, err := initResourceLoader(opts, services)
	if err != nil {
		_ = rt.Close()
		services.Close()
		return nil, err
	}
	systemPrompt := resolveSystemPrompt(&opts, skillPaths)
	session, err := rt.New(coding.SessionStartOptions{
		Model:            opts.Model,
		SystemPrompt:     systemPrompt,
		ExtraTools:       opts.ExtraTools,
		ResourceLoader:   loader,
		NoSession:        opts.NoSession,
		SkipBuiltinTools: opts.SkipBuiltinTools,
		BeforeToolCall:   opts.BeforeToolCall,
	})
	if err != nil {
		_ = rt.Close()
		services.Close()
		return nil, fmt.Errorf("pigrt: session: %w", err)
	}
	r := &Runtime{
		opts:        opts,
		services:    services,
		rt:          rt,
		session:     session,
		loader:      loader,
		alwaysLoad:  loadAlwaysLoadHints(opts),
		retryBudget: initRetryBudget(opts.RetryBudgetCap),
		drainDone:   make(chan struct{}),
	}
	go r.drain()
	if err := session.BindExtensions(ctx, coding.ExtensionBindings{}); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("pigrt: bind extensions: %w", err)
	}
	if opts.Effort != "" {
		_ = session.SetThinkingLevel(ai.ModelThinkingLevel(opts.Effort))
	}
	return r, nil
}

// Services exposes the underlying PiG services container.
func (r *Runtime) Services() *coding.AgentSessionServices { return r.services }

// SkillNames returns the names of the skills the resource loader discovered, sorted.
func (r *Runtime) SkillNames() []string {
	if r.loader == nil {
		return nil
	}
	result := r.loader.GetSkills()
	out := make([]string, 0, len(result.Skills))
	for _, s := range result.Skills {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// SkillDiagnostics returns the resource loader's per-path diagnostics.
func (r *Runtime) SkillDiagnostics() []extension.ResourceDiagnostic {
	if r.loader == nil {
		return nil
	}
	return r.loader.GetSkills().Diagnostics
}

// AgentDir is the resolved config directory.
func (r *Runtime) AgentDir() string { return r.opts.AgentDir }

// SystemPrompt returns the prompt the session sends.
func (r *Runtime) SystemPrompt() string {
	if r.session == nil {
		return ""
	}
	return r.session.SystemPrompt()
}

// ConversationID returns the id the system prompt's <user_information> section names.
func (r *Runtime) ConversationID() string { return r.opts.ConversationID }

// Close tears the session, runtime and services down in order.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			if r.session != nil {
				_ = r.session.Close()
			}
			if r.rt != nil {
				_ = r.rt.Close()
			}
			if r.services != nil {
				r.services.Close()
			}
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			r.closeErr = errors.New("pigrt: close timed out")
		}
		<-r.drainDone
	})
	return r.closeErr
}
