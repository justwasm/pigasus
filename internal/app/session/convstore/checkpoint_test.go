package convstore

import (
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func encodeVarintField(num int, v uint64) []byte {
	out := protowire.AppendTag(nil, protowire.Number(num), protowire.VarintType)
	return protowire.AppendVarint(out, v)
}

func TestParseCheckpointStep(t *testing.T) {
	inner := encodeVarintField(1, 2)
	inner = append(inner, encodeProtoField(4, 2, []byte("the intent"))...)
	inner = append(inner, encodeProtoField(5, 2, []byte("<summary>s</summary>"))...)
	inner = append(inner, encodeVarintField(11, 1)...)
	inner = append(inner, encodeVarintField(12, 4)...)
	inner = append(inner, encodeVarintField(3, 1)...)
	inner = append(inner, encodeVarintField(3, 2)...)
	// 【一手】 the reference stores each request in field 30, as a nested
	// message whose field 1 is the text.
	inner = append(inner, encodeProtoField(30, 2, encodeProtoField(1, 2, []byte("first request")))...)
	inner = append(inner, encodeProtoField(30, 2, encodeProtoField(1, 2, []byte("second request")))...)
	payload := encodeProtoField(30, 2, inner)

	s := Step{Type: StepCheckpoint}
	parseStepPayload(&s, payload)

	if s.CheckpointIndex != 2 {
		t.Errorf("checkpoint index = %d, want 2", s.CheckpointIndex)
	}
	if s.Summary != "<summary>s</summary>" {
		t.Errorf("summary = %q", s.Summary)
	}
	if s.UserIntent != "the intent" {
		t.Errorf("user intent = %q", s.UserIntent)
	}
	if s.IncludedStart != 1 || s.IncludedEnd != 4 {
		t.Errorf("included range = %d..%d, want 1..4", s.IncludedStart, s.IncludedEnd)
	}
	if len(s.IncludedSteps) != 2 || s.IncludedSteps[0] != 1 || s.IncludedSteps[1] != 2 {
		t.Errorf("included steps = %v", s.IncludedSteps)
	}
	if len(s.UserRequests) != 2 || s.UserRequests[0] != "first request" {
		t.Errorf("user requests = %v", s.UserRequests)
	}
}

func TestChatTurns_StartsAtCheckpoint(t *testing.T) {
	c := &Conversation{Steps: []Step{
		{Idx: 0, Type: StepUserInput, Text: "old request"},
		{Idx: 1, Type: StepModelResponse, Text: "old answer"},
		{Idx: 2, Type: StepCheckpoint,
			Summary:       "<summary>progress</summary>",
			UserRequests:  []string{"old request"},
			IncludedStart: 0,
			IncludedEnd:   3,
		},
		{Idx: 3, Type: StepUserInput, Text: "new request"},
		{Idx: 4, Type: StepModelResponse, Text: "new answer"},
	}}

	turns := c.ChatTurns()
	if len(turns) != 3 {
		t.Fatalf("turns = %d, want 3: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !strings.HasPrefix(turns[0].Text, "# Resuming from a compaction") {
		t.Fatalf("first turn = %+v, want the resume block", turns[0])
	}
	if !strings.Contains(turns[0].Text, "old request") || !strings.Contains(turns[0].Text, "progress") {
		t.Errorf("resume block = %q", turns[0].Text)
	}
	if turns[1].Text != "new request" || turns[2].Text != "new answer" {
		t.Errorf("post-checkpoint turns = %+v", turns[1:])
	}

	// A payload-less type-23 placeholder is not a checkpoint.
	c2 := &Conversation{Steps: []Step{
		{Idx: 0, Type: StepUserInput, Text: "a"},
		{Idx: 1, Type: StepCheckpoint},
		{Idx: 2, Type: StepUserInput, Text: "b"},
	}}
	if c2.Checkpoint() != nil {
		t.Fatal("a placeholder must not be treated as a checkpoint")
	}
	if got := c2.ChatTurns(); len(got) != 2 {
		t.Fatalf("placeholder turns = %+v, want two", got)
	}
}

func TestTranscript_IncludesCheckpointAndPreCompactionSteps(t *testing.T) {
	c := &Conversation{Steps: []Step{
		{Idx: 0, Type: StepUserInput, Text: "early user input"},
		{Idx: 1, Type: StepToolCall, ToolName: "read_file"},
		{Idx: 2, Type: StepCheckpoint,
			Summary:       "<summary>progress</summary>",
			UserRequests:  []string{"early user input"},
			IncludedStart: 0,
			IncludedEnd:   3,
		},
		{Idx: 3, Type: StepUserInput, Text: "later user input"},
		{Idx: 4, Type: StepModelResponse, Text: "later answer"},
	}}
	transcript := c.Transcript()
	if len(transcript) != 5 {
		t.Fatalf("transcript len = %d, want 5 (all pre and post steps plus checkpoint)", len(transcript))
	}
	if transcript[0].Text != "early user input" {
		t.Errorf("step 0 text = %q, want early user input", transcript[0].Text)
	}
	if transcript[2].Role() != "checkpoint" {
		t.Errorf("step 2 role = %q, want checkpoint", transcript[2].Role())
	}
	if transcript[3].Text != "later user input" {
		t.Errorf("step 3 text = %q, want later user input", transcript[3].Text)
	}
}

// TestWriteConversation_CheckpointRoundTrip pins that a checkpoint the clone
// persists is not an opaque type-23 placeholder: the summary, the included
// range and the request list survive the write and the load, so `/resume`
// can rebuild the post-compaction context.
func TestWriteConversation_CheckpointRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conv.db")
	in := []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "first"},
		{Idx: 1, Type: StepModelResponse, Status: 3, Text: "ok"},
		{Idx: 2, Type: StepCheckpoint, Status: 3,
			CheckpointIndex: 0,
			IncludedEnd:     3,
			Summary:         "<summary>progress</summary>",
			UserRequests:    []string{"first", "/compact"},
		},
		{Idx: 3, Type: StepUserInput, Status: 3, Text: "next"},
	}
	if err := WriteConversation(path, "conv", in); err != nil {
		t.Fatalf("WriteConversation: %v", err)
	}
	c, err := LoadConversation(path)
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	cp := c.Checkpoint()
	if cp == nil {
		t.Fatal("Checkpoint() = nil; the persisted checkpoint is opaque")
	}
	if cp.Summary != "<summary>progress</summary>" {
		t.Errorf("summary = %q", cp.Summary)
	}
	if cp.IncludedEnd != 3 {
		t.Errorf("included end = %d, want 3", cp.IncludedEnd)
	}
	if len(cp.UserRequests) != 2 || cp.UserRequests[0] != "first" || cp.UserRequests[1] != "/compact" {
		t.Errorf("requests = %v", cp.UserRequests)
	}

	turns := c.ChatTurns()
	if len(turns) == 0 || !strings.Contains(turns[0].Text, "<summary>progress</summary>") {
		t.Fatalf("resume block = %+v", turns)
	}
	if !strings.Contains(turns[0].Text, "/compact") {
		t.Errorf("resume block lost the request list: %q", turns[0].Text)
	}
}

// TestParseCheckpointStep_LegacyField19 keeps reading the old field the clone
// used before the live database showed the real one.
func TestParseCheckpointStep_LegacyField19(t *testing.T) {
	inner := encodeProtoField(5, 2, []byte("<summary>s</summary>"))
	inner = append(inner, encodeProtoField(19, 2, []byte("legacy"))...)
	s := Step{Type: StepCheckpoint}
	parseStepPayload(&s, encodeProtoField(30, 2, inner))
	if len(s.UserRequests) != 1 || s.UserRequests[0] != "legacy" {
		t.Errorf("requests = %v, want [legacy]", s.UserRequests)
	}
}
