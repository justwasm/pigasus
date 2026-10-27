// harnesstools_test.go — the renderings, pinned against the reference.
//
// Every expectation below is the reference binary's own output, captured
// through the SDK's protocol (一手 — the differential harness is described in
// docs/cycles/cycle-307-harness-tool-surface/deliverable.md). The captures
// are the reference's; the tests fail when the clone drifts away from them.
package harnesstools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stampTime is the timestamp the captures carry.
var stampTime = time.Date(2026, 10, 26, 2, 32, 14, 0, time.FixedZone("CST", 8*3600))

// stampLines is the two timestamp lines every result carries.
func stampLines() string {
	return "Created At: 2026-10-26T02:32:14+08:00\nCompleted At: 2026-10-26T02:32:14+08:00\n"
}

// TestRenderRunCommand pins the reference's run_command result.
//
// The reference prints a command's outcome through the step-string
// template, not through the CLI's own formatter, and its terminal is a
// pty — so the output reaches the model with CRLF line endings:
//
//	Created At: …
//	Completed At: …
//
//	The command exited with code 0.
//	Output:
//	a
//
// The captured body is "a\r\nb\n": the command wrote "a\nb", the pty
// turned the newline into CRLF, and the template closed the block.
func TestRenderRunCommand(t *testing.T) {
	out := Render(Input{
		Tool:     ToolRunCommand,
		Args:     json.RawMessage(`{"CommandLine":"printf 'a\\nb'"}`),
		Result:   "Created At: 2026-10-26T02:36:21+08:00\nCompleted At: 2026-10-26T02:36:21+08:00\nCommand exited with code 0.\n\nCommand output:\na\nb\n",
		ExitCode: 0,
		Output:   "a\nb",
		Started:  stampTime,
		Finished: stampTime,
	})
	want := stampLines() +
		"\nThe command exited with code 0.\n" +
		"Output:\n" +
		"a\r\nb\n"
	if out.Text != want {
		t.Errorf("run_command text =\n%q\nwant\n%q", out.Text, want)
	}
	if out.Output != "a\r\nb" {
		t.Errorf("run_command output = %q, want %q", out.Output, "a\r\nb")
	}
	if !out.HasOutput {
		t.Error("run_command HasOutput = false, want true")
	}
}

// TestRenderRunCommandWithoutOutput pins the reference's other branch.
//
// A command that printed nothing takes the template's stdout/stderr
// branch, and the reference's result reads:
//
//	Stdout:
//
//	Stderr:
func TestRenderRunCommandWithoutOutput(t *testing.T) {
	out := Render(Input{
		Tool:     ToolRunCommand,
		Args:     json.RawMessage(`{"CommandLine":"true"}`),
		ExitCode: 0,
		Started:  stampTime,
		Finished: stampTime,
	})
	want := stampLines() +
		"\nThe command exited with code 0.\n" +
		"Stdout:\n\nStderr:\n\n"
	if out.Text != want {
		t.Errorf("run_command text =\n%q\nwant\n%q", out.Text, want)
	}
	if out.HasOutput {
		t.Error("run_command HasOutput = true, want false")
	}
}

// TestRenderRunCommandFailure pins the exit code in the step string.
func TestRenderRunCommandFailure(t *testing.T) {
	out := Render(Input{
		Tool:     ToolRunCommand,
		Args:     json.RawMessage(`{"CommandLine":"false"}`),
		ExitCode: 1,
		Started:  stampTime,
		Finished: stampTime,
	})
	if want := "The command exited with code 1."; !strings.Contains(out.Text, want) {
		t.Errorf("run_command text = %q, want it to contain %q", out.Text, want)
	}
}

// TestRenderWriteToFile pins the reference's write_to_file result.
//
// The reference names the file it wrote with a `file://` URI and tells the
// model to run the code rather than ask for permission.
func TestRenderWriteToFile(t *testing.T) {
	out := Render(Input{
		Tool:     ToolWriteToFile,
		Args:     json.RawMessage(`{"TargetFile":"/private/tmp/probes/written.txt"}`),
		After:    "written\n",
		Started:  stampTime,
		Finished: stampTime,
	})
	want := stampLines() +
		"Created file file:///private/tmp/probes/written.txt with requested content.\n" +
		"If relevant, proactively run terminal commands to execute this code for the USER. Don't ask for permission."
	if out.Text != want {
		t.Errorf("write_to_file text =\n%q\nwant\n%q", out.Text, want)
	}
}

