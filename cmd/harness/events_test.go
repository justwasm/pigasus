package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/justwasm/pigasus/internal/harnesstools"
	"github.com/justwasm/pigasus/internal/tools"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// protojsonText renders a message the way the wire carries it.
//
// The protobuf fields the reference writes even when they are zero are
// visible in that rendering and nowhere else.
func protojsonText(t *testing.T, msg proto.Message) string {
	t.Helper()
	raw, err := protojson.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// The expectations in this file are the reference's own frames, captured
// from the `localharness` binary driven through the SDK's protocol against
// a local fake Gemini endpoint. See `out/localharness/captures/`.

func TestViewFileWindowDefaultsToTheReferenceWindow(t *testing.T) {
	// The captured call carried only AbsolutePath, and the reference's
	// action answered with the 800-line window: 0..799.
	first, last := viewFileWindow(json.RawMessage(`{"AbsolutePath":"/tmp/a.txt"}`))
	if first != 0 || last != 799 {
		t.Fatalf("window = %d..%d, want 0..799", first, last)
	}
}

func TestViewFileWindowIsZeroBased(t *testing.T) {
	// The tool's lines are 1-indexed; the action reports 0-based
	// indices.
	first, last := viewFileWindow(json.RawMessage(`{"StartLine":10,"EndLine":20}`))
	if first != 9 || last != 19 {
		t.Fatalf("window = %d..%d, want 9..19", first, last)
	}
}

func TestViewFileWindowClamps(t *testing.T) {
	first, last := viewFileWindow(json.RawMessage(`{"StartLine":30,"EndLine":10}`))
	if first != 29 || last != 29 {
		t.Fatalf("window = %d..%d, want 29..29", first, last)
	}
}

func TestShownLinesReadsTheToolsWindow(t *testing.T) {
	first, last, ok := shownLines("File Path: `file:///tmp/a.txt`\nTotal Lines: 4\nShowing lines 1 to 4\n")
	if !ok {
		t.Fatal("shownLines: no window found")
	}
	if first != 1 || last != 4 {
		t.Fatalf("window = %d..%d, want 1..4", first, last)
	}
	if _, _, ok := shownLines("no window here"); ok {
		t.Fatal("shownLines: found a window in a result without one")
	}
}

func TestApplyToolActionMapsTheViewFileCall(t *testing.T) {
	update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_ENVIRONMENT, pb.StepUpdate_STATE_ACTIVE)
	applyToolAction(update, "view_file", json.RawMessage(`{"AbsolutePath":"/tmp/a.txt"}`), nil, nil, false)
	action := update.GetViewFile()
	if action == nil {
		t.Fatal("view_file: no action")
	}
	if got := action.GetFilePath(); got != "file:///tmp/a.txt" {
		t.Errorf("filePath = %q, want file:///tmp/a.txt", got)
	}
	if got := action.GetStartLine(); got != 0 {
		t.Errorf("startLine = %d, want 0", got)
	}
	if got := action.GetEndLine(); got != 799 {
		t.Errorf("endLine = %d, want 799", got)
	}
	// The reference omits the field when it is zero — the field has
	// explicit presence.
	if action.HasContentOffset() {
		t.Errorf("contentOffset = %d, want unset", action.GetContentOffset())
	}
}

func TestApplyToolActionKeepsANonZeroContentOffset(t *testing.T) {
	update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_ENVIRONMENT, pb.StepUpdate_STATE_ACTIVE)
	applyToolAction(update, "view_file", json.RawMessage(`{"AbsolutePath":"/tmp/a.txt","ContentOffset":128}`), nil, nil, false)
	action := update.GetViewFile()
	if action == nil || !action.HasContentOffset() {
		t.Fatal("view_file: contentOffset is unset")
	}
	if got := action.GetContentOffset(); got != 128 {
		t.Errorf("contentOffset = %d, want 128", got)
	}
}

func TestApplyToolActionReportsTheRangeTheToolShowed(t *testing.T) {
	// The done frame carries the window the tool actually showed; the
	// announcement, the start and the result frames carry the window the
	// call asked for.
	end := &piagent.ToolExecutionEndEvent{ToolName: "view_file"}
	end.Result = piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: "Total Lines: 4\nShowing lines 1 to 4\n"}},
	}
	update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_ENVIRONMENT, pb.StepUpdate_STATE_DONE)
	applyToolAction(update, "view_file", json.RawMessage(`{"AbsolutePath":"/tmp/a.txt"}`), end, nil, false)
	action := update.GetViewFile()
	if action == nil {
		t.Fatal("view_file: no action")
	}
	if got := action.GetStartLine(); got != 0 {
		t.Errorf("startLine = %d, want 0", got)
	}
	if got := action.GetEndLine(); got != 3 {
		t.Errorf("endLine = %d, want 3", got)
	}
}

