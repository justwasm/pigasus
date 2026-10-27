// invalid_test.go — the reference's invalid tool call path, pinned against
// the reference.
//
// Every expectation below is the reference binary's own output, captured
// through the SDK's protocol (一手 — the differential harness is described
// in docs/cycles/cycle-310-harness-invalid-tool-calls/deliverable.md).
package harnesstools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The paragraph the model reads for a refused call.
//
// The reference's own text, with its trailing spaces: every line but the
// last one ends with a space, and the reason line ends with one too.
func TestInvalidParagraphIsTheReferencesOwn(t *testing.T) {
	at := time.Date(2026, 10, 26, 4, 51, 13, 0, time.Local)
	reason := "model output error: invalid tool call error (invalid_signature) " +
		"AbsolutePath is a required parameter. Follow the function call schema exactly."
	want := "Created At: " + at.Format(time.RFC3339) + "\n" +
		"Error invalid tool call: There was a problem parsing the tool call. \n" +
		"Error Message: " + reason + " \n" +
		"Guidance: You are trying to correct your previous tool call error, you must focus on fixing the failed tool call with sequential tool calls and try again. " +
		"Do not do parallel tool calls and if you are fixing multiple tool calls, do them one at a time. Do not apologize. \n" +
		"Retries remaining: 4."

	if got := Invalid(reason, 4, at); got != want {
		t.Errorf("paragraph =\n%q\nwant\n%q", got, want)
	}
}

// The frame of a refused call quotes the reason.
//
// The frame's own sentence is the generic one, and the text carries the
// reason in quotes.
func TestInvalidFrameQuotesTheReason(t *testing.T) {
	reason := "model output error: invalid tool call error (invalid_args) argument TargetFile not found"
	want := `The model produced an invalid tool call. ("` + reason + `")`

	if got := InvalidFrame("", reason); got != want {
		t.Errorf("frame text = %q, want %q", got, want)
	}
}

// A refusal the reference words of its own carries that sentence.
//
// An edit of a file that is not there says so in the frame's errorMessage
// and in the frame's text.
func TestInvalidFrameCarriesTheToolsOwnSentence(t *testing.T) {
	reason := "model output error: invalid tool call error (invalid_args) " +
		"/ws/nope.txt does not exist in the current location. Make sure the file path correct. " +
		"In addition, the user may have moved, renamed, or deleted file."
	want := `Agent cannot edit files that do not exist. ("` + reason + `")`

	if got := InvalidFrame("Agent cannot edit files that do not exist.", reason); got != want {
		t.Errorf("frame text = %q, want %q", got, want)
	}
}

// The reason the reference gives, with its kind.
func TestInvalidCallReason(t *testing.T) {
	call := InvalidCall{Kind: InvalidArgs, Detail: "argument Overwrite not found"}
	want := "model output error: invalid tool call error (invalid_args) argument Overwrite not found"

	if got := call.Reason(); got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}
}

// The calls the reference refuses, with the reference's own reason.
//
// The reference's runtime schema is not the schema it advertises, so a
// call the reference runs is never refused here. The order of the checks
// is the reference's own: the write tools check their own arguments
// first, then the schema's required fields, then the path.
func TestCheckCallRefusesWhatTheReferenceRefuses(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "three.txt")
	if err := os.WriteFile(existing, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}
	missing := filepath.Join(dir, "nodir.txt")

	cases := []struct {
		name   string
		tool   string
		args   string
		kind   string
		detail string
	}{
		{
			name:   "view_file without a path",
			tool:   ToolViewFile,
			args:   `{}`,
			kind:   InvalidSignature,
			detail: "AbsolutePath is a required parameter. Follow the function call schema exactly.",
		},
		{
			name:   "view_file with an empty path",
			tool:   ToolViewFile,
			args:   `{"AbsolutePath":""}`,
			kind:   InvalidSignature,
			detail: "AbsolutePath is a required parameter. Follow the function call schema exactly.",
		},
		{
			name:   "view_file with a relative path",
			tool:   ToolViewFile,
			args:   `{"AbsolutePath":"rel.txt"}`,
			kind:   InvalidArgs,
			detail: "rel.txt must be an absolute path: path is not absolute",
		},
		{
			name:   "list_dir with a relative path",
			tool:   ToolListDir,
			args:   `{"DirectoryPath":"rel"}`,
			kind:   InvalidArgs,
			detail: "rel must be an absolute path: path is not absolute",
		},
		{
			name:   "grep_search without a query",
			tool:   ToolGrepSearch,
			args:   `{}`,
			kind:   InvalidSignature,
			detail: "Query is required",
		},
		{
			name:   "grep_search with an empty query",
			tool:   ToolGrepSearch,
			args:   `{"Query":"","SearchPath":"/ws"}`,
			kind:   InvalidSignature,
			detail: "Query is required",
		},
		{
			name:   "grep_search without a search path",
			tool:   ToolGrepSearch,
			args:   `{"Query":"alpha"}`,
			kind:   InvalidSignature,
			detail: "SearchPath is required",
		},
		{
			name:   "grep_search with a relative search path",
			tool:   ToolGrepSearch,
			args:   `{"Query":"alpha","SearchPath":"rel"}`,
			kind:   InvalidArgs,
			detail: "rel must be an absolute path: path is not absolute",
		},
		{
			name:   "find_by_name without a search directory",
			tool:   ToolFindByName,
			args:   `{}`,
			kind:   InvalidSignature,
			detail: "SearchDirectory is required",
		},
		{
			name:   "write_to_file without a target",
			tool:   ToolWriteToFile,
			args:   `{}`,
			kind:   InvalidArgs,
			detail: "argument TargetFile not found",
		},
		{
			name:   "write_to_file without the overwrite flag",
			tool:   ToolWriteToFile,
			args:   `{"TargetFile":"/ws/x.txt"}`,
			kind:   InvalidArgs,
			detail: "argument Overwrite not found",
		},
		{
			name:   "write_to_file without the content",
			tool:   ToolWriteToFile,
			args:   `{"TargetFile":"/ws/x.txt","Overwrite":false}`,
			kind:   InvalidSignature,
			detail: "CodeContent is a required parameter. Please follow the function call schema exactly.",
		},
		{
			name:   "write_to_file with a relative target",
			tool:   ToolWriteToFile,
			args:   `{"TargetFile":"rel.txt","Overwrite":false,"CodeContent":"x"}`,
			kind:   InvalidArgs,
			detail: "rel.txt must be an absolute path: path is not absolute",
		},
		{
			name:   "replace_file_content without a target",
			tool:   ToolReplaceFile,
			args:   `{}`,
			kind:   InvalidArgs,
			detail: "argument TargetFile not found",
		},
		{
			name:   "replace_file_content without the chunk",
			tool:   ToolReplaceFile,
			args:   `{"TargetFile":"/ws/x.txt"}`,
			kind:   InvalidSignature,
			detail: "ReplacementChunk is required",
		},
		{
			name:   "replace_file_content with a relative target",
			tool:   ToolReplaceFile,
			args:   `{"TargetFile":"rel.txt","ReplacementChunk":"c"}`,
			kind:   InvalidSignature,
			detail: "ReplacementChunk is required",
		},
		{
			name: "replace_file_content with a target that is not there",
			tool: ToolReplaceFile,
			args: `{"TargetFile":"` + missing + `","TargetContent":"a"}`,
			kind: InvalidArgs,
			detail: missing + " does not exist in the current location. Make sure the file path correct. " +
				"In addition, the user may have moved, renamed, or deleted file.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call, ok := CheckCall(tc.tool, json.RawMessage(tc.args), nil)
			if !ok {
				t.Fatalf("the call was accepted, want a refusal")
			}
			if call.Kind != tc.kind {
				t.Errorf("kind = %q, want %q", call.Kind, tc.kind)
			}
			if call.Detail != tc.detail {
				t.Errorf("detail = %q, want %q", call.Detail, tc.detail)
			}
		})
	}
}