// TestRenderWriteToFileDiff pins the action's diff block.
//
// The reference reports a created file as an *edit* whose diff block is the
// file's new lines, all inserted, at the top of an empty file.
func TestRenderWriteToFileDiff(t *testing.T) {
	out := Render(Input{
		Tool:     ToolWriteToFile,
		Args:     json.RawMessage(`{"TargetFile":"/private/tmp/probes/written.txt"}`),
		After:    "written\n",
		Started:  stampTime,
		Finished: stampTime,
	})
	if len(out.Diff) != 1 {
		t.Fatalf("write_to_file diff blocks = %d, want 1", len(out.Diff))
	}
	block := out.Diff[0]
	if block.StartLine != 0 || block.EndLine != 0 {
		t.Errorf("diff range = %d..%d, want 0..0", block.StartLine, block.EndLine)
	}
	wantLines := []DiffLine{
		{Text: "written", Action: LineInsert},
		{Text: "", Action: LineInsert},
	}
	if len(block.Lines) != len(wantLines) {
		t.Fatalf("diff lines = %d, want %d", len(block.Lines), len(wantLines))
	}
	for i, line := range block.Lines {
		if line != wantLines[i] {
			t.Errorf("diff line %d = %+v, want %+v", i, line, wantLines[i])
		}
	}
}

// TestRenderReplaceFile pins the reference's replace_file_content result.
//
// The reference's text carries the diff of the change between its own
// markers, and the note below says how wide the context is.
func TestRenderReplaceFile(t *testing.T) {
	out := Render(Input{
		Tool:     ToolReplaceFile,
		Args:     json.RawMessage(`{"TargetFile":"/private/tmp/probes/three.txt"}`),
		Before:   "alpha\nbeta\ngamma\n",
		After:    "alpha\nBETA\ngamma\n",
		Started:  stampTime,
		Finished: stampTime,
	})
	want := stampLines() +
		"The following changes were made by the replace_file_content tool to: /private/tmp/probes/three.txt. " +
		"If relevant, proactively run terminal commands to execute this code for the USER. Don't ask for permission.\n" +
		"[diff_block_start]\n" +
		"@@ -1,4 +1,4 @@\n" +
		" alpha\n" +
		"-beta\n" +
		"+BETA\n" +
		" gamma\n" +
		" \n" +
		"[diff_block_end]\n" +
		"\nPlease note that the above snippet only shows the MODIFIED lines from the last change. " +
		"It shows up to 3 lines of unchanged lines before and after the modified lines. " +
		"The actual file contents may have many more lines not shown."
	if out.Text != want {
		t.Errorf("replace_file_content text =\n%q\nwant\n%q", out.Text, want)
	}
}

// TestRenderReplaceFileDiff pins the action's diff block.
//
// The captured block for a one-word edit in a four-line file covers old
// lines 0 to 3 — the whole file, because the context window is three
// lines — and lists the old lines with the inserted one in place.
func TestRenderReplaceFileDiff(t *testing.T) {
	out := Render(Input{
		Tool:     ToolReplaceFile,
		Args:     json.RawMessage(`{"TargetFile":"/private/tmp/probes/three.txt"}`),
		Before:   "alpha\nbeta\ngamma\n",
		After:    "alpha\nBETA\ngamma\n",
		Started:  stampTime,
		Finished: stampTime,
	})
	if len(out.Diff) != 1 {
		t.Fatalf("diff blocks = %d, want 1", len(out.Diff))
	}
	block := out.Diff[0]
	if block.StartLine != 0 || block.EndLine != 4 {
		t.Errorf("diff range = %d..%d, want 0..4", block.StartLine, block.EndLine)
	}
	wantLines := []DiffLine{
		{Text: "alpha", Action: LineNone},
		{Text: "beta", Action: LineDelete},
		{Text: "BETA", Action: LineInsert},
		{Text: "gamma", Action: LineNone},
		{Text: "", Action: LineNone},
	}
	if len(block.Lines) != len(wantLines) {
		t.Fatalf("diff lines = %d, want %d", len(block.Lines), len(wantLines))
	}
	for i, line := range block.Lines {
		if line != wantLines[i] {
			t.Errorf("diff line %d = %+v, want %+v", i, line, wantLines[i])
		}
	}
}

