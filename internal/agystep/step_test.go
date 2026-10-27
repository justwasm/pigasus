package agystep

import (
	"strings"
	"testing"
)

// TestRunCommandStringMatchesTheReferenceRecord pins the renderer against
// the reference's own artifact.
//
// The reference's transcript.jsonl carries eleven command steps from
// 2026-10-19/20 (the 1.4.2 era). One of them reads, verbatim (一手 — the
// record is in
// ~/.gemini/antigravity-cli/brain/689927ff-…/.system_generated/logs/transcript.jsonl):
//
//	Created At: 2026-10-20T02:27:51+08:00
//	Completed At: 2026-10-20T02:27:51+08:00
//
//	The command exited with code 0.
//	Output:
//	PROBE-0=zsh\r\nPROBE-SHELL=/opt/homebrew/bin/bash\r\nzsh\r\nPROBE-DONE\r\n
//
// The body after the two stamp lines is exactly what
// step_strings/run_command.tmpl renders — the template's own leading
// newline included. Rendering the template and prefixing the stamp
// reproduces the reference's record byte for byte.
func TestRunCommandStringMatchesTheReferenceRecord(t *testing.T) {
	const referenceRecord = "Created At: 2026-10-20T02:27:51+08:00\n" +
		"Completed At: 2026-10-20T02:27:51+08:00\n" +
		"\nThe command exited with code 0.\n" +
		"Output:\n" +
		"PROBE-0=zsh\r\nPROBE-SHELL=/opt/homebrew/bin/bash\r\nzsh\r\nPROBE-DONE\r\n\n"

	step := NewStep(StepOptions{
		Status: StatusDone,
		Source: SourceUserExplicit,
		RunCommand: NewRunCommand(RunCommandOptions{
			ProposedCommandLine: "printf 'PROBE-0=zsh\\r\\nPROBE-SHELL=/opt/homebrew/bin/bash\\r\\nzsh\\r\\nPROBE-DONE\\r\\n'",
			CommandLine:         "printf 'PROBE-0=zsh\\r\\nPROBE-SHELL=/opt/homebrew/bin/bash\\r\\nzsh\\r\\nPROBE-DONE\\r\\n'",
			CombinedOutput:      "PROBE-0=zsh\r\nPROBE-SHELL=/opt/homebrew/bin/bash\r\nzsh\r\nPROBE-DONE\r\n",
		}),
	})

	stamp := "Created At: 2026-10-20T02:27:51+08:00\n" +
		"Completed At: 2026-10-20T02:27:51+08:00\n"
	got := stamp + RunCommandString(step)
	if got != referenceRecord {
		t.Errorf("the rendered step string does not reproduce the reference's record\n got: %q\nwant: %q", got, referenceRecord)
	}
}

