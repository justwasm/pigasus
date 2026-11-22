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
	schemav2 "github.com/ironpark/acp-go/schema/v2"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/core/backend"
)

type acpV2Session struct {
	cwd      acp2.AbsolutePath
	session  *Session
	history  []agentlink.Message
	commands []acp2.AvailableCommand
	mu       sync.Mutex
}

func (s *acpV2Session) SessionInfo() acp2.SessionInfo {
	return acp2.SessionInfo{Cwd: s.cwd}
}

func (s *acpV2Session) AvailableCommands() []acp2.AvailableCommand {
	if s == nil || len(s.commands) == 0 {
		return defaultV2Commands()
	}
	return s.commands
}

func defaultV2Commands() []acp2.AvailableCommand {
	return []acp2.AvailableCommand{
		acp2.SlashCommand("/model", "Set a model, or run a single prompt on another model"),
		acp2.SlashCommand("/clear", "Clear conversation and start a new one"),
		acp2.SlashCommand("/plan", "Plan carefully before executing a task"),
		acp2.SlashCommand("/goal", "Run until the specified goal is completely finished"),
		acp2.SlashCommand("/tasks", "View background tasks"),
		acp2.SlashCommand("/agents", "List available custom agents"),
		acp2.SlashCommand("/context", "Visualize current context usage"),
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
	var store acp2.SessionStore[*acpV2Session]
	if srv != nil {
		store = srv.getACPV2Store()
	} else {
		store = acp2.NewMemoryStore[*acpV2Session]()
	}
	a.SessionManager = acp2.NewSessionManager(
		store,
		func(ctx context.Context, params *acp2.NewSessionRequest) (acp2.SessionID, *acpV2Session, error) {
			cwd := string(params.Cwd)
			var sess *Session
			var err error
			if srv != nil {
				sess, err = srv.sessionFor(cwd, srv.cfg.Model)
				if err != nil {
					return "", nil, err
				}
			}
			var sessID acp2.SessionID
			if sess != nil && sess.ConversationID != "" {
				sessID = acp2.SessionID(sess.ConversationID)
			} else {
				sessID = acp2.SessionID(fmt.Sprintf("sess_%d", time.Now().UnixNano()))
			}
			if cwd == "" && sess != nil {
				cwd = sess.Workspace
			}
			var cmds []acp2.AvailableCommand
			if srv != nil {
				cmds = v2CommandsFor(srv.cfg)
			} else {
				cmds = defaultV2Commands()
			}
			return sessID, &acpV2Session{
				cwd:      acp2.AbsolutePath(cwd),
				session:  sess,
				commands: cmds,
			}, nil
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
			_ = stream.SendCommands(c, commandsV2Of(resp.AvailableCommands))
		}
		if !acp.AfterReply(ctx, send) {
			send(ctx)
		}
	}
	return resp, nil
}

// ResumeSession implements acp2.Agent, continuing a session and replaying history if requested.
func (a *ACPV2Agent) ResumeSession(ctx context.Context, params *acp2.ResumeSessionRequest) (*acp2.ResumeSessionResponse, error) {
	if params == nil {
		return nil, fmt.Errorf("resume session: nil params")
	}
	s, err := a.Lookup(ctx, params.SessionID)
	if err != nil {
		cwd := string(params.Cwd)
		if cwd == "" && a.srv != nil {
			cwd = a.srv.cfg.Workspace
		}
		var sess *Session
		var sErr error
		if a.srv != nil {
			sess, sErr = a.srv.sessionFor(cwd, a.srv.cfg.Model)
			if sErr != nil {
				return nil, err
			}
		}
		recoveredHist := loadHistoryFromTranscript(string(params.SessionID))
		var cmds []acp2.AvailableCommand
		if a.srv != nil {
			cmds = v2CommandsFor(a.srv.cfg)
		} else {
			cmds = defaultV2Commands()
		}
		s = &acpV2Session{
			cwd:      acp2.AbsolutePath(cwd),
			session:  sess,
			history:  recoveredHist,
			commands: cmds,
		}
		if saveErr := a.SessionManager.Save(ctx, params.SessionID, s); saveErr != nil {
			return nil, saveErr
		}
	}

	if params.Cwd != "" {
		s.cwd = params.Cwd
	}

	for _, opt := range params.ConfigOptions {
		if idOpt, ok := opt.As[schemav2.InitialSessionConfigOptionID](); ok {
			switch idOpt.ConfigID {
			case "model":
				if s.session != nil {
					s.session.Model = string(idOpt.Value)
				}
			case "thinking_level":
				if s.session != nil {
					s.session.Effort = string(idOpt.Value)
				}
			}
		}
	}

	if a.client != nil {
		stream := acp2.NewSessionStream(a.client, params.SessionID)
		_ = stream.SendCommands(ctx, s.AvailableCommands())
		if !params.ReplayFrom.IsZero() {
			s.mu.Lock()
			hist := slices.Clone(s.history)
			s.mu.Unlock()
			for _, msg := range hist {
				msgID := acp2.GenerateMessageID()
				switch msg.Role {
				case "user":
					_ = stream.SendUserMessage(ctx, msgID, acp2.TextBlock(msg.Content))
				case "assistant":
					_ = stream.SendText(ctx, msgID, msg.Content)
				}
			}
		}
	}

	return &acp2.ResumeSessionResponse{
		AvailableCommands: s.AvailableCommands(),
	}, nil
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

	promptText := extractPromptTextV2(params.Prompt)
	if _, err := a.StartTurn(ctx, params.SessionID, stream, func(turnCtx context.Context, s *acpV2Session) (acp2.StopReason, error) {
		return a.runTurn(turnCtx, stream, s, promptText)
	}); err != nil {
		return nil, err
	}

	return &acp2.PromptResponse{MessageID: userMessageID}, nil
}

func extractPromptTextV2(blocks []acp2.ContentBlock) string {
	var texts []string
	var resources []string
	for _, block := range blocks {
		if txt, ok := block.As[schemav2.ContentBlockText](); ok {
			texts = append(texts, txt.Text)
		} else if res, ok := block.As[schemav2.ContentBlockResource](); ok {
			if tr, err := res.Resource.As[schemav2.TextResourceContents](); err == nil {
				uri := tr.URI
				path := uri
				if strings.HasPrefix(path, "file://") {
					path = strings.TrimPrefix(path, "file://")
				}
				header := uri
				if path != "" {
					header = path + " (editor contents)"
				}
				resources = append(resources, fmt.Sprintf("\n[Attached Resource: %s]\n%s", header, tr.Text))
			}
		}
	}
	prompt := strings.Join(texts, "\n")
	if len(resources) > 0 {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += strings.Join(resources, "\n")
	}
	return prompt
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

	var origPromptUser func(context.Context, backend.ToolCall) (bool, error)
	if pr, ok := s.session.Runner.(*toolrunner.PermissionRunner); ok && pr != nil {
		origPromptUser = pr.PromptUser
		pr.PromptUser = func(callCtx context.Context, call backend.ToolCall) (bool, error) {
			title := call.Name
			if target := toolrunner.ToolTarget(call); target != "" {
				title = fmt.Sprintf("%s: %s", call.Name, target)
			}
			_, allowed, err := stream.RequestPermission(callCtx, title, acp2.RequestPermissionSubject{}, acp2.DefaultPermissionOptions()...)
			return allowed, err
		}
		defer func() {
			pr.PromptUser = origPromptUser
		}()
	}

	frames, err := engine.Turn(ctx, turn)
	if err != nil {
		return acp2.StopReasonRefusal, err
	}

	_ = stream.SendCommands(ctx, s.AvailableCommands())

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

// CancelSession implements acp2.Agent, stopping background tasks and subagents.
func (a *ACPV2Agent) CancelSession(ctx context.Context, params *acp2.CancelSessionNotification) error {
	stopSessionTasks(string(params.SessionID))
	return a.SessionManager.CancelSession(ctx, params)
}

// CloseSession implements acp2.Agent, stopping background tasks and subagents.
func (a *ACPV2Agent) CloseSession(ctx context.Context, params *acp2.CloseSessionRequest) (*acp2.CloseSessionResponse, error) {
	stopSessionTasks(string(params.SessionID))
	return a.SessionManager.CloseSession(ctx, params)
}
