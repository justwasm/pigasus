package agentserver

import (
	"context"
	"errors"
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
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/interaction"
)

type acpV2Session struct {
	cwd            acp2.AbsolutePath
	session        *Session
	history        []agentlink.Message
	commands       []acp2.AvailableCommand
	pendingPrompts []*pendingACPPrompt
	activeStream   *acp2.SessionStream
	mu             sync.Mutex
}

func (s *acpV2Session) setStream(st *acp2.SessionStream) {
	if s == nil || st == nil {
		return
	}
	s.mu.Lock()
	s.activeStream = st
	s.mu.Unlock()
}

func (s *acpV2Session) getStream(fallback *acp2.SessionStream) *acp2.SessionStream {
	if s == nil {
		return fallback
	}
	s.mu.Lock()
	st := s.activeStream
	s.mu.Unlock()
	if st != nil {
		return st
	}
	return fallback
}

func (s *acpV2Session) setPendingPrompt(p *pendingACPPrompt) {
	if s == nil || p == nil {
		return
	}
	s.mu.Lock()
	s.pendingPrompts = append(s.pendingPrompts, p)
	s.mu.Unlock()
}

func (s *acpV2Session) clearPendingPrompt(p *pendingACPPrompt) {
	if s == nil || p == nil {
		return
	}
	s.mu.Lock()
	for i, cur := range s.pendingPrompts {
		if cur == p {
			s.pendingPrompts = append(s.pendingPrompts[:i], s.pendingPrompts[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
}

func (s *acpV2Session) getPendingPrompts() []*pendingACPPrompt {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.pendingPrompts)
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
		if !hasPersistedSession(string(params.SessionID)) {
			return nil, acp.SessionNotFound(params.SessionID)
		}
		cwd := string(params.Cwd)
		if cwd == "" && a.srv != nil {
			cwd = a.srv.cfg.Workspace
		}
		var sess *Session
		var sErr error
		if a.srv != nil {
			sess, sErr = a.srv.sessionForID(string(params.SessionID), cwd, a.srv.cfg.Model)
			if sErr != nil {
				return nil, sErr
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
		s.setStream(stream)
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
		if pending := s.getPendingPrompts(); len(pending) > 0 {
			reemit := func(c context.Context) {
				for _, p := range pending {
					dispatchPendingV2Prompt(c, stream, p)
				}
			}
			if !acp.AfterReply(ctx, reemit) {
				go reemit(context.Background())
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

	var cfg Config
	if a.srv != nil {
		cfg = a.srv.cfg
	}
	parsed := parseACPPromptV2(params.Prompt, cfg)
	if _, err := a.StartTurn(context.WithoutCancel(ctx), params.SessionID, stream, func(turnCtx context.Context, s *acpV2Session) (acp2.StopReason, error) {
		s.setStream(stream)
		return a.runTurn(turnCtx, stream, s, parsed)
	}); err != nil {
		return nil, err
	}

	return &acp2.PromptResponse{MessageID: userMessageID}, nil
}

func extractPromptTextV2(blocks []acp2.ContentBlock) string {
	return parseACPPromptV2(blocks, Config{}).ModelPrompt
}

func dispatchPendingV2Prompt(ctx context.Context, stream *acp2.SessionStream, p *pendingACPPrompt) {
	if stream == nil || p == nil {
		return
	}
	var opts []acp2.PermissionOption
	if len(p.options) > 0 {
		for i, opt := range p.options {
			opts = append(opts, acp2.PermissionOption{
				OptionID: acp2.PermissionOptionID(fmt.Sprintf("opt_%d", i)),
				Name:     opt,
				Kind:     acp2.PermissionOptionKindAllowOnce,
			})
		}
		opts = append(opts, acp2.PermissionOption{
			OptionID: "reject",
			Name:     "Skip / Cancel",
			Kind:     acp2.PermissionOptionKindRejectOnce,
		})
	} else {
		opts = acp2.DefaultPermissionOptions()
	}
	choice, allowed, err := stream.RequestPermission(ctx, p.title, acp2.RequestPermissionSubject{}, opts...)
	if err != nil {
		if ctx.Err() == nil && (isTransportClosedErr(err) || errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "closed")) {
			return
		}
		p.resolveErr(err)
		return
	}
	p.resolve(allowed, choice.Name)
}

type acpV2InteractionHost struct {
	sess   *acpV2Session
	stream *acp2.SessionStream
}

func (h *acpV2InteractionHost) Ask(ctx context.Context, req interaction.Request) (interaction.Answer, error) {
	if req.Kind == interaction.KindQuestion && len(req.Questions) > 0 {
		answers := make([]string, 0, len(req.Questions))
		for _, q := range req.Questions {
			p := &pendingACPPrompt{
				id:        fmt.Sprintf("ask_%d", time.Now().UnixNano()),
				title:     q.Prompt,
				kind:      req.Kind,
				questions: []interaction.Question{q},
				options:   q.Options,
				replyCh:   make(chan pendingACPReply, 1),
			}
			h.sess.setPendingPrompt(p)
			go dispatchPendingV2Prompt(ctx, h.sess.getStream(h.stream), p)

			var rep pendingACPReply
			select {
			case <-ctx.Done():
				h.sess.clearPendingPrompt(p)
				return interaction.Answer{Cancelled: true}, ctx.Err()
			case rep = <-p.replyCh:
				h.sess.clearPendingPrompt(p)
			}
			if rep.err != nil {
				return interaction.Answer{Cancelled: true}, rep.err
			}
			if !rep.allowed {
				return interaction.Answer{Cancelled: true}, nil
			}
			ans := rep.choice
			if ans == "" && len(q.Options) > 0 {
				ans = q.Options[0]
			}
			answers = append(answers, ans)
		}
		return interaction.Answer{Answers: answers, Text: strings.Join(answers, "\n")}, nil
	}

	title := req.ToolName
	if req.Action != "" && req.Target != "" {
		title = fmt.Sprintf("%s: %s", req.Action, req.Target)
	} else if req.Reason != "" {
		title = req.Reason
	}
	p := &pendingACPPrompt{
		id:      fmt.Sprintf("ask_%d", time.Now().UnixNano()),
		title:   title,
		kind:    req.Kind,
		replyCh: make(chan pendingACPReply, 1),
	}
	h.sess.setPendingPrompt(p)
	defer h.sess.clearPendingPrompt(p)

	go dispatchPendingV2Prompt(ctx, h.sess.getStream(h.stream), p)

	select {
	case <-ctx.Done():
		return interaction.Answer{Cancelled: true}, ctx.Err()
	case rep := <-p.replyCh:
		if rep.err != nil {
			return interaction.Answer{Cancelled: true}, rep.err
		}
		if !rep.allowed {
			return interaction.Answer{Cancelled: true}, nil
		}
		return interaction.Answer{Allowed: true, Text: rep.choice}, nil
	}
}

func (a *ACPV2Agent) runTurn(ctx context.Context, stream *acp2.SessionStream, s *acpV2Session, parsed parsedACPPrompt) (acp2.StopReason, error) {
	if parsed.Command == "clear" && parsed.CommandArgs == "" {
		s.mu.Lock()
		s.history = nil
		s.mu.Unlock()
	}
	if parsed.Command == "tasks" && parsed.CommandArgs == "" {
		summary := formatTasksSummary()
		agentMsgID := acp2.GenerateMessageID()
		_ = stream.SendText(ctx, agentMsgID, summary)
		return acp2.StopReasonEndTurn, nil
	}
	if parsed.Command == "agents" && parsed.CommandArgs == "" {
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
		Content: parsed.ModelPrompt,
	})
	s.mu.Unlock()

	turnCtx := interaction.WithHost(ctx, &acpV2InteractionHost{sess: s, stream: stream})

	var origPromptUser func(context.Context, backend.ToolCall) (bool, error)
	if pr, ok := s.session.Runner.(*toolrunner.PermissionRunner); ok && pr != nil {
		origPromptUser = pr.PromptUser
		pr.PromptUser = func(callCtx context.Context, call backend.ToolCall) (bool, error) {
			callID := call.ID
			if callID == "" {
				callID = fmt.Sprintf("call_%s_%d", call.Name, time.Now().UnixNano())
			}
			title := call.Name
			if target := toolrunner.ToolTarget(call); target != "" {
				title = fmt.Sprintf("%s: %s", call.Name, target)
			}
			p := &pendingACPPrompt{
				id:      callID,
				title:   title,
				kind:    interaction.KindPermission,
				replyCh: make(chan pendingACPReply, 1),
			}
			s.setPendingPrompt(p)
			defer s.clearPendingPrompt(p)

			go dispatchPendingV2Prompt(callCtx, s.getStream(stream), p)

			select {
			case <-callCtx.Done():
				return false, callCtx.Err()
			case rep := <-p.replyCh:
				return rep.allowed, rep.err
			}
		}
		defer func() {
			pr.PromptUser = origPromptUser
		}()
	}

	_ = stream.SendCommands(turnCtx, s.AvailableCommands())

	var baseSubTotal int
	if s.session.SubagentRegistry != nil {
		_, _, baseSubTotal, _ = s.session.SubagentRegistry.TotalUsage()
	}

	agentMsgID := acp2.GenerateMessageID()
	stopReason := acp2.StopReasonEndTurn
	var totalTokens uint64
	var allAssistantText strings.Builder

	runSingleTurn := func(hist []agentlink.Message) (string, bool, error) {
		turn := agentturn.Turn{
			Model:   s.session.Model,
			History: hist,
		}
		frames, err := engine.Turn(turnCtx, turn)
		if err != nil {
			return "", false, err
		}

		inProgressTools := make(map[string]string)
		var turnAssistant strings.Builder
		var turnTotal uint64
		var turnFailed bool

		for frame := range frames {
			curStream := s.getStream(stream)
			switch frame.Kind {
			case agentlink.KindText:
				turnAssistant.WriteString(frame.Text)
				allAssistantText.WriteString(frame.Text)
				_ = curStream.SendText(turnCtx, agentMsgID, frame.Text)
			case agentlink.KindThought:
				_ = curStream.SendThought(turnCtx, agentMsgID, frame.Text)
			case agentlink.KindTool:
				if frame.Tool != nil {
					callID := acp2.ToolCallID(frame.ID)
					if callID == "" {
						callID = acp2.ToolCallID(fmt.Sprintf("call_%s", frame.Tool.Name))
					}
					if frame.Tool.Phase == agentlink.ToolStart {
						inProgressTools[string(callID)] = frame.Tool.Name
						var opts []acp2.ToolCallOption
						if rawInput := rawJSONOrQuoted(frame.Tool.Args); rawInput != nil {
							opts = append(opts, acp2.WithRawInput(rawInput))
						}
						_ = curStream.StartToolCall(turnCtx, callID, frame.Tool.Name, acp2.ToolKindExecute, opts...)
					} else if frame.Tool.Phase == agentlink.ToolEnd {
						delete(inProgressTools, string(callID))
						failed := frame.Tool.Err || tools.IsFailureText(frame.Tool.Result)
						resultText := frame.Tool.Result
						if failed && resultText == "" {
							resultText = frame.Error
							if resultText == "" {
								resultText = "Tool execution failed"
							}
						}
						var opts []acp2.ToolCallOption
						if rawOutput := rawJSONOrQuoted(resultText); rawOutput != nil {
							opts = append(opts, acp2.WithRawOutput(rawOutput))
						}
						if resultText != "" {
							opts = append(opts, acp2.WithToolContent(acp2.ToolText(resultText)))
						}
						if failed {
							_ = curStream.FailToolCall(turnCtx, callID, opts...)
						} else {
							_ = curStream.CompleteToolCall(turnCtx, callID, opts...)
						}
					}
				}
			case agentlink.KindResult:
				if frame.Error != "" {
					turnFailed = true
					stopReason = acp2.StopReasonRefusal
				}
				if frame.Usage != nil {
					t := frame.Usage.TotalTokens
					if t == 0 {
						t = frame.Usage.PromptTokens + frame.Usage.CompletionTokens
					}
					turnTotal = uint64(t)
				}
			}
		}

		if len(inProgressTools) > 0 {
			failCtx, cancel := context.WithTimeout(context.WithoutCancel(turnCtx), 2*time.Second)
			curStream := s.getStream(stream)
			for id := range inProgressTools {
				var opts []acp2.ToolCallOption
				if rawOutput := rawJSONOrQuoted("Cancelled"); rawOutput != nil {
					opts = append(opts, acp2.WithRawOutput(rawOutput))
				}
				opts = append(opts, acp2.WithToolContent(acp2.ToolText("Cancelled")))
				_ = curStream.FailToolCall(failCtx, acp2.ToolCallID(id), opts...)
			}
			cancel()
		}

		totalTokens += turnTotal
		return turnAssistant.String(), turnFailed, nil
	}

	firstReply, failed, err := runSingleTurn(turnHistory)
	if err != nil {
		return acp2.StopReasonRefusal, err
	}
	if firstReply != "" {
		turnHistory = append(turnHistory, agentlink.Message{Role: "assistant", Content: firstReply})
	}

	if !failed && !acp.TurnCancelled(turnCtx) && turnCtx.Err() == nil {
		for {
			if acp.TurnCancelled(turnCtx) || turnCtx.Err() != nil {
				stopReason = acp2.StopReasonCancelled
				break
			}
			if s.session.TaskInbox != nil && s.session.TaskInbox.Len() > 0 {
				items := s.session.TaskInbox.Drain()
				var parts []string
				for _, item := range items {
					parts = append(parts, agentturn.RenderTaskNotification(item))
				}
				turnHistory = append(turnHistory, agentlink.Message{
					Role:    "user",
					Content: strings.Join(parts, "\n\n"),
				})
				reply, f, tErr := runSingleTurn(turnHistory)
				if tErr != nil {
					stopReason = acp2.StopReasonRefusal
					break
				}
				if reply != "" {
					turnHistory = append(turnHistory, agentlink.Message{Role: "assistant", Content: reply})
				}
				if f {
					break
				}
				continue
			}
			if !hasNonDaemonBackgroundWork(s.session) {
				break
			}
			ticker := time.NewTicker(50 * time.Millisecond)
			select {
			case <-turnCtx.Done():
				ticker.Stop()
			case <-s.session.notifyWake:
				ticker.Stop()
			case <-ticker.C:
				ticker.Stop()
			}
		}
	}

	if s.session.SubagentRegistry != nil {
		_, _, curSubTotal, _ := s.session.SubagentRegistry.TotalUsage()
		if curSubTotal > baseSubTotal {
			totalTokens += uint64(curSubTotal - baseSubTotal)
		}
	}

	if totalTokens > 0 {
		_ = s.getStream(stream).SendUsage(context.WithoutCancel(turnCtx), totalTokens, 1_000_000, nil)
	}

	if acp.TurnCancelled(turnCtx) || turnCtx.Err() != nil {
		stopReason = acp2.StopReasonCancelled
	}

	if stopReason != acp2.StopReasonRefusal && stopReason != acp2.StopReasonCancelled && !(parsed.Command == "clear" && parsed.CommandArgs == "") {
		s.mu.Lock()
		s.history = append(s.history,
			agentlink.Message{Role: "user", Content: parsed.HistoryText},
			agentlink.Message{Role: "assistant", Content: allAssistantText.String()},
		)
		s.mu.Unlock()
	}

	return stopReason, nil
}

// CancelSession implements acp2.Agent, stopping background tasks and subagents.
func (a *ACPV2Agent) CancelSession(ctx context.Context, params *acp2.CancelSessionNotification) error {
	var sess *Session
	if s, err := a.Lookup(ctx, params.SessionID); err == nil && s != nil {
		sess = s.session
	}
	err := a.SessionManager.CancelSession(ctx, params)
	stopSessionWork(string(params.SessionID), sess)
	return err
}

// CloseSession implements acp2.Agent, stopping background tasks and subagents.
func (a *ACPV2Agent) CloseSession(ctx context.Context, params *acp2.CloseSessionRequest) (*acp2.CloseSessionResponse, error) {
	var sess *Session
	if s, err := a.Lookup(ctx, params.SessionID); err == nil && s != nil {
		sess = s.session
	}
	stopSessionWork(string(params.SessionID), sess)
	return a.SessionManager.CloseSession(ctx, params)
}
