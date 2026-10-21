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
// Any backend drives the session. An agw client describes its endpoint, so
// agent.NativeProvider builds the native agw provider; every other backend
// (the offline mock the shims and parity harnesses use, and any future one) is
// wrapped by piagentbridge.BackendProvider, which reuses the BackendStreamFn
// translation the clone's loop already exercised.
package main

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
// transport in the loop. An agw backend yields the native provider (the
// gateway, the fingerprint and the *backend.HTTPError classification are
// unchanged); anything else is adapted from the backend.Client itself.
func pigPrintModel(be backend.Client, modelName string) *pi.Model {
	if provider := agent.NativeProvider(be); provider != nil {
		return provider.Model(modelName)
	}
	return piagentbridge.NewBackendProvider(be, modelName).Model(modelName)
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
}

// runPigPrintTurn runs one prompt on PiG's coding runtime and returns the names
// of the tool calls the permission policy denied (for the headless notice).
func runPigPrintTurn(ctx context.Context, cfg pigPrintConfig) ([]string, error) {
	model := pigPrintModel(cfg.Backend, cfg.Model)
	if model == nil {
		return nil, fmt.Errorf("pig runtime: backend %T has no provider", cfg.Backend)
	}
	slot := &pigrt.ErrorSlot{}
	// Wrap the model's provider out of band: PiG flattens a transport error to
	// its message, and the print path needs the *backend.HTTPError type back
	// for the AGY_ERROR envelope and exit code 3.
	model.Provider = pigrt.RecordProvider(model.Provider, slot)

	// A custom agent gets the reference's restricted seven-tool surface and
	// no subagent tools (it cannot invoke_subagent). The default agent keeps
	// the full catalogue plus the subagent tools.
	var extraTools []piagent.AgentTool
	if cfg.AgentBody != "" {
		extraTools = agenttools.Tools(cfg.Catalog)
	} else {
		// Cycle 228: build the subagent tools (invoke_subagent +
		// send_message + manage_subagents). They need a coding.Services,
		// so we open one parallel to the one pigrt.New opens — both share
		// AgentDir, so the subagents/ transcript directory pigrt/print
		// writes to is the same one subagenttools reads. Two parallel
		// services instances are fine: each is stateless, auth.json is
		// flock-serialised on the read paths we use today.
		subServices, subErr := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
			CWD:      cfg.Workspace,
			AgentDir: cfg.AgentDir,
		})
		if subErr != nil {
			return nil, fmt.Errorf("pig runtime: subagent services: %w", subErr)
		}
		defer subServices.Close()
		subTools := subagenttools.Tools(subagenttools.Deps{
			Services: subServices,
			Model:    model,
			CWD:      cfg.Workspace,
			AgentDir: cfg.AgentDir,
		})
		// Cycle 293: the reference declares twelve tools, not the 60-name
		// init catalogue. SurfaceTools pairs each captured declaration with
		// its executor (subagenttools where one exists, the catalogue
		// otherwise), so the wire matches 1.4.4 byte for byte while the
		// subagent implementations stay live.
		extraTools = piagentbridge.SurfaceTools(cfg.Catalog, subTools)
	}

	// Cycle subagent-question-picker: every print turn runs in a
	// headless subagentquestions Manager so an ask_question call
	// auto-skips with SkipAnswer — the AGY 1.3.6 changelog line
	// "Headless -p runs skip these questions so the subagent can
	// continue." The manager is wired into the catalog's
	// ask_question tool so the model sees SkipAnswer instead of
	// the cycle-214 noUserText reminder. We re-register the tool
	// in place rather than rebuilding the catalog so any other
	// tool state (aliases, descriptions) is preserved.
	if cfg.Catalog != nil {
		cfg.Catalog.Register(&tools.AskQuestion{
			SubAgentQuestions: subagentquestions.NewManager(true),
		})
	}

	var denials []string
	hook := piagent.BeforeToolCallHook(func(_ context.Context, _ string, toolName string, args json.RawMessage) piagent.ToolCallHookResult {
		target := toolTarget(backend.ToolCall{Name: toolName, Arguments: args})
		if cfg.Policy != nil && cfg.Policy.Decide(permissions.Request{Tool: toolName, Target: target}) != permissions.Allow {
			denials = append(denials, toolName)
			// Block with the reference's denial text so the model sees the same
			// soft-deny it would have received from the clone's gate. PiG
			// surfaces Reason as the tool result's error.
			return piagent.ToolCallHookResult{
				Block:  true,
				Reason: permissions.DeniedToolResult(toolName, target),
			}
		}
		return piagent.ToolCallHookResult{}
	})

	rt, err := pigrt.New(ctx, pigrt.Options{
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
		// Cycle auto-continue-interrupt: a stream-interrupt or
		// iteration-cap is recovered transparently — the print path
		// streams the model's continued response to stdout instead of
		// surfacing a stuck terminal. The recovery copy is the AGY
		// verbatim text from docs/research/agy-feature-strings.md §3.
		AutoCont: true,
		// A print run is one shot: no conversation file, matching the
		// reference, which writes none for `-p` either.
		NoSession:     true,
		MCPConfigPath: mcpConfigPath(cfg.AgentDir),
		// Cycle 227: TODO §4 long-running task continuity row. AGY 1.4.0
		// echoes compaction as a single dim-gray line on stderr / stdout
		// when a turn's context budget is exhausted mid-stream; the
		// clone's print path stays silent otherwise. We mirror that —
		// a one-liner per compact event, with the same `⎿  ` prefix the
		// reference uses for slash-command echoes.
		CompactEnabled: ptrBool(true),
		// Cycle effort-picker: forward --effort to PiG's SetThinkingLevel
		// so the provider sees the level the user typed. The (model,
		// effort) validation that resolveModelSelection runs in main.go
		// covers the headless path; the TUI path applies it via the loop.
		Effort: cfg.Effort,
	})
	if err != nil {
		return denials, err
	}
	defer func() {
		if cerr := rt.Close(); cerr != nil {
			printModeSilenceLogger.Printf("[pigrt] close: %v", cerr)
		}
	}()

	// A resumed conversation replays its earlier turns into the session log, so
	// the model answers with the prior context. This replaces the old loop's
	// straight replay of backend.Messages.
	if err := rt.SeedHistory(cfg.History); err != nil {
		return denials, err
	}

	// Synthesise the chunk vocabulary the handler already speaks. Text deltas
	// stream straight through; the terminating chunk carries the usage PiG
	// accumulated on the assistant messages. The OnEvent hook also captures
	// CompactionStartEvent / CompactionEndEvent so the print path can echo
	// them on stdout — the clone's TODO §4 row was the missing piece before
	// cycle 227.
	msgs, runErr := rt.RunWith(ctx, cfg.Prompt, func(delta string) {
		if err := cfg.Handler(backend.StreamChunk{Text: delta}); err != nil {
			printModeSilenceLogger.Printf("[pigrt] handler: %v", err)
		}
	}, printCompactionObserver())

	if runErr != nil {
		// The clone's own loop told the handler about a failure with a terminal
		// error chunk, and that chunk is what marks the turn as started — it
		// drives the `user_input` step and the envelope's num_turns. Keep it:
		// the handler drops the error itself, and api_error.go reports it on
		// stderr with the AGY_ERROR envelope and exit code 3.
		if herr := cfg.Handler(backend.StreamChunk{Error: runErr, Done: true}); herr != nil {
			return denials, herr
		}
		return denials, runErr
	}
	meta := chunkMetaFromMessages(msgs)
	if err := cfg.Handler(backend.StreamChunk{Done: true, Meta: meta}); err != nil {
		return denials, err
	}
	return denials, nil
}

// chunkMetaFromMessages maps PiG's usage to the clone's terminating-chunk
// shape. cache_read_tokens has no counterpart on the agw transport and stays 0
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
func pigHistory(sel conversationSelection) []pigrt.HistoryTurn {
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
					tokenK(e.TokensBefore), tokenK(e.EstimatedTokensAfter),
					"",
				)
			}
		}
	}
}

// tokenK renders a count of tokens as "184 K" / "1.2 M" — the dim gray
// formatting matches cycle-225's quota countdowns and agy's own
// post-compact stdout line. 0 yields "-" so the print path never prints
// "0 K → 0 K tokens" on an event that lacked the upstream figure.
func tokenK(tokens int) string {
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
