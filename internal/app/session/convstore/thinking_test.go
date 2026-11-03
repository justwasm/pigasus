package convstore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/justwasm/pigasus/lsproto"
)

// TestModelResponseThinkingRoundTrip checks that the model's thinking
// and the time it took survive the store.
//
// 【一手】 the reference's own stored model-response blob carries both:
// its planner-response message reads back with the answer, the model's
// reasoning and a "6.773079s" duration. The Web UI's collapsible
// "Thought for Ns" panel is rendered from the last two, so a stored step
// without them shows the answer alone once the history comes from the
// store.
func TestModelResponseThinkingRoundTrip(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "thinking-conv"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "what is the capital of France?"},
		{
			Idx:              1,
			Type:             StepModelResponse,
			Status:           3,
			Text:             "Paris.",
			Thinking:         "The user asks for a capital. France's capital is Paris.",
			ThinkingDuration: 6733079 * time.Microsecond,
		},
	}
	if err := SaveConversation(convID, "/workspace/openagy", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	// The clone's own reader gets both back.
	c, err := LoadConversation(ConversationPath(convID))
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("loaded steps = %d, want 2", len(c.Steps))
	}
	if c.Steps[1].Thinking != steps[1].Thinking {
		t.Errorf("thinking = %q, want %q", c.Steps[1].Thinking, steps[1].Thinking)
	}
	if c.Steps[1].ThinkingDuration != steps[1].ThinkingDuration {
		t.Errorf("thinking duration = %v, want %v", c.Steps[1].ThinkingDuration, steps[1].ThinkingDuration)
	}

	// The reference's reader gets them too: the blob is a
	// gemini_coder.Step whose planner response carries the answer, the
	// thinking and the duration.
	rows, err := StoredStepPayloads(convID)
	if err != nil {
		t.Fatalf("StoredStepPayloads: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("stored rows = %d, want 2", len(rows))
	}
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	raw, err := codec.DecodeStep(rows[1].Payload)
	if err != nil {
		t.Fatalf("DecodeStep: %v", err)
	}
	var step map[string]any
	if err := json.Unmarshal(raw, &step); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	planner, ok := step["plannerResponse"].(map[string]any)
	if !ok {
		t.Fatalf("step has no plannerResponse: %v", step)
	}
	if planner["response"] != "Paris." {
		t.Errorf("response = %v, want %q", planner["response"], "Paris.")
	}
	if planner["thinking"] != steps[1].Thinking {
		t.Errorf("thinking = %v, want %q", planner["thinking"], steps[1].Thinking)
	}
	// A google.protobuf.Duration is a proto-JSON string, not an
	// object: the frontend's protobuf-es decoder rejects the object
	// form.
	if planner["thinkingDuration"] != "6.733079s" {
		t.Errorf("thinkingDuration = %v, want %q", planner["thinkingDuration"], "6.733079s")
	}
}
