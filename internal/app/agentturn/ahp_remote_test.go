package agentturn

import (
	"context"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/microsoft/agent-host-protocol/clients/go/ahptypes"
)

// TestAHPRemote_InterfacePins asserts that AHPRemote implements all expected interfaces.
func TestAHPRemote_InterfacePins(t *testing.T) {
	r := &AHPRemote{
		defaultModel: "gemini-3.8-flash",
		models: []ModelInfo{
			{ID: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash"},
			{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6"},
		},
		sessionURI: "ahp-session:/test-sess",
		chatURI:    "ahp-chat:/test-chat",
		frames:     make(chan agentlink.Frame, 64),
		doneCh:     make(chan struct{}),
	}

	var eng Engine = r
	if eng == nil {
		t.Fatal("AHPRemote must implement Engine")
	}

	var dm DefaultModeler = r
	if got := dm.DefaultModel(); got != "gemini-3.8-flash" {
		t.Errorf("DefaultModel() = %q, want gemini-3.8-flash", got)
	}

	var ml ModelLister = r
	models, err := ml.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("ListModels returned %d models, want 2", len(models))
	}

	var cp CapabilityProvider = r
	if name := cp.EngineName(); name != "ahp" {
		t.Errorf("EngineName() = %q, want ahp", name)
	}
	if caps := cp.Capabilities(); caps != CapModelSwitch {
		t.Errorf("Capabilities() = %v, want CapModelSwitch", caps)
	}

	var ah ActionHandler = r
	res, err := ah.ExecuteAction(context.Background(), "model", "claude-sonnet-4-6")
	if err != nil || !res.Success {
		t.Fatalf("ExecuteAction model failed: %v, res: %+v", err, res)
	}
	if r.DefaultModel() != "claude-sonnet-4-6" {
		t.Errorf("expected model switched to claude-sonnet-4-6, got %s", r.DefaultModel())
	}

	var cl CommandLister = r
	if cmds := cl.AvailableCommands(); len(cmds) != 1 || cmds[0].Name != "model" {
		t.Errorf("unexpected AvailableCommands: %+v", cmds)
	}

	var pa PermissionAware = r
	pa.SetPermissionHandler(func(ctx context.Context, req PermissionRequest) (PermissionDecision, error) {
		return PermissionDecision{Approved: true}, nil
	})

	if r.SessionURI() != "ahp-session:/test-sess" {
		t.Errorf("SessionURI() = %q, want ahp-session:/test-sess", r.SessionURI())
	}
	if r.ChatURI() != "ahp-chat:/test-chat" {
		t.Errorf("ChatURI() = %q, want ahp-chat:/test-chat", r.ChatURI())
	}
}

// TestAHPRemote_ActionTranslation verifies that AHP chat actions are faithfully translated to agentlink frames.
func TestAHPRemote_ActionTranslation(t *testing.T) {
	r := &AHPRemote{
		sessionURI:    "ahp-session:/test-sess",
		chatURI:       "ahp-chat:/test-chat",
		frames:        make(chan agentlink.Frame, 64),
		doneCh:        make(chan struct{}),
		activeTools:   make(map[string]*ahpToolTracker),
		busy:          true,
		currentTurnID: "turn-100",
	}

	// 1. ChatTurnStartedAction -> KindState Working
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatTurnStartedAction{
				Type:   ahptypes.ActionTypeChatTurnStarted,
				TurnId: "turn-100",
			},
		},
	})
	f1 := <-r.frames
	if f1.Kind != agentlink.KindState || f1.Phase != agentlink.PhaseWorking {
		t.Errorf("expected KindState working, got %+v", f1)
	}

	// 2. ChatResponsePartAction (Reasoning) -> KindThought
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatResponsePartAction{
				Type:   ahptypes.ActionTypeChatResponsePart,
				TurnId: "turn-100",
				Part: ahptypes.ResponsePart{
					Value: &ahptypes.ReasoningResponsePart{
						Id:      "part-1",
						Content: "Thinking deeply...",
					},
				},
			},
		},
	})
	f2 := <-r.frames
	if f2.Kind != agentlink.KindThought || f2.Text != "Thinking deeply..." {
		t.Errorf("expected KindThought, got %+v", f2)
	}

	// 3. ChatReasoningAction -> KindThought delta
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatReasoningAction{
				Type:    ahptypes.ActionTypeChatReasoning,
				TurnId:  "turn-100",
				Content: " Still thinking.",
			},
		},
	})
	f3 := <-r.frames
	if f3.Kind != agentlink.KindThought || f3.Text != " Still thinking." {
		t.Errorf("expected KindThought delta, got %+v", f3)
	}

	// 4. ChatDeltaAction -> KindText delta
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatDeltaAction{
				Type:    ahptypes.ActionTypeChatDelta,
				TurnId:  "turn-100",
				Content: "Hello world!",
			},
		},
	})
	f4 := <-r.frames
	if f4.Kind != agentlink.KindText || f4.Text != "Hello world!" {
		t.Errorf("expected KindText delta, got %+v", f4)
	}

	// 5. ChatToolCallStartAction -> registers tool without premature emission
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatToolCallStartAction{
				Type:        ahptypes.ActionTypeChatToolCallStart,
				TurnId:      "turn-100",
				ToolCallId:  "call-1",
				ToolName:    "read_file",
				DisplayName: "Read File",
			},
		},
	})
	select {
	case unexpected := <-r.frames:
		t.Fatalf("unexpected frame after ChatToolCallStartAction: %+v", unexpected)
	default:
	}

	// 6. ChatToolCallReadyAction -> single KindTool Start with Args
	rawInput := `{"path":"main.go"}`
	confirmed := ahptypes.ToolCallConfirmationReasonNotNeeded
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatToolCallReadyAction{
				Type:       ahptypes.ActionTypeChatToolCallReady,
				TurnId:     "turn-100",
				ToolCallId: "call-1",
				ToolInput:  &ahptypes.ToolInput{Inline: &rawInput},
				Confirmed:  &confirmed,
			},
		},
	})
	f5 := <-r.frames
	if f5.Kind != agentlink.KindTool || f5.Tool.Phase != agentlink.ToolStart || f5.Tool.Name != "Read File" || f5.Tool.Args != rawInput {
		t.Errorf("expected KindTool Start with args, got %+v", f5)
	}

	// 7. ChatToolCallCompleteAction -> KindTool End with Result
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatToolCallCompleteAction{
				Type:       ahptypes.ActionTypeChatToolCallComplete,
				TurnId:     "turn-100",
				ToolCallId: "call-1",
				Result: ahptypes.ToolCallResult{
					Success:          true,
					PastTenseMessage: ahptypes.NewStringOrMarkdownPlain("File content read successfully"),
				},
			},
		},
	})
	f6 := <-r.frames
	if f6.Kind != agentlink.KindTool || f6.Tool.Phase != agentlink.ToolEnd || f6.Tool.Result != "File content read successfully" || f6.Tool.Err {
		t.Errorf("expected KindTool End, got %+v", f6)
	}

	// 8. ChatUsageAction -> KindState with Usage
	inTok := int64(150)
	outTok := int64(45)
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatUsageAction{
				Type:   ahptypes.ActionTypeChatUsage,
				TurnId: "turn-100",
				Usage: ahptypes.UsageInfo{
					InputTokens:  &inTok,
					OutputTokens: &outTok,
				},
			},
		},
	})
	f7 := <-r.frames
	if f7.Usage == nil || f7.Usage.PromptTokens != 150 || f7.Usage.CompletionTokens != 45 || f7.Usage.TotalTokens != 195 {
		t.Errorf("expected Usage frame, got %+v", f7)
	}

	// 9. ChatTurnCompleteAction -> KindResult with lastUsage
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatTurnCompleteAction{
				Type:   ahptypes.ActionTypeChatTurnComplete,
				TurnId: "turn-100",
			},
		},
	})
	f8 := <-r.frames
	if f8.Kind != agentlink.KindResult || f8.Usage == nil || f8.Usage.TotalTokens != 195 {
		t.Errorf("expected KindResult with usage, got %+v", f8)
	}
}