// TestRunCommandStringBranches walks the branches the template has, so a
// template that lost one cannot pass unnoticed.
func TestRunCommandStringBranches(t *testing.T) {
	run := func(o StepOptions) string {
		return RunCommandString(NewStep(o))
	}
	done := func(rc *RunCommand) StepOptions {
		return StepOptions{Status: StatusDone, Source: SourceUserExplicit, RunCommand: rc}
	}

	// The plain path.
	got := run(done(NewRunCommand(RunCommandOptions{CommandLine: "echo hi", CombinedOutput: "hi\n"})))
	if !strings.Contains(got, "The command exited with code 0.") {
		t.Errorf("the plain path does not report the exit code:\n%q", got)
	}
	if !strings.Contains(got, "Output:\nhi\n") {
		t.Errorf("the plain path does not print the output:\n%q", got)
	}

	// An edited command line.
	got = run(done(NewRunCommand(RunCommandOptions{
		ProposedCommandLine: "rm -rf /",
		CommandLine:         "rm -rf /tmp/x",
		CombinedOutput:      "",
	})))
	if !strings.Contains(got, "The user changed the command to be run to: rm -rf /tmp/x") {
		t.Errorf("the edited-command branch is not reachable:\n%q", got)
	}

	// A background command.
	got = run(done(NewRunCommand(RunCommandOptions{
		CommandLine:            "sleep 10",
		CommandID:              "task-1",
		CombinedOutputSnapshot: "so far\n",
	})))
	if !strings.Contains(got, "Background command ID: task-1") {
		t.Errorf("the background branch does not name the command id:\n%q", got)
	}
	if !strings.Contains(got, "Output snapshot:\nso far\n") {
		t.Errorf("the background branch does not print the snapshot:\n%q", got)
	}

	// An empty command.
	got = run(done(NewRunCommand(RunCommandOptions{CommandLine: "true"})))
	if !strings.Contains(got, "No output") {
		t.Errorf("the empty-output branch is not reachable:\n%q", got)
	}

	// A persistent terminal.
	got = run(done(NewRunCommand(RunCommandOptions{CommandLine: "ls", TerminalID: "term-7"})))
	if !strings.Contains(got, "Terminal ID: term-7") {
		t.Errorf("the terminal-id branch is not reachable:\n%q", got)
	}

	// A sandboxed failure.
	got = run(StepOptions{
		Status: StatusDone,
		Source: SourceUserExplicit,
		RunCommand: NewRunCommand(RunCommandOptions{
			CommandLine:   "curl https://example.com",
			ExitCode:      7,
			SandboxErrors: "network is unreachable",
		}),
	})
	if !strings.Contains(got, "There were sandbox errors that may or may not be related to the failure.") {
		t.Errorf("the sandbox-guidance branch is not reachable:\n%q", got)
	}

	// A trusted CLI that must stay inside the sandbox.
	got = run(StepOptions{
		Status: StatusDone,
		Source: SourceUserExplicit,
		RunCommand: NewRunCommand(RunCommandOptions{
			CommandLine:         "gcloud auth login",
			ExitCode:            1,
			SandboxErrors:       "network is unreachable",
			SandboxedTrustedCLI: "gcloud",
		}),
	})
	if !strings.Contains(got, "which MUST run inside the sandbox") {
		t.Errorf("the trusted-CLI branch is not reachable:\n%q", got)
	}

	// A canceled step.
	got = run(StepOptions{
		Status: StatusCanceled,
		Source: SourceUserExplicit,
		Error:  "the user pressed ctrl-c",
		RunCommand: NewRunCommand(RunCommandOptions{
			CommandLine: "sleep 10",
		}),
	})
	if !strings.Contains(got, "Step was canceled: the user pressed ctrl-c") {
		t.Errorf("the canceled branch is not reachable:\n%q", got)
	}

	// A failed step.
	got = run(StepOptions{
		Status: StatusError,
		Source: SourceUserExplicit,
		Error:  "the shell could not be started",
		RunCommand: NewRunCommand(RunCommandOptions{
			CommandLine: "nope",
		}),
	})
	if !strings.Contains(got, "Encountered error in step execution: the shell could not be started") {
		t.Errorf("the error branch is not reachable:\n%q", got)
	}

	// A running step.
	got = run(StepOptions{
		Status: StatusRunning,
		Source: SourceUserExplicit,
		RunCommand: NewRunCommand(RunCommandOptions{
			CommandLine: "sleep 10",
		}),
	})
	if !strings.Contains(got, "Step is still running") {
		t.Errorf("the running branch is not reachable:\n%q", got)
	}
}

// TestSendCommandInputString pins the other step string.
func TestSendCommandInputString(t *testing.T) {
	got := SendCommandInputString(NewStep(StepOptions{
		Status:           StatusDone,
		Source:           SourceUserExplicit,
		SendCommandInput: NewSendCommandInput(SendCommandInputOptions{Output: "so far\n", Running: true}),
	}))
	if !strings.Contains(got, "Input sent successfully.") {
		t.Errorf("the step string does not confirm the input:\n%q", got)
	}
	if !strings.Contains(got, "The command is still running.") {
		t.Errorf("the step string does not report the running command:\n%q", got)
	}
	if !strings.Contains(got, "Output:\nso far\n") {
		t.Errorf("the step string does not print the output:\n%q", got)
	}

	got = SendCommandInputString(NewStep(StepOptions{
		Status:           StatusDone,
		Source:           SourceUserExplicit,
		SendCommandInput: NewSendCommandInput(SendCommandInputOptions{ExitCode: 3}),
	}))
	if !strings.Contains(got, "The command exited with code 3.") {
		t.Errorf("the finished step string does not report the exit code:\n%q", got)
	}
}
