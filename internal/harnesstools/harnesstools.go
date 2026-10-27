// Package harnesstools — the reference harness's own tool surface.
//
// # Why this package exists
//
// The harness does not use the CLI's tool surface. The reference's harness
// ships its own tools, and they differ from the CLI's in three observable
// ways:
//
//   - the result text they print;
//   - the action the protocol frame carries (the reference reports an edit
//     as `editFile` with a diff block, not as `createFile` with the file's
//     contents);
//   - the shape of the `functionResponse` the model receives (a search
//     answers with `{"results": [...]}` rather than `{"output": "…"}`).
//
// The clone runs the CLI's executors — the same code, the same sandbox, the
// same permission policy — and this package re-renders their results the
// way the reference's harness renders them.
//
// # The evidence
//
// Every text and every field below was captured from the reference binary
// driven through the SDK's own protocol (一手 — the differential harness is
// described in docs/cycles/cycle-307-harness-tool-surface/deliverable.md).
// The captures are the reference's own output; the shapes here are
// transcribed from them.
//
// # What is not reproduced
//
// The reference's diff is a real line diff with a context window; this
// package computes one change region (a common prefix and suffix around a
// single edit) and widens it with context. A file with several separated
// changes is reported as one region. See the 存疑 section of the cycle
// report.
package harnesstools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/agystep"
)

// The harness's tool names.
//
// The reference's harness names its tools after the operation, not after the
// CLI's catalogue: it reads a file with `view_file`, writes one with
// `write_to_file` and edits one with `replace_file_content`.
const (
	ToolViewFile      = "view_file"
	ToolRunCommand    = "run_command"
	ToolWriteToFile   = "write_to_file"
	ToolReplaceFile   = "replace_file_content"
	ToolGrepSearch    = "grep_search"
	ToolListDir       = "list_dir"
	ToolFindByName    = "find_by_name"
	ToolAskQuestion   = "ask_question"
	ToolSendMessage   = "send_message"
	ToolInvokeSubagen = "invoke_subagent"
)

// LineAction is the reference's per-line diff action.
//
// The wire spelling is the protocol enum's name; the reference writes
// "LINE_ACTION_NONE", "LINE_ACTION_INSERT" and "LINE_ACTION_DELETE".
type LineAction string

// The three diff actions the reference emits.
const (
	// LineNone marks a line that is present on both sides.
	LineNone LineAction = "LINE_ACTION_NONE"
	// LineInsert marks a line that only the new file has.
	LineInsert LineAction = "LINE_ACTION_INSERT"
	// LineDelete marks a line that only the old file has.
	LineDelete LineAction = "LINE_ACTION_DELETE"
)

// DiffLine is one line of a diff block.
type DiffLine struct {
	// Text is the line's content, without its newline.
	Text string
	// Action says what happened to the line.
	Action LineAction
}

// DiffBlock is one hunk of a diff: a range of the old file and the lines
// that replace it.
type DiffBlock struct {
	// StartLine is the first old line the block covers, zero-based.
	StartLine int
	// EndLine is the exclusive end of the old range the block covers.
	EndLine int
	// Lines are the block's lines, in file order.
	Lines []DiffLine
}

// Entry is one entry of a directory listing.
type Entry struct {
	// Name is the entry's name, without a path.
	Name string
	// Size is the entry's size in bytes, as a string — the reference's
	// protocol carries sizes as strings.
	Size string
	// IsDirectory says whether the entry is a directory.
	IsDirectory bool
}

