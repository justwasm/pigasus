// print_actor.go — the print-mode turn on the Actor runtime.
package printmode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/agenttools"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/supervisor"
	browsersubagent "github.com/justwasm/pigasus/internal/app/subagent/browser"
	builtinsubagent "github.com/justwasm/pigasus/internal/app/subagent/builtin"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/browser"
	"github.com/justwasm/pigasus/internal/app/tools/tasks"
	"github.com/justwasm/pigasus/internal/core/backend"
	agentmessage "github.com/justwasm/pigasus/internal/core/msg"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
)

// printHistoryTurn is one turn to replay ahead of the new prompt.
type printHistoryTurn struct {
	Role string
	Text string
}

// actorPrintConfig is everything the print-runtime turn needs from runPrintMode.
type actorPrintConfig struct {
	Backend        backend.Client
	Model          string
	Prompt         string
	History        []printHistoryTurn
	Catalog        *tools.Registry
	Policy         *permissions.Policy
	Handler        func(backend.StreamChunk) error
	Workspace      string
	Workspaces     []string
	AgentDir       string
	ConversationID string
	Effort         string
	JSONSchema     json.RawMessage
	AgentBody      string
	Definitions    *subagenttools.Store
	Registry       *subagenttools.Registry
	Supervisor     *supervisor.Supervisor
	Selection      ConversationSelection
}

func preparePrintProvider(cfg actorPrintConfig) (actor.Provider, error) {
	if cfg.Backend == nil {
		return nil, errors.New("actor runtime: backend must not be nil")
	}
	return agentturn.NewBackendProviderAdapter(cfg.Backend, cfg.Model), nil
}

func buildPrintTools(cfg actorPrintConfig, provider actor.Provider, systemPrompt func() string, denials *[]string) ([]actor.Tool, func(), error) {
	if cfg.AgentBody != "" {
		rawTools := agenttools.Tools(cfg.Catalog)
		gated := make([]actor.Tool, 0, len(rawTools))
		for _, t := range rawTools {
			gated = append(gated, agentturn.NewGatedTool(t, cfg.Policy, denials))
		}
		return gated, nil, nil
	}

	catalogTools := agenttools.Tools(cfg.Catalog)

	subTools := subagenttools.Tools(subagenttools.Deps{
		Provider:          provider,
		Model:             cfg.Model,
		CWD:               cfg.Workspace,
		AgentDir:          cfg.AgentDir,
		Definitions:       cfg.Definitions,
		SystemPrompt:      systemPrompt,
		Catalogue:         catalogTools,
		Registry:          cfg.Registry,
		ConversationID:    cfg.ConversationID,
		PermissionGateFor: printSubagentGateFactory(cfg),
	})

	installBrowserSubagent(cfg, provider)

	if cfg.Catalog != nil {
		reg := builtinsubagent.Default()
		if err := browsersubagent.InstallBuiltin(reg, subagenttools.Deps{
			Provider: provider,
			Model:    cfg.Model,
			CWD:      cfg.Workspace,
			AgentDir: cfg.AgentDir,
		}, cfg.Catalog); err != nil {
			printModeSilenceLogger.Printf("[print] browser built-in: %v", err)
		} else {
			subagenttools.SetBuiltinSubagentRegistry(reg)
		}
	}

	merged := make([]actor.Tool, 0, len(catalogTools)+len(subTools))
	merged = append(merged, catalogTools...)
	merged = append(merged, subTools...)

	gated := make([]actor.Tool, 0, len(merged))
	for _, t := range merged {
		gated = append(gated, agentturn.NewGatedTool(t, cfg.Policy, denials))
	}
	return gated, nil, nil
}

func installBrowserSubagent(cfg actorPrintConfig, provider actor.Provider) {
	if provider == nil || cfg.Catalog == nil {
		return
	}
	browser.SetBrowserSubagentProvider(browsersubagent.New(subagenttools.Deps{
		Provider: provider,
		Model:    cfg.Model,
		CWD:      cfg.Workspace,
		AgentDir: cfg.AgentDir,
	}, browsersubagent.Tools(cfg.Catalog)))
}

func setupPrintAskQuestion(catalog *tools.Registry) {
	if catalog != nil {
		catalog.Register(&tasks.AskQuestion{
			SubAgentQuestions: subagentquestions.NewManager(true),
		})
	}
}

