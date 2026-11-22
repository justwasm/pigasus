package voice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
)

func TestThinkerBridge_DispatchSuccess(t *testing.T) {
	mockProv := actor.NewMockProvider()
	mockProv.EnqueueChunks([]actor.StreamChunk{
		{
			ToolCalls: []actor.ToolCall{
				{ID: "tc-1", Name: "read_file", Input: `{"path": "/app/main.go"}`},
			},
		},
	})
	mockProv.EnqueueChunks([]actor.StreamChunk{
		{Delta: "Refactoring completed successfully.", FinishReason: "stop"},
	})

	tool := actor.NewFuncTool("read_file", "reads file", func(ctx context.Context, input string) (string, error) {
		return "package main\nfunc main() {}", nil
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mockProv,
			Tools:    []actor.Tool{tool},
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	bridge := NewThinkerBridge(act)
	mockBackend := NewMockLiveBackend()
	sess, err := mockBackend.CreateSession(context.Background(), LiveSessionConfig{Voice: "Puck"})
	if err != nil {
		t.Fatalf("failed to create live session: %v", err)
	}
	defer sess.Close()

	err = bridge.Dispatch(context.Background(), sess, "inv-100", "Please inspect main.go")
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	// Wait for background turn completion
	deadline := time.Now().Add(2 * time.Second)
	mockSess := sess.(*MockLiveSession)
	for time.Now().Before(deadline) {
		if len(mockSess.InAnswers()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	answers := mockSess.InAnswers()
	if len(answers) == 0 {
		t.Fatalf("expected thinker answer to be sent to live session")
	}
	if !strings.Contains(answers[0], "Refactoring completed successfully.") {
		t.Errorf("unexpected answer: %q", answers[0])
	}

	notes := mockSess.InNotes()
	if len(notes) == 0 {
		t.Errorf("expected thinker progress notes to be sent to live session")
	} else if notes[0] != "Reading file main.go" {
		t.Errorf("expected canonical semantic note 'Reading file main.go', got: %q", notes[0])
	}

	toolResponses := mockSess.ToolResponses()
	if _, ok := toolResponses["inv-100"]; !ok {
		t.Errorf("expected tool response for inv-100, got: %+v", toolResponses)
	}
}

func TestThinkerBridge_NilActor(t *testing.T) {
	bridge := NewThinkerBridge(nil)
	err := bridge.Dispatch(context.Background(), nil, "inv-1", "test")
	if err == nil {
		t.Fatal("expected error with nil actor")
	}
}

func TestHumanizeTool(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		input    string
		expected string
	}{
		{
			name:     "read_file with path",
			tool:     "read_file",
			input:    `{"path": "/app/main.go"}`,
			expected: "Reading file main.go",
		},
		{
			name:     "read_file without path",
			tool:     "read_file",
			input:    `{}`,
			expected: "Reading code file",
		},
		{
			name:     "edit_file",
			tool:     "edit_file",
			input:    `{"path": "internal/auth.go"}`,
			expected: "Editing file auth.go",
		},
		{
			name:     "go test",
			tool:     "run_command",
			input:    `{"CommandLine": "go test ./..."}`,
			expected: "Running unit tests",
		},
		{
			name:     "make build",
			tool:     "run_command",
			input:    `{"CommandLine": "make build"}`,
			expected: "Building project",
		},
		{
			name:     "git diff",
			tool:     "run_command",
			input:    `{"CommandLine": "git diff HEAD~1"}`,
			expected: "Checking git diff",
		},
		{
			name:     "git status",
			tool:     "run_command",
			input:    `{"CommandLine": "git status"}`,
			expected: "Checking git status",
		},
		{
			name:     "arbitrary command",
			tool:     "run_command",
			input:    `{"CommandLine": "docker-compose up -d"}`,
			expected: "Running command: docker-compose up -d",
		},
		{
			name:     "search_web",
			tool:     "search_web",
			input:    `{"query": "gemini live api"}`,
			expected: "Searching web for: gemini live api",
		},
		{
			name:     "list_dir",
			tool:     "list_dir",
			input:    `{}`,
			expected: "Listing directory contents",
		},
		{
			name:     "invoke_subagent",
			tool:     "invoke_subagent",
			input:    `{}`,
			expected: "Dispatching specialized subagent",
		},
		{
			name:     "unknown tool",
			tool:     "custom_tool",
			input:    `{}`,
			expected: "Calling tool: custom_tool",
		},
		{
			name:     "empty tool name",
			tool:     "",
			input:    `{}`,
			expected: "Processing background task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HumanizeTool(tt.tool, tt.input)
			if got != tt.expected {
				t.Errorf("HumanizeTool(%q, %q) = %q, want %q", tt.tool, tt.input, got, tt.expected)
			}
		})
	}
}

func TestNoteThrottler(t *testing.T) {
	throttler := NewNoteThrottler(500 * time.Millisecond)
	baseTime := time.Now()

	// First note should pass
	if !throttler.ShouldSend(baseTime, "note 1", false) {
		t.Errorf("expected first note to pass")
	}

	// Repeated duplicate note should be suppressed even after time passes
	if throttler.ShouldSend(baseTime.Add(1*time.Second), "note 1", false) {
		t.Errorf("expected duplicate note to be suppressed")
	}

	// Rapid distinct note within interval should be suppressed
	if throttler.ShouldSend(baseTime.Add(200*time.Millisecond), "note 2", false) {
		t.Errorf("expected note within minInterval to be throttled")
	}

	// Distinct note after interval should pass
	if !throttler.ShouldSend(baseTime.Add(600*time.Millisecond), "note 2", false) {
		t.Errorf("expected distinct note after minInterval to pass")
	}

	// Force note should always pass even if within interval
	if !throttler.ShouldSend(baseTime.Add(650*time.Millisecond), "critical milestone", true) {
		t.Errorf("expected force note to bypass throttling")
	}
}

func TestNoteThrottler_DefaultInterval(t *testing.T) {
	t.Setenv("OPENAGY_VOICE_NOTE_INTERVAL", "")
	t.Setenv("GEMINI_VOICE_NOTE_INTERVAL", "")
	throttler := NewNoteThrottler(0)
	if throttler.minInterval != 5*time.Second {
		t.Fatalf("expected default minInterval 5s, got %v", throttler.minInterval)
	}

	t.Setenv("OPENAGY_VOICE_NOTE_INTERVAL", "8s")
	throttlerEnv := NewNoteThrottler(0)
	if throttlerEnv.minInterval != 8*time.Second {
		t.Fatalf("expected env minInterval 8s, got %v", throttlerEnv.minInterval)
	}
}
