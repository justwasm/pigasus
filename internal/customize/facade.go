// Package customize is a backward-compatible facade over internal/app/prompt/customize.
package customize

import (
	appcustom "github.com/justwasm/pigasus/internal/app/prompt/customize"
)

type Budget = appcustom.Budget

var (
	DefaultBudget = appcustom.DefaultBudget
	NewBudget     = appcustom.NewBudget
)

type Priority = appcustom.Priority

const (
	PriorityCustomize = appcustom.PriorityCustomize
	PriorityConfig    = appcustom.PriorityConfig
	PriorityAgents    = appcustom.PriorityAgents
	PriorityHooks     = appcustom.PriorityHooks
	PriorityMCP       = appcustom.PriorityMCP
	PriorityPlugin    = appcustom.PriorityPlugin
	PrioritySkills    = appcustom.PrioritySkills
	PriorityCommands  = appcustom.PriorityCommands
)

var (
	PriorityFor  = appcustom.PriorityFor
	CommandNames = appcustom.CommandNames
)

var (
	RecordToken    = appcustom.RecordToken
	SnapshotTokens = appcustom.SnapshotTokens
	ResetRecorder  = appcustom.ResetRecorder
	TrimTokens     = appcustom.TrimTokens
)

type CustomizeSource = appcustom.CustomizeSource
type ConfigSource = appcustom.ConfigSource
type AgentsSource = appcustom.AgentsSource
type HooksSource = appcustom.HooksSource
type MCPSource = appcustom.MCPSource
type PluginSource = appcustom.PluginSource
type SkillsSource = appcustom.SkillsSource
type CommandsSource = appcustom.CommandsSource

var RegisterSources = appcustom.RegisterSources

type Token = appcustom.Token

var (
	EstimateTokens = appcustom.EstimateTokens
	NewToken       = appcustom.NewToken
)

type Source = appcustom.Source
type Trimmer = appcustom.Trimmer

var NewTrimmer = appcustom.NewTrimmer
