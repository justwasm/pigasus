// overflow.go — the reference's context-overflow error.
//
// 【strings】 agy 1.4.8 carries the message at file offset 0x36c73e2
// (vmaddr 0x1036c73e2), 438 bytes — the only copy in the binary. The
// clone's copy of it until this cycle was the 1.4.5 revision, which
// stopped after the "Most recent step" line; 1.4.7 added the
// "Consider reducing …" tail and 1.4.8 leaves the bytes unchanged.
//
// The reference prints it when the trajectory's chat conversion cannot fit
// the most recent step into the effective context limit. The clone's
// compaction is PiG's, so it does not produce this error yet — the type
// is here so the wording and the arithmetic stay pinned while the
// cascade grows.
package modelctx

import (
	"errors"
	"fmt"
)

// OverflowFormat is the reference's overflow message, byte for byte.
//
// The leading %w is the cause the reference wraps; the numbers are the
// arithmetic the message spells out.
const OverflowFormat = "%w; the most recent step (%s) could not fit in the available context limit:\n" +
	"  Total attempted tokens: ~%d (exceeding cascade_config.checkpoint_config.max_token_limit (%d))\n" +
	"    - Base context: ~%d tokens (instructions: ~%d, tool definitions: ~%d)\n" +
	"    - Most recent step: ~%d tokens (text: %d, image: %d)\n" +
	"\n" +
	"Consider reducing the size of the system instructions or tool definitions, " +
	"or adjusting tool implementations to return smaller outputs"

// Overflow is the reference's context-overflow error: the most recent step
// of a trajectory did not fit into the effective context limit.
type Overflow struct {
	// Err is the cause the reference wraps (the %w).
	Err error
	// Step names the step that did not fit.
	Step string
	// Attempted is the total token count the conversion tried.
	Attempted int
	// Limit is the effective context limit — the checkpoint's
	// max_token_limit.
	Limit int
	// Base is the token count of the context the step was added to.
	Base int
	// Instructions is the token count of the system instructions.
	Instructions int
	// ToolDefinitions is the token count of the tool declarations.
	ToolDefinitions int
	// StepTokens is the token count of the step that did not fit.
	StepTokens int
	// TextTokens is the step's text token count.
	TextTokens int
	// ImageTokens is the step's image token count.
	ImageTokens int
}

// Error renders the reference's message.
//
// The reference builds it with fmt.Errorf, so its leading %w is the cause
// and the cause's text leads the sentence. The clone renders the same
// bytes through the same verb.
func (o *Overflow) Error() string {
	cause := o.Err
	if cause == nil {
		cause = errNoCause
	}
	return fmt.Errorf(OverflowFormat,
		cause, o.Step, o.Attempted, o.Limit,
		o.Base, o.Instructions, o.ToolDefinitions,
		o.StepTokens, o.TextTokens, o.ImageTokens).Error()
}

// errNoCause stands in for a missing cause: the reference always wraps
// one, and an empty error renders the same empty slot.
var errNoCause = errors.New("")

// Unwrap exposes the cause so errors.Is and errors.As reach it.
func (o *Overflow) Unwrap() error { return o.Err }
