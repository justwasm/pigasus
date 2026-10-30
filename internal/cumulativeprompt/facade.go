// Package cumulativeprompt is a backward-compatible facade over internal/app/prompt/cumulative.
package cumulativeprompt

import (
	"github.com/justwasm/pigasus/internal/app/prompt/cumulative"
)

const RoleUser = cumulative.RoleUser

type (
	Message = cumulative.Message
	Options = cumulative.Options
)

var Convert = cumulative.Convert
