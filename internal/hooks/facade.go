package hooks

import (
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
)

type (
	Event      = hooks.Event
	Decision   = hooks.Decision
	Payload    = hooks.Payload
	HookFunc   = hooks.HookFunc
	HookRecord = hooks.HookRecord
	Manager    = hooks.Manager
)

const (
	PreSideQuestion  = hooks.PreSideQuestion
	PostSideQuestion = hooks.PostSideQuestion

	DecisionAllow  = hooks.DecisionAllow
	DecisionBlock  = hooks.DecisionBlock
	DecisionModify = hooks.DecisionModify
)

var (
	KnownEvents  = hooks.KnownEvents
	IsKnown      = hooks.IsKnown
	SideQuestion = hooks.SideQuestion
	NewManager   = hooks.NewManager
)
