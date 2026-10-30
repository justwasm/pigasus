// Package toolrunner is a backward-compatible facade over internal/features/tools/toolrunner.
package toolrunner

import (
	"github.com/justwasm/pigasus/internal/features/tools/toolrunner"
)

type (
	RegistryRunner   = toolrunner.RegistryRunner
	PermissionRunner = toolrunner.PermissionRunner
	Gate             = toolrunner.Gate
)

var (
	ToolDeclsFromRegistry   = toolrunner.ToolDeclsFromRegistry
	AdvertisedToolDecls     = toolrunner.AdvertisedToolDecls
	Decide                  = toolrunner.Decide
	SandboxDeniedToolResult = toolrunner.SandboxDeniedToolResult
	ToolTarget              = toolrunner.ToolTarget
)
