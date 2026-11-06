package agentserver

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp2"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
)

type acpV2Session struct {
	cwd     acp2.AbsolutePath
	session *Session
	history []agentlink.Message
	mu      sync.Mutex
}

func (s *acpV2Session) SessionInfo() acp2.SessionInfo {
	return acp2.SessionInfo{Cwd: s.cwd}
}

func (s *acpV2Session) AvailableCommands() []acp2.AvailableCommand {
	return defaultV2Commands()
}

func defaultV2Commands() []acp2.AvailableCommand {
	return []acp2.AvailableCommand{
		{Name: "/model", Description: "Set a model, or run a single prompt on another model"},
		{Name: "/clear", Description: "Clear conversation and start a new one"},
		{Name: "/plan", Description: "Plan carefully before executing a task"},
		{Name: "/goal", Description: "Run until the specified goal is completely finished"},
		{Name: "/tasks", Description: "View background tasks"},
		{Name: "/agents", Description: "List available custom agents"},
		{Name: "/context", Description: "Visualize current context usage"},
	}
}

// ACPV2Agent implements the draft ACP v2 Agent interface.
type ACPV2Agent struct {
	*acp2.SessionManager[*acpV2Session]
	srv    *Server
	client acp2.Client
}

var _ acp2.Agent = (*ACPV2Agent)(nil)

// NewACPV2Agent creates a new ACP v2 draft agent backed by Server.
func NewACPV2Agent(srv *Server, client acp2.Client) *ACPV2Agent {
	a := &ACPV2Agent{
		srv:    srv,
		client: client,
	}
	a.SessionManager = acp2.NewSessionManager(
		acp2.NewMemoryStore[*acpV2Session](),
		func(ctx context.Context, params *acp2.NewSessionRequest) (acp2.SessionID, *acpV2Session, error) {
			sess, err := srv.session()
			if err != nil {
				return "", nil, err
			}
			sessID := acp2.SessionID(sess.ConversationID)
			if sessID == "" {
				sessID = acp2.SessionID(fmt.Sprintf("sess_%d", time.Now().UnixNano()))
			}
			cwd := params.Cwd
			if cwd == "" {
				cwd = acp2.AbsolutePath(sess.Workspace)
			}
			return sessID, &acpV2Session{cwd: cwd, session: sess}, nil
		},
	)
	return a
}

// Initialize implements acp2.Agent.
func (a *ACPV2Agent) Initialize(ctx context.Context, params *acp2.InitializeRequest) (*acp2.InitializeResponse, error) {
	title := "Google Antigravity"
	return &acp2.InitializeResponse{
		ProtocolVersion: acp2.ProtocolVersion,
		Info: acp2.Implementation{
			Name:    "antigravity-acp",
			Title:   &title,
			Version: Version,
		},
		Capabilities: acp2.CapabilitiesOf(a),
	}, nil
}

// NewSession implements acp2.Agent.
func (a *ACPV2Agent) NewSession(ctx context.Context, params *acp2.NewSessionRequest) (*acp2.NewSessionResponse, error) {
	resp, err := a.SessionManager.NewSession(ctx, params)
	if err != nil {
		return nil, err
	}
	if a.client != nil {
		send := func(c context.Context) {
			stream := acp2.NewSessionStream(a.client, resp.SessionID)
			_ = stream.SendCommands(c, defaultV2Commands())
		}
		if !acp.AfterReply(ctx, send) {
			send(ctx)
		}
	}
	return resp, nil
}

// Prompt implements acp2.Agent.
// In ACP v2, Prompt immediately acknowledges message acceptance with MessageID,
// and runs the turn asynchronously via state_update notifications.
func (a *ACPV2Agent) Prompt(ctx context.Context, params *acp2.PromptRequest) (*acp2.PromptResponse, error) {
	userMessageID := acp2.GenerateMessageID()
	stream := acp2.NewSessionStream(a.client, params.SessionID)

	// In ACP v2, agent echoes the user message it accepted
	if err := stream.SendUserMessage(ctx, userMessageID, params.Prompt...); err != nil {
		return nil, err
	}

	promptText := acp2.JoinTexts(params.Prompt)
	if _, err := a.StartTurn(ctx, params.SessionID, stream, func(turnCtx context.Context, s *acpV2Session) (acp2.StopReason, error) {
		return a.runTurn(turnCtx, stream, s, promptText)
	}); err != nil {
		return nil, err
	}

	return &acp2.PromptResponse{MessageID: userMessageID}, nil
}

