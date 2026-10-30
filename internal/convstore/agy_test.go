package convstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T) *Conversation {
	t.Helper()
	c, err := LoadConversation(filepath.Join("testdata", "transcript.db"))
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	return c
}

func TestLoadConversationIdentity(t *testing.T) {
	c := loadFixture(t)
	if c.ID != "cafe0000-0000-4000-8000-000000000001" {
		t.Errorf("ID = %q", c.ID)
	}
	if c.TrajectoryID != "traj-fixture" {
		t.Errorf("TrajectoryID = %q", c.TrajectoryID)
	}
	if len(c.Steps) != 9 {
		t.Fatalf("Steps = %d, want 9", len(c.Steps))
	}
	for i, s := range c.Steps {
		if s.Idx != int64(i) {
			t.Errorf("step %d has idx %d", i, s.Idx)
		}
	}
}

func TestStepPayloadShapes(t *testing.T) {
	c := loadFixture(t)
	byIdx := map[int64]Step{}
	for _, s := range c.Steps {
		byIdx[s.Idx] = s
	}
	if got := byIdx[0].Text; got != "summarise the repository layout" {
		t.Errorf("user text = %q", got)
	}
	if got := byIdx[1].Text; got != "I will start by listing the top level." {
		t.Errorf("model text = %q", got)
	}
	if s := byIdx[2]; s.ToolName != "run_command" || s.ToolArgs != `{"CommandLine":"ls","Cwd":"/tmp"}` {
		t.Errorf("tool step = %q %q", s.ToolName, s.ToolArgs)
	}
	if got := byIdx[4].Text; got != "quota reached" {
		t.Errorf("error text = %q", got)
	}
	if got := byIdx[5].Text; got != "[Notice] background tasks were stopped." {
		t.Errorf("system text = %q", got)
	}
	if got := byIdx[3].Text; got != "" {
		t.Errorf("empty generation produced text %q", got)
	}
	if got := byIdx[7].Text; got != "" {
		t.Errorf("subagent step produced text %q", got)
	}
}

func TestRoles(t *testing.T) {
	c := loadFixture(t)
	want := map[int64]string{
		0: "user", 1: "model", 2: "tool", 4: "error", 5: "system", 7: "subagent",
	}
	for _, s := range c.Steps {
		if w, ok := want[s.Idx]; ok {
			if got := s.Role(); got != w {
				t.Errorf("idx %d role = %q, want %q", s.Idx, got, w)
			}
		}
	}
}

func TestTranscriptDropsOpaqueSteps(t *testing.T) {
	c := loadFixture(t)
	got := c.Transcript()
	var idxs []int64
	for _, s := range got {
		idxs = append(idxs, s.Idx)
	}
	want := []int64{0, 1, 2, 4, 5, 6, 8}
	if len(idxs) != len(want) {
		t.Fatalf("Transcript idxs = %v, want %v", idxs, want)
	}
	for i := range want {
		if idxs[i] != want[i] {
			t.Fatalf("Transcript idxs = %v, want %v", idxs, want)
		}
	}
}

