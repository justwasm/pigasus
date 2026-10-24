// print_pig.go — the print-mode turn on PiG's coding runtime.
//
// Cycle 209 moved the default runtime off the clone's own loop; cycle 210
// finishes the move for `-p`: PiG is now the only engine headless prints use.
// The clone's loop, its cascade/lifecycle shadow walk and the OPENAGY_RUNTIME
// rollback switch are gone from this path.
//
// The reference behaviour that matters (what lands on stdout, the exit code,
// the stderr envelope) is unchanged, because this file feeds the SAME chunk
// handler runPrintMode always had: it synthesises a backend.StreamChunk
// sequence from PiG's agent events, so the sanitizer, the `user_input` step,
// the usage block and the trailing newline keep coming from one place.
//
// What actually changes when a turn runs here:
//
//   - MCP servers from the agy mcp.json are connected (PiG's built-in mcp
//     extension, via internal/pigrt's translation) and their tools are
//     declared to the model;
//   - skills from the agy roots are discovered and injected into the system
//     prompt by PiG's resource loader;
//   - tool calls are executed by PiG's session, gated by the clone's recovered
//     permission policy through a BeforeToolCall hook;
//   - a resumed conversation's earlier turns are replayed into the session's
//     log before the new prompt (see pigrt.SeedHistory).
//
// Any backend drives the session. An gateway client describes its endpoint, so
// agent.NativeProvider builds the native gateway provider; every other backend
// (the offline mock the shims and parity harnesses use, and any future one) is
// wrapped by piagentbridge.BackendProvider, which reuses the BackendStreamFn
// translation the clone's loop already exercised.
package printmode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agenttools"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/subagentquestions"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

// pigPrintModel builds the PiG model the session calls, keeping the clone's
// transport in the loop. An gateway backend yields the native provider (the
// gateway, the fingerprint and the *backend.HTTPError classification are
// unchanged); anything else is adapted from the backend.Client itself.
func pigPrintModel(be backend.Client, modelName string) *pi.Model {
	if provider := agent.NativeProvider(be); provider != nil {
		return provider.Model(modelName)
	}
	return piagentbridge.NewBackendProvider(be, modelName).Model(modelName)
}

// PigPrintModel builds the PiG model for a backend and model name.
func PigPrintModel(be backend.Client, modelName string) *pi.Model {
	return pigPrintModel(be, modelName)
}

// pigPrintConfig is everything the pig-runtime turn needs from runPrintMode.
type pigPrintConfig struct {
	// Backend supplies the transport the provider is derived from.
	Backend backend.Client
	// Model is the wire model id.
	Model string
	// Prompt is the user text (after slash expansion).
	Prompt string
	// History is the resumed conversation's earlier turns, replayed ahead of
	// the prompt. Nil for a fresh run.
	History []pigrt.HistoryTurn
	// Catalog is the clone's tool registry, handed to the session as extra
	// tools so the advertised surface is the reference's own names.
	Catalog *tools.Registry
	// Policy gates every tool call, MCP tools included.
	Policy *permissions.Policy
	// Handler receives the synthesised chunk stream. It is runPrintMode's
	// existing handler, so the output contract is unchanged.
	Handler func(backend.StreamChunk) error
	// Workspace is the session cwd.
	Workspace string
	// AgentDir is the agy config home.
	AgentDir string
	// ConversationID is the id the system prompt's <user_information> section
	// names. Empty mints one in pigrt.
	ConversationID string
	// Effort is the reasoning-effort level forwarded to PiG's
	// SetThinkingLevel. Empty leaves the model default.
	Effort string
	// AgentBody is the Markdown body of a --agent custom agent. Non-empty
	// selects that agent's system prompt and its restricted seven-tool
	// surface instead of the default prompt and full catalogue.
	AgentBody string
	// Definitions is the conversation's subagent definition store, filled
	// by the rehydration pass when the run continues a conversation.
	//
	// The reference scopes a definition to the conversation, so a
	// continued conversation keeps the subagents it defined (【反编译】
	// the reference's `cortex/rehydration` package; see
	// out/ide-language-server/README.md §10.9).
	Definitions *subagenttools.Store
}

