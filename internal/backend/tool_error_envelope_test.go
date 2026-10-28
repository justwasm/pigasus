package backend

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/geminicode"
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

// toolResponseEnvelope returns the `response` member of the one
// functionResponse in a built request.
func toolResponseEnvelope(t *testing.T, contents []geminicode.Content) map[string]any {
	t.Helper()
	for _, c := range contents {
		for _, part := range c.Parts {
			if part.FunctionResponse == nil {
				continue
			}
			raw, err := json.Marshal(part.FunctionResponse.Response)
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}
			var out map[string]any
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			return out
		}
	}
	t.Fatal("no functionResponse in the built contents")
	return nil
}

// A failed call travels under `error`; a successful one under `output`.
//
// 【一手】 agy 1.4.8, captured live — see
// internal/tools/testdata/tool_errors_1.4.8.txt.
func TestCloudCode_ToolCallEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		message Message
		wantKey string
	}{
		{
			name: "a call that failed",
			message: Message{
				Role: "tool", Content: capturedFailure, ToolCallID: "call_1", Name: "read_file",
				IsError: true,
			},
			wantKey: "error",
		},
		{
			name: "a call that succeeded",
			message: Message{
				Role: "tool", Content: capturedSuccess, ToolCallID: "call_1", Name: "read_file",
			},
			wantKey: "output",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envelope := geminicode.BuildRequest(geminicode.Turn{
				Messages: []geminicode.TurnMessage{
					{
						Role: "assistant",
						ToolCalls: []geminicode.TurnToolCall{{
							ID: "call_1", Name: "read_file", Arguments: []byte(`{"path":"x"}`),
						}},
					},
					{
						Role:       tc.message.Role,
						Content:    tc.message.Content,
						ToolCallID: tc.message.ToolCallID,
						Name:       tc.message.Name,
						IsError:    tc.message.IsError,
					},
				},
			})
			contents := envelope.Request.Contents
			body := toolResponseEnvelope(t, contents)
			if len(body) != 1 {
				t.Fatalf("response = %v, want exactly one member", body)
			}
			text, ok := body[tc.wantKey].(string)
			if !ok {
				t.Fatalf("response = %v, want a string under %q", body, tc.wantKey)
			}
			if text != tc.message.Content {
				t.Errorf("body = %q, want %q", text, tc.message.Content)
			}
		})
	}
}

// The Gemini API-key dialect is the reference's own path, so it takes the
// same envelope as the CloudCode one.
func TestGemini_ToolCallEnvelope(t *testing.T) {
	failed := toolResponseFor(Message{Content: capturedFailure, IsError: true})
	if _, ok := failed["error"]; !ok {
		t.Fatalf("failed call = %v, want an `error` member", failed)
	}
	if _, ok := failed["output"]; ok {
		t.Fatalf("failed call = %v, want no `output` member", failed)
	}
	ok := toolResponseFor(Message{Content: capturedSuccess})
	if _, ok := ok["output"]; !ok {
		t.Fatalf("successful call = %v, want an `output` member", ok)
	}
	if _, ok := ok["error"]; ok {
		t.Fatalf("successful call = %v, want no `error` member", ok)
	}
}