// The calls the reference runs, with the reference's own reason.
//
// The reference's runtime schema is not the advertised one: a
// `run_command` with no `CommandLine` runs, a `list_dir` with no
// `DirectoryPath` runs, a `find_by_name` with no `Pattern` runs, and a
// `find_by_name` with a relative directory runs — the step fails
// instead.
func TestCheckCallRunsWhatTheReferenceRuns(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "three.txt")
	if err := os.WriteFile(existing, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}

	cases := []struct {
		name string
		tool string
		args string
	}{
		{"list_dir without a directory", ToolListDir, `{}`},
		{"run_command without a command line", ToolRunCommand, `{}`},
		{"run_command with a relative cwd", ToolRunCommand, `{"CommandLine":"ls","Cwd":"rel"}`},
		{"find_by_name without a pattern", ToolFindByName, `{"SearchDirectory":"/ws"}`},
		{"find_by_name with a relative directory", ToolFindByName, `{"SearchDirectory":"rel","Pattern":"*.txt"}`},
		{"write_to_file with a description too", ToolWriteToFile, `{"TargetFile":"/ws/x.txt","Overwrite":false,"CodeContent":"x","Description":"d"}`},
		{"replace_file_content with the target content", ToolReplaceFile, `{"TargetFile":"` + existing + `","TargetContent":"a"}`},
		{"replace_file_content with the replacement content", ToolReplaceFile, `{"TargetFile":"` + existing + `","ReplacementContent":"b"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call, ok := CheckCall(tc.tool, json.RawMessage(tc.args), nil)
			if ok {
				t.Fatalf("the call was refused (%s): %s", call.Kind, call.Detail)
			}
		})
	}
}

// A call to a tool the session does not advertise is an unknown tool.
func TestCheckCallRefusesAnUnknownTool(t *testing.T) {
	known := map[string]bool{ToolViewFile: true, ToolListDir: true}

	call, ok := CheckCall("no_such_tool", json.RawMessage(`{}`), known)
	if !ok {
		t.Fatalf("the call was accepted, want a refusal")
	}
	if call.Kind != UnknownTool {
		t.Errorf("kind = %q, want %q", call.Kind, UnknownTool)
	}
	if call.Detail != "unknown tool name: `no_such_tool`" {
		t.Errorf("detail = %q, want the reference's own", call.Detail)
	}
}

// A tool of the session is not an unknown tool.
func TestCheckCallAcceptsAToolOfTheSession(t *testing.T) {
	known := map[string]bool{ToolViewFile: true}

	if _, ok := CheckCall(ToolViewFile, json.RawMessage(`{"AbsolutePath":"/ws/a.txt"}`), known); ok {
		t.Fatalf("the call was refused, want it to run")
	}
}

// An argument that is there is an argument.
//
// A boolean that is false is still the model's answer: `Overwrite: false`
// is not a missing argument. A string that is empty does not fill a
// field: `Query: ""` is still a call without a query.
func TestArgumentPresence(t *testing.T) {
	args := json.RawMessage(`{"Overwrite":false,"Query":"","Path":"/ws/a.txt"}`)

	if !argPresent(args, "Overwrite") {
		t.Errorf("Overwrite is present, want it found")
	}
	if argPresent(args, "Missing") {
		t.Errorf("Missing is absent, want it not found")
	}
	if argFilled(args, "Query") {
		t.Errorf("Query is empty, want it not filled")
	}
	if !argFilled(args, "Path") {
		t.Errorf("Path is filled, want it found")
	}
}