// runPigPrintTurn runs one prompt on PiG's coding runtime and returns the names
// of the tool calls the permission policy denied (for the headless notice).
func runPigPrintTurn(ctx context.Context, cfg pigPrintConfig) ([]string, error) {
	model, slot, err := preparePigPrintModel(cfg)
	if err != nil {
		return nil, err
	}
	extraTools, cleanup, err := buildPigPrintTools(cfg, model)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	setupPrintAskQuestion(cfg.Catalog)

	var denials []string
	hook := createPigPrintHook(cfg.Policy, &denials)

	rt, err := newPigPrintRuntime(ctx, cfg, model, extraTools, hook, slot)
	if err != nil {
		return denials, err
	}
	defer func() {
		if cerr := rt.Close(); cerr != nil {
			printModeSilenceLogger.Printf("[pigrt] close: %v", cerr)
		}
	}()

	if err := rt.SeedHistory(cfg.History); err != nil {
		return denials, err
	}
	return denials, executePigPrintRunWith(ctx, rt, cfg)
}

func preparePigPrintModel(cfg pigPrintConfig) (*pi.Model, *pigrt.ErrorSlot, error) {
	model := pigPrintModel(cfg.Backend, cfg.Model)
	if model == nil {
		return nil, nil, fmt.Errorf("pig runtime: backend %T has no provider", cfg.Backend)
	}
	slot := &pigrt.ErrorSlot{}
	model.Provider = pigrt.RecordProvider(model.Provider, slot)
	return model, slot, nil
}

func buildPigPrintTools(cfg pigPrintConfig, model *pi.Model) ([]piagent.AgentTool, func(), error) {
	if cfg.AgentBody != "" {
		return agenttools.Tools(cfg.Catalog), nil, nil
	}
	subServices, subErr := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      cfg.Workspace,
		AgentDir: cfg.AgentDir,
	})
	if subErr != nil {
		return nil, nil, fmt.Errorf("pig runtime: subagent services: %w", subErr)
	}
	subTools := subagenttools.Tools(subagenttools.Deps{
		Services: subServices,
		Model:    model,
		CWD:      cfg.Workspace,
		AgentDir: cfg.AgentDir,
		// The conversation's definition store, so a continued
		// conversation keeps the subagents it defined.
		Definitions: cfg.Definitions,
	})
	extraTools := piagentbridge.SurfaceTools(cfg.Catalog, subTools)
	return extraTools, func() { subServices.Close() }, nil
}

func setupPrintAskQuestion(catalog *tools.Registry) {
	if catalog != nil {
		catalog.Register(&tools.AskQuestion{
			SubAgentQuestions: subagentquestions.NewManager(true),
		})
	}
}

func createPigPrintHook(policy *permissions.Policy, denials *[]string) piagent.BeforeToolCallHook {
	return piagent.BeforeToolCallHook(func(_ context.Context, _ string, toolName string, args json.RawMessage) piagent.ToolCallHookResult {
		target := toolTarget(backend.ToolCall{Name: toolName, Arguments: args})
		if policy != nil && policy.Decide(permissions.Request{Tool: toolName, Target: target}) != permissions.Allow {
			*denials = append(*denials, toolName)
			return piagent.ToolCallHookResult{
				Block:  true,
				Reason: permissions.DeniedToolResult(toolName, target),
			}
		}
		return piagent.ToolCallHookResult{}
	})
}

func newPigPrintRuntime(ctx context.Context, cfg pigPrintConfig, model *pi.Model, extraTools []piagent.AgentTool, hook piagent.BeforeToolCallHook, slot *pigrt.ErrorSlot) (*pigrt.Runtime, error) {
	return pigrt.New(ctx, pigrt.Options{
		CWD:              cfg.Workspace,
		AgentDir:         cfg.AgentDir,
		ConversationID:   cfg.ConversationID,
		Model:            model,
		ExtraTools:       extraTools,
		SkipBuiltinTools: true,
		AgentBody:        cfg.AgentBody,
		CustomSubagents:  customSubagents(),
		BeforeToolCall:   []piagent.BeforeToolCallHook{hook},
		ErrorSlot:        slot,
		AutoCont:         true,
		NoSession:        true,
		MCPConfigPath:    mcpConfigPath(cfg.AgentDir),
		CompactEnabled:   ptrBool(true),
		Effort:           cfg.Effort,
	})
}

func executePigPrintRunWith(ctx context.Context, rt *pigrt.Runtime, cfg pigPrintConfig) error {
	msgs, runErr := rt.RunWith(ctx, cfg.Prompt, func(delta string) {
		if err := cfg.Handler(backend.StreamChunk{Text: delta}); err != nil {
			printModeSilenceLogger.Printf("[pigrt] handler: %v", err)
		}
	}, printCompactionObserver())

	if runErr != nil {
		if herr := cfg.Handler(backend.StreamChunk{Error: runErr, Done: true}); herr != nil {
			return herr
		}
		return runErr
	}
	meta := chunkMetaFromMessages(msgs)
	return cfg.Handler(backend.StreamChunk{Done: true, Meta: meta})
}