// TestAHPRemote_ActionErrorAndCancel verifies error and cancel action translations.
func TestAHPRemote_ActionErrorAndCancel(t *testing.T) {
	r := &AHPRemote{
		frames:        make(chan agentlink.Frame, 64),
		doneCh:        make(chan struct{}),
		activeTools:   make(map[string]*ahpToolTracker),
		busy:          true,
		currentTurnID: "turn-err",
	}

	// ChatErrorAction -> KindResult Error
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatErrorAction{
				Type:   ahptypes.ActionTypeChatError,
				TurnId: "turn-err",
				Part: ahptypes.ErrorResponsePart{
					Error: ahptypes.ErrorInfo{
						Message: "rate limit exceeded",
					},
				},
			},
		},
	})
	f1 := <-r.frames
	if f1.Kind != agentlink.KindResult || f1.Error != "rate limit exceeded" {
		t.Errorf("expected error result frame, got %+v", f1)
	}

	// ChatTurnCancelledAction -> KindResult "turn cancelled"
	r.currentTurnID = "turn-cancel"
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatTurnCancelledAction{
				Type:   ahptypes.ActionTypeChatTurnCancelled,
				TurnId: "turn-cancel",
			},
		},
	})
	f2 := <-r.frames
	if f2.Kind != agentlink.KindResult || f2.Error != "turn cancelled" {
		t.Errorf("expected turn cancelled result frame, got %+v", f2)
	}
}

