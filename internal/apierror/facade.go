// Package apierror is a backward-compatible facade over internal/ui/headless/apierror.
package apierror

import (
	"github.com/justwasm/pigasus/internal/ui/headless/apierror"
)

const HeadlessAPIExitCode = apierror.HeadlessAPIExitCode

type APIError = apierror.APIError

var (
	ClassifyAPIError = apierror.ClassifyAPIError
	NewErrorID       = apierror.NewErrorID
)
