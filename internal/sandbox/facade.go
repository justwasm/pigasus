// Package sandbox is a backward-compatible facade over internal/features/tools/sandbox.
package sandbox

import (
	"github.com/justwasm/pigasus/internal/features/tools/sandbox"
)

type (
	Sandbox       = sandbox.Sandbox
	CommandResult = sandbox.CommandResult
)

var Resolve = sandbox.Resolve
