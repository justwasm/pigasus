// Package atfiles is a backward-compatible facade over internal/app/prompt/mentions/files.
package atfiles

import (
	"github.com/justwasm/pigasus/internal/app/prompt/mentions/files"
)

const (
	MaxPrefixLen   = files.MaxPrefixLen
	MaxCandidates  = files.MaxCandidates
	MaxWalkEntries = files.MaxWalkEntries
)

type (
	Mention  = files.Mention
	Entry    = files.Entry
	Dropdown = files.Dropdown
)

var (
	Parse         = files.Parse
	List          = files.List
	FindEntries   = files.FindEntries
	FilterEntries = files.FilterEntries
	RenderRow     = files.RenderRow
	HintRow       = files.HintRow
)
