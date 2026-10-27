// step.go — the reference's Cortex step, as the step-string templates see it.
//
// The reference renders a step into a human-readable string with a template
// (google3/third_party/jetski/…/step_strings/run_command.tmpl, whose header
// names the converter: "Template for RunCommandStringConverter.
// ToOutputString"). The template's data model is the reference's own step
// proto, and the template reads it through proto getters:
//
//	.Step.GetRunCommand.Get{ProposedCommandLine,CommandLine,CommandId,
//	                         CombinedOutput,CombinedOutputSnapshot,ExitCode,
//	                         SandboxBypassDisabled,SandboxboxedTrustedCli,
//	                         SandboxOverride,SandboxErrors,IsTrustedCli,
//	                         StdoutOutput,StderrOutput,TerminalId}
//	.Step.GetStatus.String
//	.Step.GetMetadata.GetSource.String
//	.Step.GetMetadata.GetToolCall
//	.Step.GetError.GetShortError
//	.Step.GetSendCommandInput.Get{Output,Running,ExitCode}
//
// The types below answer exactly those names. They are not the reference's
// Go types — that lives in the closed-source monorepo — but the template is
// the contract, so the field names have to match it exactly.
package agystep

// Status is the Cortex step status. The templates compare its String
// against the enum's proto names, so the String form is the proto name and
// not a Go constant name.
type Status int

// The Cortex step statuses. The numbers are the reference's proto
// numbering; only the names matter to the templates.
const (
	StatusUnspecified Status = iota
	StatusPending
	StatusRunning
	StatusWaiting
	StatusDone
	StatusError
	StatusInvalid
	StatusCanceled
)

// String returns the proto enum name, which is what the templates compare
// against ("CORTEX_STEP_STATUS_DONE" and friends).
func (s Status) String() string {
	switch s {
	case StatusPending:
		return "CORTEX_STEP_STATUS_PENDING"
	case StatusRunning:
		return "CORTEX_STEP_STATUS_RUNNING"
	case StatusWaiting:
		return "CORTEX_STEP_STATUS_WAITING"
	case StatusDone:
		return "CORTEX_STEP_STATUS_DONE"
	case StatusError:
		return "CORTEX_STEP_STATUS_ERROR"
	case StatusInvalid:
		return "CORTEX_STEP_STATUS_INVALID"
	case StatusCanceled:
		return "CORTEX_STEP_STATUS_CANCELED"
	default:
		return "CORTEX_STEP_STATUS_UNSPECIFIED"
	}
}

// Source is where a step came from. The templates compare its String
// against the proto enum names.
type Source int

// The Cortex step sources.
const (
	SourceUnspecified Source = iota
	SourceUserExplicit
	SourceModel
	SourceSystem
)

// String returns the proto enum name.
func (s Source) String() string {
	switch s {
	case SourceUserExplicit:
		return "CORTEX_STEP_SOURCE_USER_EXPLICIT"
	case SourceModel:
		return "CORTEX_STEP_SOURCE_MODEL"
	case SourceSystem:
		return "CORTEX_STEP_SOURCE_SYSTEM"
	default:
		return "CORTEX_STEP_SOURCE_UNSPECIFIED"
	}
}

// Output is a step's captured output. The templates read its Full.
type Output struct {
	full string
}

// NewOutput returns an Output carrying body.
func NewOutput(body string) *Output { return &Output{full: body} }

// GetFull returns the captured bytes.
func (o *Output) GetFull() string {
	if o == nil {
		return ""
	}
	return o.full
}

// StepError is a step's failure. The templates read its ShortError.
type StepError struct {
	short string
}

// NewStepError returns a StepError carrying msg.
func NewStepError(msg string) *StepError { return &StepError{short: msg} }

// GetShortError returns the short form of the failure.
func (e *StepError) GetShortError() string {
	if e == nil {
		return ""
	}
	return e.short
}

// StepMetadata is the metadata a step carries.
type StepMetadata struct {
	source   Source
	toolCall bool
}

// NewStepMetadata returns metadata for one source.
func NewStepMetadata(source Source) *StepMetadata {
	return &StepMetadata{source: source}
}

