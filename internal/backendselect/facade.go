// Package backendselect is a backward-compatible facade over internal/infra/backend/select.
package backendselect

import (
	bselect "github.com/justwasm/pigasus/internal/infra/backend/select"
)

type CatalogueIDFunc = bselect.CatalogueIDFunc

var (
	Pick             = bselect.Pick
	PickWithLog      = bselect.PickWithLog
	IsReferenceModel = bselect.IsReferenceModel
	Resolve          = bselect.Resolve
	Normalise        = bselect.Normalise
	HasOAuthToken    = bselect.HasOAuthToken
	AuthHome         = bselect.AuthHome
)
