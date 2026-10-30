// Package implicit is a backward-compatible facade over internal/app/prompt/implicit.
package implicit

import (
	"github.com/justwasm/pigasus/internal/app/prompt/implicit"
)

const (
	Key                        = implicit.Key
	MaxFiles                   = implicit.MaxFiles
	StepTypeRunCommand         = implicit.StepTypeRunCommand
	StepStatusDone             = implicit.StepStatusDone
	StepStatusCanceled         = implicit.StepStatusCanceled
	StepSourceUserExplicit     = implicit.StepSourceUserExplicit
	TrajectoryTypeUserMainline = implicit.TrajectoryTypeUserMainline
)

type (
	Store      = implicit.Store
	RunCommand = implicit.RunCommand
	Step       = implicit.Step
)

var (
	Open         = implicit.Open
	WorkspaceURI = implicit.WorkspaceURI
)
