// rehydrate.go — the bridge from the stored trajectory to the
// rehydration pass.
//
// The rehydration package deliberately does not know about the store:
// it takes a reduced step shape and returns the state deltas. This
// file is the one place that knows both, so the store's step model
// stays out of the rehydration's contract.
package convstore

import "github.com/justwasm/pigasus/internal/rehydration"

// RehydrationSteps reduces the stored steps to the shape the
// rehydration pass reads.
//
// The stored step carries more than the pass needs — the text, the
// tool's result, the trajectory id — so the reduction keeps the step
// index, the step type, the tool name, the tool's arguments and the
// step's status.
//
// A step that is not a tool call contributes nothing: its type is not
// 132 and its tool name is empty, and the dispatcher skips both.
func (c *Conversation) RehydrationSteps() []rehydration.Step {
	if c == nil {
		return nil
	}
	out := make([]rehydration.Step, 0, len(c.Steps))
	for _, s := range c.Steps {
		out = append(out, rehydration.Step{
			Idx:      s.Idx,
			Type:     s.Type,
			ToolName: s.ToolName,
			ToolArgs: s.ToolArgs,
			Status:   s.Status,
		})
	}
	return out
}