// TestRenderListDir pins the reference's list_dir result.
//
// The reference lists every entry on its own line as a small JSON object
// and closes with a summary of what it found.
func TestRenderListDir(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "one.txt"), "one\n")
	write(t, filepath.Join(dir, "out.txt"), "written\n")
	write(t, filepath.Join(dir, "three.txt"), "alpha\nbeta\ngamma\n")
	write(t, filepath.Join(dir, "written.txt"), "written\n")

	out := Render(Input{
		Tool:     ToolListDir,
		Args:     json.RawMessage(`{"DirectoryPath":"` + dir + `"}`),
		Started:  stampTime,
		Finished: stampTime,
	})
	want := stampLines() +
		"{\"name\":\"one.txt\", \"sizeBytes\":\"4\"}\n" +
		"{\"name\":\"out.txt\", \"sizeBytes\":\"8\"}\n" +
		"{\"name\":\"three.txt\", \"sizeBytes\":\"17\"}\n" +
		"{\"name\":\"written.txt\", \"sizeBytes\":\"8\"}\n" +
		"\nSummary: This directory contains 0 subdirectories and 4 files."
	if out.Text != want {
		t.Errorf("list_dir text =\n%q\nwant\n%q", out.Text, want)
	}
	if len(out.Entries) != 4 {
		t.Fatalf("list_dir entries = %d, want 4", len(out.Entries))
	}
	if out.Entries[0].Name != "one.txt" || out.Entries[0].Size != "4" {
		t.Errorf("first entry = %+v, want {one.txt 4}", out.Entries[0])
	}
}

// TestRenderFindByName pins the reference's find_by_name result.
//
// The reference counts the results and lists them; the action's own
// `output` field carries the names without the count.
func TestRenderFindByName(t *testing.T) {
	out := Render(Input{
		Tool:     ToolFindByName,
		Args:     json.RawMessage(`{"DirectoryPath":"/private/tmp/probes"}`),
		Result:   "one.txt\nout.txt\nthree.txt\nwritten.txt",
		Started:  stampTime,
		Finished: stampTime,
	})
	want := stampLines() + "Found 4 results\none.txt\nout.txt\nthree.txt\nwritten.txt"
	if out.Text != want {
		t.Errorf("find_by_name text =\n%q\nwant\n%q", out.Text, want)
	}
	if len(out.Found) != 4 || out.Found[0] != "one.txt" {
		t.Errorf("find_by_name names = %v, want four names starting with one.txt", out.Found)
	}
}

// TestRenderGrepSearch pins the reference's grep_search answer.
//
// The reference's search does not answer with text at all: it answers with
// the files it matched, one entry per file.
func TestRenderGrepSearch(t *testing.T) {
	out := Render(Input{
		Tool:     ToolGrepSearch,
		Args:     json.RawMessage(`{"SearchPath":"/private/tmp/probes"}`),
		Result:   "three.txt:2: beta\n",
		Started:  stampTime,
		Finished: stampTime,
	})
	results, ok := out.Response["results"].([]any)
	if !ok {
		t.Fatalf("grep_search response = %#v, want a results list", out.Response)
	}
	if len(results) != 1 {
		t.Fatalf("grep_search results = %d, want 1", len(results))
	}
	entry, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("grep_search result = %#v, want an object", results[0])
	}
	if entry["path"] != "/private/tmp/probes/three.txt" {
		t.Errorf("grep_search path = %v, want /private/tmp/probes/three.txt", entry["path"])
	}
	if out.Matches != 1 {
		t.Errorf("grep_search matches = %d, want 1", out.Matches)
	}
}

// TestRenderAskQuestion pins the reference's result for a question it could
// not ask.
//
// The questions API lives in the GAOS dialect, and the localharness dialect
// does not carry it: the call fails as a cancelled question, and the model
// sees the failed step — with a completion time, because the step ran to
// its end.
func TestRenderAskQuestion(t *testing.T) {
	out := Render(Input{
		Tool:     ToolAskQuestion,
		Args:     json.RawMessage(`{"questions":[{"question":"Which one?","options":["alpha","beta"]}]}`),
		Result:   "The user cannot be asked in this mode, so no answer was provided.",
		Started:  stampTime,
		Finished: stampTime,
	})
	want := stampLines() + "Encountered error in step execution: user cancelled ask_question"
	if out.Text != want {
		t.Errorf("ask_question text =\n%q\nwant\n%q", out.Text, want)
	}
	if out.Response != nil {
		t.Errorf("ask_question response = %#v, want none", out.Response)
	}
}

// TestRenderViewFileKeepsTheCLIText pins that a tool the harness does not
// re-render keeps the clone's own text.
//
// The reference's view_file output is byte-identical to the clone's, so
// there is nothing to re-render.
func TestRenderViewFileKeepsTheCLIText(t *testing.T) {
	const text = "Created At: 2026-10-26T02:29:00+08:00\nCompleted At: 2026-10-26T02:29:00+08:00\nFile Path: `file:///tmp/three.txt`\nTotal Lines: 4\n"
	out := Render(Input{
		Tool:     ToolViewFile,
		Args:     json.RawMessage(`{"AbsolutePath":"/tmp/three.txt"}`),
		Result:   text,
		Started:  stampTime,
		Finished: stampTime,
	})
	if out.Text != text {
		t.Errorf("view_file text = %q, want %q", out.Text, text)
	}
}

// write puts a file in the filesystem for a test.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
