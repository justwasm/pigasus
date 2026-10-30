// Package modelcatalog is a backward-compatible facade over internal/features/models/catalog.
package modelcatalog

import (
	featurecatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
)

const DefaultModel = featurecatalog.DefaultModel

type BaseModelSpec = featurecatalog.BaseModelSpec

var (
	BaseModelEfforts       = featurecatalog.BaseModelEfforts
	ValidEfforts           = featurecatalog.ValidEfforts
	FormatModelLabel       = featurecatalog.FormatModelLabel
	ResolveEffectiveModel  = featurecatalog.ResolveEffectiveModel
	CanonicalModelLabel    = featurecatalog.CanonicalModelLabel
	ModelAvailable         = featurecatalog.ModelAvailable
	ResolveDefaultEffort   = featurecatalog.ResolveDefaultEffort
	CatalogueIDBySlug      = featurecatalog.CatalogueIDBySlug
	CatalogueID            = featurecatalog.CatalogueID
	CatalogueLabel         = featurecatalog.CatalogueLabel
	AvailableModelsBlock   = featurecatalog.AvailableModelsBlock
	ResolveModelSelection  = featurecatalog.ResolveModelSelection
	ParseEffortWord        = featurecatalog.ParseEffortWord
	ModelBaseSlug          = featurecatalog.ModelBaseSlug
	ModelEffortFromID      = featurecatalog.ModelEffortFromID
	ModelSlug              = featurecatalog.ModelSlug
	WireModel              = featurecatalog.WireModel
	WireLabel              = featurecatalog.WireLabel
)
