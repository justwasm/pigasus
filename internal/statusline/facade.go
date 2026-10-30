// Package statusline is a backward-compatible facade over internal/ui/views/statusline.
package statusline

import (
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)

const (
	ScriptTimeout          = statusline.ScriptTimeout
	ScriptInterval         = statusline.ScriptInterval
	MaxConsecutiveFailures = statusline.MaxConsecutiveFailures
	ScriptWaitDelay        = statusline.ScriptWaitDelay
	DisabledMessage        = statusline.DisabledMessage
)

type (
	Config        = statusline.Config
	Payload       = statusline.Payload
	ModelInfo     = statusline.ModelInfo
	Workspace     = statusline.Workspace
	ContextWindow = statusline.ContextWindow
	CurrentUsage  = statusline.CurrentUsage
	QuotaBucket   = statusline.QuotaBucket
	VCSInfo       = statusline.VCSInfo
	SandboxInfo   = statusline.SandboxInfo
	SubagentInfo  = statusline.SubagentInfo
	AgentInfo     = statusline.AgentInfo
	VimInfo       = statusline.VimInfo
	CostInfo      = statusline.CostInfo
	PricingRow    = statusline.PricingRow
	Result        = statusline.Result
	State         = statusline.State
)

var (
	PricingTable        = statusline.PricingTable
	LoadConfig          = statusline.LoadConfig
	LoadTitleConfig     = statusline.LoadTitleConfig
	PersistVisibility   = statusline.PersistVisibility
	PersistCommand      = statusline.PersistCommand
	ClearCommand        = statusline.ClearCommand
	PersistTitleEnabled = statusline.PersistTitleEnabled
	PricingForModel     = statusline.PricingForModel
	SplitCommand        = statusline.SplitCommand
	ExpandHome          = statusline.ExpandHome
	ShellCommand        = statusline.ShellCommand
	RunScript           = statusline.RunScript
	SanitizeTitle       = statusline.SanitizeTitle
	DetectVCS           = statusline.DetectVCS
)