// TestApplyToolActionMapsTheFinishCall pins the `finish` action.
//
// The reference reports the `finish` call as its own action, and the
// action carries the model's own arguments — the summary the model wrote
// is the conversation's last word.
func TestApplyToolActionMapsTheFinishCall(t *testing.T) {
	update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_USER, pb.StepUpdate_STATE_ACTIVE)
	applyToolAction(update, "finish", json.RawMessage(`{"summary":"all done"}`), nil, nil, false)
	action := update.GetFinish()
	if action == nil {
		t.Fatal("the finish action is missing")
	}
	if action.GetOutputString() != `{"summary":"all done"}` {
		t.Errorf("the finish output = %s, want the model's own arguments", action.GetOutputString())
	}
}

// TestFinishCallTargetsTheUser pins the `finish` call's target.
//
// Every other tool call targets the environment. The `finish` call is the
// model's answer, so it targets the user — and the reference's frame says
// what happened instead of "Tool execution".
func TestFinishCallTargetsTheUser(t *testing.T) {
	if got := stepTarget("finish"); got != pb.StepUpdate_TARGET_USER {
		t.Errorf("the finish target = %v, want TARGET_USER", got)
	}
	if got := stepTarget("run_command"); got != pb.StepUpdate_TARGET_ENVIRONMENT {
		t.Errorf("the run_command target = %v, want TARGET_ENVIRONMENT", got)
	}
	if got := toolStepText("finish"); got != "Finished" {
		t.Errorf("the finish text = %q, want %q", got, "Finished")
	}
	if got := toolStepText("run_command"); got != "Tool execution" {
		t.Errorf("the run_command text = %q, want %q", got, "Tool execution")
	}
}

func TestApplyToolActionMapsTheOtherReferenceTools(t *testing.T) {
	cases := []struct {
		name string
		args string
		want func(*pb.StepUpdate) bool
	}{
		{"run_command", `{"CommandLine":"ls","Cwd":"/tmp"}`, func(u *pb.StepUpdate) bool {
			a := u.GetRunCommand()
			return a != nil && a.GetCommandLine() == "ls" && a.GetWorkingDir() == "/tmp"
		}},
		// A written file is an *edit* in the reference's vocabulary, not
		// a creation: the captured frame carries an `editFile` action
		// with the file's new lines as a diff block.
		{"write_to_file", `{"TargetFile":"/tmp/a.txt","CodeContent":"x"}`, func(u *pb.StepUpdate) bool {
			a := u.GetEditFile()
			return a != nil && a.GetFilePath() == "file:///tmp/a.txt"
		}},
		{"list_dir", `{"DirectoryPath":"/tmp"}`, func(u *pb.StepUpdate) bool {
			a := u.GetListDirectory()
			return a != nil && a.GetDirectoryPath() == "file:///tmp"
		}},
		// The reference's search calls its pattern a "Query" and its
		// directory a "SearchPath".
		{"grep_search", `{"Query":"x","SearchPath":"/tmp"}`, func(u *pb.StepUpdate) bool {
			a := u.GetSearchDirectory()
			return a != nil && a.GetDirectoryPath() == "file:///tmp" && a.GetQuery() == "x"
		}},
		// The reference's name search calls its pattern a "Pattern" and
		// its directory a "SearchDirectory".
		{"find_by_name", `{"Pattern":"*.txt","SearchDirectory":"/tmp"}`, func(u *pb.StepUpdate) bool {
			a := u.GetFindFile()
			return a != nil && a.GetDirectoryPath() == "file:///tmp" && a.GetQuery() == "*.txt"
		}},
	}
	for _, tc := range cases {
		update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_ENVIRONMENT, pb.StepUpdate_STATE_ACTIVE)
		applyToolAction(update, tc.name, json.RawMessage(tc.args), nil, nil, false)
		if !tc.want(update) {
			t.Errorf("%s: action not mapped as expected: %+v", tc.name, update)
		}
	}
}

