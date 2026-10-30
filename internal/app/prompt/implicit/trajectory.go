// Package implicit / trajectory.go — the reference's implicit-trajectory
// message layout.
//
// 【一手】 The reference's 1.4.7 binary writes an
// `exa.jetski_cortex_pb.ImplicitTrajectory` into
// `<gemini-dir>/implicit/<uuid>.pb`. The wire dump of a real record (the
// `printf 'a\nb\n'` command of 2026-10-22) is:
//
//	#1 message(23755) {            <- ImplicitTrajectory.trajectory
//	  #1 string(36)  "8b2cf1f3-…"          <- Trajectory.trajectory_id
//	  #2 message(155) {             <- Trajectory.steps (repeated Step)
//	    #1 varint 21                       <- CORTEX_STEP_TYPE_RUN_COMMAND
//	    #4 varint 3                        <- CORTEX_STEP_STATUS_DONE
//	    #5 message(28) {                   <- CortexStepMetadata
//	      #1 message(11) { #1 varint 1792644998 #2 varint 180119000 }
//	      #3 varint 4                        <- CORTEX_STEP_SOURCE_USER_EXPLICIT
//	      #8 message(11) { #1 varint 1792644998 #2 varint 196151000 }
//	    }
//	    #28 message(118) {                 <- CortexStepRunCommand
//	      #2  string(65) "/private/var/…/T/opencode"
//	      #6  varint 0                         <- exit_code
//	      #11 varint 1                         <- blocking
//	      #21 message(5) { #1 string(3) "a\nb" }
//	      #22 varint 1                         <- used_ide_terminal
//	      #23 string(15) "printf 'a\nb\n'"     <- command_line
//	      #25 string(15) "printf 'a\nb\n'"     <- proposed_command_line
//	    }
//	  }
//	  #4 varint 1                    <- CORTEX_TRAJECTORY_TYPE_USER_MAINLINE
//	  #6 string(36) "3a75e51c-…"     <- cascade_id (== the file name)
//	  #7 message(130) {               <- CortexTrajectoryMetadata
//	    #1 message(76) { #1 string(72) "file:///private/var/…" }
//	    #2 message(12) { #1 varint 1792434460 #2 varint 561202000 }
//	    #3 string(36) "f818e378-…"
//	  }
//	}
//	#5 string(74) "\nHfile:///private/var/…"
//	                                  <- ImplicitTrajectory.trajectory_scope
//
// The enum numbers are the reference's own (confirmed against the recovered
// `out/local-macos/protos/cortex_1.4.6.proto`): CORTEX_STEP_TYPE_RUN_COMMAND
// = 21, CORTEX_STEP_STATUS_DONE = 3, CORTEX_STEP_STATUS_CANCELED = 6,
// CORTEX_STEP_SOURCE_USER_EXPLICIT = 4, CORTEX_TRAJECTORY_TYPE_USER_MAINLINE
// = 1.
//
// `internal/cortex_pb` used to carry a *reconstructed* enum with different
// numbers (its `CortexStepTypeRunCommand` was 3, not 21), which is why the
// numbers were repeated here. Cycle 307 rewrote that package's enums from the
// same .proto, so the constants below are borrowed from there now — one
// source of truth, and the values it holds are the reference's.
package implicit

import (
	"time"

	"github.com/justwasm/pigasus/internal/cortex_pb"
)

// The reference's enum values. Grade 【一手】 — every one of them was read
// out of a real record, and confirmed against the recovered .proto.
const (
	// StepTypeRunCommand is CORTEX_STEP_TYPE_RUN_COMMAND.
	StepTypeRunCommand = int(cortex_pb.CortexStepTypeRunCommand)

	// StepStatusDone is CORTEX_STEP_STATUS_DONE.
	StepStatusDone = int(cortex_pb.CortexStepStatusDone)

	// StepStatusCanceled is CORTEX_STEP_STATUS_CANCELED.
	StepStatusCanceled = int(cortex_pb.CortexStepStatusCanceled)

	// StepSourceUserExplicit is CORTEX_STEP_SOURCE_USER_EXPLICIT: the
	// command was typed by the user, not proposed by the model.
	StepSourceUserExplicit = int(cortex_pb.CortexStepSourceUserExplicit)

	// TrajectoryTypeUserMainline is CORTEX_TRAJECTORY_TYPE_USER_MAINLINE.
	TrajectoryTypeUserMainline = int(cortex_pb.CortexTrajectoryTypeUserMainline)
)

