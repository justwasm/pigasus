package piagentbridge

import (
	"testing"

	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
)

// The bodies below are the reference's own, copied from the 1.4.8 capture
// (internal/tools/testdata/tool_errors_1.4.8.txt).
const (
	capturedFailure = "Created At: 2026-10-27T23:34:41+08:00\n" +
		"Completed At: 2026-10-27T23:34:41+08:00\n" +
		"Encountered error in tool execution: file does not exist"
	capturedSuccess = "Created At: 2026-10-27T23:40:16+08:00\n" +
		"Completed At: 2026-10-27T23:40:16+08:00\n" +
		"Showing lines 1-2 of 2\n1: hello 1.4.8\n2: second line"
)

func toolResultRequest(t *testing.T, result pi.ToolResultMessage) backend.StreamRequest {
	t.Helper()
	transcript := pi.NormalizeContext(pi.Context{
		SystemPrompt: "be terse",
		Messages: []pi.Message{
			pi.UserMessage{Content: pi.UserText("read it")},
			result,
		},
	})
	return ToStreamRequest(transcript, "gemini-3.8-flash-high", pi.StreamOptions{})
}

// A tool that failed is flagged on the wire, whichever way the failure
// reached the bridge: the runtime's own flag, or the reference's failure
// text (the clone's tools write that text into an ordinary result).
//
// 【一手】 agy 1.4.8 — see internal/tools/testdata/tool_errors_1.4.8.txt.
func TestToStreamRequest_FailedToolCallIsFlagged(t *testing.T) {
	cases := []struct {
		name   string
		result pi.ToolResultMessage
		want   bool
	}{
		{
			name: "the failure text the reference's own tools write",
			result: pi.ToolResultMessage{
				ToolCallID: "call_1", ToolName: "read_file",
				Content: []pi.ToolResultMessageContent{pi.TextContent{Text: capturedFailure}},
			},
			want: true,
		},
		{
			name: "a tool that threw",
			result: pi.ToolResultMessage{
				ToolCallID: "call_1", ToolName: "read_file", IsError: true,
				Content: []pi.ToolResultMessageContent{pi.TextContent{Text: "boom"}},
			},
			want: true,
		},
		{
			name: "a call that succeeded",
			result: pi.ToolResultMessage{
				ToolCallID: "call_1", ToolName: "read_file",
				Content: []pi.ToolResultMessageContent{pi.TextContent{Text: capturedSuccess}},
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := toolResultRequest(t, tc.result)
			var tool *backend.Message
			for i := range req.Messages {
				if req.Messages[i].Role == "tool" {
					tool = &req.Messages[i]
				}
			}
			if tool == nil {
				t.Fatalf("no tool message in %+v", req.Messages)
			}
			if tool.IsError != tc.want {
				t.Errorf("IsError = %v, want %v", tool.IsError, tc.want)
			}
		})
	}
}