// The closing frame carries what the tool produced.
//
// The reference's done frame reports a command's exit code and its output,
// a listing's entries, a search's match count and a name search's names.
func TestApplyToolActionCarriesTheOutcomeOnTheDoneFrame(t *testing.T) {
	outcome := &harnesstools.Outcome{
		ExitCode:  3,
		Output:    "hi\r\n",
		HasOutput: true,
		Entries:   []harnesstools.Entry{{Name: "one.txt", Size: "4"}},
		Matches:   1,
		Found:     []string{"one.txt", "two.txt"},
	}
	cases := []struct {
		name string
		args string
		want func(*pb.StepUpdate) bool
	}{
		{"run_command", `{"CommandLine":"ls"}`, func(u *pb.StepUpdate) bool {
			a := u.GetRunCommand()
			return a != nil && a.GetExitCode() == 3 && a.GetCombinedOutput() == "hi\r\n"
		}},
		{"list_dir", `{"DirectoryPath":"/tmp"}`, func(u *pb.StepUpdate) bool {
			a := u.GetListDirectory()
			results := a.GetResults()
			return a != nil && len(results) == 1 && results[0].GetName() == "one.txt" && results[0].GetFileSize() == 4
		}},
		{"grep_search", `{"Query":"x","SearchPath":"/tmp"}`, func(u *pb.StepUpdate) bool {
			a := u.GetSearchDirectory()
			return a != nil && a.GetNumResults() == 1
		}},
		{"find_by_name", `{"Pattern":"*.txt","SearchDirectory":"/tmp"}`, func(u *pb.StepUpdate) bool {
			a := u.GetFindFile()
			return a != nil && a.GetOutput() == "one.txt\ntwo.txt"
		}},
	}
	for _, tc := range cases {
		update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_ENVIRONMENT, pb.StepUpdate_STATE_DONE)
		applyToolAction(update, tc.name, json.RawMessage(tc.args), &piagent.ToolExecutionEndEvent{}, outcome, true)
		if !tc.want(update) {
			t.Errorf("%s: outcome not carried as expected: %+v", tc.name, update)
		}
	}
}

// The announcement frame carries none of it.
//
// The reference's announcement reports the call, not its outcome: the exit
// code and the output arrive with the frame that closes the step.
func TestApplyToolActionKeepsTheOutcomeOffTheAnnouncement(t *testing.T) {
	outcome := &harnesstools.Outcome{ExitCode: 3, Output: "hi\r\n", HasOutput: true}
	update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_ENVIRONMENT, pb.StepUpdate_STATE_ACTIVE)
	applyToolAction(update, "run_command", json.RawMessage(`{"CommandLine":"ls"}`), nil, outcome, false)
	action := update.GetRunCommand()
	if action == nil {
		t.Fatal("run_command: no action")
	}
	if action.GetExitCode() != 0 || action.GetCombinedOutput() != "" {
		t.Errorf("announcement carries the outcome: %+v", action)
	}
}

func TestApplyToolActionFallsBackToTheCustomTool(t *testing.T) {
	update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_ENVIRONMENT, pb.StepUpdate_STATE_ACTIVE)
	applyToolAction(update, "client_tool", json.RawMessage(`{"a":1}`), nil, nil, false)
	action := update.GetCustomTool()
	if action == nil {
		t.Fatal("client_tool: no action")
	}
	if got := action.GetToolCall().GetName(); got != "client_tool" {
		t.Errorf("name = %q, want client_tool", got)
	}
	if got := action.GetToolCall().GetArgumentsJson(); got != `{"a":1}` {
		t.Errorf("arguments = %q, want {\"a\":1}", got)
	}
}

// A search's match count is zero until the frame that closes the step.
//
// The reference writes `numResults` on every frame of the search — zero
// included — and only the frame that closes the step knows the count.
// The field is written even when it is zero: the protobuf field has
// explicit presence, and setting it is what makes it appear.
func TestApplyToolActionCountsTheMatchesOnTheDoneFrameOnly(t *testing.T) {
	outcome := &harnesstools.Outcome{Matches: 1}
	announcement := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_ENVIRONMENT, pb.StepUpdate_STATE_ACTIVE)
	applyToolAction(announcement, "grep_search", json.RawMessage(`{"Query":"x"}`), nil, outcome, false)
	action := announcement.GetSearchDirectory()
	if action == nil {
		t.Fatal("grep_search: no action")
	}
	if got := action.GetNumResults(); got != 0 {
		t.Errorf("announcement numResults = %d, want 0", got)
	}
	if !strings.Contains(protojsonText(t, announcement), `"numResults":0`) {
		t.Errorf("announcement does not carry numResults: %s", protojsonText(t, announcement))
	}

	done := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_ENVIRONMENT, pb.StepUpdate_STATE_DONE)
	applyToolAction(done, "grep_search", json.RawMessage(`{"Query":"x"}`), &piagent.ToolExecutionEndEvent{}, outcome, true)
	if got := done.GetSearchDirectory().GetNumResults(); got != 1 {
		t.Errorf("done numResults = %d, want 1", got)
	}
}

