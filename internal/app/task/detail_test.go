package task

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// TestTaskDetailBodyRule — the reference draws a forty-cell rule
// between the task detail's metadata block and its output.
//
// 【反编译】 model.TaskDetailModel.buildTaskDetailLines calls
// strings.Repeat("─", 0x28).
func TestTaskDetailBodyRule(t *testing.T) {
	task := &Task{
		ID:        "sub-1",
		Name:      "sleep",
		Kind:      KindShell,
		State:     StateDone,
		Command:   "bash -c sleep 60",
		Output:    "line one",
		StartedAt: time.Now(),
	}
	lines := TaskDetailBody(task)
	rule := strings.Repeat("─", 40)
	found := false
	for _, l := range lines {
		if l == rule {
			found = true
		}
	}
	if !found {
		t.Errorf("the task detail has no forty-cell rule:\n%q", lines)
	}
	// The rule sits between the metadata block and the output.
	typeIdx, outIdx := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "Type:") && typeIdx < 0 {
			typeIdx = i
		}
		if l == "Output" && outIdx < 0 {
			outIdx = i
		}
	}
	if typeIdx < 0 || outIdx < 0 || outIdx != typeIdx+2 {
		t.Errorf("the rule is not between the metadata and the output: %q", lines)
	}
}

// TestSubagentDetailHeader — the reference's subagent-detail header is
// five lines deep.
//
// 【反编译】 model.SubagentDetailModel.buildHeaderLines renders
// "main ▸ <title>", the elapsed time, "<a> <b> · <c> · <N> steps",
// "ID: <id>" and a rule.
func TestSubagentDetailHeader(t *testing.T) {
	task := &Task{
		ID:        "sub-2",
		Name:      "reviewer",
		Kind:      KindSubagent,
		State:     StateDone,
		Tools:     []string{"read_file", "run_command"},
		Steps:     []string{"read_file", "run_command"},
		StartedAt: time.Now().Add(-12 * time.Second),
		EndedAt:   time.Now(),
	}
	lines := SubagentDetailHeader(task, SubagentHeaderContext{Width: 80})
	if len(lines) != 5 {
		t.Fatalf("the subagent header has %d lines, want 5: %q", len(lines), lines)
	}
	if lines[0] != "main ▸ reviewer" {
		t.Errorf("header line 1 = %q, want %q", lines[0], "main ▸ reviewer")
	}
	if lines[1] != "worked 12s" {
		t.Errorf("header line 2 = %q, want %q", lines[1], "worked 12s")
	}
	if !strings.Contains(lines[2], "· 2 steps") {
		t.Errorf("header line 3 = %q, want the step count", lines[2])
	}
	if !strings.Contains(lines[2], "reviewer") {
		t.Errorf("header line 3 = %q, want the agent's name", lines[2])
	}
	if lines[3] != "ID: sub-2" {
		t.Errorf("header line 4 = %q, want %q", lines[3], "ID: sub-2")
	}
	// The rule is min(60, width-4) cells, indented by two.
	want := "  " + strings.Repeat("─", 60)
	if lines[4] != want {
		t.Errorf("header line 5 = %q, want %q", lines[4], want)
	}
}

// TestSubagentDetailHeaderRuleWidth — the rule follows the panel
// width, capped at sixty cells.
//
// 【反编译】 buildHeaderLines computes min(0x3c, width-4).
func TestSubagentDetailHeaderRuleWidth(t *testing.T) {
	task := &Task{ID: "sub-3", Name: "narrow", Kind: KindSubagent, State: StateRunning}
	lines := SubagentDetailHeader(task, SubagentHeaderContext{Width: 24})
	want := "  " + strings.Repeat("─", 20)
	if lines[4] != want {
		t.Errorf("the rule = %q, want %q", lines[4], want)
	}
}

