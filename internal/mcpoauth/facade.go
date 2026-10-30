package mcpoauth

import (
	"github.com/justwasm/pigasus/internal/features/auth/mcpoauth"
)

type (
	HTTPStatusError = mcpoauth.HTTPStatusError
	HTTPError       = mcpoauth.HTTPError
	OAuthError      = mcpoauth.OAuthError
	Signin          = mcpoauth.Signin
	Store           = mcpoauth.Store
)

var (
	OAuthCode         = mcpoauth.OAuthCode
	HTTPStatus        = mcpoauth.HTTPStatus
	IsTransient       = mcpoauth.IsTransient
	IsRejection       = mcpoauth.IsRejection
	ShouldClear       = mcpoauth.ShouldClear
	StepUp            = mcpoauth.StepUp
	SanitizeServerURL = mcpoauth.SanitizeServerURL
	NewStore          = mcpoauth.NewStore
	NewStoreAt        = mcpoauth.NewStoreAt
	StorePath         = mcpoauth.StorePath
)