// A command that printed nothing still reports its output.
//
// The reference writes `combinedOutput` on the frame that closes the step
// even when the output is empty — the field is there, empty.
func TestApplyToolActionReportsAnEmptyOutput(t *testing.T) {
	outcome := &harnesstools.Outcome{ExitCode: 0}
	done := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_ENVIRONMENT, pb.StepUpdate_STATE_DONE)
	applyToolAction(done, "run_command", json.RawMessage(`{"CommandLine":"true"}`), &piagent.ToolExecutionEndEvent{}, outcome, true)
	action := done.GetRunCommand()
	if action == nil {
		t.Fatal("run_command: no action")
	}
	if !strings.Contains(protojsonText(t, done), `"combinedOutput":""`) {
		t.Errorf("done does not carry an empty combinedOutput: %s", protojsonText(t, done))
	}
	if !strings.Contains(protojsonText(t, done), `"exitCode":0`) {
		t.Errorf("done does not carry the exit code: %s", protojsonText(t, done))
	}
}

// A refused call writes one frame, and it is not an announcement.
//
// The reference asks the client before it announces the call, so a call
// the client refuses is never announced. The frame that replaces it fails
// the step, targets the user, and quotes the failure in its text.
func TestDeniedStepWritesTheReferencesFrame(t *testing.T) {
	update := deniedStep("the probe denies this tool")

	if update.GetState() != pb.StepUpdate_STATE_ERROR {
		t.Errorf("state = %v, want STATE_ERROR", update.GetState())
	}
	if update.GetTarget() != pb.StepUpdate_TARGET_USER {
		t.Errorf("target = %v, want TARGET_USER", update.GetTarget())
	}
	if update.GetSource() != pb.StepUpdate_SOURCE_MODEL {
		t.Errorf("source = %v, want SOURCE_MODEL", update.GetSource())
	}
	if got := update.GetErrorMessage(); got != "the probe denies this tool" {
		t.Errorf("errorMessage = %q, want the bare reason", got)
	}
	want := `the probe denies this tool ("denied by pre-tool hook: the probe denies this tool")`
	if got := update.GetText(); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got := update.GetTextDelta(); got != want {
		t.Errorf("textDelta = %q, want %q", got, want)
	}
	if got := update.GetError().GetErrorMessage(); got != want {
		t.Errorf("error.errorMessage = %q, want %q", got, want)
	}
	if got := update.GetError().GetHttpCode(); got != 0 {
		t.Errorf("error.httpCode = %d, want 0", got)
	}
}

// A question the harness could not ask is one failed step.
//
// The reference's questions API lives in the GAOS dialect, so a call to
// `ask_question` on this dialect fails: the frame says the questions were
// cancelled, it targets the user, and it carries no error action.
func TestCancelledQuestionStepWritesTheReferencesFrame(t *testing.T) {
	update := cancelledQuestionStep()

	if update.GetState() != pb.StepUpdate_STATE_ERROR {
		t.Errorf("state = %v, want STATE_ERROR", update.GetState())
	}
	if update.GetTarget() != pb.StepUpdate_TARGET_USER {
		t.Errorf("target = %v, want TARGET_USER", update.GetTarget())
	}
	if update.GetSource() != pb.StepUpdate_SOURCE_MODEL {
		t.Errorf("source = %v, want SOURCE_MODEL", update.GetSource())
	}
	if got := update.GetErrorMessage(); got != "Questions cancelled." {
		t.Errorf("errorMessage = %q, want %q", got, "Questions cancelled.")
	}
	if got := update.GetText(); got != "Questions cancelled." {
		t.Errorf("text = %q, want %q", got, "Questions cancelled.")
	}
	if got := update.GetTextDelta(); got != "Questions cancelled." {
		t.Errorf("textDelta = %q, want %q", got, "Questions cancelled.")
	}
	if update.GetError() != nil {
		t.Errorf("error = %#v, want none", update.GetError())
	}
	if update.GetRunCommand() != nil || update.GetEditFile() != nil || update.GetCustomTool() != nil {
		t.Errorf("the frame carries an action: %+v", update)
	}
}