// TestSubagentDetailHeaderParent — a nested agent shows its parent.
func TestSubagentDetailHeaderParent(t *testing.T) {
	task := &Task{ID: "sub-4", Name: "leaf", Kind: KindSubagent, State: StateRunning}
	lines := SubagentDetailHeader(task, SubagentHeaderContext{Parent: "brancher"})
	if lines[0] != "main ▸ leaf ▸ brancher" {
		t.Errorf("header line 1 = %q, want %q", lines[0], "main ▸ leaf ▸ brancher")
	}
}

// TestSubagentDetailHeaderRunningHasNoWorkedPrefix — while the agent
// runs, the elapsed line is the bare duration.
//
// 【反编译】 buildHeaderLines compares the status with "running"; the
// running branch formats the duration on its own.
func TestSubagentDetailHeaderRunningHasNoWorkedPrefix(t *testing.T) {
	task := &Task{
		ID:        "sub-5",
		Name:      "busy",
		Kind:      KindSubagent,
		State:     StateRunning,
		StartedAt: time.Now().Add(-3 * time.Second),
	}
	lines := SubagentDetailHeader(task, SubagentHeaderContext{Width: 80})
	if lines[1] != "3s" {
		t.Errorf("header line 2 = %q, want %q", lines[1], "3s")
	}
}

// TestSubagentStateVocabulary — the subagent detail uses its own four
// state words, not model.taskStatus()'s.
//
// 【反编译】 buildHeaderLines compares the status with "error",
// "killed", "done" and "running".
func TestSubagentStateVocabulary(t *testing.T) {
	cases := map[State]string{
		StateRunning: "running",
		StatePending: "running",
		StateDone:    "done",
		StateFailed:  "error",
		StateKilled:  "killed",
	}
	for state, want := range cases {
		if got := SubagentStateWord(state); got != want {
			t.Errorf("SubagentStateWord(%q) = %q, want %q", state, got, want)
		}
	}
	// The colours: 196 for an errored or killed agent, 34 for a
	// finished one, 220 for the rest.
	if got := SubagentStateColor(StateFailed); got != "\x1b[38;5;196m" {
		t.Errorf("the error colour = %q", got)
	}
	if got := SubagentStateColor(StateKilled); got != "\x1b[38;5;196m" {
		t.Errorf("the killed colour = %q", got)
	}
	if got := SubagentStateColor(StateDone); got != "\x1b[38;5;34m" {
		t.Errorf("the done colour = %q", got)
	}
	if got := SubagentStateColor(StateRunning); got != "\x1b[38;5;220m" {
		t.Errorf("the running colour = %q", got)
	}
}

// TestSubagentDetailBodyIndent — the reference indents the subagent
// detail's body by two cells and its tool list by four.
//
// 【反编译】 buildContentLines prefixes the headings and their
// content with the two-byte string at 0x1035128da ("  ") and the tool
// items with the four-byte string at 0x1033c6525 ("    ").
func TestSubagentDetailBodyIndent(t *testing.T) {
	task := &Task{
		ID:     "sub-6",
		Name:   "worker",
		Kind:   KindSubagent,
		State:  StateDone,
		Prompt: "do the thing",
		Tools:  []string{"read_file"},
		Output: "done",
	}
	lines := SubagentDetailBody(task)
	want := []string{
		"  Prompt",
		"  do the thing",
		"",
		"  Tools",
		"    read_file",
		"",
		"  Answer",
		"  done",
	}
	for i, w := range want {
		if i >= len(lines) {
			t.Fatalf("the body has %d lines, want %d: %q", len(lines), len(want), lines)
		}
		if lines[i] != w {
			t.Errorf("body line %d = %q, want %q", i, lines[i], w)
		}
	}
}

// TestSubagentDetailBodyEmptyForms — the reference's empty forms carry
// the same two-cell indent.
func TestSubagentDetailBodyEmptyForms(t *testing.T) {
	task := &Task{ID: "sub-7", Name: "empty", Kind: KindSubagent, State: StateRunning}
	lines := SubagentDetailBody(task)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"  (no prompt available)",
		"  No tools used yet",
		"  (no output)",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the subagent detail is missing %q:\n%s", want, joined)
		}
	}
}

