package cumulative

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/implicit"
)

// doneStep is a `!` command that ran and printed something.
func doneStep(cmd, output string) implicit.Step {
	return implicit.Step{
		Status: implicit.StepStatusDone,
		RunCommand: implicit.RunCommand{
			CommandLine:         cmd,
			ProposedCommandLine: cmd,
			CombinedOutput:      output,
			ExitCode:            0,
			HasExitCode:         true,
		},
		CreatedAt:   time.Date(2026, 10, 26, 12, 34, 56, 0, time.Local),
		CompletedAt: time.Date(2026, 10, 26, 12, 34, 57, 0, time.Local),
	}
}

// TestConvertUserStep is the reference's shape for a `!` command: two
// messages, the first saying what the user did and the second carrying the
// step's output.
func TestConvertUserStep(t *testing.T) {
	steps := []implicit.Step{doneStep("ls -la", "total 12\n")}
	msgs := Convert(steps, Options{})
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	for i, m := range msgs {
		if m.Role != RoleUser {
			t.Errorf("message %d: role = %q, want %q", i, m.Role, RoleUser)
		}
	}

	want := "Step Id: 0\n" +
		"Created At: " + localStamp(2026, 10, 26, 12, 34, 56) + "\n" +
		"Completed At: " + localStamp(2026, 10, 26, 12, 34, 57) + "\n" +
		"The USER performed the following action:\n" +
		"Command: ls -la"
	if msgs[0].Content != want {
		t.Errorf("message 0:\n got %q\nwant %q", msgs[0].Content, want)
	}

	// The second message carries the reference's own run_command step
	// string — the template's rendering, verbatim.
	wantOut := "Step Id: 0\n" +
		"Created At: " + localStamp(2026, 10, 26, 12, 34, 56) + "\n" +
		"Completed At: " + localStamp(2026, 10, 26, 12, 34, 57) + "\n" +
		"\nThe command exited with code 0.\nOutput:\ntotal 12\n\n"
	if msgs[1].Content != wantOut {
		t.Errorf("message 1:\n got %q\nwant %q", msgs[1].Content, wantOut)
	}
}

// TestConvertInputString pins the reference's own input string: the
// command, labelled, and the working directory on a second line.
//
// 【反编译】 code.(*RunCommandStringConverter).ToInputString, 0x101423100.
func TestConvertInputString(t *testing.T) {
	step := doneStep("ls -la", "total 12\n")
	step.RunCommand.CWD = "/private/var/tmp"
	msgs := Convert([]implicit.Step{step}, Options{})
	if got := inputOf(t, msgs[0].Content); got != "Command: ls -la\nCWD: /private/var/tmp" {
		t.Errorf("the input is %q, want the command and the working directory", got)
	}

	// No working directory: no second line.
	step.RunCommand.CWD = ""
	msgs = Convert([]implicit.Step{step}, Options{})
	if got := inputOf(t, msgs[0].Content); got != "Command: ls -la" {
		t.Errorf("the input is %q, want the command alone", got)
	}
}

// TestConvertInputStringPrefersProposedLine: the reference's converter
// prefers the proposed command line and falls back to the command line.
func TestConvertInputStringPrefersProposedLine(t *testing.T) {
	step := doneStep("ls -la", "")
	step.RunCommand.CommandLine = "ls -la --color"
	step.RunCommand.ProposedCommandLine = "ls -la"
	msgs := Convert([]implicit.Step{step}, Options{})
	if got := inputOf(t, msgs[0].Content); got != "Command: ls -la" {
		t.Errorf("the input is %q, want the proposed command", got)
	}

	step.RunCommand.ProposedCommandLine = ""
	msgs = Convert([]implicit.Step{step}, Options{})
	if got := inputOf(t, msgs[0].Content); got != "Command: ls -la --color" {
		t.Errorf("the input is %q, want the command line", got)
	}
}

// inputOf is the message's own text — everything behind the lead-in.
func inputOf(t *testing.T, content string) string {
	t.Helper()
	const prefix = "The USER performed the following action:\n"
	idx := strings.Index(content, prefix)
	if idx < 0 {
		t.Fatalf("the message does not name the action:\n%s", content)
	}
	return content[idx+len(prefix):]
}