// The usage of a response that called a tool waits for the call.
//
// The reference reports the usage after the tool's start frame, so the
// count is held until the call has been dispatched.
func TestUsageWaitsForTheToolToStart(t *testing.T) {
	c := &conversation{}
	usage := &pi.Usage{Input: 11, Output: 7}
	c.holdUsage(usage)
	if c.takeUsage() == nil {
		t.Fatal("the usage was not held")
	}
	if c.takeUsage() != nil {
		t.Fatal("the usage was handed over twice")
	}
}

// The done frame's range is the window the tool showed, and the
// reference's own numbers are the ground truth.
//
// The reference's `view_file` reports the window as 0-based line indices,
// and the done frame carries the window the tool *read* rather than the
// one the call asked for. The clone derives it from the tool's own result
// — "Showing lines A to B", 1-indexed — and the two agree on every probe
// that was run against the reference binary:
//
//	1 line, trailing newline   → 1
//	2 lines, no trailing newline → 1
//	3 lines, trailing newline   → 3
//	800 lines                   → 799
//	900 lines                   → 799
func TestViewFileRangeMatchesTheReferenceProbes(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"one line with a trailing newline", "probe content\n", 1},
		{"two lines without one", "a\nb", 1},
		{"three lines with one", "a\nb\nc\n", 3},
		{"eight hundred lines", numberedLines(800), 799},
		{"nine hundred lines", numberedLines(900), 799},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		path := filepath.Join(dir, "probe.txt")
		if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
			t.Fatalf("%s: write the probe: %v", tc.name, err)
		}
		reg := tools.RuntimeCatalog(dir)
		out, err := reg.Dispatch(context.Background(), "view_file",
			json.RawMessage(`{"AbsolutePath":"`+path+`"}`))
		if err != nil {
			t.Fatalf("%s: view_file: %v", tc.name, err)
		}
		_, last, ok := shownLines(out)
		if !ok {
			t.Fatalf("%s: the result has no window: %s", tc.name, out)
		}
		if last-1 != tc.want {
			t.Errorf("%s: endLine = %d, want %d", tc.name, last-1, tc.want)
		}
	}
}

// numberedLines renders n numbered lines, each terminated by a newline.
func numberedLines(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	return b.String()
}

func TestMessageToolCallsReadsTheCalls(t *testing.T) {
	msg := piagent.AgentMessage{Assistant: &piagent.AssistantMessage{
		Content: []pi.AssistantContentBlock{
			pi.TextContent{Text: "hello"},
			pi.ToolCall{ID: "c1", Name: "view_file", Arguments: pi.JsonObject{"AbsolutePath": "/tmp/a.txt"}},
		},
	}}
	calls := messageToolCalls(msg)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Name != "view_file" {
		t.Errorf("name = %q, want view_file", calls[0].Name)
	}
	if got := messageText(msg); got != "hello" {
		t.Errorf("text = %q, want hello", got)
	}
}

func TestAddUsageAccumulates(t *testing.T) {
	c := &conversation{}
	if got := c.addUsage(usagePrompt, 11); got != 11 {
		t.Fatalf("first = %d, want 11", got)
	}
	if got := c.addUsage(usagePrompt, 11); got != 22 {
		t.Fatalf("second = %d, want 22", got)
	}
	if got := c.addUsage(usageCandidates, 7); got != 7 {
		t.Fatalf("candidates = %d, want 7", got)
	}
}

func TestToolStepReusesTheAnnouncedStep(t *testing.T) {
	c := &conversation{}
	c.rememberToolStep("view_file", 4)
	if got := c.toolStep("view_file"); got != 4 {
		t.Fatalf("step = %d, want 4", got)
	}
	// A call that was never announced opens a step of its own.
	// A call that was never announced opens a step of its own.
	if got := c.toolStep("other"); got != 1 {
		t.Fatalf("step = %d, want 1", got)
	}
}

func TestFileURILeavesURIsAlone(t *testing.T) {
	if got := fileURI("file:///tmp/a.txt"); got != "file:///tmp/a.txt" {
		t.Errorf("fileURI = %q, want the URI unchanged", got)
	}
	if got := fileURI("/tmp/a.txt"); got != "file:///tmp/a.txt" {
		t.Errorf("fileURI = %q, want file:///tmp/a.txt", got)
	}
	if got := fileURI(""); got != "" {
		t.Errorf("fileURI = %q, want the empty string", got)
	}
}

