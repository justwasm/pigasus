package accountrouting

import (
	"github.com/justwasm/pigasus/internal/features/accounts/routing"
)

const DebugEnvVar = routing.DebugEnvVar

type Router = routing.Router

var (
	NewRouter = routing.NewRouter
	Shared    = routing.Shared
	Policy    = routing.Policy
	Enabled   = routing.Enabled
	Wrap      = routing.Wrap
	Unwrap    = routing.Unwrap
)