// TestAHPRemote_IgnoresStaleOrOtherTurnActions verifies actions for mismatching turn IDs are dropped.
func TestAHPRemote_IgnoresStaleOrOtherTurnActions(t *testing.T) {
	r := &AHPRemote{
		frames:        make(chan agentlink.Frame, 64),
		doneCh:        make(chan struct{}),
		busy:          true,
		currentTurnID: "active-turn",
	}

	// Action for different turn ID
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatDeltaAction{
				Type:    ahptypes.ActionTypeChatDelta,
				TurnId:  "old-turn",
				Content: "Late text from previous turn",
			},
		},
	})

	select {
	case f := <-r.frames:
		t.Fatalf("unexpected frame received for mismatching turn: %+v", f)
	case <-time.After(50 * time.Millisecond):
		// Success, nothing sent
	}
}

// TestAHPRemote_TurnPreconditions checks busy and closed state rejections.
func TestAHPRemote_TurnPreconditions(t *testing.T) {
	r := &AHPRemote{
		closed: true,
	}
	_, err := r.Turn(context.Background(), Turn{})
	if err == nil || err.Error() != "agentturn: the engine is closed" {
		t.Errorf("expected closed error, got %v", err)
	}

	r2 := &AHPRemote{
		busy: true,
	}
	_, err = r2.Turn(context.Background(), Turn{})
	if err == nil || err.Error() != "agentturn: a turn is already running" {
		t.Errorf("expected busy error, got %v", err)
	}
}

// TestAHPRemote_ClosePins verifies multiple Close calls and connectionError fallback.
func TestAHPRemote_ClosePins(t *testing.T) {
	r := &AHPRemote{
		frames: make(chan agentlink.Frame, 64),
		doneCh: make(chan struct{}),
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	// Calling Close again should be a no-op
	if err := r.Close(); err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
	if msg := r.connectionError(); msg != "agentturn: the connection closed" {
		t.Errorf("connectionError() = %q, want agentturn: the connection closed", msg)
	}
}

// TestAHPRemote_ToolConfirmation verifies that unconfirmed tool calls trigger the permission handler.
func TestAHPRemote_ToolConfirmation(t *testing.T) {
	r := &AHPRemote{
		frames:        make(chan agentlink.Frame, 64),
		doneCh:        make(chan struct{}),
		activeTools:   make(map[string]*ahpToolTracker),
		busy:          true,
		currentTurnID: "turn-perm",
		sessionURI:    "ahp-session:/sess-perm",
		chatURI:       "ahp-chat:/chat-perm",
	}

	handlerCalled := make(chan PermissionRequest, 1)
	r.SetPermissionHandler(func(ctx context.Context, req PermissionRequest) (PermissionDecision, error) {
		handlerCalled <- req
		return PermissionDecision{Approved: true}, nil
	})

	rawInput := `{"cmd":"ls"}`
	confTitle := ahptypes.NewStringOrMarkdownPlain("Execute ls command")
	r.handleAction(ahptypes.ActionEnvelope{
		Action: ahptypes.StateAction{
			Value: &ahptypes.ChatToolCallReadyAction{
				Type:              ahptypes.ActionTypeChatToolCallReady,
				TurnId:            "turn-perm",
				ToolCallId:        "tool-perm-1",
				ToolInput:         &ahptypes.ToolInput{Inline: &rawInput},
				ConfirmationTitle: &confTitle,
				Confirmed:         nil, // needs confirmation
				Options: []ahptypes.ConfirmationOption{
					{Id: "opt-allow", Label: "Allow", Kind: ahptypes.ConfirmationOptionKindApprove},
					{Id: "opt-deny", Label: "Deny", Kind: ahptypes.ConfirmationOptionKindDeny},
				},
			},
		},
	})

	select {
	case req := <-handlerCalled:
		if req.ToolCallID != "tool-perm-1" {
			t.Errorf("expected toolCallId tool-perm-1, got %s", req.ToolCallID)
		}
		if req.Title != "Execute ls command" {
			t.Errorf("expected title 'Execute ls command', got %s", req.Title)
		}
		if req.RawInput != rawInput {
			t.Errorf("expected raw input %s, got %s", rawInput, req.RawInput)
		}
		if len(req.Options) != 2 {
			t.Errorf("expected 2 options, got %d", len(req.Options))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission handler to be called")
	}

	// ToolStart frame should also have been emitted
	f := <-r.frames
	if f.Kind != agentlink.KindTool || f.Tool.Phase != agentlink.ToolStart {
		t.Errorf("expected KindTool ToolStart frame, got %+v", f)
	}
}
