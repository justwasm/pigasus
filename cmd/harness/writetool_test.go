package main

import (
	"context"
	"encoding/json"
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
)

// The reference's write tool ends the file with a newline.
//
// A content that does not end with one gets one, and a content that
// already ends with one does not get a second.
func TestWriteArgsEndsTheContentWithANewline(t *testing.T) {
	tool := &harnessTool{inner: &stubTool{name: "write_to_file"}}

	args := tool.writeArgs(json.RawMessage(`{"TargetFile":"/tmp/x.txt","CodeContent":"x"}`), "")
	if got := contentOf(t, args); got != "x\n" {
		t.Errorf("content = %q, want %q", got, "x\n")
	}

	args = tool.writeArgs(json.RawMessage(`{"TargetFile":"/tmp/x.txt","CodeContent":"x\n"}`), "")
	if got := contentOf(t, args); got != "x\n" {
		t.Errorf("content = %q, want %q", got, "x\n")
	}
}

// The reference's write tool appends when the call says so, and the file's
// own content is left alone.
func TestWriteArgsAppends(t *testing.T) {
	tool := &harnessTool{inner: &stubTool{name: "write_to_file"}}

	args := tool.writeArgs(json.RawMessage(`{"TargetFile":"/tmp/x.txt","CodeContent":"y","Append":true}`), "x\n")
	if got := contentOf(t, args); got != "x\ny\n" {
		t.Errorf("content = %q, want %q", got, "x\ny\n")
	}
	if !overwriteOf(t, args) {
		t.Errorf("the append does not overwrite: %s", args)
	}
}

// A tool that does not write a file keeps its arguments.
func TestWriteArgsLeavesTheOtherToolsAlone(t *testing.T) {
	tool := &harnessTool{inner: &stubTool{name: "view_file"}}
	raw := json.RawMessage(`{"AbsolutePath":"/tmp/x.txt"}`)
	if got := tool.writeArgs(raw, ""); string(got) != string(raw) {
		t.Errorf("args = %s, want %s", got, raw)
	}
}

// contentOf reads the content a rewritten call carries.
func contentOf(t *testing.T, args json.RawMessage) string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	raw, ok := fields["content"]
	if !ok {
		t.Fatalf("the args carry no content: %s", args)
	}
	var content string
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	return content
}

// overwriteOf reports whether a rewritten call overwrites the file.
func overwriteOf(t *testing.T, args json.RawMessage) bool {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	raw, ok := fields["overwrite"]
	if !ok {
		return false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode overwrite: %v", err)
	}
	return value
}

// stubTool is a tool that does nothing but answer with its name.
type stubTool struct{ name string }

func (s *stubTool) Name() string { return s.name }

func (s *stubTool) Label() string { return s.name }

func (s *stubTool) Schema() pi.ToolSchema { return pi.ToolSchema{} }

func (s *stubTool) ExecutionMode() piagent.ToolExecutionMode { return piagent.ToolModeParallel }

func (s *stubTool) Execute(context.Context, string, json.RawMessage, piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	return piagent.AgentToolResult{}, nil
}
