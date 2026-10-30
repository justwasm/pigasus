// Package diffaction is a backward-compatible facade over internal/ui/views/diff/action.
package diffaction

import (
	"github.com/justwasm/pigasus/internal/ui/views/diff/action"
)

type (
	Change      = action.Change
	DiffFunc    = action.DiffFunc
	GitHeadFunc = action.GitHeadFunc
	RowStyler   = action.RowStyler
)

var (
	Views           = action.Views
	RunGitEnvKVs    = action.RunGitEnvKVs
	VCSDiffFunc     = action.VCSDiffFunc
	GitHeadFuncSeam = action.GitHeadFuncSeam

	ParseGitStatusPorcelain = action.ParseGitStatusPorcelain
	ApplyGitNumstat         = action.ApplyGitNumstat
	GitChanges              = action.GitChanges
	CountFileLines          = action.CountFileLines
	Atoi                    = action.Atoi
	SortChanges             = action.SortChanges
	UnifiedDiff             = action.UnifiedDiff
	StripANSI               = action.StripANSI
	RenderDiffFooter        = action.RenderDiffFooter
	FilesPreamble           = action.FilesPreamble
	Feedback                = action.Feedback
	GitDiffHeadFeedback     = action.GitDiffHeadFeedback
	FirstChangedPath        = action.FirstChangedPath
	GitDiffBody             = action.GitDiffBody
	InlineStyle             = action.InlineStyle
	InlineLegend            = action.InlineLegend
	Counts                  = action.Counts
	TabRow                  = action.TabRow
	AllChangesRow           = action.AllChangesRow
	AllChangesLines         = action.AllChangesLines
	AllChangesLinesWindowed = action.AllChangesLinesWindowed
	NewRowStyler            = action.NewRowStyler
	MaxLineNumber           = action.MaxLineNumber
	RunGitEnv               = action.RunGitEnv
	RunGit                  = action.RunGit
	DetectVCS               = action.DetectVCS
	PathExists              = action.PathExists
	VCSDiff                 = action.VCSDiff
)

func DiffFeedback(workspace string, maxCells int) string {
	action.VCSDiffFunc = VCSDiffFunc
	action.GitHeadFuncSeam = GitHeadFuncSeam
	return action.Feedback(workspace, maxCells, VCSDiffFunc, GitHeadFuncSeam)
}
