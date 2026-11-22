// validate.go — the reference's checkpoint-config validator.
//
// 【反编译】 cortex/utils.ValidateCheckpointConfigForModelInfo
// (0x1012aabd0) checks a checkpoint configuration against the models it
// names and reports four errors, all of them in the binary's string
// table. The clone keeps them byte for byte: they are the messages the
// reference prints when a checkpoint payload disagrees with the models it
// is about to summarise.
//
// The companion clamps live here too: cortex/utils.GetCheckpointConfig
// (0x1012aa4d0) is what applies them before the validator ever sees the
// configuration.
package modelctx

import (
	"errors"
	"fmt"
)

// The reference's own error messages. 【strings】 — every one of them is a
// literal in agy 1.4.8's string table.
//
//nolint:gochecknoglobals // the reference's message set, one per failure
var (
	// ErrPlannerModelInfoNil is the reference's message for a missing
	// planner model (30 bytes, 0x10359e069).
	ErrPlannerModelInfoNil = errors.New("plannerModelInfo cannot be nil")
)

// The ceiling the reference clamps a checkpoint configuration to.
//
// 【反编译】 cortex/utils.GetCheckpointConfig clamps max_token_limit to at
// most 256000 and max_output_tokens to at most 16384; an output cap below
// one becomes 16384.
const (
	// MaxCheckpointTokenLimit is the ceiling of max_token_limit.
	MaxCheckpointTokenLimit = 256_000
	// MaxCheckpointOutputTokens is the ceiling of max_output_tokens.
	MaxCheckpointOutputTokens = 16_384
)

// The band a checkpoint's max_overhead_ratio must fall in.
//
// 【反编译】 the check is `if (0.0 < ratio && ratio < 0.5) return nil` —
// both bounds are exclusive, and the upper bound is the f64 constant 0.5
// (0x103de3fa0).
const (
	// MinOverheadRatio is the exclusive lower bound.
	MinOverheadRatio = 0.0
	// MaxOverheadRatio is the exclusive upper bound.
	MaxOverheadRatio = 0.5
)

// MillionTokenModelID is the model id that gets a 1 000 000-token window
// whatever the payload said.
//
// 【反编译】 cortex/utils.GetCheckpointConfig returns a config with
// max_token_limit = 1000000 and enabled = true for model id 0x143 (323),
// ignoring everything else.
const MillionTokenModelID = 323

// MillionTokenLimit is the window that model id gets.
const MillionTokenLimit = 1_000_000

// ModelInfo is the model half of the validator's input: the model's own
// context window limit and its own output cap.
type ModelInfo struct {
	// ContextWindowLimit is the model's input context limit.
	ContextWindowLimit int
	// MaxOutputTokens is the model's own output cap.
	MaxOutputTokens int
}

// Config is the checkpoint configuration the validator checks.
//
// It is the slice of exa.cortex_pb.CheckpointConfig (31 fields) the
// validator and the clamps actually read.
//
// Enabled mirrors the proto's `*bool`: the reference distinguishes an
// unset switch from an explicit false, and `shouldCheckpoint` treats
// "unset" as "not explicitly disabled".
type Config struct {
	// MaxTokenLimit is the checkpoint's own context limit.
	MaxTokenLimit uint32
	// MaxOutputTokens is the output cap the checkpoint holds back.
	MaxOutputTokens uint32
	// MaxOverheadRatio is the overhead budget: the share of the window
	// the checkpoint may spend on its own overhead.
	MaxOverheadRatio float32
	// Enabled is the checkpoint switch.
	Enabled bool
}

// Validate checks a checkpoint configuration against the models it names.
//
// planner is the model the session runs on: its context window limit and
// its own output cap bound the checkpoint's limit. checkpoint is the model
// the checkpoint summarises with (the checkpoint_model knob): its output
// cap bounds the checkpoint's.
//
// 【反编译】 the four messages are the reference's own, byte for byte. The
// order of the checks is the order the strings sit in the binary's string
// table — 【推测】 the decompilation does not pin it.
func Validate(cfg Config, planner, checkpoint *ModelInfo) error {
	if planner == nil {
		return ErrPlannerModelInfoNil
	}
	if checkpoint == nil {
		// The reference's only nil check names the planner model, so a
		// missing checkpoint model falls back to the planner's own
		// bounds rather than inventing a second message.
		checkpoint = planner
	}
	if limit := planner.ContextWindowLimit - planner.MaxOutputTokens; int(cfg.MaxTokenLimit) > limit {
		return fmt.Errorf(
			"config.MaxTokenLimit (%d) cannot exceed the planner model's context window limit (%d) minus the planner model's MaxOutputTokens (%d)",
			cfg.MaxTokenLimit, planner.ContextWindowLimit, planner.MaxOutputTokens)
	}
	if int(cfg.MaxOutputTokens) > checkpoint.MaxOutputTokens {
		return fmt.Errorf(
			"config.MaxOutputTokens %d is greater than checkpointModelInfo.MaxOutputTokens %d",
			cfg.MaxOutputTokens, checkpoint.MaxOutputTokens)
	}
	if !(cfg.MaxOverheadRatio > MinOverheadRatio && cfg.MaxOverheadRatio < MaxOverheadRatio) {
		return fmt.Errorf("overheadRatio %f is not between 0 and %f",
			cfg.MaxOverheadRatio, MaxOverheadRatio)
	}
	return nil
}

// Clamp applies the reference's clamps to a checkpoint configuration.
//
// 【反编译】 cortex/utils.GetCheckpointConfig: max_token_limit is capped
// at 256000 and max_output_tokens at 16384, and an output cap below one
// becomes 16384.
func Clamp(cfg Config) Config {
	if cfg.MaxTokenLimit > MaxCheckpointTokenLimit {
		cfg.MaxTokenLimit = MaxCheckpointTokenLimit
	}
	if cfg.MaxOutputTokens > MaxCheckpointOutputTokens {
		cfg.MaxOutputTokens = MaxCheckpointOutputTokens
	}
	if cfg.MaxOutputTokens < 1 {
		cfg.MaxOutputTokens = MaxCheckpointOutputTokens
	}
	return cfg
}

// ClampForModel applies the clamps and the one model-specific exception.
//
// 【反编译】 model id 323 (0x143) gets a 1 000 000-token window and an
// enabled checkpoint whatever the payload said; every other model gets
// the clamped configuration.
func ClampForModel(cfg Config, modelID int) Config {
	if modelID == MillionTokenModelID {
		cfg.MaxTokenLimit = MillionTokenLimit
		cfg.Enabled = true
		return cfg
	}
	return Clamp(cfg)
}