// The field numbers of the messages above.
const (
	fieldTrajectory       = 1  // ImplicitTrajectory.trajectory
	fieldTrajectoryScope  = 5  // ImplicitTrajectory.trajectory_scope
	fieldTrajectoryID     = 1  // Trajectory.trajectory_id
	fieldSteps            = 2  // Trajectory.steps
	fieldTrajectoryType   = 4  // Trajectory.trajectory_type
	fieldCascadeID        = 6  // Trajectory.cascade_id
	fieldTrajectoryMeta   = 7  // Trajectory.metadata
	fieldStepType         = 1  // Step.type
	fieldStepStatus       = 4  // Step.status
	fieldStepMeta         = 5  // Step.metadata
	fieldStepRunCommand   = 28 // Step.run_command
	fieldMetaCreatedAt    = 1  // CortexStepMetadata.created_at
	fieldMetaSource       = 3  // CortexStepMetadata.source
	fieldMetaCompletedAt  = 8  // CortexStepMetadata.completed_at
	fieldCmdCwd           = 2  // CortexStepRunCommand.cwd
	fieldCmdExitCode      = 6  // CortexStepRunCommand.exit_code
	fieldCmdBlocking      = 11 // CortexStepRunCommand.blocking
	fieldCmdCommandID     = 13 // CortexStepRunCommand.command_id
	fieldCmdCombinedOut   = 21 // CortexStepRunCommand.combined_output
	fieldCmdUsedIDETerm   = 22 // CortexStepRunCommand.used_ide_terminal
	fieldCmdCommandLine   = 23 // CortexStepRunCommand.command_line
	fieldCmdProposedLine  = 25 // CortexStepRunCommand.proposed_command_line
	fieldCmdNotifyTimeout = 30 // CortexStepRunCommand.notification_timeout_seconds
	fieldOutputFull       = 1  // RunCommandOutput.full
	fieldScopeWorkspace   = 1  // TrajectoryScope.workspace_uri
	fieldScopeBranch      = 3  // TrajectoryScope.branch_name
	fieldMetaScope        = 1  // CortexTrajectoryMetadata.scope
	fieldMetaTimestamps   = 2  // CortexTrajectoryMetadata.timestamps
	fieldMetaUUID         = 3  // CortexTrajectoryMetadata.uuid
	fieldTimestampSeconds = 1  // Timestamp.seconds
	fieldTimestampNanos   = 2  // Timestamp.nanos
)

// RunCommand is one recorded shell command — the payload of a
// `CORTEX_STEP_TYPE_RUN_COMMAND` step.
type RunCommand struct {
	// CommandLine is the command as it was run.
	CommandLine string

	// ProposedCommandLine is the command as it was proposed. The reference
	// writes the same bytes in both fields for a command the user typed.
	ProposedCommandLine string

	// CWD is the working directory the command ran in.
	CWD string

	// ExitCode is the command's exit status. It is absent from the record
	// when the command was canceled: the reference never set it.
	ExitCode int

	// HasExitCode tells a zero exit code from an unset one. The reference
	// writes the field explicitly for a command that ran (its
	// `printf 'a\nb\n'` record carries `#6 varint 0`) and omits it for a
	// canceled command.
	HasExitCode bool

	// CombinedOutput is the command's stdout and stderr together.
	CombinedOutput string

	// CommandID identifies the command for the reference's task tracker.
	// It is absent for a command that finished before the tracker
	// registered it.
	CommandID string

	// Blocking is the reference's "this command ran in the foreground"
	// flag. The reference sets it for every `!` command.
	Blocking bool

	// UsedIDETerminal marks a command that ran in the IDE's terminal.
	// The reference sets it for every `!` command.
	UsedIDETerminal bool

	// NotificationTimeoutSeconds is how long the command may keep running
	// after the wait window before the system tells the model that it is
	// still running. The reference's proto field is
	// `notification_timeout_seconds = 30` (uint64) — 【一手】 the
	// reference's own descriptor
	// (out/local-macos/protos/cortex_1.4.6.proto). The clone writes it
	// only when it is set: proto3 omits a zero.
	NotificationTimeoutSeconds int
}

