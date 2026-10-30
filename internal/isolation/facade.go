// Package isolation is a backward-compatible facade over internal/features/tools/isolation.
package isolation

import (
	featureiso "github.com/justwasm/pigasus/internal/features/tools/isolation"
)

type Workspace = featureiso.Workspace
type FeedbackFn = featureiso.FeedbackFn

var (
	EnvKeys        = featureiso.EnvKeys
	New            = featureiso.New
	EnvSnapshot    = featureiso.EnvSnapshot
	BranchFeedback = featureiso.BranchFeedback
	ShareFeedback  = featureiso.ShareFeedback
)