func (a *ACPV2Agent) runTurn(ctx context.Context, stream *acp2.SessionStream, s *acpV2Session, promptText string) (acp2.StopReason, error) {
	trimmed := strings.TrimSpace(promptText)
	if trimmed == "/clear" {
		s.mu.Lock()
		s.history = nil
		s.mu.Unlock()
	}
	if trimmed == "/tasks" {
		summary := formatTasksSummary()
		agentMsgID := acp2.GenerateMessageID()
		_ = stream.SendText(ctx, agentMsgID, summary)
		return acp2.StopReasonEndTurn, nil
	}
	if trimmed == "/agents" {
		summary := formatAgentsSummary(string(s.cwd))
		agentMsgID := acp2.GenerateMessageID()
		_ = stream.SendText(ctx, agentMsgID, summary)
		return acp2.StopReasonEndTurn, nil
	}

	engine := agentturn.NewLocal(s.session.env)
	defer engine.Close()

	s.mu.Lock()
	turnHistory := append(slices.Clone(s.history), agentlink.Message{
		Role:    "user",
		Content: promptText,
	})
	s.mu.Unlock()

	turn := agentturn.Turn{
		Model:   s.session.Model,
		History: turnHistory,
	}

	frames, err := engine.Turn(ctx, turn)
	if err != nil {
		return acp2.StopReasonRefusal, err
	}

	_ = stream.SendCommands(ctx, defaultV2Commands())

	agentMsgID := acp2.GenerateMessageID()
	stopReason := acp2.StopReasonEndTurn
	var usageSent bool
	var lastTotal uint64
	var assistantText strings.Builder

	for frame := range frames {
		switch frame.Kind {
		case agentlink.KindText:
			assistantText.WriteString(frame.Text)
			_ = stream.SendText(ctx, agentMsgID, frame.Text)
		case agentlink.KindThought:
			_ = stream.SendThought(ctx, agentMsgID, frame.Text)
		case agentlink.KindTool:
			if frame.Tool != nil {
				callID := acp2.ToolCallID(frame.ID)
				if callID == "" {
					callID = acp2.ToolCallID(fmt.Sprintf("call_%s", frame.Tool.Name))
				}
				if frame.Tool.Phase == agentlink.ToolStart {
					var opts []acp2.ToolCallOption
					if rawInput := rawJSONOrQuoted(frame.Tool.Args); rawInput != nil {
						opts = append(opts, acp2.WithRawInput(rawInput))
					}
					_ = stream.StartToolCall(ctx, callID, frame.Tool.Name, acp2.ToolKindExecute, opts...)
				} else if frame.Tool.Phase == agentlink.ToolEnd {
					var opts []acp2.ToolCallOption
					if rawOutput := rawJSONOrQuoted(frame.Tool.Result); rawOutput != nil {
						opts = append(opts, acp2.WithRawOutput(rawOutput))
					}
					if frame.Tool.Result != "" {
						opts = append(opts, acp2.WithToolContent(acp2.ToolText(frame.Tool.Result)))
					}
					if frame.Tool.Err {
						_ = stream.FailToolCall(ctx, callID, opts...)
					} else {
						_ = stream.CompleteToolCall(ctx, callID, opts...)
					}
				}
			}
		case agentlink.KindResult:
			if frame.Error != "" {
				stopReason = acp2.StopReasonRefusal
			}
			if frame.Usage != nil {
				total := frame.Usage.TotalTokens
				if total == 0 {
					total = frame.Usage.PromptTokens + frame.Usage.CompletionTokens
				}
				lastTotal = uint64(total)
				if total > 0 || frame.Usage.PromptTokens > 0 {
					_ = stream.SendUsage(ctx, uint64(total), 1_000_000, nil)
					usageSent = true
				}
			}
		}
	}

	if !usageSent && lastTotal > 0 {
		_ = stream.SendUsage(ctx, lastTotal, 1_000_000, nil)
	}

	if acp.TurnCancelled(ctx) {
		stopReason = acp2.StopReasonCancelled
	}

	if stopReason != acp2.StopReasonRefusal && stopReason != acp2.StopReasonCancelled && strings.TrimSpace(promptText) != "/clear" {
		s.mu.Lock()
		s.history = append(s.history,
			agentlink.Message{Role: "user", Content: promptText},
			agentlink.Message{Role: "assistant", Content: assistantText.String()},
		)
		s.mu.Unlock()
	}

	return stopReason, nil
}
