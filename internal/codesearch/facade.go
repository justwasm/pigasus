// Package codesearch is a backward-compatible facade over internal/features/tools/codesearch.
package codesearch

import (
	"github.com/justwasm/pigasus/internal/features/tools/codesearch"
)

const (
	WidthInset     = codesearch.WidthInset
	FixedRows      = codesearch.FixedRows
	MaxHits        = codesearch.MaxHits
	HeaderOpen     = codesearch.HeaderOpen
	HeaderReset    = codesearch.HeaderReset
	ErrorOpen      = codesearch.ErrorOpen
	StateRunning   = codesearch.StateRunning
	StateDone      = codesearch.StateDone
	StateCancelled = codesearch.StateCancelled
	StateFailed    = codesearch.StateFailed
)

var (
	SpinnerFrames = codesearch.SpinnerFrames
	SkipDirs      = codesearch.SkipDirs
	ErrEmptyQuery = codesearch.ErrEmptyQuery
)

type (
	Hit    = codesearch.Hit
	Match  = codesearch.Match
	Search = codesearch.Search
	Args   = codesearch.Args
	Runner = codesearch.Runner
)

var (
	AppendFileHits = codesearch.AppendFileHits
	BlockRows      = codesearch.BlockRows
	ContentWidth   = codesearch.ContentWidth
	Row            = codesearch.Row
	Fit            = codesearch.Fit
	HasUpper       = codesearch.HasUpper
	CompileMatcher = codesearch.CompileMatcher
	Scan           = codesearch.Scan
	HitFromMatch   = codesearch.HitFromMatch
	StatusRow      = codesearch.StatusRow
	ListBudget     = codesearch.ListBudget
	HeaderRow      = codesearch.HeaderRow
	RuleRow        = codesearch.RuleRow
	Fill           = codesearch.Fill
	WindowStart    = codesearch.WindowStart
	Follow         = codesearch.Follow
	ParseArgs      = codesearch.ParseArgs
	Truncate       = codesearch.Truncate
	NewRunner      = codesearch.NewRunner
	ParseArgsFlat  = codesearch.ParseArgsFlat
)
