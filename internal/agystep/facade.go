// Package agystep is a backward-compatible facade over internal/app/prompt/step.
package agystep

import (
	"github.com/justwasm/pigasus/internal/app/prompt/step"
)

type Status = step.Status

const (
	StatusUnspecified = step.StatusUnspecified
	StatusPending     = step.StatusPending
	StatusRunning     = step.StatusRunning
	StatusWaiting     = step.StatusWaiting
	StatusDone        = step.StatusDone
	StatusError       = step.StatusError
	StatusInvalid     = step.StatusInvalid
	StatusCanceled    = step.StatusCanceled
)

type Source = step.Source

const (
	SourceUnspecified  = step.SourceUnspecified
	SourceUserExplicit = step.SourceUserExplicit
	SourceModel        = step.SourceModel
	SourceSystem       = step.SourceSystem
)

type (
	Output                  = step.Output
	StepError               = step.StepError
	StepMetadata            = step.StepMetadata
	RunCommand              = step.RunCommand
	RunCommandOptions       = step.RunCommandOptions
	SendCommandInput        = step.SendCommandInput
	SendCommandInputOptions = step.SendCommandInputOptions
	Step                    = step.Step
	StepOptions             = step.StepOptions
	Result                  = step.Result
)

var (
	NewOutput              = step.NewOutput
	NewStepError           = step.NewStepError
	NewStepMetadata        = step.NewStepMetadata
	NewRunCommand          = step.NewRunCommand
	NewSendCommandInput    = step.NewSendCommandInput
	NewStep                = step.NewStep
	RunCommandString       = step.RunCommandString
	SendCommandInputString = step.SendCommandInputString
	ParseRunCommandResult  = step.ParseRunCommandResult
)
