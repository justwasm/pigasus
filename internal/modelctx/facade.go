package modelctx

import "github.com/justwasm/pigasus/internal/features/models/ctx"

const (
	DefaultContextWindow      = ctx.DefaultContextWindow
	DefaultReserveTokens      = ctx.DefaultReserveTokens
	OverflowFormat            = ctx.OverflowFormat
	CheckpointVariant         = ctx.CheckpointVariant
	StrategyUnspecified       = ctx.StrategyUnspecified
	StrategySameModel         = ctx.StrategySameModel
	StrategySinglePrompt      = ctx.StrategySinglePrompt
	MaxCheckpointTokenLimit   = ctx.MaxCheckpointTokenLimit
	MaxCheckpointOutputTokens = ctx.MaxCheckpointOutputTokens
	MinOverheadRatio          = ctx.MinOverheadRatio
	MaxOverheadRatio          = ctx.MaxOverheadRatio
	MillionTokenModelID       = ctx.MillionTokenModelID
	MillionTokenLimit         = ctx.MillionTokenLimit
)

var (
	ErrPlannerModelInfoNil = ctx.ErrPlannerModelInfoNil
)

type (
	Limit     = ctx.Limit
	Overflow  = ctx.Overflow
	Profile   = ctx.Profile
	ModelInfo = ctx.ModelInfo
	Config    = ctx.Config
)

var (
	For              = ctx.For
	ContextWindowFor = ctx.ContextWindowFor
	ProfileFor       = ctx.ProfileFor
	Validate         = ctx.Validate
	Clamp            = ctx.Clamp
	ClampForModel    = ctx.ClampForModel
)