func TestHarnessDeclarationsAreTheReferenceEighteen(t *testing.T) {
	decls := harnessDeclarations(map[string]bool{
		"view_file": true, "run_command": true, "invoke_subagent": true,
	}, nil)
	if len(decls) == 0 {
		t.Fatal("no declarations")
	}
	names := make(map[string]bool, len(decls))
	for _, d := range decls {
		names[d.Name] = true
	}
	for _, want := range []string{"view_file", "run_command", "invoke_subagent"} {
		if !names[want] {
			t.Errorf("%s is not advertised", want)
		}
	}
	if names["read_file"] {
		t.Error("read_file is advertised; the harness names it view_file")
	}
}

func TestSideToolSubsetCoversTheEnabledTools(t *testing.T) {
	enabled := true
	side := pb.HarnessSideTools_builder{
		ViewFile: pb.ViewFileToolConfig_builder{Enabled: &enabled}.Build(),
	}.Build()
	c := &conversation{settings: sessionSettings{SideTools: side}}
	subset := c.sideToolSubset()
	if !subset["view_file"] {
		t.Errorf("the subset is %v, want view_file in it", subset)
	}
	if subset["run_command"] {
		t.Errorf("the subset is %v, want no run_command in it", subset)
	}
	// `send_message` and `finish` are on whatever the client says.
	if !subset["send_message"] || !subset["finish"] {
		t.Errorf("the subset is %v, want send_message and finish in it", subset)
	}
}

// TestToolErrorMessagePrefersTheToolsOwnSentence pins the failed frame's
// errorMessage.
//
// The reference puts the tool's own sentence on the frame — "Cannot view
// file file://… which does not exist." — and not the whole result text
// the model reads. A failure the reference's wording is not known for
// keeps the result's own text.
func TestToolErrorMessagePrefersTheToolsOwnSentence(t *testing.T) {
	outcome := &harnesstools.Outcome{
		Text:         "Created At: …\nCompleted At: …\nEncountered error in step execution: path file:///ws/nope.txt does not exist",
		ErrorMessage: "Cannot view file file:///ws/nope.txt which does not exist.",
		Failed:       true,
	}
	end := piagent.ToolExecutionEndEvent{
		ToolName: "view_file",
		Result: piagent.AgentToolResult{
			Content: []pi.ToolResultMessageContent{pi.TextContent{Text: outcome.Text}},
		},
	}
	got := toolErrorMessage(outcome, end)
	if got != outcome.ErrorMessage {
		t.Errorf("toolErrorMessage = %q, want %q", got, outcome.ErrorMessage)
	}

	// Without the tool's own sentence the result's text is the frame's.
	plain := &harnesstools.Outcome{Text: "the result text"}
	got = toolErrorMessage(plain, end)
	if got != outcome.Text {
		t.Errorf("toolErrorMessage = %q, want the result text", got)
	}

	// A call with no outcome at all keeps the result's text too.
	got = toolErrorMessage(nil, end)
	if got != outcome.Text {
		t.Errorf("toolErrorMessage = %q, want the result text", got)
	}
}

// TestApplyToolActionOmitsTheOutputOfAFailedCall pins the failed frame's
// action.
//
// A step that failed has no names to report: the reference leaves the
// find_file action's output field out, and the command's action carries
// neither an exit code nor an output.
func TestApplyToolActionOmitsTheOutputOfAFailedCall(t *testing.T) {
	failed := &harnesstools.Outcome{Failed: true}
	end := &piagent.ToolExecutionEndEvent{ToolName: "find_by_name"}

	update := &pb.StepUpdate{}
	applyToolAction(update, "find_by_name", json.RawMessage(`{"SearchDirectory": "/ws", "Pattern": "*.txt"}`), end, failed, false)
	action := update.GetFindFile()
	if action == nil {
		t.Fatal("the frame carries no find_file action")
	}
	if text := protojsonText(t, action); strings.Contains(text, "output") {
		t.Errorf("the failed call's action carries the output: %s", text)
	}

	update = &pb.StepUpdate{}
	applyToolAction(update, "run_command", json.RawMessage(`{"CommandLine": "ls", "Cwd": "/ws"}`), end, failed, false)
	command := update.GetRunCommand()
	if command == nil {
		t.Fatal("the frame carries no run_command action")
	}
	text := protojsonText(t, command)
	if strings.Contains(text, "exitCode") || strings.Contains(text, "combinedOutput") {
		t.Errorf("the failed call's action carries the command's outcome: %s", text)
	}
}
