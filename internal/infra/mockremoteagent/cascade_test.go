package mockremoteagent

import "testing"

// TestFormatProtoStepGenericCarriesToolCall pins the generic step shape:
// a tool that is not run_command travels as CORTEX_STEP_TYPE_GENERIC with
// metadata.toolCall {name, argumentsJson} and generic.result.result.
func TestFormatProtoStepGenericCarriesToolCall(t *testing.T) {
	session := NewCascadeSession("cascade-1", "project-1", "gemini-3.8-flash-high", nil, nil)
	step := TrajectoryStep{
		Type:    "GENERIC",
		Status:  "DONE",
		Content: "view_file",
		ToolCalls: []any{map[string]any{
			"toolName": "view_file",
			"args":     `{"AbsolutePath":"/tmp/x.txt"}`,
			"result":   "file contents",
		}},
	}

	got := formatProtoStep(session, step, 0)
	meta, _ := got["metadata"].(map[string]any)
	call, _ := meta["toolCall"].(map[string]any)
	if call["name"] != "view_file" {
		t.Errorf("metadata.toolCall.name = %v", call["name"])
	}
	if call["argumentsJson"] != `{"AbsolutePath":"/tmp/x.txt"}` {
		t.Errorf("metadata.toolCall.argumentsJson = %v", call["argumentsJson"])
	}
	generic, _ := got["generic"].(map[string]any)
	result, _ := generic["result"].(map[string]any)
	if result["result"] != "file contents" {
		t.Errorf("generic.result.result = %v", result["result"])
	}
}

// TestCascadeFramesAdvanceResumeToken pins the monotonic resume token:
// every broadcast is a new frame, even when it only updates an existing
// step in place. The 1.5.3 client drops a frame whose resumeToken did not
// advance, which left the TUI spinning after the final answer.
func TestCascadeFramesAdvanceResumeToken(t *testing.T) {
	session := NewCascadeSession("cascade-1", "project-1", "gemini-3.8-flash-high", nil, nil)
	ch := session.Subscribe()
	defer session.Unsubscribe(ch)

	first := <-ch
	session.AddStep("USER_INPUT", "hi", "", nil)
	second := <-ch
	session.SetStatus("IDLE")
	third := <-ch

	tokens := []string{first.ResumeToken, second.ResumeToken, third.ResumeToken}
	for i := 1; i < len(tokens); i++ {
		if tokens[i] == tokens[i-1] {
			t.Fatalf("resumeToken did not advance at frame %d: %v", i, tokens)
		}
	}
	if !third.Update["fullyIdle"].(bool) {
		t.Errorf("final frame fullyIdle = %v, want true", third.Update["fullyIdle"])
	}
}
