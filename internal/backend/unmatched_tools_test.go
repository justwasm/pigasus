package backend

import (
	"encoding/json"
	"testing"
)

// The Gemini API-key dialect drops the same orphans the CloudCode one
// does — 【一手】 agy 1.4.9: "unmatched tool calls and results are now
// left out of the request."
//
// The two transports are the reference's own paths, so they take the same
// rule; the one implementation of the pairing lives in geminicode.

// geminiBody builds the Gemini request body for a conversation.
func geminiBody(t *testing.T, messages ...Message) geminiRequest {
	t.Helper()
	client := &GeminiClient{}
	return client.buildRequest(StreamRequest{Messages: messages})
}

// geminiCallIDsIn collects the ids of the functionCall parts in a body.
func geminiCallIDsIn(t *testing.T, body geminiRequest) []string {
	t.Helper()
	var ids []string
	for _, c := range body.Contents {
		for _, p := range c.Parts {
			if p.FunctionCall != nil {
				ids = append(ids, p.FunctionCall.ID)
			}
		}
	}
	return ids
}

// geminiResponseIDsIn collects the ids of the functionResponse parts.
func geminiResponseIDsIn(t *testing.T, body geminiRequest) []string {
	t.Helper()
	var ids []string
	for _, c := range body.Contents {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				ids = append(ids, p.FunctionResponse.ID)
			}
		}
	}
	return ids
}

func TestGemini_DropsAnUnmatchedResult(t *testing.T) {
	body := geminiBody(t,
		Message{Role: "user", Content: "read it"},
		Message{Role: "tool", ToolCallID: "call_gone", Name: "read_file", Content: "hello"},
	)
	if got := geminiResponseIDsIn(t, body); len(got) != 0 {
		t.Errorf("functionResponse ids = %v, want none: the call is gone", got)
	}
}

func TestGemini_DropsAnUnmatchedCall(t *testing.T) {
	body := geminiBody(t,
		Message{Role: "user", Content: "read it"},
		Message{Role: "assistant", ToolCalls: []ToolCall{
			{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a"}`)},
		}},
	)
	if got := geminiCallIDsIn(t, body); len(got) != 0 {
		t.Errorf("functionCall ids = %v, want none: the result never arrived", got)
	}
}

func TestGemini_KeepsThePairedPair(t *testing.T) {
	body := geminiBody(t,
		Message{Role: "assistant", ToolCalls: []ToolCall{
			{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a"}`)},
		}},
		Message{Role: "tool", ToolCallID: "call_1", Name: "read_file", Content: "hello"},
	)
	if got := geminiCallIDsIn(t, body); len(got) != 1 || got[0] != "call_1" {
		t.Errorf("functionCall ids = %v, want [call_1]", got)
	}
	if got := geminiResponseIDsIn(t, body); len(got) != 1 || got[0] != "call_1" {
		t.Errorf("functionResponse ids = %v, want [call_1]", got)
	}
}