// Step is one recorded command with its status and timestamps.
type Step struct {
	// Status is StepStatusDone or StepStatusCanceled.
	Status int

	// RunCommand is the command's payload.
	RunCommand RunCommand

	// CreatedAt is when the command was recorded as started.
	CreatedAt time.Time

	// CompletedAt is when it finished.
	CompletedAt time.Time
}

// encodeTimestamp encodes a google.protobuf.Timestamp.
func encodeTimestamp(t time.Time) []byte {
	if t.IsZero() {
		return nil
	}
	out := appendVarintField(nil, fieldTimestampSeconds, uint64(t.Unix()))
	if nanos := uint32(t.Nanosecond()); nanos != 0 {
		out = appendVarintField(out, fieldTimestampNanos, uint64(nanos))
	}
	return out
}

// decodeTimestamp decodes a google.protobuf.Timestamp.
func decodeTimestamp(b []byte) time.Time {
	fields := parseFields(b)
	secs, _ := firstVarint(fields, fieldTimestampSeconds)
	nanos, _ := firstVarint(fields, fieldTimestampNanos)
	return time.Unix(int64(secs), int64(nanos)).UTC()
}

// encodeScope encodes a TrajectoryScope.
func encodeScope(workspaceURI string) []byte {
	out := appendStringField(nil, fieldScopeWorkspace, workspaceURI)
	// branch_name is field 3 and empty in the reference's records; it is
	// written as an empty bytes field, not omitted.
	return appendBytesField(out, fieldScopeBranch, nil)
}

// encodeRunCommand encodes a CortexStepRunCommand.
func encodeRunCommand(rc RunCommand) []byte {
	out := appendStringField(nil, fieldCmdCwd, rc.CWD)
	if rc.HasExitCode {
		out = appendVarintField(out, fieldCmdExitCode, uint64(rc.ExitCode))
	}
	out = appendBoolField(out, fieldCmdBlocking, rc.Blocking)
	if rc.CommandID != "" {
		out = appendStringField(out, fieldCmdCommandID, rc.CommandID)
	}
	if rc.CombinedOutput != "" {
		output := appendStringField(nil, fieldOutputFull, rc.CombinedOutput)
		out = appendMessageField(out, fieldCmdCombinedOut, output)
	}
	out = appendBoolField(out, fieldCmdUsedIDETerm, rc.UsedIDETerminal)
	out = appendStringField(out, fieldCmdCommandLine, rc.CommandLine)
	out = appendStringField(out, fieldCmdProposedLine, rc.ProposedCommandLine)
	if rc.NotificationTimeoutSeconds > 0 {
		out = appendVarintField(out, fieldCmdNotifyTimeout, uint64(rc.NotificationTimeoutSeconds))
	}
	return out
}

// encodeStep encodes a Step.
func encodeStep(step Step) []byte {
	meta := appendMessageField(nil, fieldMetaCreatedAt, encodeTimestamp(step.CreatedAt))
	meta = appendVarintField(meta, fieldMetaSource, uint64(StepSourceUserExplicit))
	meta = appendMessageField(meta, fieldMetaCompletedAt, encodeTimestamp(step.CompletedAt))

	out := appendVarintField(nil, fieldStepType, uint64(StepTypeRunCommand))
	out = appendVarintField(out, fieldStepStatus, uint64(step.Status))
	out = appendMessageField(out, fieldStepMeta, meta)
	out = appendMessageField(out, fieldStepRunCommand, encodeRunCommand(step.RunCommand))
	return out
}

