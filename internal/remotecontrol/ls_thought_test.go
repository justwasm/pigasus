package remotecontrol

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestLSThinkScannerSplitsTheBlock checks that the reasoning is kept out
// of the visible text.
func TestLSThinkScannerSplitsTheBlock(t *testing.T) {
	sc := &lsThinkScanner{}
	if got := sc.Push("<think>reasoning</think>answer"); got != "answer" {
		t.Fatalf("visible text = %q, want %q", got, "answer")
	}
	if got := sc.Thought(); got != "reasoning" {
		t.Fatalf("thought = %q, want %q", got, "reasoning")
	}
}

// TestLSThinkScannerAcrossChunks checks that a marker split across two
// deltas is still recognised — the transport splits one think block
// across many chunks.
func TestLSThinkScannerAcrossChunks(t *testing.T) {
	sc := &lsThinkScanner{}
	if got := sc.Push("<thi"); got != "" {
		t.Fatalf("visible text = %q, want empty", got)
	}
	if got := sc.Push("nk>reason"); got != "" {
		t.Fatalf("visible text = %q, want empty", got)
	}
	if got := sc.Push("ing</thi"); got != "" {
		t.Fatalf("visible text = %q, want empty", got)
	}
	if got := sc.Push("nk>answer"); got != "answer" {
		t.Fatalf("visible text = %q, want %q", got, "answer")
	}
	if got := sc.Thought(); got != "reasoning" {
		t.Fatalf("thought = %q, want %q", got, "reasoning")
	}
}

// TestLSThinkScannerDropsTheBlankLineAfterTheBlock checks the reference
// CLI's "no gap" rule: the whitespace that follows a think block does
// not start the answer.
func TestLSThinkScannerDropsTheBlankLineAfterTheBlock(t *testing.T) {
	sc := &lsThinkScanner{}
	if got := sc.Push("<think>r</think>\n\nanswer"); got != "answer" {
		t.Fatalf("visible text = %q, want %q", got, "answer")
	}
}

// TestLSThinkScannerDuration checks that the thinking is timed.
func TestLSThinkScannerDuration(t *testing.T) {
	sc := &lsThinkScanner{}
	sc.Push("<think>r</think>")
	if sc.Duration() <= 0 {
		t.Fatalf("duration = %v, want a positive span", sc.Duration())
	}
}

// TestLSThinkScannerEmptyStream checks that a stream without thinking
// reports no thought and no duration.
func TestLSThinkScannerEmptyStream(t *testing.T) {
	sc := &lsThinkScanner{}
	sc.Push("just an answer")
	if got := sc.Thought(); got != "" {
		t.Fatalf("thought = %q, want empty", got)
	}
	if sc.Duration() != 0 {
		t.Fatalf("duration = %v, want 0", sc.Duration())
	}
}

// TestLSStripThink checks the one-shot strip the turn's reply uses.
func TestLSStripThink(t *testing.T) {
	if got := lsStripThink("<think>r</think>\n\nhello"); got != "hello" {
		t.Fatalf("stripped = %q, want %q", got, "hello")
	}
	if got := lsStripThink("hello"); got != "hello" {
		t.Fatalf("stripped = %q, want %q", got, "hello")
	}
}

