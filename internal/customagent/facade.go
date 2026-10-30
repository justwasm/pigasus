// Package customagent is a backward-compatible facade over internal/app/prompt/customagent.
package customagent

import (
	appagent "github.com/justwasm/pigasus/internal/app/prompt/customagent"
)

type Agent = appagent.Agent
type ParseError = appagent.ParseError

var (
	ErrEmpty       = appagent.ErrEmpty
	LoadAgent      = appagent.LoadAgent
	LoadAgentDir   = appagent.LoadAgentDir
	ResolvedAgents = appagent.ResolvedAgents
)
