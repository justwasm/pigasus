// failure_test.go — the reference's wording for a tool call that failed,
// pinned against the reference.
//
// Every expectation below is the reference binary's own output, captured
// through the SDK's protocol (一手 — the differential harness is described
// in docs/cycles/cycle-308-harness-tool-surface/deliverable.md).
package harnesstools

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"
)

// TestFailedWordingPerTool pins the wording of the four tools whose
// failure the captures show.
//
// The step's reason names the path in the tool's own words — "path",
// "directory", "search path", "search directory" — and the frame's
// sentence is the tool's own sentence. Two of the four print the path
// bare in the reason; the frame always prints the URI.
func TestFailedWordingPerTool(t *testing.T) {
	cases := []struct {
		tool     string
		args     string
		reason   string
		sentence string
	}{
		{
			tool:     ToolViewFile,
			args:     `{"AbsolutePath": "/ws/nope.txt"}`,
			reason:   "path file:///ws/nope.txt does not exist",
			sentence: "Cannot view file file:///ws/nope.txt which does not exist.",
		},
		{
			tool:     ToolListDir,
			args:     `{"DirectoryPath": "/ws/nodir"}`,
			reason:   "directory /ws/nodir does not exist",
			sentence: "Cannot list directory file:///ws/nodir which does not exist.",
		},
		{
			tool:     ToolGrepSearch,
			args:     `{"SearchPath": "/ws/nodir", "Query": "zz"}`,
			reason:   "search path file:///ws/nodir does not exist",
			sentence: "Cannot search path file:///ws/nodir which does not exist.",
		},
		{
			tool:     ToolFindByName,
			args:     `{"SearchDirectory": "/ws/nodir", "Pattern": "zz"}`,
			reason:   "search directory /ws/nodir does not exist",
			sentence: "Cannot search directory file:///ws/nodir which does not exist.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			at := time.Date(2026, 10, 26, 4, 52, 19, 0, time.Local)
			in := Input{
				Tool:     tc.tool,
				Args:     json.RawMessage(tc.args),
				Result:   "open /ws/nope.txt: no such file or directory",
				Started:  at,
				Finished: at,
			}
			out := Failed(in, errors.New("open /ws/nope.txt: no such file or directory"))
			want := "Created At: " + in.Started.Format(time.RFC3339) + "\n" +
				"Completed At: " + in.Finished.Format(time.RFC3339) + "\n" +
				"Encountered error in step execution: " + tc.reason
			if out.Text != want {
				t.Errorf("text =\n%q\nwant\n%q", out.Text, want)
			}
			if out.ErrorMessage != tc.sentence {
				t.Errorf("frame sentence = %q, want %q", out.ErrorMessage, tc.sentence)
			}
			if !out.Failed {
				t.Error("the outcome does not say the call failed")
			}
		})
	}
}

// TestFailedKeepsTheCloneError pins the fallback.
//
// A failure the reference's wording is not known for keeps the tool's own
// error — in the reference's shape, but with the clone's words.
func TestFailedKeepsTheCloneError(t *testing.T) {
	in := Input{Tool: ToolWriteToFile, Args: json.RawMessage(`{"TargetFile": "/ws/x.txt"}`), Result: "permission denied"}
	out := Failed(in, errors.New("permission denied"))
	if !strings.Contains(out.Text, "Encountered error in step execution: permission denied") {
		t.Errorf("text = %q, want the clone's own error", out.Text)
	}
	if out.ErrorMessage != "permission denied" {
		t.Errorf("frame sentence = %q, want the clone's own error", out.ErrorMessage)
	}
}

// TestFailedUsesTheSnakeCaseSpelling pins the alias table.
//
// The clone's own catalogue spells the paths in snake_case; the harness's
// tools spell them in PascalCase, and the alias table lets both through.
func TestFailedUsesTheSnakeCaseSpelling(t *testing.T) {
	in := Input{Tool: ToolViewFile, Args: json.RawMessage(`{"path": "/ws/nope.txt"}`), Result: "x"}
	out := Failed(in, fs.ErrNotExist)
	if !strings.Contains(out.Text, "path file:///ws/nope.txt does not exist") {
		t.Errorf("text = %q, want the reference's wording", out.Text)
	}
}

// TestFailedRunCommand pins the command's own shape.
//
// A command's failure is printed through the step string, so the text
// carries the step's error branch — with a blank line between the stamp
// and the error — and the frame carries the reason itself.
func TestFailedRunCommand(t *testing.T) {
	at := time.Date(2026, 10, 26, 5, 11, 11, 0, time.Local)
	in := Input{
		Tool:     ToolRunCommand,
		Args:     json.RawMessage(`{"CommandLine": "ls", "Cwd": "/ws/nodir"}`),
		Result:   "error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND: /ws/nodir: no such directory",
		Started:  at,
		Finished: at,
	}
	out := Failed(in, errors.New(in.Result))
	want := "Created At: " + in.Started.Format(time.RFC3339) + "\n" +
		"Completed At: " + in.Finished.Format(time.RFC3339) + "\n" +
		"\nEncountered error in step execution: " + in.Result + "\n"
	if out.Text != want {
		t.Errorf("text =\n%q\nwant\n%q", out.Text, want)
	}
	if out.ErrorMessage != in.Result {
		t.Errorf("frame sentence = %q, want the reason", out.ErrorMessage)
	}
}

// TestMissingDirectoryError pins the reference's wording for a command
// whose working directory is gone.
//
// The reference names the step type the command failed in; the CLI's own
// runner keeps the workspace instead, so the harness checks first.
func TestMissingDirectoryError(t *testing.T) {
	err := MissingDirectoryError("/ws/nodir")
	want := "error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND: /ws/nodir: no such directory"
	if err.Error() != want {
		t.Errorf("MissingDirectoryError = %q, want %q", err.Error(), want)
	}
}
