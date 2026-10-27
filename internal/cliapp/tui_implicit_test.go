package cliapp

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/implicit"
)

// implicitRunMode builds a run mode whose implicit-trajectory store lives in
// a fresh directory.
func implicitRunMode(t *testing.T, home, workspace string) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", home)
	m := NewRunMode(&fakeClient{}, "u@t", "Gemni 3.8 Flash (Medium)", "", workspace, false)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// TestBashCommandIsRecordedInTheImplicitTrajectory pins the reference's
// implicit trajectory: a `!` command never reaches the conversation, so the
// trajectory is the only place it is written down.
//
// 【一手】 agy 1.4.7: the record is a CORTEX_STEP_TYPE_RUN_COMMAND step with
// the command written twice (command_line and proposed_command_line),
// blocking and used_ide_terminal set, and the exit code present.
func TestBashCommandIsRecordedInTheImplicitTrajectory(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	m := implicitRunMode(t, home, workspace)

	m.finishBashCommand(bashDoneMsg{cmd: "printf 'a\\nb\\n'", output: "a\nb", hasCode: true})

	steps, err := implicit.Open(home).Steps(implicit.WorkspaceURI(workspace))
	if err != nil {
		t.Fatalf("steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("got %d steps, want 1", len(steps))
	}
	step := steps[0]
	if step.Status != implicit.StepStatusDone {
		t.Errorf("status = %d, want %d", step.Status, implicit.StepStatusDone)
	}
	if step.RunCommand.CommandLine != "printf 'a\\nb\\n'" {
		t.Errorf("command_line = %q", step.RunCommand.CommandLine)
	}
	if step.RunCommand.ProposedCommandLine != step.RunCommand.CommandLine {
		t.Errorf("proposed_command_line = %q, want %q",
			step.RunCommand.ProposedCommandLine, step.RunCommand.CommandLine)
	}
	if step.RunCommand.CWD != workspace {
		t.Errorf("cwd = %q, want %q", step.RunCommand.CWD, workspace)
	}
	if step.RunCommand.CombinedOutput != "a\nb" {
		t.Errorf("combined_output = %q, want %q", step.RunCommand.CombinedOutput, "a\nb")
	}
	if !step.RunCommand.HasExitCode || step.RunCommand.ExitCode != 0 {
		t.Errorf("exit_code = %d (set %v), want 0 (set)", step.RunCommand.ExitCode, step.RunCommand.HasExitCode)
	}
	if !step.RunCommand.Blocking {
		t.Error("blocking is not set")
	}
	if !step.RunCommand.UsedIDETerminal {
		t.Error("used_ide_terminal is not set")
	}
}

// TestACanceledBashCommandIsRecordedAsCanceled pins the reference's canceled
// record: the status is CANCELED and the exit code was never set.
func TestACanceledBashCommandIsRecordedAsCanceled(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	m := implicitRunMode(t, home, workspace)

	m.finishBashCommand(bashDoneMsg{cmd: "top", cancelled: true})

	steps, err := implicit.Open(home).Steps(implicit.WorkspaceURI(workspace))
	if err != nil {
		t.Fatalf("steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("got %d steps, want 1", len(steps))
	}
	if steps[0].Status != implicit.StepStatusCanceled {
		t.Errorf("status = %d, want %d", steps[0].Status, implicit.StepStatusCanceled)
	}
	if steps[0].RunCommand.HasExitCode {
		t.Error("the canceled step carries an exit code")
	}
}

// TestTheTrajectoryIsPerWorkspace pins the scope: two workspaces have two
// trajectories.
func TestTheTrajectoryIsPerWorkspace(t *testing.T) {
	home := t.TempDir()
	one := t.TempDir()
	two := t.TempDir()
	m := implicitRunMode(t, home, one)

	m.finishBashCommand(bashDoneMsg{cmd: "echo one", output: "one", hasCode: true})
	m.cwd = two
	m.finishBashCommand(bashDoneMsg{cmd: "echo two", output: "two", hasCode: true})

	store := implicit.Open(home)
	first, err := store.Steps(implicit.WorkspaceURI(one))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Steps(implicit.WorkspaceURI(two))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].RunCommand.CommandLine != "echo one" {
		t.Errorf("the first workspace's trajectory = %#v", first)
	}
	if len(second) != 1 || second[0].RunCommand.CommandLine != "echo two" {
		t.Errorf("the second workspace's trajectory = %#v", second)
	}
}

// TestBashExitCode pins the exit status the record carries.
func TestBashExitCode(t *testing.T) {
	if code, ok := bashExitCode(nil); !ok || code != 0 {
		t.Errorf("bashExitCode(nil) = (%d, %v), want (0, true)", code, ok)
	}
	if _, ok := bashExitCode(errNotAnExit{}); ok {
		t.Error("bashExitCode claimed an exit code for an error that is not one")
	}
}

// errNotAnExit is an error that carries no exit status.
type errNotAnExit struct{}

func (errNotAnExit) Error() string { return "not an exit error" }