// TestConvertCanceledStep: a canceled command has no exit code and no
// output, and the reference's template says so.
func TestConvertCanceledStep(t *testing.T) {
	step := implicit.Step{
		Status: implicit.StepStatusCanceled,
		RunCommand: implicit.RunCommand{
			CommandLine:         "sleep 100",
			ProposedCommandLine: "sleep 100",
		},
		CreatedAt:   time.Date(2026, 10, 26, 12, 34, 56, 0, time.Local),
		CompletedAt: time.Date(2026, 10, 26, 12, 34, 57, 0, time.Local),
	}
	msgs := Convert([]implicit.Step{step}, Options{})
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if !strings.Contains(msgs[1].Content, "Step was canceled.") {
		t.Errorf("message 1 = %q, want the canceled line", msgs[1].Content)
	}
	if strings.Contains(msgs[1].Content, "exited with code") {
		t.Errorf("message 1 = %q, want no exit-code line", msgs[1].Content)
	}
}

// TestConvertFromCursor: only the steps no prompt has carried yet are
// converted, and the step id keeps the trajectory's own numbering.
func TestConvertFromCursor(t *testing.T) {
	steps := []implicit.Step{
		doneStep("first", "a\n"),
		doneStep("second", "b\n"),
		doneStep("third", "c\n"),
	}
	msgs := Convert(steps, Options{From: 1})
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4", len(msgs))
	}
	if !strings.HasPrefix(msgs[0].Content, "Step Id: 1\n") {
		t.Errorf("message 0 = %q, want the second step's id", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "second") {
		t.Errorf("message 0 = %q, want the second command", msgs[0].Content)
	}
}

// TestConvertStepID pins the reference's id formula.
//
// 【反编译】 the handler's renumbering, 0x102236488–0x1022364ac: the id is
// the increment, plus the step's own index, and — when the start index is
// set — less the number of messages an earlier prompt already carried.
func TestConvertStepID(t *testing.T) {
	steps := []implicit.Step{
		doneStep("first", "a\n"),
		doneStep("second", "b\n"),
	}

	// The default: the step's own index.
	msgs := Convert(steps, Options{})
	if !strings.HasPrefix(msgs[0].Content, "Step Id: 0\n") {
		t.Errorf("message 0 = %q, want the step's own index", msgs[0].Content)
	}

	// The increment alone.
	msgs = Convert(steps, Options{StepIndexIncrement: 7})
	if !strings.HasPrefix(msgs[0].Content, "Step Id: 7\n") {
		t.Errorf("message 0 = %q, want the index plus the increment", msgs[0].Content)
	}

	// The start index, less the cached messages.
	start := 10
	msgs = Convert(steps, Options{TrajectoryStartIndex: &start, CachedMessages: 4})
	if !strings.HasPrefix(msgs[0].Content, "Step Id: 6\n") {
		t.Errorf("message 0 = %q, want start + index - cached", msgs[0].Content)
	}
}

// TestConvertGate: a step type the reference's map does not carry — or
// carries as false — contributes nothing.
//
// 【反编译】 prompt.shouldFilterTrajectoryStep, 0x101b2d7c0: the entry must
// be present and true. The map is keyed by the step type's name.
func TestConvertGate(t *testing.T) {
	steps := []implicit.Step{doneStep("ls -la", "total 12\n")}
	msgs := Convert(steps, Options{AllowedStepTypes: map[string]bool{"CORTEX_STEP_TYPE_VIEW_FILE": true}})
	if len(msgs) != 0 {
		t.Fatalf("got %d messages, want 0", len(msgs))
	}
	msgs = Convert(steps, Options{AllowedStepTypes: map[string]bool{"CORTEX_STEP_TYPE_RUN_COMMAND": false}})
	if len(msgs) != 0 {
		t.Fatalf("got %d messages, want 0", len(msgs))
	}
	msgs = Convert(steps, Options{AllowedStepTypes: map[string]bool{"CORTEX_STEP_TYPE_RUN_COMMAND": true}})
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	// An empty map filters everything.
	msgs = Convert(steps, Options{AllowedStepTypes: map[string]bool{}})
	if len(msgs) != 0 {
		t.Fatalf("got %d messages, want 0", len(msgs))
	}
}