func TestCleanToolResult(t *testing.T) {
	// Cycle 178: tool results recovered from step_payload are framed
	// with "The command exited with code N.\nOutput:\n<actual stdout>\n"
	// and use CRLF line endings internally. The renderer needs the raw
	// stdout (no boilerplate, no stray \r) or every interior row is
	// blanked out.
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty",
			raw:  "",
			want: "",
		},
		{
			name: "no prefix",
			raw:  "hello\nworld\n",
			want: "hello\nworld",
		},
		{
			name: "run_command prefix stripped",
			raw:  "The command exited with code 0.\nOutput:\nhello\nworld\n",
			want: "hello\nworld",
		},
		{
			name: "CRLF normalised",
			raw:  "The command exited with code 0.\nOutput:\nhello\r\nworld\r\n",
			want: "hello\nworld",
		},
		{
			name: "Output: not at head preserved",
			raw:  "user said: Output: is a valid word\nnext line\n",
			want: "user said: Output: is a valid word\nnext line",
		},
		{
			name: "head blank + Output: still strips",
			raw:  "\n\nThe command exited with code 1.\nOutput:\nline a\nline b\n",
			want: "line a\nline b",
		},
		{
			name: "lone \\r on a line stays in place",
			raw:  "Output:\nfoo\rbar\n",
			want: "foo\rbar",
		},
		{
			// The reference's 1.4.4+ shape: the CLI's own formatter.
			name: "current shape prefix stripped",
			raw:  "Command exited with code 0.\n\nCommand output:\nhello\nworld\n",
			want: "hello\nworld",
		},
		{
			name: "current shape with the stamp",
			raw:  "Created At: x\nCompleted At: y\nCommand exited with code 2.\n\nCommand output:\nboom\n",
			want: "boom",
		},
		{
			name: "current shape, no output",
			raw:  "Created At: x\nCompleted At: y\nCommand exited with code 0.\n\nNo output from the command.",
			want: "Created At: x\nCompleted At: y\nCommand exited with code 0.\n\nNo output from the command.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanToolResult(tt.raw)
			if got != tt.want {
				t.Errorf("cleanToolResult(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestChatTurns(t *testing.T) {
	c := loadFixture(t)
	turns := c.ChatTurns()
	want := []ChatTurn{
		{Role: "user", Text: "summarise the repository layout"},
		{Role: "assistant", Text: "I will start by listing the top level."},
		{Role: "user", Text: "now do the same for internal/"},
		{Role: "assistant", Text: "Done: internal/ has these packages."},
	}
	if len(turns) != len(want) {
		t.Fatalf("ChatTurns = %d turns, want %d: %+v", len(turns), len(want), turns)
	}
	for i := range want {
		if turns[i] != want[i] {
			t.Errorf("turn %d = %+v, want %+v", i, turns[i], want[i])
		}
	}
}

func TestLastChatTurnsStartsOnUser(t *testing.T) {
	c := loadFixture(t)
	turns := c.LastChatTurns(3)
	if len(turns) != 2 || turns[0].Role != "user" {
		t.Fatalf("LastChatTurns(3) = %+v, want a 2-turn window starting with user", turns)
	}
	if turns[0].Text != "now do the same for internal/" {
		t.Errorf("window starts at %q", turns[0].Text)
	}
	if got := c.LastChatTurns(0); len(got) != 4 {
		t.Errorf("LastChatTurns(0) = %d turns, want all 4", len(got))
	}
}

func TestConversationExistsMatchesReference(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "cafe0000-0000-4000-8000-000000000002"

	if HasConversation(id) {
		t.Fatal("reported a conversation that does not exist")
	}
	// A zero-byte file is accepted by the reference, so it must be accepted here.
	if err := os.WriteFile(ConversationPath(id), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasConversation(id) {
		t.Error("a zero-byte conversations/<id>.db should count as existing")
	}
	if got := FindConversation(id); got == "" {
		t.Error("FindConversation returned empty for an existing conversation")
	}
	if HasConversation("") {
		t.Error("the empty id must never resolve to a conversation")
	}
	// The lookup compares directory entries, so it stays case-sensitive even
	// on a case-insensitive filesystem. The reference behaves this way:
	// `--conversation=F65CFBA7-…` reports "not found" for the existing
	// conversations/f65cfba7-….db.
	if HasConversation(strings.ToUpper(id)) {
		t.Error("an upper-cased id must not match a lower-case conversation")
	}
	if HasConversation(id + "/x") {
		t.Error("an id containing a path separator must not resolve")
	}

	// A directory named <id>.db does not count: the reference prints
	// `warning: conversation %q not found` for it.
	const dirID = "cafe0000-0000-4000-8000-000000000003"
	if err := os.Mkdir(ConversationPath(dirID), 0o755); err != nil {
		t.Fatal(err)
	}
	if HasConversation(dirID) {
		t.Error("a directory named <id>.db should not count as a conversation")
	}
}

func TestLoadConversationToleratesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.db")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConversation(p)
	if err != nil {
		t.Fatalf("LoadConversation on a zero-byte store: %v", err)
	}
	if len(c.Steps) != 0 || len(c.Transcript()) != 0 {
		t.Errorf("expected an empty conversation, got %d steps", len(c.Steps))
	}
}

func TestParseUserInputStep_Shapes(t *testing.T) {
	// Shape 1: field 19 -> field 2 (CortexStepUserInput.user_response)
	payload1 := encodeProtoField(19, 2, encodeProtoField(2, 2, []byte("/boost hello world")))
	s1 := &Step{Type: StepUserInput}
	parseStepPayload(s1, payload1)
	if s1.Text != "/boost hello world" {
		t.Errorf("shape 1 (user_response): got %q, want %q", s1.Text, "/boost hello world")
	}

	// Shape 2: field 19 with multiple field 3 items (first is subagent orchestrator config, second is user prompt text)
	orchestratorItem := encodeProtoField(2, 2, []byte("orchestrator config binary bytes"))
	userPromptItem := encodeProtoField(1, 2, []byte("actual user prompt"))
	f19Bytes := append(encodeProtoField(3, 2, orchestratorItem), encodeProtoField(3, 2, userPromptItem)...)
	payload2 := encodeProtoField(19, 2, f19Bytes)
	s2 := &Step{Type: StepUserInput}
	parseStepPayload(s2, payload2)
	if s2.Text != "actual user prompt" {
		t.Errorf("shape 2 (multi item): got %q, want %q", s2.Text, "actual user prompt")
	}

	// Shape 3: user_response takes precedence over items
	f19WithBoth := append(encodeProtoField(2, 2, []byte("user verbatim response")), encodeProtoField(3, 2, userPromptItem)...)
	payload3 := encodeProtoField(19, 2, f19WithBoth)
	s3 := &Step{Type: StepUserInput}
	parseStepPayload(s3, payload3)
	if s3.Text != "user verbatim response" {
		t.Errorf("shape 3 (precedence): got %q, want %q", s3.Text, "user verbatim response")
	}

	// Shape 4: binary non-printable bytes in field 3 must never leak into s.Text
	binaryItem := []byte{0x12, 0xaa, 0x04, 0xda, 0x01, 0xa6, 0x04}
	payload4 := encodeProtoField(19, 2, encodeProtoField(3, 2, binaryItem))
	s4 := &Step{Type: StepUserInput}
	parseStepPayload(s4, payload4)
	if s4.Text != "" {
		t.Errorf("shape 4 (binary reject): got %q, want empty", s4.Text)
	}
}

