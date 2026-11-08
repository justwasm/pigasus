package rchost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/agentbrowser"
	"github.com/justwasm/pigasus/internal/app/agenttools"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/app/tools/tasks"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/features/checkpoint"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

type RemoteCascadeOptions struct {
	Backend        backend.Client
	BackendFactory func(string) backend.Client
	Workspace      string
	AgentDir       string
	Browser        *agentbrowser.Broker
	// InteractionHost answers ask_question / ask_permission from the
	// remote transport. Nil keeps the tools' headless behaviour.
	InteractionHost interaction.Host
}

type browserActorTool struct {
	tool *agentbrowser.Tool
	decl agentbrowser.Declaration
}

func (b *browserActorTool) Name() string        { return b.decl.Name }
func (b *browserActorTool) Description() string { return b.decl.Description }
func (b *browserActorTool) Definition() actor.ToolDefinition {
	var params map[string]any
	if len(b.decl.Parameters) > 0 {
		_ = json.Unmarshal(b.decl.Parameters, &params)
	}
	return actor.ToolDefinition{
		Name:        b.decl.Name,
		Description: b.decl.Description,
		Parameters:  params,
	}
}
func (b *browserActorTool) RequiresApproval(input string) bool { return false }
func (b *browserActorTool) Execute(ctx context.Context, input string) (actor.ToolResult, error) {
	res, err := b.tool.Execute(ctx, json.RawMessage(input))
	if err != nil {
		return actor.ToolResult{Error: err.Error()}, nil
	}
	if !res.OK && res.ErrorCode != "" {
		return actor.ToolResult{Error: fmt.Sprintf("[%s] %s", res.ErrorCode, res.Text)}, nil
	}
	return actor.ToolResult{Output: res.Text}, nil
}

type remoteCascadeSession struct {
	opts   RemoteCascadeOptions
	prompt cascades.Prompt
	sOpts  cascades.SendOptions
}

func (s *remoteCascadeSession) Close() error {
	return nil
}

