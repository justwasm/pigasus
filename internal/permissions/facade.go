// Package permissions is a backward-compatible facade over
// internal/core/policy/permissions.
package permissions

import (
	core "github.com/justwasm/pigasus/internal/core/policy/permissions"
)

// Modes accepted by the global --mode flag.
const (
	ModeAcceptEdits = core.ModeAcceptEdits
	ModeAuto        = core.ModeAuto
	ModePlan        = core.ModePlan
)

var (
	Modes    = core.Modes
	ModeList = core.ModeList
	ValidMode = core.ValidMode
)

// Verdict is the decision for one tool call.
type Verdict = core.Verdict

const (
	Allow                = core.Allow
	Deny                 = core.Deny
	Ask                  = core.Ask
	ForceAsk             = core.ForceAsk
	DenyUnlessPriorGrant = core.DenyUnlessPriorGrant
)

type Class = core.Class

const (
	ClassRead    = core.ClassRead
	ClassEdit    = core.ClassEdit
	ClassCommand = core.ClassCommand
	ClassOther   = core.ClassOther
)

var Classify = core.Classify

type Rule = core.Rule
type Request = core.Request

var SuggestAllowAlwaysPattern = core.SuggestAllowAlwaysPattern

type Policy = core.Policy

var (
	StrictFromEnv        = core.StrictFromEnv
	HeadlessNotice       = core.HeadlessNotice
	HeadlessDenialNotice = core.HeadlessDenialNotice
	DeniedToolResult     = core.DeniedToolResult
)

const (
	SkipPermissionsNotice   = core.SkipPermissionsNotice
	CycleHint               = core.CycleHint
	AcceptEditsBanner       = core.AcceptEditsBanner
	AutoBanner              = core.AutoBanner
	PlanBanner              = core.PlanBanner
	NoToolCallNudge         = core.NoToolCallNudge
	NoToolCallNudgeHeadless = core.NoToolCallNudgeHeadless
)

var (
	CycleModes = core.CycleModes
	NextMode   = core.NextMode
	ModeBanner = core.ModeBanner
)

// Sandbox
const DefaultSandboxProfilePath = core.DefaultSandboxProfilePath

var DefaultDenyPaths = core.DefaultDenyPaths

type SeatbeltProfile = core.SeatbeltProfile

var LoadUserSandboxProfile = core.LoadUserSandboxProfile

// Settings
type Settings = core.Settings

var LoadRules = core.LoadRules

// Panel Rules
const (
	PermScopeProject = core.PermScopeProject
	PermScopeShared  = core.PermScopeShared
	PermScopeGlobal  = core.PermScopeGlobal
)

var (
	PermScopeNames        = core.PermScopeNames
	PermListNames         = core.PermListNames
	PermListKeys          = core.PermListKeys
	ReadSettingsPermList  = core.ReadSettingsPermList
	AnyStringSlice        = core.AnyStringSlice
	ProjectConfigPath     = core.ProjectConfigPath
	ReadProjectPermList   = core.ReadProjectPermList
	WriteProjectPermList  = core.WriteProjectPermList
	ValidateGrant         = core.ValidateGrant
	ValidGrantAction      = core.ValidGrantAction
	PermTabRow            = core.PermTabRow
)