func buildPrintSystemPrompt(cfg actorPrintConfig) string {
	convID := cfg.ConversationID
	if convID == "" {
		convID = agyprompt.NewConversationID()
	}
	skillPaths := agyprompt.AgySkillPaths(cfg.AgentDir, cfg.Workspace)
	promptOpts := agyprompt.DefaultOptions(cfg.Workspace, cfg.AgentDir, convID)
	if len(cfg.Workspaces) > 0 {
		promptOpts.Workspaces = cfg.Workspaces
	}
	promptOpts.Skills = agyprompt.DiscoverSkills(skillPaths)
	promptOpts.Subagents = append(promptOpts.Subagents, printSubagents(cfg.AgentBody)...)
	if cfg.AgentBody != "" {
		return agyprompt.BuildAgent(cfg.AgentBody, promptOpts)
	}
	return agyprompt.Build(promptOpts)
}

func runActorPrintTurn(ctx context.Context, cfg actorPrintConfig) ([]string, error) {
	if cfg.ConversationID == "" {
		cfg.ConversationID = agyprompt.NewConversationID()
	}

	provider, err := preparePrintProvider(cfg)
	if err != nil {
		return nil, err
	}

	var denials []string

	registry := subagenttools.NewRegistry(cfg.ConversationID)
	deliveries := make(chan subagentDelivery, 8)
	registry.SetNotify(func(sender, content string) {
		select {
		case deliveries <- subagentDelivery{sender: sender, content: content}:
		default:
		}
	})
	cfg.Registry = registry
	defer registry.Close()

	sysPrompt := buildPrintSystemPrompt(cfg)
	toolsList, cleanup, err := buildPrintTools(cfg, provider, func() string { return sysPrompt }, &denials)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	setupPrintAskQuestion(cfg.Catalog)

	historyTurns := actorSeedHistory(cfg)
	initialHistory := make([]actor.ConversationMessage, 0, len(historyTurns))
	for _, ht := range historyTurns {
		initialHistory = append(initialHistory, actor.ConversationMessage{
			Role:    actor.Role(strings.ToLower(ht.Role)),
			Content: ht.Text,
		})
	}

	act, err := actor.Spawn(actor.Config{
		ID:             cfg.ConversationID,
		Name:           "print_agent",
		SystemPrompt:   sysPrompt,
		Model:          cfg.Model,
		Provider:       provider,
		Tools:          toolsList,
		InitialHistory: initialHistory,
	})
	if err != nil {
		return denials, err
	}
	defer func() {
		_ = act.Close()
	}()

	return denials, executeActorPrintRun(ctx, act, cfg, deliveries)
}

type subagentDelivery struct {
	sender  string
	content string
}

var deliveryWaitTimeout = 10 * time.Minute

func waitForSubagentDelivery(ctx context.Context, registry *subagenttools.Registry, deliveries <-chan subagentDelivery) (subagentDelivery, bool) {
	select {
	case d := <-deliveries:
		return d, true
	default:
	}
	if registry == nil || !registry.Busy() {
		return subagentDelivery{}, false
	}
	timer := time.NewTimer(deliveryWaitTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return subagentDelivery{}, false
	case <-timer.C:
		return subagentDelivery{}, false
	case d := <-deliveries:
		return d, true
	}
}

func actorSeedHistory(cfg actorPrintConfig) []printHistoryTurn {
	seed := actorImplicitHistory(cfg.Workspace)
	if len(seed) == 0 {
		return cfg.History
	}
	return append(seed, cfg.History...)
}

func actorImplicitHistory(workspace string) []printHistoryTurn {
	if workspace == "" {
		return nil
	}
	sup := supervisor.New(workspace)
	sup.CollectImplicit()
	msgs := sup.ImplicitMessages()
	if len(msgs) == 0 {
		return nil
	}
	out := make([]printHistoryTurn, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, printHistoryTurn{Role: m.Role, Text: m.Content})
	}
	return out
}

