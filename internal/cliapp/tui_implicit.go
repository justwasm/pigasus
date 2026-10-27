// tui_implicit.go — the reference's implicit-trajectory store.
//
// 【一手】 agy 1.4.7: a `!` command never reaches the conversation. The
// reference records it in a separate, per-workspace store —
// `<gemini-dir>/implicit/<uuid>.pb`, AES-256-GCM encrypted — as a
// `CORTEX_STEP_TYPE_RUN_COMMAND` step, and the cumulative-prompt handler
// later converts that trajectory into chat messages for the next prompt.
//
// The conversation store holds no USER_IMPLICIT record and the brain's
// transcript.jsonl only carries MODEL / SYSTEM / USER_EXPLICIT sources: the
// command lives *only* here. The clone therefore writes the record as the
// command finishes, and it is the only place the clone writes it.
package cliapp

import (
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/implicit"
)

// recordImplicitStep writes a finished shell command into the reference's
// implicit trajectory store.
//
// The step's fields are the reference's own:
//
//   - the command is written twice, as `command_line` and as
//     `proposed_command_line` — the reference writes the same bytes in both
//     for a command the user typed;
//   - `blocking` and `used_ide_terminal` are set for every `!` command;
//   - a canceled command has no exit code: the reference never set the
//     field, so the record leaves it out.
//
// The write is best-effort: the reference writes the trajectory from its
// server goroutine and a failure there does not reach the user either.
func (m *runMode) recordImplicitStep(cmd, output string, exitCode int, hasExitCode, canceled bool) {
	if cmd == "" {
		return
	}
	workspace := implicit.WorkspaceURI(m.cwd)
	now := time.Now()
	step := implicit.Step{
		Status: implicit.StepStatusDone,
		RunCommand: implicit.RunCommand{
			CommandLine:         cmd,
			ProposedCommandLine: cmd,
			CWD:                 m.cwd,
			ExitCode:            exitCode,
			HasExitCode:         hasExitCode,
			CombinedOutput:      output,
			Blocking:            true,
			UsedIDETerminal:     true,
		},
		CreatedAt:   now,
		CompletedAt: now,
	}
	if canceled {
		step.Status = implicit.StepStatusCanceled
	}
	_ = implicit.Open(convstore.StoreDir()).Record(workspace, step)
}