// TestSubagentDetailTrajectory — the reference's trajectory section
// carries the agent's name and its step count, and numbers the steps.
//
// 【反编译】 model.SubagentDetailModel.buildContentLines formats
// "Trajectory — %s (%d steps)" (0x10358a89d, referenced at +0x1ac0)
// and the numbered steps with "%d. %s" (0x1033df139, at +0x293c).
func TestSubagentDetailTrajectory(t *testing.T) {
	task := &Task{
		ID:     "sub-8",
		Name:   "worker",
		Kind:   KindSubagent,
		State:  StateDone,
		Steps:  []string{"read_file", "run_command"},
		Output: "done",
	}
	lines := SubagentDetailBody(task)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"  Trajectory — worker (2 steps)",
		"    1. read_file",
		"    2. run_command",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the subagent detail is missing %q:\n%s", want, joined)
		}
	}
	// The trajectory comes after the answer.
	answerAt := strings.Index(joined, "  Answer")
	trajAt := strings.Index(joined, "  Trajectory")
	if answerAt < 0 || trajAt < 0 || trajAt < answerAt {
		t.Errorf("the trajectory section does not follow the answer:\n%s", joined)
	}
}

// TestSubagentDetailTrajectoryEmpty — the reference's empty trajectory
// says so.
//
// 【反编译】 buildContentLines references "No steps yet" (0x10346f0a3)
// at +0x1cd8.
func TestSubagentDetailTrajectoryEmpty(t *testing.T) {
	task := &Task{ID: "sub-9", Name: "idle", Kind: KindSubagent, State: StateRunning}
	lines := SubagentDetailBody(task)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "  Trajectory — idle (0 steps)") {
		t.Errorf("the trajectory heading is wrong:\n%s", joined)
	}
	if !strings.Contains(joined, "  No steps yet") {
		t.Errorf("the empty trajectory is missing its form:\n%s", joined)
	}
}

// TestSubagentDetailBodyLineWrapAndReflow verifies that long paragraphs in Prompt
// and Answer wrap to fit width while keeping indentation and paragraph breaks,
// and reflow cleanly when resized.
func TestSubagentDetailBodyLineWrapAndReflow(t *testing.T) {
	longPrompt := "This is a long prompt paragraph that definitely exceeds thirty columns.\n\nSecond paragraph in prompt with 中文测试."
	longAnswer := "First answer paragraph that is also quite long and should be wrapped into multiple lines.\n\n第二段回答包含较长的中文内容，确保在小窗口时能够折叠成多行，并且在窗口放大时重新回流排版。"
	task := &Task{
		ID:     "sub-wrap-1",
		Name:   "wrapper",
		Kind:   KindSubagent,
		State:  StateDone,
		Prompt: longPrompt,
		Output: longAnswer,
		Tools:  []string{"very_long_tool_name_that_should_also_wrap_if_width_is_small"},
		Steps:  []string{"Long step description explaining what was investigated in this turn"},
	}

	// 1. When width is 40 columns:
	lines40 := SubagentDetailBody(task, 40)
	for i, l := range lines40 {
		// No line should exceed 40 columns
		if len([]rune(l)) > 40 {
			t.Errorf("line %d at width 40 exceeds 40 runes (%d runes): %q", i, len([]rune(l)), l)
		}
		// Non-empty lines under Prompt/Answer must maintain 2-space indent
		if l != "" && !strings.HasPrefix(l, "  ") {
			t.Errorf("line %d does not have at least 2-space indent: %q", i, l)
		}
	}

	// 2. When width is 80 columns:
	lines80 := SubagentDetailBody(task, 80)
	for i, l := range lines80 {
		if len([]rune(l)) > 80 {
			t.Errorf("line %d at width 80 exceeds 80 runes: %q", i, l)
		}
	}

	// 3. Resizing reflow: narrower width must yield more lines than wider width
	if len(lines40) <= len(lines80) {
		t.Errorf("expected lines at width 40 (%d) to be greater than lines at width 80 (%d) due to reflow", len(lines40), len(lines80))
	}

	// 4. Paragraph breaks (empty lines) preserved
	joined40 := strings.Join(lines40, "\n")
	if !strings.Contains(joined40, "  Prompt\n  This is a long") {
		t.Errorf("missing prompt header followed by wrapped prompt:\n%s", joined40)
	}
	if !strings.Contains(joined40, "  Answer\n  First answer") {
		t.Errorf("missing answer header followed by wrapped answer:\n%s", joined40)
	}
}