func (s *remoteCascadeSession) Send(ctx context.Context, promptText string, events chan<- remotecontrol.SessionEvent) ([]remotecontrol.SessionMessage, error) {
	modelName := resolveModelName(s.prompt.Model)
	var be backend.Client
	if s.opts.BackendFactory != nil {
		be = s.opts.BackendFactory(modelName)
	} else {
		be = s.opts.Backend
	}
	if be == nil {
		be = backend.NewMock()
	}
	provider := agentturn.NewBackendProviderAdapter(be, modelName)

	catalog := surface.RuntimeCatalog(s.opts.Workspace)
	if host := s.opts.InteractionHost; host != nil {
		// The interactive tools are the only ones that need the
		// transport: re-register them so their calls park on the host
		// instead of answering for themselves.
		catalog.Register(&tasks.AskQuestion{Host: host})
		catalog.Register(&tasks.AskPermission{Host: host})
		catalog.Register(&tasks.AskCustomPermission{Host: host})
	}
	catalogTools := agenttools.MainAgentTools(catalog)
	subTools := subagenttools.Tools(subagenttools.Deps{
		Provider:       provider,
		Backend:        s.opts.Backend,
		Model:          modelName,
		CWD:            s.opts.Workspace,
		AgentDir:       s.opts.AgentDir,
		ConversationID: s.prompt.ConversationID,
	})

	allTools := make([]actor.Tool, 0, len(catalogTools)+len(subTools)+1)
	allTools = append(allTools, catalogTools...)
	allTools = append(allTools, subTools...)
	if s.opts.Browser != nil {
		allTools = append(allTools, &browserActorTool{
			tool: agentbrowser.NewTool(s.opts.Browser, agentbrowser.Options{
				RootConversationID: func() string { return s.prompt.ConversationID },
				CascadeID:          func() string { return s.prompt.ConversationID },
				OwnerID:            s.prompt.ConversationID,
			}),
			decl: agentbrowser.ToolDeclaration(),
		})
	}

	convID := s.prompt.ConversationID
	if convID == "" {
		convID = agyprompt.NewConversationID()
	}
	promptOpts := agyprompt.DefaultOptions(s.opts.Workspace, s.opts.AgentDir, convID)
	sysPrompt := agyprompt.Build(promptOpts)

	act, err := actor.Spawn(actor.Config{
		ID: convID,
		Default: actor.TurnConfig{
			SystemPrompt: sysPrompt,
			Model:        modelName,
			Provider:     provider,
			Tools:        allTools,
		},
	})
	if err != nil {
		events <- remotecontrol.SessionEvent{Type: remotecontrol.SessionEventError, Error: err}
		return nil, err
	}
	defer act.Close()

	seedCascadeHistory(act, s.prompt.History)

	var decls []actor.ToolDefinition
	for _, t := range allTools {
		decls = append(decls, t.Definition())
	}
	toolDecls, _ := json.Marshal(decls)
	policy := checkpoint.New(checkpoint.Config{
		Model:            modelName,
		SystemPrompt:     sysPrompt,
		ToolDeclarations: toolDecls,
		Backend:          s.opts.Backend,
		BackendFor:       s.opts.BackendFactory,
	})

	events <- remotecontrol.SessionEvent{Type: remotecontrol.SessionEventStart}

	var replyBuf strings.Builder
	var toolCalls []cascades.ToolCall
	drainDone := make(chan struct{})
	turnDone := make(chan struct{})
	var turnDoneOnce sync.Once
	signalDone := func() { turnDoneOnce.Do(func() { close(turnDone) }) }

	turn, startErr := act.StartTurn(ctx, actor.TurnRequest{
		Prompt: promptText,
		TurnConfig: actor.TurnConfig{
			Checkpoint: func(stepCtx context.Context, probe actor.CheckpointProbe) (actor.CheckpointDecision, error) {
				return policy.BeforeStep(stepCtx, act, probe)
			},
		},
	})
	if startErr != nil {
		events <- remotecontrol.SessionEvent{
			Type:  remotecontrol.SessionEventError,
			Error: startErr,
		}
		act.Close()
		return nil, startErr
	}

	go func() {
		defer close(drainDone)
		defer policy.Close()
		for ev := range turn.Events() {
			switch ev.Type {
			case actor.EventDelta:
				if ev.Delta != "" {
					replyBuf.WriteString(ev.Delta)
					events <- remotecontrol.SessionEvent{
						Type:  remotecontrol.SessionEventTextDelta,
						Delta: ev.Delta,
					}
				}
			case actor.EventToolCallFinished:
				if ev.ToolCall != nil {
					raw := []byte(ev.ToolCall.Input)
					if len(raw) == 0 {
						raw = []byte("{}")
					}
					out := ""
					ok := true
					if ev.ToolResult != nil {
						out = ev.ToolResult.Output
						ok = ev.ToolResult.Error == ""
					}
					tc := cascades.ToolCall{
						Name:   ev.ToolCall.Name,
						Args:   raw,
						Result: out,
						OK:     ok,
					}
					toolCalls = append(toolCalls, tc)
					events <- remotecontrol.SessionEvent{
						Type:     remotecontrol.SessionEventToolCall,
						ToolCall: tc,
					}
				}
			case actor.EventTurnCompleted, actor.EventCancelled:
				signalDone()
			case actor.EventError:
				if ev.Error != "" {
					events <- remotecontrol.SessionEvent{
						Type:  remotecontrol.SessionEventError,
						Error: errors.New(ev.Error),
					}
				}
				signalDone()
			}
		}
	}()

	select {
	case <-ctx.Done():
		_ = act.CancelTurn("context cancelled")
		select {
		case <-turnDone:
		case <-time.After(2 * time.Second):
		}
	case <-turnDone:
	}

	act.Close()
	<-drainDone

	stopReason := cascades.StopReasonEndTurn
	events <- remotecontrol.SessionEvent{
		Type:       remotecontrol.SessionEventDone,
		StopReason: stopReason,
	}

	msg := remotecontrol.SessionMessage{
		Role:       "assistant",
		Content:    replyBuf.String(),
		ToolCalls:  toolCalls,
		StopReason: stopReason,
	}
	return []remotecontrol.SessionMessage{msg}, nil
}

func seedCascadeHistory(act *actor.Actor, turns []cascades.HistoryTurn) {
	if act == nil || len(turns) == 0 {
		return
	}
	traj := act.Trajectory()
	var current *actor.TurnRecord
	for _, t := range turns {
		if strings.EqualFold(strings.TrimSpace(t.Role), "assistant") {
			if current != nil && current.FinalOutput == "" {
				current.FinalOutput = t.Text
				traj.AddTurn(current)
				current = nil
			} else {
				tr := &actor.TurnRecord{
					ID:          fmt.Sprintf("hist_%d", time.Now().UnixNano()),
					Input:       "",
					FinalOutput: t.Text,
					Status:      "done",
				}
				traj.AddTurn(tr)
			}
		} else {
			if current != nil {
				traj.AddTurn(current)
			}
			current = &actor.TurnRecord{
				ID:     fmt.Sprintf("hist_%d", time.Now().UnixNano()),
				Input:  t.Text,
				Status: "done",
			}
		}
	}
	if current != nil {
		traj.AddTurn(current)
	}
}

func NewRemoteCascadeSession(opts RemoteCascadeOptions) remotecontrol.SessionFactory {
	return func(ctx context.Context, prompt cascades.Prompt, sOpts cascades.SendOptions) (remotecontrol.Session, error) {
		return &remoteCascadeSession{
			opts:   opts,
			prompt: prompt,
			sOpts:  sOpts,
		}, nil
	}
}
