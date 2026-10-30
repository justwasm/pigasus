// Package subagent is a backward-compatible facade over internal/app/subagent/core.
package subagent

import (
	"github.com/justwasm/pigasus/internal/app/subagent/core"
)

type (
	StateChangeFunc = core.StateChangeFunc
	Subagent        = core.Subagent
	Manager         = core.Manager
	OnStopFunc      = core.OnStopFunc
)

var (
	NewSubagent        = core.NewSubagent
	ExploreSubagent    = core.ExploreSubagent
	CodeReviewSubagent = core.CodeReviewSubagent
	TestSubagent       = core.TestSubagent
	BuildSubagent      = core.BuildSubagent
	DeploySubagent     = core.DeploySubagent
	NewManager         = core.NewManager
)
