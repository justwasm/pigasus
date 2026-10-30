// Package rehydration is a backward-compatible facade over internal/app/session/rehydration.
package rehydration

import (
	"github.com/justwasm/pigasus/internal/app/session/rehydration"
)

const (
	StepToolCall  = rehydration.StepToolCall
	DisableEnvVar = rehydration.DisableEnvVar
)

type (
	Metadata    = rehydration.Metadata
	Step        = rehydration.Step
	Delta       = rehydration.Delta
	Rehydrator  = rehydration.Rehydrator
	Applier     = rehydration.Applier
	Tool        = rehydration.Tool
	Dispatcher  = rehydration.Dispatcher
	State       = rehydration.State
	Trajectory  = rehydration.Trajectory
)

var (
	NewDispatcher        = rehydration.NewDispatcher
	Restore              = rehydration.Restore
	MigrateLegacyPayload = rehydration.MigrateLegacyPayload
	NewTrajectory        = rehydration.NewTrajectory
	Succeeded            = rehydration.Succeeded
	New                  = rehydration.New
	Enabled              = rehydration.Enabled
)
