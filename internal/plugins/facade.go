// Package plugins is a backward-compatible facade over internal/features/plugins/core.
package plugins

import (
	core "github.com/justwasm/pigasus/internal/features/plugins/core"
)

type (
	AgentEntry  = core.AgentEntry
	AgentsJSON  = core.AgentsJSON
	Row         = core.Row
	Manifest    = core.Manifest
	Marketplace = core.Marketplace
)

var (
	WatcherPtr        = &core.WatcherPtr
	InstalledFallback = core.InstalledFallback

	ReadAgentsJSON        = core.ReadAgentsJSON
	NormalizeAgentsJSON   = core.NormalizeAgentsJSON
	AgentsMarkdown        = core.AgentsMarkdown
	Agents                = core.Agents
	AgentsAtPath          = core.AgentsAtPath
	UnpackedAgents        = core.UnpackedAgents
	MaybeConsumeStaleness = core.MaybeConsumeStaleness
	StateDir              = core.StateDir
	Dir                   = core.Dir
	ReadManifest          = core.ReadManifest
	InstalledRows         = core.InstalledRows
	FallbackDesc          = core.FallbackDesc
	Skills                = core.Skills
	MarketplaceRoot       = core.MarketplaceRoot
	Marketplaces          = core.Marketplaces
	MarketplaceItems      = core.MarketplaceItems
	MarketplaceOf         = core.MarketplaceOf
	MarketplaceDesc       = core.MarketplaceDesc
	MarketplaceVersion    = core.MarketplaceVersion
	DescFor               = core.DescFor
	InstalledNames        = core.InstalledNames
	Enabled               = core.Enabled
	LoadedDirs            = core.LoadedDirs
	SkillDirs             = core.SkillDirs
)