// TestTaskDetailBodyLineWrapAndReflow verifies that long Command and Output
// wrap to fit width and reflow on resize.
func TestTaskDetailBodyLineWrapAndReflow(t *testing.T) {
	task := &Task{
		ID:      "task-wrap-1",
		Name:    "bash",
		Kind:    KindShell,
		State:   StateDone,
		Command: "bash -c 'echo very long command line that exceeds thirty columns and should wrap'",
		Output:  "Line 1 of output that is very long and should be wrapped into multiple rows\n\nLine 2 after empty line",
	}

	lines35 := TaskDetailBody(task, 35)
	for i, l := range lines35 {
		if len([]rune(l)) > 35 {
			t.Errorf("task detail line %d exceeds 35 runes: %q", i, l)
		}
	}

	lines70 := TaskDetailBody(task, 70)
	if len(lines35) <= len(lines70) {
		t.Errorf("expected lines at width 35 (%d) to be greater than at width 70 (%d)", len(lines35), len(lines70))
	}
}

// TestTaskDetailBodyOutputFuncLiveStreaming verifies that TaskDetailBody prefers
// the live output provided by OutputFunc while the task is running.
func TestTaskDetailBodyOutputFuncLiveStreaming(t *testing.T) {
	task := &Task{
		ID:      "task-live-1",
		Name:    "server",
		Kind:    KindShell,
		State:   StateRunning,
		Command: "server run",
		Output:  "initial output",
		OutputFunc: func() string {
			return "live streaming line 1\nlive streaming line 2"
		},
	}

	lines := TaskDetailBody(task, 80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "live streaming line 1") {
		t.Errorf("expected live streaming output in TaskDetailBody, got:\n%s", joined)
	}
	if strings.Contains(joined, "initial output") {
		t.Errorf("expected OutputFunc to override static Output while running")
	}
}

func TestTaskDetailBody_TabExpansionAndWrap(t *testing.T) {
	tabbedOutput := "\tfunc main() {\n\t\tr.mu.Lock()\n\t\tdefer r.mu.Unlock()\n\t}"
	task := &Task{
		ID:      "task-tab-1",
		Command: "cat main.go",
		Kind:    KindShell,
		State:   StateDone,
		Output:  tabbedOutput,
	}

	lines := TaskDetailBody(task, 40)
	for i, l := range lines {
		if strings.Contains(l, "\t") {
			t.Errorf("line %d contains tab: %q", i, l)
		}
		if ansi.StringWidth(l) > 40 {
			t.Errorf("line %d exceeds 40 width: %q", i, l)
		}
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "    func main() {") {
		t.Errorf("expected 4-space tab expansion, got:\n%s", joined)
	}
}

// TestSubagentDetailHeaderShowsModelAndEffort pins the AGY 1.5.3 detail
// line: the subagent's full model and effort.
func TestSubagentDetailHeaderShowsModelAndEffort(t *testing.T) {
	header := SubagentDetailHeader(&Task{
		ID:     "sub-1",
		Name:   "worker",
		Kind:   KindSubagent,
		State:  StateRunning,
		Model:  "Gemini 3.8 Flash (High)",
		Effort: "high",
	}, SubagentHeaderContext{Width: 80})
	joined := strings.Join(header, "\n")
	if !strings.Contains(joined, "Model: Gemini 3.8 Flash (High) (effort: high)") {
		t.Errorf("header = %q, want the model and effort line", joined)
	}
}
