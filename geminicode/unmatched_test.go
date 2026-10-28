package geminicode

import (
	"testing"
)

// The 1.4.9 rule: a tool call with no result and a result with no call
// are left out of the request.
//
// 【一手】 agy 1.4.9, the vendor's own release notes: "Fixed conversations
// getting stuck failing on every turn after a tool result was left without
// its matching tool call, such as after older history was trimmed;
// unmatched tool calls and results are now left out of the request."
//
// The API pairs the two halves itself, so a request that carries only one
// of them fails — and, in the reference, it failed *every* later turn of
// the conversation, because the orphan stayed in the history.

// contentsOf builds the request contents for a conversation.
func contentsOf(messages ...TurnMessage) []Content {
	contents, _ := buildContents(messages, "", nil)
	return contents
}

// callIDs collects the ids of the functionCall parts in the contents.
func callIDs(contents []Content) []string {
	var ids []string
	for _, c := range contents {
		for _, p := range c.Parts {
			if p.FunctionCall != nil {
				ids = append(ids, p.FunctionCall.ID)
			}
		}
	}
	return ids
}

// responseIDs collects the ids of the functionResponse parts in the contents.
func responseIDs(contents []Content) []string {
	var ids []string
	for _, c := range contents {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				ids = append(ids, p.FunctionResponse.ID)
			}
		}
	}
	return ids
}

func TestBuildContents_KeepsPairedCallsAndResults(t *testing.T) {
	contents := contentsOf(
		TurnMessage{Role: "user", Content: "read it"},
		TurnMessage{Role: "assistant", ToolCalls: []TurnToolCall{
			{ID: "call_1", Name: "read_file", Arguments: []byte(`{"path":"a"}`)},
		}},
		TurnMessage{Role: "tool", ToolCallID: "call_1", Name: "read_file", Content: "hello"},
	)
	if got := callIDs(contents); len(got) != 1 || got[0] != "call_1" {
		t.Errorf("functionCall ids = %v, want [call_1]", got)
	}
	if got := responseIDs(contents); len(got) != 1 || got[0] != "call_1" {
		t.Errorf("functionResponse ids = %v, want [call_1]", got)
	}
}

func TestBuildContents_DropsAnUnmatchedResult(t *testing.T) {
	contents := contentsOf(
		TurnMessage{Role: "user", Content: "read it"},
		TurnMessage{Role: "tool", ToolCallID: "call_gone", Name: "read_file", Content: "hello"},
	)
	if got := responseIDs(contents); len(got) != 0 {
		t.Errorf("functionResponse ids = %v, want none: the call is gone", got)
	}
}

func TestBuildContents_DropsAnUnmatchedCall(t *testing.T) {
	contents := contentsOf(
		TurnMessage{Role: "user", Content: "read it"},
		TurnMessage{Role: "assistant", ToolCalls: []TurnToolCall{
			{ID: "call_1", Name: "read_file", Arguments: []byte(`{"path":"a"}`)},
		}},
	)
	if got := callIDs(contents); len(got) != 0 {
		t.Errorf("functionCall ids = %v, want none: the result never arrived", got)
	}
}

func TestBuildContents_KeepsThePairedHalfOfAMixedTurn(t *testing.T) {
	contents := contentsOf(
		TurnMessage{Role: "assistant", Content: "two reads", ToolCalls: []TurnToolCall{
			{ID: "call_1", Name: "read_file", Arguments: []byte(`{"path":"a"}`)},
			{ID: "call_2", Name: "read_file", Arguments: []byte(`{"path":"b"}`)},
		}},
		TurnMessage{Role: "tool", ToolCallID: "call_1", Name: "read_file", Content: "hello"},
	)
	if got := callIDs(contents); len(got) != 1 || got[0] != "call_1" {
		t.Errorf("functionCall ids = %v, want [call_1]: only the answered call survives", got)
	}
	if got := responseIDs(contents); len(got) != 1 || got[0] != "call_1" {
		t.Errorf("functionResponse ids = %v, want [call_1]", got)
	}
}

// A call the model made and the agent answered, with the answer trimmed
// away by an older history cut: the call is an orphan and goes too.
func TestBuildContents_DropsACallWhoseAnswerWasTrimmed(t *testing.T) {
	contents := contentsOf(
		TurnMessage{Role: "assistant", ToolCalls: []TurnToolCall{
			{ID: "call_1", Name: "read_file", Arguments: []byte(`{"path":"a"}`)},
		}},
		TurnMessage{Role: "user", Content: "next question"},
	)
	if got := callIDs(contents); len(got) != 0 {
		t.Errorf("functionCall ids = %v, want none: the answer was trimmed away", got)
	}
}

// The assistant's own text survives the drop of its orphan call.
func TestBuildContents_KeepsTheTextOfATurnWhoseCallWasDropped(t *testing.T) {
	contents := contentsOf(
		TurnMessage{Role: "assistant", Content: "let me look", ToolCalls: []TurnToolCall{
			{ID: "call_1", Name: "read_file", Arguments: []byte(`{"path":"a"}`)},
		}},
	)
	if len(contents) != 1 {
		t.Fatalf("the request carries %d contents, want the assistant's text alone", len(contents))
	}
	if len(contents[0].Parts) != 1 || contents[0].Parts[0].Text != "let me look" {
		t.Errorf("the assistant's parts = %+v, want the text alone", contents[0].Parts)
	}
}

func TestPairedToolCallIDs(t *testing.T) {
	paired := PairedToolCallIDs([]string{"a", "b"}, []string{"b", "c"})
	if !paired["b"] {
		t.Error("b is called and answered, so it is paired")
	}
	if paired["a"] || paired["c"] {
		t.Errorf("a and c are orphans, so they are not paired: %v", paired)
	}
}