func executeActorPrintRun(ctx context.Context, act *actor.Actor, cfg actorPrintConfig, deliveries <-chan subagentDelivery) error {
	prompt := cfg.Prompt
	if len(cfg.JSONSchema) > 0 {
		prompt = fmt.Sprintf("%s\n\n[System constraint: You must output ONLY valid JSON matching this schema exactly, with no markdown code fences and no extra text:\n%s]", prompt, string(cfg.JSONSchema))
	}

	var lastSummary *actor.TurnSummary
	var totalReply strings.Builder

	defer func() {
		savePrintConversation(cfg, act, totalReply.String())
	}()

	for {
		sink := agentturn.NewChunkSink(agentturn.ChunkSinkOptions{
			Handler: func(chunk backend.StreamChunk) error {
				if chunk.Text != "" {
					totalReply.WriteString(chunk.Text)
				}
				if chunk.Done && chunk.Error == nil {
					return nil
				}
				return cfg.Handler(chunk)
			},
			OnCompact: func() {
				fmt.Fprintf(os.Stdout, "%s  \u23bf  %sCompacting context…%s\n", cDimGrayOpen, cDimGrayClose, "")
			},
		})

		runErr := agentturn.ExecuteActorTurn(ctx, act, prompt, sink)
		if finishErr := sink.Finish(); finishErr != nil {
			return finishErr
		}
		if runErr != nil {
			if herr := cfg.Handler(backend.StreamChunk{Error: runErr, Done: true}); herr != nil {
				return herr
			}
			return runErr
		}

		delivery, ok := waitForSubagentDelivery(ctx, cfg.Registry, deliveries)
		if !ok {
			break
		}
		prompt = agentmessage.Render(delivery.sender, delivery.content, time.Now(), agentmessage.PriorityHigh)
	}

	trajTurns := act.Trajectory().Turns()
	if len(trajTurns) > 0 {
		last := trajTurns[len(trajTurns)-1]
		if len(last.Steps) > 0 {
			lastStep := last.Steps[len(last.Steps)-1]
			if lastStep.Usage != nil {
				lastSummary = &actor.TurnSummary{
					PromptTokens: lastStep.Usage.PromptTokens,
					OutputTokens: lastStep.Usage.OutputTokens,
					TotalTokens:  lastStep.Usage.TotalTokens,
				}
			}
		}
	}

	var meta *backend.ChunkMeta
	if lastSummary != nil {
		meta = &backend.ChunkMeta{
			PromptTokens:     lastSummary.PromptTokens,
			CompletionTokens: lastSummary.OutputTokens,
			TotalTokens:      lastSummary.TotalTokens,
		}
	}

	if cfg.Supervisor != nil {
		cfg.Supervisor.Append(backend.Message{Role: "user", Content: cfg.Prompt})
		replyText := totalReply.String()
		if replyText != "" {
			cfg.Supervisor.Append(backend.Message{Role: "assistant", Content: replyText})
		}
	}

	return cfg.Handler(backend.StreamChunk{Done: true, Meta: meta})
}

type printSubagentGate struct {
	policy   *permissions.Policy
	sandbox  sandbox.Sandbox
	worktree string
}

func (g *printSubagentGate) Decide(_ context.Context, toolName string, args json.RawMessage) subagenttools.GateDecision {
	sb := g.sandbox
	if sb.Enabled && g.worktree != "" {
		extra := make([]string, 0, len(sb.ExtraPaths)+1)
		extra = append(extra, sb.ExtraPaths...)
		extra = append(extra, g.worktree)
		sb.ExtraPaths = extra
	}
	decision := toolrunner.Decide(g.policy, sb, backend.ToolCall{Name: toolName, Arguments: args})
	return subagenttools.GateDecision{Allow: decision.Allow, Reason: decision.Reason}
}

func printSubagentGateFactory(cfg actorPrintConfig) func(worktree string) subagenttools.BeforeToolCallGate {
	sb := sandbox.Sandbox{Enabled: false, Cwd: cfg.Workspace}
	if cfg.Policy == nil || !cfg.Policy.SkipPermissions {
		sb = printModeSandbox(nil, cfg.Workspace, cfg.ConversationID)
	}
	return func(worktree string) subagenttools.BeforeToolCallGate {
		return &printSubagentGate{policy: cfg.Policy, sandbox: sb, worktree: worktree}
	}
}

func printSubagents(agentBody string) []agyprompt.Subagent {
	if agentBody != "" {
		return nil
	}
	return append([]agyprompt.Subagent{browsersubagent.Subagent()}, customSubagents()...)
}