// Outcome is the harness's rendering of one tool call.
//
// Text is what the model reads. Response is the structured payload some of
// the reference's tools answer with. The remaining fields feed the protocol
// frame the harness writes for the call.
type Outcome struct {
	// Text is the tool's result, the way the reference's harness prints it.
	Text string
	// Response is the structured functionResponse payload. It is nil for a
	// tool that answers with text — the transport then wraps the text in
	// `{"output": "…"}`.
	Response map[string]any
	// Diff is the edit the tool made, when it made one.
	Diff []DiffBlock
	// Entries is the directory listing, when the tool listed one.
	Entries []Entry
	// Found is the list of names `find_by_name` produced.
	Found []string
	// Matches is the number of files a search matched.
	Matches int
	// Output is the command's output, the way the reference's terminal
	// reports it (CRLF line endings).
	Output string
	// ExitCode is the command's exit status.
	ExitCode int
	// HasOutput says whether the command printed anything.
	HasOutput bool
	// ErrorMessage is the sentence a failed call's frame carries.
	//
	// The reference puts the tool's own sentence there — "Cannot view
	// file file://… which does not exist." — and not the whole result
	// text. It is empty for a call that did not fail.
	ErrorMessage string
	// Failed says whether the call failed.
	//
	// A failed call has no output: the reference's frame leaves the
	// action's own output field out when the step failed.
	Failed bool
}

// Input is what the renderer needs beyond the tool's own result text.
//
// The wrapper fills it in: the file's contents before and after an edit, the
// command's outcome, the tool's arguments.
type Input struct {
	// Tool is the harness's tool name.
	Tool string
	// Args are the call's arguments, in the reference's spelling.
	Args json.RawMessage
	// Result is the CLI tool's own result text.
	Result string
	// Before is the edited file's content before the tool ran.
	Before string
	// After is the edited file's content after the tool ran.
	After string
	// ExitCode and Output are the command's outcome.
	ExitCode int
	Output   string
	// Started and Finished are the tool's own timestamps.
	Started  time.Time
	Finished time.Time
	// TruncateTokens is the client's tool-output truncation budget, in
	// tokens. Zero leaves the output whole; see truncate.go for the
	// reference's rule.
	TruncateTokens int
}

// Render re-renders a tool result the way the reference's harness does.
//
// A tool the harness renders in its own way gets its own text; every other
// tool keeps the CLI's text, which already matches (the reference's
// `view_file` output is byte-identical to the clone's).
func Render(in Input) Outcome {
	switch in.Tool {
	case ToolRunCommand:
		return renderRunCommand(in)
	case ToolWriteToFile:
		return renderWriteToFile(in)
	case ToolReplaceFile:
		return renderReplaceFile(in)
	case ToolListDir:
		return renderListDir(in)
	case ToolFindByName:
		return renderFindByName(in)
	case ToolGrepSearch:
		return renderGrepSearch(in)
	case ToolAskQuestion:
		return renderAskQuestion(in)
	default:
		return Outcome{Text: Truncate(in.Result, in.TruncateTokens)}
	}
}

// QuestionsCancelled is what the harness reports for a question it could
// not ask.
//
// The reference's questions API lives in the GAOS dialect — the one the
// SDK's interactions connection speaks — and the localharness dialect does
// not carry it. A call to `ask_question` on this dialect therefore fails as
// a cancelled question.
const QuestionsCancelled = "Questions cancelled."

// renderAskQuestion renders a question the harness could not ask.
//
// The reference's text is the failed step:
//
//	Created At: 2026-10-26T02:33:49+08:00
//	Completed At: 2026-10-26T02:33:49+08:00
//	Encountered error in step execution: user cancelled ask_question
//
// The step *completed*: it failed, and a failed step that ran to its end
// carries a completion time — unlike a refused call, which never started.
func renderAskQuestion(in Input) Outcome {
	return Outcome{Text: stamp(in) + "Encountered error in step execution: user cancelled ask_question"}
}

// Denied renders the text the model sees when a call is refused.
//
// The reference renders the failed step through its own step template:
//
//	Created At: 2026-10-26T03:12:43+08:00
//	Encountered error in step execution: denied by pre-tool hook: the reason
//
// A step that failed carries no completion time: the frame has the moment
// the call started and the failure, nothing more.
func Denied(reason string, at time.Time) string {
	if at.IsZero() {
		at = time.Now()
	}
	return "Created At: " + at.Format(time.RFC3339) + "\n" +
		"Encountered error in step execution: " + reason
}

