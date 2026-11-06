package tui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
)

func TestSlashForkCancelsActiveGoal(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", tmpDir)

	m := &runMode{
		cwd:            tmpDir,
		conversationID: "src-conv-123",
		activeGoal:     convstore.NewGoalState("test goal", 0),
		session: []backend.Message{
			{Role: "user", Content: "initial prompt"},
			{Role: "assistant", Content: "initial response"},
		},
	}

	if m.activeGoal == nil {
		t.Fatal("expected activeGoal to be non-nil before fork")
	}

	handled, cmd := m.slashFork("")
	if !handled || cmd != nil {
		t.Errorf("slashFork: handled=%v, cmd=%v", handled, cmd)
	}

	// Active goal must be cancelled/cleared upon fork
	if m.activeGoal != nil {
		t.Errorf("expected activeGoal to be nil after fork, got %+v", m.activeGoal)
	}

	// New conversation ID should be different from original
	if m.conversationID == "src-conv-123" || m.conversationID == "" {
		t.Errorf("m.conversationID after fork = %q, want new UUID", m.conversationID)
	}

	// Check that a fork notice was appended to the session
	lastMsg := m.session[len(m.session)-1]
	if !strings.Contains(lastMsg.Content, "<fork_notice>") {
		t.Errorf("expected last session message to contain <fork_notice>, got %q", lastMsg.Content)
	}
	if !strings.Contains(lastMsg.Content, "src-conv-123") {
		t.Errorf("expected fork_notice to reference source ID src-conv-123")
	}
}

func TestSlashForkWithProjectID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", tmpDir)

	m := &runMode{
		cwd:            tmpDir,
		conversationID: "source-conv-456",
		session: []backend.Message{
			{Role: "user", Content: "task prompt"},
			{Role: "assistant", Content: "task response"},
		},
	}

	handled, _ := m.slashFork("my-special-project")
	if !handled {
		t.Fatal("slashFork returned unhandled")
	}

	// Verify response transcript contains project ID
	foundProjectMsg := false
	for _, line := range m.response {
		if strings.Contains(line, "my-special-project") {
			foundProjectMsg = true
			break
		}
	}
	if !foundProjectMsg {
		t.Errorf("expected response to mention project ID 'my-special-project', got: %v", m.response)
	}
}