func printHistory(sel ConversationSelection) []printHistoryTurn {
	if len(sel.History) == 0 {
		return nil
	}
	out := make([]printHistoryTurn, 0, len(sel.History))
	for _, turn := range sel.History {
		out = append(out, printHistoryTurn{Role: turn.Role, Text: turn.Text})
	}
	return out
}

// Compatibility aliases during Actor migration
type pigPrintConfig = actorPrintConfig

func runPigPrintTurn(ctx context.Context, cfg pigPrintConfig) ([]string, error) {
	return runActorPrintTurn(ctx, cfg)
}

func pigHistory(sel ConversationSelection) []printHistoryTurn {
	return printHistory(sel)
}

func pigSeedHistory(cfg pigPrintConfig) []printHistoryTurn {
	return actorSeedHistory(cfg)
}

func pigImplicitHistory(workspace string) []printHistoryTurn {
	return actorImplicitHistory(workspace)
}

func savePrintConversation(cfg actorPrintConfig, act *actor.Actor, reply string) {
	if cfg.ConversationID == "" {
		return
	}
	var steps []convstore.Step
	if cfg.Selection.Store != nil && len(cfg.Selection.Store.Steps) > 0 {
		steps = append(steps, cfg.Selection.Store.Steps...)
	}

	nextIdx := convstore.NextStepIndex(steps)

	var addedTurns bool
	if act != nil {
		traj := act.Trajectory()
		if traj != nil {
			turns := traj.Turns()
			seededCount := 0
			for _, t := range turns {
				if strings.HasPrefix(t.ID, "hist_") {
					seededCount++
				}
			}
			newTurns := turns[seededCount:]
			if len(newTurns) > 0 {
				addedTurns = true
			}
			for _, tr := range newTurns {
				// User step
				userText := cfg.Prompt
				if u, ok := tr.Input.(*actor.UserMessage); ok && u.Text != "" {
					userText = u.Text
				}
				steps = append(steps, convstore.Step{
					Idx:    nextIdx,
					Type:   convstore.StepUserInput,
					Status: 3,
					Text:   userText,
				})
				nextIdx++

				// Tool calls
				for _, st := range tr.Steps {
					if len(st.ToolCalls) > 0 {
						for _, tc := range st.ToolCalls {
							toolStep := convstore.Step{
								Idx:      nextIdx,
								Type:     convstore.StepToolCall,
								Status:   3,
								ToolName: tc.Name,
								ToolArgs: tc.Input,
								Thinking: st.Thought,
							}
							for _, res := range st.ToolResults {
								if res.ToolCallID == tc.ID || len(st.ToolCalls) == 1 {
									if res.Error != "" {
										toolStep.ToolResult = fmt.Sprintf("Error: %s", res.Error)
									} else {
										toolStep.ToolResult = res.Output
									}
									break
								}
							}
							steps = append(steps, toolStep)
							nextIdx++
						}
					}
				}

				// Model reply
				respText := tr.FinalOutput
				if respText == "" && reply != "" {
					respText = reply
				}
				if respText != "" {
					var thinking string
					for _, st := range tr.Steps {
						if st.Thought != "" {
							thinking += st.Thought
						}
					}
					steps = append(steps, convstore.Step{
						Idx:      nextIdx,
						Type:     convstore.StepModelResponse,
						Status:   3,
						Text:     backend.TrimTrailingBlankLines(respText),
						Thinking: thinking,
					})
					nextIdx++
				}
			}
		}
	}

	if !addedTurns {
		if cfg.Prompt != "" {
			steps = append(steps, convstore.Step{
				Idx:    nextIdx,
				Type:   convstore.StepUserInput,
				Status: 3,
				Text:   cfg.Prompt,
			})
			nextIdx++
		}
		if reply != "" {
			steps = append(steps, convstore.Step{
				Idx:    nextIdx,
				Type:   convstore.StepModelResponse,
				Status: 3,
				Text:   backend.TrimTrailingBlankLines(reply),
			})
			nextIdx++
		}
	}

	if len(steps) > 0 {
		ws := cfg.Workspace
		if ws == "" {
			ws, _ = os.Getwd()
		}
		_ = convstore.SaveConversation(cfg.ConversationID, ws, steps)
	}
}