// GetSource returns the step's source.
func (m *StepMetadata) GetSource() Source {
	if m == nil {
		return SourceUnspecified
	}
	return m.source
}

// GetToolCall reports whether the step is a tool call. The template uses
// it to pick between "Error invalid tool call:" and "Error:".
func (m *StepMetadata) GetToolCall() bool {
	if m == nil {
		return false
	}
	return m.toolCall
}

// WithToolCall marks the step as a tool call.
func (m *StepMetadata) WithToolCall(v bool) *StepMetadata {
	m.toolCall = v
	return m
}

// RunCommand is the payload of a run_command step.
type RunCommand struct {
	proposedCommandLine   string
	commandLine           string
	commandID             string
	combinedOutput        *Output
	combinedOutputSnaphot *Output
	exitCode              int
	sandboxBypassDisabled bool
	sandboxedTrustedCLI   string
	sandboxOverride       bool
	sandboxErrors         string
	isTrustedCLI          bool
	stdoutOutput          *Output
	stderrOutput          *Output
	terminalID            string
}

// RunCommandOptions is the builder input for a RunCommand step.
type RunCommandOptions struct {
	// ProposedCommandLine is the command the model asked for.
	ProposedCommandLine string
	// CommandLine is the command that actually ran. When it differs from
	// the proposal the template prints "The user changed the command to
	// be run to:".
	CommandLine string
	// CommandID is the background command's id. A non-empty id takes the
	// template's background branch.
	CommandID string
	// CombinedOutput is stdout and stderr merged, the way a shell
	// presents them.
	CombinedOutput string
	// CombinedOutputSnapshot is the output as it stood when the step was
	// written. It only shows up on the background branch.
	CombinedOutputSnapshot string
	// ExitCode is the command's exit status.
	ExitCode int
	// SandboxBypassDisabled suppresses the sandbox guidance entirely.
	SandboxBypassDisabled bool
	// SandboxedTrustedCLI names a CLI that must stay inside the sandbox.
	SandboxedTrustedCLI string
	// SandboxOverride records that the command already ran with a
	// sandbox override.
	SandboxOverride bool
	// SandboxErrors is the sandbox's error text.
	SandboxErrors string
	// IsTrustedCLI marks a command the sandbox considers trusted.
	IsTrustedCLI bool
	// StdoutOutput and StderrOutput are the two streams apart. The
	// template prints them only when there is no combined output.
	StdoutOutput string
	// StderrOutput is stderr on its own.
	StderrOutput string
	// TerminalID names the persistent terminal the command ran in.
	TerminalID string
}

// NewRunCommand builds the run_command payload.
func NewRunCommand(o RunCommandOptions) *RunCommand {
	rc := &RunCommand{
		proposedCommandLine:   o.ProposedCommandLine,
		commandLine:           o.CommandLine,
		commandID:             o.CommandID,
		exitCode:              o.ExitCode,
		sandboxBypassDisabled: o.SandboxBypassDisabled,
		sandboxedTrustedCLI:   o.SandboxedTrustedCLI,
		sandboxOverride:       o.SandboxOverride,
		sandboxErrors:         o.SandboxErrors,
		isTrustedCLI:          o.IsTrustedCLI,
		terminalID:            o.TerminalID,
	}
	if o.CombinedOutput != "" {
		rc.combinedOutput = NewOutput(o.CombinedOutput)
	}
	if o.CombinedOutputSnapshot != "" {
		rc.combinedOutputSnaphot = NewOutput(o.CombinedOutputSnapshot)
	}
	if o.StdoutOutput != "" {
		rc.stdoutOutput = NewOutput(o.StdoutOutput)
	}
	if o.StderrOutput != "" {
		rc.stderrOutput = NewOutput(o.StderrOutput)
	}
	return rc
}

func (r *RunCommand) GetProposedCommandLine() string { return r.proposedCommandLine }
func (r *RunCommand) GetCommandLine() string         { return r.commandLine }
func (r *RunCommand) GetCommandId() string           { return r.commandID }
func (r *RunCommand) GetExitCode() int               { return r.exitCode }

