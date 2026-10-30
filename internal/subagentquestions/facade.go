// Package subagentquestions is a backward-compatible facade over internal/app/subagent/questions.
package subagentquestions

import (
	"github.com/justwasm/pigasus/internal/app/subagent/questions"
)

const (
	ParentKey     = questions.ParentKey
	SkipAnswer    = questions.SkipAnswer
	StatePending  = questions.StatePending
	StateAnswered = questions.StateAnswered
	StateSkipped  = questions.StateSkipped
)

type Question = questions.Question
type Manager = questions.Manager

var NewManager = questions.NewManager