// TestLSPlannerResponseStepCarriesTheThinking checks that the step the
// Web UI renders carries the thought text and the duration in the
// proto-JSON form the reference uses.
func TestLSPlannerResponseStepCarriesTheThinking(t *testing.T) {
	step := lsPlannerResponseStep("hello", "reasoning", 1500*time.Millisecond)
	raw, err := json.Marshal(step)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		PlannerResponse struct {
			Response         string `json:"response"`
			Thinking         string `json:"thinking"`
			ThinkingDuration string `json:"thinkingDuration"`
		} `json:"plannerResponse"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PlannerResponse.Response != "hello" {
		t.Fatalf("response = %q", decoded.PlannerResponse.Response)
	}
	if decoded.PlannerResponse.Thinking != "reasoning" {
		t.Fatalf("thinking = %q", decoded.PlannerResponse.Thinking)
	}
	// google.protobuf.Duration is proto-JSON encoded as a string; the
	// frontend's decoder rejects the object form.
	if decoded.PlannerResponse.ThinkingDuration != "1.500s" {
		t.Fatalf("thinkingDuration = %q, want %q", decoded.PlannerResponse.ThinkingDuration, "1.500s")
	}
}

// TestLSPlannerResponseStepWithoutThinking checks that a step without
// thinking carries no thinking fields at all — the frontend renders
// nothing for an empty thought.
func TestLSPlannerResponseStepWithoutThinking(t *testing.T) {
	step := lsPlannerResponseStep("hello", "", 0)
	raw, err := json.Marshal(step)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp := decoded["plannerResponse"].(map[string]any)
	if _, ok := resp["thinking"]; ok {
		t.Fatalf("unexpected thinking field: %v", resp)
	}
	if _, ok := resp["thinkingDuration"]; ok {
		t.Fatalf("unexpected thinkingDuration field: %v", resp)
	}
}

// TestLSThoughtSinkForwardsOnlyTheVisibleText checks that the sink
// wrapper keeps the reasoning out of the stream the UI renders.
func TestLSThoughtSinkForwardsOnlyTheVisibleText(t *testing.T) {
	inner := &recordingSink{}
	sink := newLSThoughtSink(inner)
	sink.OnTextDelta(nil, "<think>r</think>answer")
	if inner.text != "answer" {
		t.Fatalf("forwarded text = %q, want %q", inner.text, "answer")
	}
	thinking, _ := sink.Thought()
	if thinking != "r" {
		t.Fatalf("thinking = %q, want %q", thinking, "r")
	}
}

// recordingSink collects the text the wrapper forwarded.
type recordingSink struct {
	text string
}

func (r *recordingSink) OnState(cascade *Cascade, frame StateFrame)          {}
func (r *recordingSink) OnTextDelta(cascade *Cascade, delta string)          { r.text += delta }
func (r *recordingSink) OnToolCall(cascade *Cascade, call ToolCall)          {}
func (r *recordingSink) OnError(cascade *Cascade, err error)                 {}
func (r *recordingSink) OnUserInteraction(c *Cascade, i *PendingInteraction) {}

// TestLSTurnCarriesTheThinking drives one Web UI turn with a backend
// that answers with a think block and checks that the trajectory step
// carries the thinking the frontend renders as "Thought for Ns".
func TestLSTurnCarriesTheThinking(t *testing.T) {
	sess := NewSessionState("1.4.7")
	disp, err := NewDispatcher(Config{
		Session:        sess,
		MessageBackend: &StubBackend{Reply: "<think>reasoning</think>answer"},
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ctx := context.Background()
	if _, err := disp.DispatchDirect(ctx, "StartCascade", `{"cascadeId":"c1"}`); err != nil {
		t.Fatalf("StartCascade: %v", err)
	}
	if _, err := disp.DispatchDirect(ctx, "SendUserCascadeMessage",
		`{"cascadeId":"c1","items":[{"text":"hi"}]}`); err != nil {
		t.Fatalf("SendUserCascadeMessage: %v", err)
	}
	traj := disp.trajectories.Get("c1")
	if traj == nil {
		t.Fatal("no trajectory for the cascade")
	}
	var planner map[string]any
	for _, step := range traj.Snapshot().Steps {
		if pr, ok := step["plannerResponse"].(map[string]any); ok {
			planner = pr
		}
	}
	if planner == nil {
		t.Fatalf("no planner response step: %v", traj.Snapshot().Steps)
	}
	if planner["response"] != "answer" {
		t.Fatalf("response = %v, want %q", planner["response"], "answer")
	}
	if planner["thinking"] != "reasoning" {
		t.Fatalf("thinking = %v, want %q", planner["thinking"], "reasoning")
	}
	if _, ok := planner["thinkingDuration"]; !ok {
		t.Fatalf("no thinkingDuration: %v", planner)
	}
}
