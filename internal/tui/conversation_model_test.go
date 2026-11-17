package tui

import (
	"testing"
	"time"
)

// TestConversationModel_TurnState pins the turn predicates that used to
// live on the conversation model now.
func TestConversationModel_TurnState(t *testing.T) {
	var c ConversationModel
	if c.IsStreamActive() || c.IsThinkingActive() || c.WasTurnCancelled() {
		t.Fatal("zero model reports an active turn")
	}
	c.SetStreamActive(true)
	c.SetThinkingActive(true)
	c.SetTurnCancelled(true)
	if !c.IsStreamActive() || !c.IsThinkingActive() || !c.WasTurnCancelled() {
		t.Fatal("turn predicates did not follow the model")
	}
}

// TestConversationModel_OwnsRenderRows is the ownership assertion: the
// rendered transcript rows, the block indexes and the live reply buffers
// live on the conversation model.
func TestConversationModel_OwnsRenderRows(t *testing.T) {
	c := ConversationModel{replyStart: -1, liveReplyIdx: -1, liveThoughtRunIdx: -1}
	c.response = append(c.response, "hello")
	if len(c.response) != 1 || c.replyStart != -1 {
		t.Fatalf("render state = %+v", c)
	}
}

// TestConversationModel_ThinkingLifecycle pins the thought transitions the
// stream handlers drive. A second start does not reopen the block; a chunk
// that arrives first opens it; clearing drops only the live buffers.
func TestConversationModel_ThinkingLifecycle(t *testing.T) {
	var c ConversationModel
	now := time.Unix(100, 0)
	if !c.BeginThinking(now) {
		t.Fatal("first thought did not open a block")
	}
	if !c.thinking || !c.streaming || !c.thinkingSince.Equal(now) {
		t.Fatalf("state after start = %+v", c)
	}
	if c.BeginThinking(now.Add(time.Second)) {
		t.Fatal("second thought start reopened the block")
	}
	c.AppendThought("plan", now)
	c.AppendThought("ning", now)
	if c.liveThinking != "planning" {
		t.Fatalf("live thought = %q", c.liveThinking)
	}
	c.ClearThinking()
	if c.thinking || c.liveThinking != "" || c.liveThinkingSnippet != "" {
		t.Fatalf("ClearThinking left %+v", c)
	}

	var fresh ConversationModel
	fresh.AppendThought("delta", now)
	if !fresh.thinking || fresh.liveThinking != "delta" {
		t.Fatal("a chunk before the start message did not open the block")
	}
}

// TestConversationModel_ReplyLifecycle pins the reply region and stream
// flags: opening at a row, appending chunks, then ending the stream.
func TestConversationModel_ReplyLifecycle(t *testing.T) {
	var c ConversationModel
	c.BeginReply(4)
	if c.replyStart != 4 || c.liveTextLines != 0 || c.liveReplyBuf != "" {
		t.Fatalf("BeginReply = %+v", c)
	}
	c.AppendReplyChunk("hello")
	c.AppendReplyChunk(" world")
	if c.liveReplyBuf != "hello world" || !c.streamedText || !c.batchBreak || !c.streaming {
		t.Fatalf("AppendReplyChunk = %+v", c)
	}
	c.EndStream()
	if c.replyStart != -1 || c.liveReplyIdx != -1 || c.streaming || c.thinking {
		t.Fatalf("EndStream = %+v", c)
	}
}

// TestConversationModel_ToolLifecycle pins the running/finished transition.
func TestConversationModel_ToolLifecycle(t *testing.T) {
	var c ConversationModel
	c.BeginToolRun("read_file")
	if c.activeTool != "read_file" || !c.anyToolRun || !c.streaming {
		t.Fatalf("BeginToolRun = %+v", c)
	}
	c.BeginToolResult()
	if c.activeTool != "" || c.replyStart != -1 || c.liveTextLines != 0 || c.liveReplyBuf != "" {
		t.Fatalf("BeginToolResult = %+v", c)
	}
}

// TestConversationModel_ModelRetryStatus pins the official agy 1.4.6 / 1.5.9
// status line formatting for unavailable model retries.
func TestConversationModel_ModelRetryStatus(t *testing.T) {
	var c ConversationModel

	// No retry active
	if status, ok := c.ModelRetryStatus(); ok || status != "" {
		t.Errorf("expected empty retry status, got %q (ok=%v)", status, ok)
	}

	// Active retry countdown (12s)
	c.SetModelRetry(2, 10, time.Now().Add(12*time.Second))
	status, ok := c.ModelRetryStatus()
	if !ok || status != "Model unavailable, retrying in 12s (attempt 2/10)" {
		t.Errorf("expected countdown string, got %q (ok=%v)", status, ok)
	}

	// Retry timer expired / firing now
	c.SetModelRetry(2, 10, time.Time{})
	status, ok = c.ModelRetryStatus()
	if !ok || status != "Model unavailable, retrying (attempt 2/10)" {
		t.Errorf("expected firing string, got %q (ok=%v)", status, ok)
	}

	// Cleared retry
	c.ClearModelRetry()
	if status, ok := c.ModelRetryStatus(); ok || status != "" {
		t.Errorf("expected cleared retry, got %q (ok=%v)", status, ok)
	}

	// Cleared on EndStream
	c.SetModelRetry(3, 10, time.Now().Add(5*time.Second))
	c.EndStream()
	if status, ok := c.ModelRetryStatus(); ok || status != "" {
		t.Errorf("expected EndStream to clear retry status, got %q (ok=%v)", status, ok)
	}
}