// DeniedFrame renders the text a refused call's frame carries.
//
// The frame says why the call did not run and quotes the step's own
// failure:
//
//	the reason ("denied by pre-tool hook: the reason")
func DeniedFrame(reason string) string {
	return reason + ` ("denied by pre-tool hook: ` + reason + `")`
}

// stamp renders the two timestamp lines every harness result carries.
//
// The reference writes them on every tool's result, including the tools the
// CLI's catalogue prints without one (`list_dir`, `find_by_name`).
func stamp(in Input) string {
	started := in.Started
	finished := in.Finished
	if started.IsZero() {
		started = time.Now()
	}
	if finished.IsZero() {
		finished = started
	}
	return "Created At: " + started.Format(time.RFC3339) + "\n" +
		"Completed At: " + finished.Format(time.RFC3339) + "\n"
}

// renderRunCommand renders a command's outcome as the reference's step
// string.
//
// The reference's harness prints a command's result through the step-string
// template (`step_strings/run_command.tmpl`), not through the CLI's own
// formatter — the two shapes are different, and the harness uses the step
// string:
//
//	Created At: …
//	Completed At: …
//
//	The command exited with code 0.
//	Output:
//	hi
//
// The template is the reference's own (recovered into
// internal/agytemplates/templates/step_strings/), so the clone renders it
// rather than re-implementing it.
func renderRunCommand(in Input) Outcome {
	out := Outcome{ExitCode: in.ExitCode}
	// The reference's terminal is a pty, so a newline the command wrote
	// reaches the model as CRLF. The clone's own run_command gives the pty
	// to the command's *stdin* only (see internal/tools/pty_unix.go), so
	// the output arrives with the command's own line endings and the
	// harness's renderer applies the terminal's output mapping itself.
	out.Output = onlcr(in.Output)
	// The reference truncates the command's output — the bytes the
	// terminal produced — and the notice it writes is part of the output
	// the model reads and of the frame the client sees.
	out.Output = Truncate(out.Output, in.TruncateTokens)
	out.HasOutput = out.Output != ""
	out.Text = stamp(in) + runCommandString(argString(in.Args, "CommandLine", "command", "command_line"), in.ExitCode, out.Output)
	return out
}

// onlcr maps every newline to CRLF, the way a terminal's output processing
// does (ONLCR).
//
// The reference's harness reads a command's output through a pty, and the
// terminal's line discipline rewrites NL as CR-NL on the way out. A carriage
// return the command wrote is left alone, so "x\r\ny" reaches the model as
// "x\r\r\ny" — exactly what the reference produced.
func onlcr(text string) string {
	return strings.ReplaceAll(text, "\n", "\r\n")
}

// runCommandString renders the reference's run_command step string.
//
// The template needs a step, and the step needs the pieces the template
// reads: the command line, the exit code and the combined output. The step
// is a model call, not a user one — the reference's step carries
// CORTEX_STEP_SOURCE_MODEL, which is what makes the template print
// "Stdout:"/"Stderr:" instead of "Output:" when the command printed
// nothing.
//
// The template itself is the reference's own, recovered into
// internal/agytemplates/templates/step_strings/, and the clone renders it
// through internal/agystep rather than re-implementing the wording.
func runCommandString(command string, exitCode int, output string) string {
	step := agystep.NewStep(agystep.StepOptions{
		Status: agystep.StatusDone,
		Source: agystep.SourceModel,
		RunCommand: agystep.NewRunCommand(agystep.RunCommandOptions{
			ProposedCommandLine: command,
			CommandLine:         command,
			CombinedOutput:      output,
			ExitCode:            exitCode,
		}),
	})
	return agystep.RunCommandString(step)
}

