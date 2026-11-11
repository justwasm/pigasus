package agentserver

import (
	"context"
	"testing"

	"github.com/ironpark/acp-go/acp1"
)

func TestACPV1_AutoModeAndSessionModesReporter(t *testing.T) {
	srv, err := New(Config{Workspace: t.TempDir(), Model: "gemini-3.8-flash"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	agent := NewACPV1Agent(srv, nil)

	ctx := context.Background()
	sessResp, err := agent.NewSession(ctx, &acp1.NewSessionRequest{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// Verify auto mode is in available modes
	var foundAuto bool
	for _, m := range sessResp.Modes.AvailableModes {
		if m.ID == "auto" {
			foundAuto = true
			break
		}
	}
	if !foundAuto {
		t.Fatalf("expected 'auto' mode in available modes: %+v", sessResp.Modes.AvailableModes)
	}

	// Switch to auto mode
	_, err = agent.SetSessionMode(ctx, &acp1.SetSessionModeRequest{
		SessionID: sessResp.SessionID,
		ModeID:    "auto",
	})
	if err != nil {
		t.Fatalf("SetSessionMode(auto): %v", err)
	}

	// Verify session retains auto mode via SessionModesReporter
	sess, err := agent.Lookup(ctx, sessResp.SessionID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	modes := sess.SessionModes()
	if modes.CurrentModeID != "auto" {
		t.Fatalf("expected CurrentModeID='auto', got %q", modes.CurrentModeID)
	}

	// Test LoadSession preserves mode
	loadResp, err := agent.LoadSession(ctx, &acp1.LoadSessionRequest{
		SessionID: sessResp.SessionID,
	})
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loadResp.Modes == nil || loadResp.Modes.CurrentModeID != "auto" {
		t.Fatalf("LoadSession did not preserve 'auto' mode: %+v", loadResp.Modes)
	}
}
