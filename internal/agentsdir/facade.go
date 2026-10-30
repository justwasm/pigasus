// Package agentsdir is a backward-compatible facade over internal/app/prompt/agentsdir.
package agentsdir

import (
	appagents "github.com/justwasm/pigasus/internal/app/prompt/agentsdir"
)

var DefaultDirNames = appagents.DefaultDirNames

type Entry = appagents.Entry

var (
	Walk              = appagents.Walk
	Discover          = appagents.Discover
	FirstProjectLocal = appagents.FirstProjectLocal
	ErrEmptyCWD       = appagents.ErrEmptyCWD
)

type Manifest = appagents.Manifest

var (
	LoadManifest       = appagents.LoadManifest
	LoadAllManifests   = appagents.LoadAllManifests
	MergeManifestBytes = appagents.MergeManifestBytes
)

type AgentsJSONEntry = appagents.AgentsJSONEntry
type AgentsJSON = appagents.AgentsJSON

var ParseAgentsJSON = appagents.ParseAgentsJSON