// decodeStep decodes a Step.
func decodeStep(b []byte) (Step, bool) {
	fields := parseFields(b)
	step := Step{}
	if v, ok := firstVarint(fields, fieldStepType); ok && v != uint64(StepTypeRunCommand) {
		// A step of another type — the reference records a second
		// COMMAND_STATUS step for a long-running command. The clone
		// does not write those, and does not read them.
		return step, false
	}
	if v, ok := firstVarint(fields, fieldStepStatus); ok {
		step.Status = int(v)
	}
	if meta, ok := firstMessage(fields, fieldStepMeta); ok {
		metaFields := parseFields(meta)
		if created, ok := firstMessage(metaFields, fieldMetaCreatedAt); ok {
			step.CreatedAt = decodeTimestamp(created)
		}
		if completed, ok := firstMessage(metaFields, fieldMetaCompletedAt); ok {
			step.CompletedAt = decodeTimestamp(completed)
		}
	}
	rc, ok := firstMessage(fields, fieldStepRunCommand)
	if !ok {
		return step, false
	}
	rcFields := parseFields(rc)
	step.RunCommand.CWD, _ = firstString(rcFields, fieldCmdCwd)
	if v, ok := firstVarint(rcFields, fieldCmdExitCode); ok {
		step.RunCommand.ExitCode = int(int32(v))
		step.RunCommand.HasExitCode = true
	}
	if v, ok := firstVarint(rcFields, fieldCmdBlocking); ok {
		step.RunCommand.Blocking = v != 0
	}
	step.RunCommand.CommandID, _ = firstString(rcFields, fieldCmdCommandID)
	if output, ok := firstMessage(rcFields, fieldCmdCombinedOut); ok {
		outputFields := parseFields(output)
		step.RunCommand.CombinedOutput, _ = firstString(outputFields, fieldOutputFull)
	}
	if v, ok := firstVarint(rcFields, fieldCmdUsedIDETerm); ok {
		step.RunCommand.UsedIDETerminal = v != 0
	}
	step.RunCommand.CommandLine, _ = firstString(rcFields, fieldCmdCommandLine)
	step.RunCommand.ProposedCommandLine, _ = firstString(rcFields, fieldCmdProposedLine)
	if v, ok := firstVarint(rcFields, fieldCmdNotifyTimeout); ok {
		step.RunCommand.NotificationTimeoutSeconds = int(v)
	}
	return step, true
}

// trajectory is a decoded ImplicitTrajectory.
type trajectory struct {
	// raw is the Trajectory message's bytes, kept verbatim so the fields
	// the clone does not understand survive a rewrite.
	raw []byte

	// steps are the trajectory's run-command steps.
	steps []Step

	// scope is the trajectory's scope.
	scope string
}

// decodeTrajectory decodes an ImplicitTrajectory message.
func decodeTrajectory(b []byte) (trajectory, error) {
	fields := parseFields(b)
	traj := trajectory{}
	raw, ok := firstMessage(fields, fieldTrajectory)
	if !ok {
		return traj, errNoTrajectory
	}
	traj.raw = raw
	for _, f := range parseFields(raw) {
		if f.num != fieldSteps || f.wire != 2 {
			continue
		}
		step, ok := decodeStep(f.data)
		if !ok {
			continue
		}
		traj.steps = append(traj.steps, step)
	}
	if scope, ok := firstMessage(fields, fieldTrajectoryScope); ok {
		scopeFields := parseFields(scope)
		traj.scope, _ = firstString(scopeFields, fieldScopeWorkspace)
	}
	return traj, nil
}

// appendStep returns the trajectory's bytes with one more step appended.
//
// Every field the clone does not know about is written back unchanged, so a
// trajectory the reference wrote stays readable by the reference.
func appendStep(raw []byte, step Step) []byte {
	fields := parseFields(raw)
	out := make([]byte, 0, len(raw)+256)
	written := false
	for _, f := range fields {
		if f.num == fieldSteps && f.wire == 2 {
			if !written {
				out = append(out, fieldBytes(f)...)
				written = true
			}
			continue
		}
		out = append(out, fieldBytes(f)...)
	}
	out = appendBytesField(out, fieldSteps, encodeStep(step))
	return out
}