// runCommandErrorString renders the reference's step string for a command
// that failed.
//
// The step's error branch is the one the reference's own template prints:
// "Encountered error in step execution: …". The exit code is left out —
// a command that never ran has no exit code to report.
func runCommandErrorString(command, reason string) string {
	step := agystep.NewStep(agystep.StepOptions{
		Status: agystep.StatusError,
		Source: agystep.SourceModel,
		Error:  reason,
		RunCommand: agystep.NewRunCommand(agystep.RunCommandOptions{
			ProposedCommandLine: command,
			CommandLine:         command,
		}),
	})
	return agystep.RunCommandString(step)
}

// renderWriteToFile renders a file creation the way the reference does.
//
// The reference's text names the file it wrote and tells the model to run
// the code rather than ask for permission:
//
//	Created file file:///…/written.txt with requested content.
//	If relevant, proactively run terminal commands to execute this code for the USER. Don't ask for permission.
//
// The action the protocol frame carries is an *edit*, not a creation: the
// reference reports the new file's lines as an inserted diff block.
func renderWriteToFile(in Input) Outcome {
	path := argString(in.Args, "TargetFile", "AbsolutePath", "path", "file_path")
	out := Outcome{Text: stamp(in) + "Created file " + fileURI(path) + " with requested content.\n" +
		"If relevant, proactively run terminal commands to execute this code for the USER. Don't ask for permission."}
	if blocks, ok := Diff(splitLines(in.Before), splitLines(in.After)); ok {
		out.Diff = blocks
	}
	return out
}

// renderReplaceFile renders an edit the way the reference does.
//
// The reference's text carries the diff of the change, wrapped in the
// markers its own tool writes:
//
//	The following changes were made by the replace_file_content tool to: /…/three.txt. If relevant, proactively run terminal commands to execute this code for the USER. Don't ask for permission.
//	[diff_block_start]
//	@@ -1,4 +1,4 @@
//	 alpha
//	-beta
//	+BETA
//	 gamma
//
//	[diff_block_end]
//
//	Please note that the above snippet only shows the MODIFIED lines from the last change. It shows up to 3 lines of unchanged lines before and after the modified lines. The actual file contents may have many more lines not shown.
//
// The note's "3 lines of unchanged lines" is the context window the
// reference's diff uses; DiffBlock widens the change by the same amount.
func renderReplaceFile(in Input) Outcome {
	path := argString(in.Args, "TargetFile", "AbsolutePath", "path", "file_path")
	blocks, ok := Diff(splitLines(in.Before), splitLines(in.After))
	out := Outcome{}
	if ok {
		out.Diff = blocks
	}
	body := "The following changes were made by the replace_file_content tool to: " + path +
		". If relevant, proactively run terminal commands to execute this code for the USER. Don't ask for permission.\n"
	if ok {
		body += "[diff_block_start]\n" + unifiedDiff(blocks) + "[diff_block_end]\n"
	}
	body += "\nPlease note that the above snippet only shows the MODIFIED lines from the last change. " +
		"It shows up to 3 lines of unchanged lines before and after the modified lines. " +
		"The actual file contents may have many more lines not shown."
	out.Text = stamp(in) + body
	return out
}

// renderListDir renders a directory listing the way the reference does.
//
// The reference lists every entry on its own line as a small JSON object and
// closes with a summary:
//
//	{"name":"one.txt", "sizeBytes":"4"}
//	{"name":"out.txt", "sizeBytes":"12"}
//
//	Summary: This directory contains 0 subdirectories and 4 files.
//
// The sizes are the entry's size in bytes, as a string.
func renderListDir(in Input) Outcome {
	dir := argString(in.Args, "DirectoryPath", "path", "directory_path")
	entries := listEntries(dir)
	out := Outcome{Entries: entries}
	var body strings.Builder
	for _, entry := range entries {
		body.WriteString(`{"name":"` + entry.Name + `", "sizeBytes":"` + entry.Size + `"}` + "\n")
	}
	dirs, files := 0, 0
	for _, entry := range entries {
		if entry.IsDirectory {
			dirs++
		} else {
			files++
		}
	}
	body.WriteString(fmt.Sprintf("\nSummary: This directory contains %d subdirectories and %d files.", dirs, files))
	out.Text = stamp(in) + body.String()
	return out
}