// TestDefaultGate: the gate the reference's own default config carries is
// the eight step types, every one of them true — the run command among
// them, so a `!` command reaches the prompt by default.
//
// 【反编译】 prompt.map.init.0, 0x101b2b2f0 — the map the default config
// copies into its field 22.
func TestDefaultGate(t *testing.T) {
	want := []string{
		"CORTEX_STEP_TYPE_CODE_ACTION",
		"CORTEX_STEP_TYPE_GIT_COMMIT",
		"CORTEX_STEP_TYPE_GREP_SEARCH",
		"CORTEX_STEP_TYPE_VIEW_FILE",
		"CORTEX_STEP_TYPE_RUN_COMMAND",
		"CORTEX_STEP_TYPE_FIND",
		"CORTEX_STEP_TYPE_COMMAND_STATUS",
		"CORTEX_STEP_TYPE_CLIPBOARD",
	}
	if len(defaultAllowedStepTypes) != len(want) {
		t.Fatalf("gate has %d entries, want %d", len(defaultAllowedStepTypes), len(want))
	}
	for _, name := range want {
		if !defaultAllowedStepTypes[name] {
			t.Errorf("gate[%q] = false, want true", name)
		}
	}
	steps := []implicit.Step{doneStep("ls -la", "total 12\n")}
	if msgs := Convert(steps, Options{}); len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
}

// TestConvertNoTimestamps: a step with no timestamps gets no timestamp
// lines — the reference tests the timestamp's presence before formatting it.
func TestConvertNoTimestamps(t *testing.T) {
	step := implicit.Step{
		Status: implicit.StepStatusDone,
		RunCommand: implicit.RunCommand{
			CommandLine:         "ls -la",
			ProposedCommandLine: "ls -la",
		},
	}
	msgs := Convert([]implicit.Step{step}, Options{})
	if strings.Contains(msgs[0].Content, "Created At") {
		t.Errorf("message 0 = %q, want no timestamp line", msgs[0].Content)
	}
	if !strings.HasPrefix(msgs[0].Content, "Step Id: 0\n") {
		t.Errorf("message 0 = %q, want the step id first", msgs[0].Content)
	}
}

// TestConvertOutOfRange: a cursor past the end converts nothing, and a
// negative cursor starts at the beginning.
func TestConvertOutOfRange(t *testing.T) {
	steps := []implicit.Step{doneStep("ls -la", "total 12\n")}
	if msgs := Convert(steps, Options{From: 5}); len(msgs) != 0 {
		t.Fatalf("got %d messages, want 0", len(msgs))
	}
	msgs := Convert(steps, Options{From: -3})
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if !strings.HasPrefix(msgs[0].Content, "Step Id: 0\n") {
		t.Errorf("message 0 = %q, want the first step's id", msgs[0].Content)
	}
}

// TestConvertTokenLimit: the per-step token limit cuts the text and adds
// the reference's own note. The limit is inert until the server sends one.
//
// 【反编译】 (*TrajectoryChatConverter).truncateStringToTokenLimit,
// 0x101518bf0.
func TestConvertTokenLimit(t *testing.T) {
	steps := []implicit.Step{doneStep("ls -la", strings.Repeat("x\n", 400))}

	// No limit: the text is untouched.
	msgs := Convert(steps, Options{})
	if strings.Contains(msgs[1].Content, "NOTE: The output was truncated") {
		t.Errorf("message 1 = %q, want no truncation", msgs[1].Content)
	}

	// A limit the text is under: untouched.
	msgs = Convert(steps, Options{MaxTokensPerStep: 1000})
	if strings.Contains(msgs[1].Content, "NOTE: The output was truncated") {
		t.Errorf("message 1 = %q, want no truncation", msgs[1].Content)
	}

	// A limit the text is over: the text is cut and the note is added.
	msgs = Convert(steps, Options{MaxTokensPerStep: 20})
	if !strings.Contains(msgs[1].Content, truncationNote) {
		t.Errorf("message 1 = %q, want the truncation note", msgs[1].Content)
	}
}

// TestTimestampLayout pins the reference's own time layout: RFC 3339 to the
// second, in the machine's local zone.
func TestTimestampLayout(t *testing.T) {
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})$`)
	stamp := localStamp(2026, 10, 26, 12, 34, 56)
	if !re.MatchString(stamp) {
		t.Errorf("stamp = %q, want RFC 3339 to the second", stamp)
	}
}

// localStamp is the reference's rendering of a local wall time.
func localStamp(y int, mo time.Month, d, h, mi, s int) string {
	return time.Date(y, mo, d, h, mi, s, 0, time.Local).Format(timestampLayout)
}
