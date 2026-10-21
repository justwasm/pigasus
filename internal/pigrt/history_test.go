package pigrt

import (
	"testing"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// TestHistoryMessageRoles pins the replay shape: an assistant turn becomes an
// assistant message with text content and a clean stop; anything else is a user
// turn, matching the clone's old backend.Message replay.
func TestHistoryMessageRoles(t *testing.T) {
	assistant, err := historyMessage(HistoryTurn{Role: "assistant", Text: "Paris."})
	if err != nil {
		t.Fatalf("historyMessage: %v", err)
	}
	if assistant.Assistant == nil || assistant.Assistant.Role != "assistant" {
		t.Fatalf("assistant turn = %+v", assistant)
	}
	if len(assistant.Assistant.Content) != 1 {
		t.Fatalf("assistant content = %+v", assistant.Assistant.Content)
	}
	if text, ok := assistant.Assistant.Content[0].(ai.TextContent); !ok || text.Text != "Paris." {
		t.Fatalf("assistant text block = %+v", assistant.Assistant.Content[0])
	}
	if assistant.Assistant.StopReason != ai.StopReasonStop {
		t.Fatalf("assistant stop reason = %q", assistant.Assistant.StopReason)
	}

	for _, role := range []string{"user", "USER", "", "system"} {
		msg, err := historyMessage(HistoryTurn{Role: role, Text: "hi"})
		if err != nil {
			t.Fatalf("historyMessage(%q): %v", role, err)
		}
		if msg.User == nil || msg.User.Role != "user" {
			t.Fatalf("role %q mapped to %+v, want a user turn", role, msg)
		}
	}
}

// TestTurnFailure covers the recovery of a failure PiG encoded in the message
// list rather than returned as a Go error.
func TestTurnFailure(t *testing.T) {
	tests := []struct {
		name string
		msgs []agent.AgentMessage
		want string
	}{
		{name: "empty"},
		{
			name: "clean stop",
			msgs: []agent.AgentMessage{{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonStop}}},
		},
		{
			name: "error",
			msgs: []agent.AgentMessage{{Assistant: &agent.AssistantMessage{
				StopReason:   ai.StopReasonError,
				ErrorMessage: "boom",
			}}},
			want: "boom",
		},
		{
			name: "aborted",
			msgs: []agent.AgentMessage{{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonAborted}}},
			want: "agent: upstream stream failed",
		},
		{
			name: "error then recovered",
			msgs: []agent.AgentMessage{
				{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonError, ErrorMessage: "rate limited"}},
				{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonStop}},
			},
		},
		{
			name: "tool use then clean stop",
			msgs: []agent.AgentMessage{
				{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonToolUse}},
				{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonStop}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := turnFailure(tc.msgs)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("turnFailure = %v, want nil", err)
			case tc.want != "" && err == nil:
				t.Fatalf("turnFailure = nil, want %q", tc.want)
			case tc.want != "" && err.Error() != tc.want:
				t.Fatalf("turnFailure = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}