// listEntries lists a directory the way the reference's listing does.
//
// The entries are sorted by name — the captured listing reads
// "one.txt", "out.txt", "three.txt", "written.txt" — and the summary
// counts the directories and the files apart.
func listEntries(dir string) []Entry {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]Entry, 0, len(items))
	for _, item := range items {
		entry := Entry{Name: item.Name()}
		if item.IsDir() {
			entry.IsDirectory = true
			entry.Size = "0"
		} else if info, err := item.Info(); err == nil {
			entry.Size = strconv.FormatInt(info.Size(), 10)
		} else {
			entry.Size = "0"
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// renderFindByName renders a name search the way the reference does.
//
// The reference counts the results and lists them:
//
//	Found 4 results
//	one.txt
//	out.txt
//
// The action's own `output` field carries the names without the count.
func renderFindByName(in Input) Outcome {
	names := strings.Split(strings.TrimRight(in.Result, "\n"), "\n")
	if len(names) == 1 && names[0] == "" {
		names = nil
	}
	out := Outcome{Found: names}
	var body strings.Builder
	body.WriteString(fmt.Sprintf("Found %d results\n", len(names)))
	body.WriteString(strings.Join(names, "\n"))
	out.Text = stamp(in) + body.String()
	return out
}

// renderGrepSearch renders a content search the way the reference does.
//
// The reference's search answers with a structured payload, not with text:
//
//	{"results": [{"path": "/…/three.txt"}]}
//
// One entry per file that matched — the matches themselves are not listed.
func renderGrepSearch(in Input) Outcome {
	root := argString(in.Args, "SearchPath", "DirectoryPath", "path", "directory_path")
	paths := grepPaths(root, in.Result)
	results := make([]any, 0, len(paths))
	for _, path := range paths {
		results = append(results, map[string]any{"path": path})
	}
	return Outcome{
		Text:     in.Result,
		Response: map[string]any{"results": results},
		Matches:  len(paths),
	}
}

// grepPaths reads the files a search matched out of the CLI's own result.
//
// The clone's `grep_search` prints one line per match, as
// "<name>:<line>: <text>", with the name relative to the search root. The
// reference's payload lists the matching *files*, with their absolute paths.
func grepPaths(root, result string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(result, "\n") {
		name, _, ok := strings.Cut(line, ":")
		if !ok || name == "" {
			continue
		}
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, name)
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

// splitLines splits a file's content into its lines.
//
// A trailing newline does not add a line of its own, but the empty line
// after it counts: "alpha\nbeta\n" is two lines, and "alpha\nbeta" is also
// two. The reference's own view_file counts the same way — it reports four
// lines for "a\nb\nc\n", the empty last line included — and so does its
// diff: the captured block for "alpha\nbeta\ngamma\n" lists five lines,
// the empty one included.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

// argString reads the first of a list of argument names that carries a
// string.
//
// The reference's harness spells its arguments in PascalCase
// ("TargetFile", "SearchPath"); the clone's own catalogue uses snake_case.
// A harness call reaches the renderer in the reference's spelling.
func argString(args json.RawMessage, names ...string) string {
	if len(args) == 0 {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(args, &fields); err != nil {
		return ""
	}
	for _, name := range names {
		if value, ok := fields[name]; ok {
			if text, ok := value.(string); ok {
				return text
			}
		}
	}
	return ""
}

// fileURI renders an absolute path as a `file://` URI.
func fileURI(path string) string {
	if path == "" {
		return ""
	}
	if strings.Contains(path, "://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		return path
	}
	return "file://" + path
}