func (r *RunCommand) GetCombinedOutput() *Output {
	if r == nil {
		return nil
	}
	return r.combinedOutput
}

func (r *RunCommand) GetCombinedOutputSnapshot() *Output {
	if r == nil {
		return nil
	}
	return r.combinedOutputSnaphot
}

func (r *RunCommand) GetStdoutOutput() *Output {
	if r == nil {
		return nil
	}
	return r.stdoutOutput
}

func (r *RunCommand) GetStderrOutput() *Output {
	if r == nil {
		return nil
	}
	return r.stderrOutput
}

func (r *RunCommand) GetSandboxBypassDisabled() bool { return r.sandboxBypassDisabled }
func (r *RunCommand) GetSandboxedTrustedCli() string { return r.sandboxedTrustedCLI }
func (r *RunCommand) GetSandboxOverride() bool       { return r.sandboxOverride }
func (r *RunCommand) GetSandboxErrors() string       { return r.sandboxErrors }
func (r *RunCommand) GetIsTrustedCli() bool          { return r.isTrustedCLI }
func (r *RunCommand) GetTerminalId() string          { return r.terminalID }

// SendCommandInput is the payload of a send_command_input step.
type SendCommandInput struct {
	output   *Output
	running  bool
	exitCode int
}

// SendCommandInputOptions is the builder input for a send_command_input
// step.
type SendCommandInputOptions struct {
	// Output is the command's output as it stood when the input was sent.
	Output string
	// Running reports whether the command is still running.
	Running bool
	// ExitCode is the command's exit status, once it has one.
	ExitCode int
}

// NewSendCommandInput builds the send_command_input payload.
func NewSendCommandInput(o SendCommandInputOptions) *SendCommandInput {
	s := &SendCommandInput{running: o.Running, exitCode: o.ExitCode}
	if o.Output != "" {
		s.output = NewOutput(o.Output)
	}
	return s
}

func (s *SendCommandInput) GetOutput() *Output {
	if s == nil {
		return nil
	}
	return s.output
}

func (s *SendCommandInput) GetRunning() bool { return s.running }
func (s *SendCommandInput) GetExitCode() int { return s.exitCode }

// Step is one Cortex step: a status, a metadata record, an optional error
// and one of the two payloads the reference's step strings cover.
type Step struct {
	status           Status
	metadata         *StepMetadata
	stepError        *StepError
	runCommand       *RunCommand
	sendCommandInput *SendCommandInput
}

// StepOptions is the builder input for a Step.
type StepOptions struct {
	// Status is the step's status.
	Status Status
	// Source is where the step came from.
	Source Source
	// ToolCall marks the step as a tool call.
	ToolCall bool
	// Error is the step's failure, if any.
	Error string
	// RunCommand is the run_command payload.
	RunCommand *RunCommand
	// SendCommandInput is the send_command_input payload.
	SendCommandInput *SendCommandInput
}

// NewStep builds a step.
func NewStep(o StepOptions) *Step {
	s := &Step{
		status:           o.Status,
		metadata:         NewStepMetadata(o.Source).WithToolCall(o.ToolCall),
		runCommand:       o.RunCommand,
		sendCommandInput: o.SendCommandInput,
	}
	if o.Error != "" {
		s.stepError = NewStepError(o.Error)
	}
	return s
}

// GetStatus returns the step's status.
func (s *Step) GetStatus() Status {
	if s == nil {
		return StatusUnspecified
	}
	return s.status
}

// GetMetadata returns the step's metadata.
func (s *Step) GetMetadata() *StepMetadata {
	if s == nil {
		return nil
	}
	return s.metadata
}

// GetError returns the step's failure.
func (s *Step) GetError() *StepError {
	if s == nil {
		return nil
	}
	return s.stepError
}

// GetRunCommand returns the run_command payload, if any.
func (s *Step) GetRunCommand() *RunCommand {
	if s == nil {
		return nil
	}
	return s.runCommand
}

// GetSendCommandInput returns the send_command_input payload, if any.
func (s *Step) GetSendCommandInput() *SendCommandInput {
	if s == nil {
		return nil
	}
	return s.sendCommandInput
}