// chunkMetaFromMessages maps PiG's usage to the clone's terminating-chunk
// shape. cache_read_tokens has no counterpart on the gateway transport and stays 0
// in the envelope, exactly as the clone's own parser left it.
func chunkMetaFromMessages(msgs []piagent.AgentMessage) *backend.ChunkMeta {
	usage, ok := pigrt.UsageOf(msgs)
	if !ok {
		return nil
	}
	meta := &backend.ChunkMeta{
		PromptTokens:     usage.Input,
		CompletionTokens: usage.Output,
		TotalTokens:      usage.TotalTokens,
	}
	if usage.Reasoning != nil {
		meta.ReasoningTokens = *usage.Reasoning
	}
	return meta
}

// mcpConfigPath is where the agy MCP servers live: <agent dir>/mcp.json.
func mcpConfigPath(agentDir string) string {
	if agentDir == "" {
		return ""
	}
	return filepath.Join(agentDir, "mcp.json")
}

// pigHistory converts the resolved conversation selection into the session
// seed. It lives here so main.go stays free of pigrt types.
func pigHistory(sel ConversationSelection) []pigrt.HistoryTurn {
	if len(sel.History) == 0 {
		return nil
	}
	out := make([]pigrt.HistoryTurn, 0, len(sel.History))
	for _, turn := range sel.History {
		out = append(out, pigrt.HistoryTurn{Role: turn.Role, Text: turn.Text})
	}
	return out
}

// ptrBool returns &b — used at the call site so the Options.CompactEnabled
// override reads as `ptrBool(true)` instead of the noisy `&[]bool{true}[0]`
// idiom. Cycle 227 added the *bool override specifically because
// `Options.CompactEnabled = true` would compile but read as always-on.
func ptrBool(b bool) *bool { return &b }

// printCompactionObserver is the OnEvent hook that the print path hands to
// rt.RunWith in cycle 227. It listens for PiG's CompactionStartEvent /
// CompactionEndEvent and writes one dim-gray line per event to stdout, the
// format the reference prints when AGY 1.4.0 runs auto-compact during a
// headless turn:
//
//	⎿  Compacting context… 184 K → 42 K tokens
//
// The end-event line only carries tokens-after when the upstream end event
// reported it; on overflow / aborted compact the end line falls back to
// the start line so the user still sees *something*. The values themselves
// come from event.TokensBefore / event.EstimatedTokensAfter — these are the
// numbers the reference echoes, and they are the only ones PiG guarantees.
//
// Channel / context-budget limits push compact to fire during a long
// headless run; the print path was silent on this before cycle 227.
func printCompactionObserver() pigrt.EventFunc {
	return func(ev piagent.AgentEvent) {
		switch e := ev.(type) {
		case piagent.CompactionStartEvent:
			fmt.Fprintf(os.Stdout, "%s  \u23bf  %sCompacting context…%s\n",
				cDimGrayOpen, cDimGrayClose, "",
			)
			_ = e
		case piagent.CompactionEndEvent:
			if e.TokensBefore > 0 && e.EstimatedTokensAfter > 0 {
				fmt.Fprintf(os.Stdout, "%s  \u23bf  %sCompacted context %s K → %s K tokens%s\n",
					cDimGrayOpen, cDimGrayClose,
					TokenK(e.TokensBefore), TokenK(e.EstimatedTokensAfter),
					"",
				)
			}
		}
	}
}

// TokenK renders a count of tokens as "184 K" / "1.2 M" — the dim gray
// formatting matches cycle-225's quota countdowns and agy's own
// post-compact stdout line. 0 yields "-" so the print path never prints
// "0 K → 0 K tokens" on an event that lacked the upstream figure.
func TokenK(tokens int) string {
	if tokens <= 0 {
		return "-"
	}
	if tokens < 1000 {
		return strconv.Itoa(tokens)
	}
	if tokens < 1_000_000 {
		// Round to 1 decimal, drop trailing ".0" for whole kilo figures.
		kilo := float64(tokens) / 1000.0
		return strings.TrimSuffix(strconv.FormatFloat(kilo, 'f', 1, 64), ".0") + " K"
	}
	mega := float64(tokens) / 1_000_000.0
	return strings.TrimSuffix(strconv.FormatFloat(mega, 'f', 1, 64), ".0") + " M"
}
