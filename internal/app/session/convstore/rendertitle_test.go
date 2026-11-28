package convstore

import "testing"

// TestRenderTitleUnwrapsTheDisplayVerb — the write path must store the
// noun phrase, not the collapsed display line.
//
// The bug this pins: toolsteps.Step.Summary is "Ran (go build ./...)".
// Storing it verbatim put the verb in field 9, and every reader that
// joins field 5 to field 9 printed "Ran Ran (go build ./...)".
func TestRenderTitleUnwrapsTheDisplayVerb(t *testing.T) {
	cases := []struct {
		name    string
		summary string
		tool    string
		args    string
		want    string
	}{
		{
			name:    "terminal parenthesised",
			summary: "Ran (go build ./...)",
			tool:    "run_command",
			args:    `{"CommandLine":"go build ./..."}`,
			want:    "go build ./...",
		},
		{
			name:    "file parenthesised",
			summary: "Explored 1 file (main.go)",
			tool:    "view_file",
			args:    `{"Path":"/tmp/main.go"}`,
			want:    "main.go",
		},
		{
			name:    "placeholder falls back to the arguments",
			summary: "Ran (a command)",
			tool:    "run_command",
			args:    `{"CommandLine":"echo one"}`,
			want:    "echo one",
		},
		{
			name:    "already a noun phrase",
			summary: "go build ./...",
			tool:    "run_command",
			args:    `{"CommandLine":"go build ./..."}`,
			want:    "go build ./...",
		},
		{
			name:    "bare verb start is unwrapped",
			summary: "Ran go build ./...",
			tool:    "run_command",
			args:    `{"CommandLine":"go build ./..."}`,
			want:    "go build ./...",
		},
		{
			name:    "empty summary derives from the command",
			summary: "",
			tool:    "run_command",
			args:    `{"CommandLine":"git status --short"}`,
			want:    "git status --short",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TitleFromSummary(c.summary, c.tool, c.args); got != c.want {
				t.Errorf("TitleFromSummary(%q) = %q, want %q", c.summary, got, c.want)
			}
		})
	}
}

// TestRenderInfoStoresTheTitleWithoutTheVerb — the round trip that broke.
//
// encodeStepRenderInfo writes field 5 (prefix) and field 9 (title). The
// joined form a reader builds must contain exactly one verb.
func TestRenderInfoStoresTheTitleWithoutTheVerb(t *testing.T) {
	s := Step{
		Type:        StepToolCall,
		Status:      3,
		ToolName:    "run_command",
		ToolArgs:    `{"CommandLine":"go build ./..."}`,
		ToolResult:  "ok",
		ToolSummary: "Ran (go build ./...)",
	}
	rinfo := encodeStepRenderInfo(s)

	// Read the two fields back the way agy.go does.
	fields := protoFields(rinfo)
	prefix := protoText(fields, 5)
	title := ""
	if p9 := protoBytes(fields, 9); len(p9) > 0 {
		title = protoText(protoFields(p9), 1)
	}

	if prefix != "Ran" {
		t.Fatalf("field 5 = %q, want %q", prefix, "Ran")
	}
	if title != "go build ./..." {
		t.Errorf("field 9 = %q, want the noun phrase %q", title, "go build ./...")
	}
	if joined := prefix + " " + title; joined != "Ran go build ./..." {
		t.Errorf("joined = %q, want exactly one verb", joined)
	}
}
