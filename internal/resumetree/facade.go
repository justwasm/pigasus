// Package resumetree is a backward-compatible facade over internal/app/session/tree.
package resumetree

import (
	"github.com/justwasm/pigasus/internal/app/session/tree"
)

const NoWorkspaceLabel = tree.NoWorkspaceLabel

type Item = tree.Item
type Group = tree.Group
type Summary = tree.Summary

var (
	URIToLocalPath        = tree.URIToLocalPath
	NormalizeWorkspaceURI = tree.NormalizeWorkspaceURI
	WorkspaceSegments     = tree.WorkspaceSegments
	IsGenericPathSegment  = tree.IsGenericPathSegment
	BaseWorkspaceLabel    = tree.BaseWorkspaceLabel
	JoinLabelSegments     = tree.JoinLabelSegments
	WorkspaceLabels       = tree.WorkspaceLabels
	ItemWorkspaceURI      = tree.ItemWorkspaceURI
	Build                 = tree.Build
	StoreDir              = tree.StoreDir
	LookupSummaries       = tree.LookupSummaries
	LookupCacheSummaries  = tree.LookupCacheSummaries
)
