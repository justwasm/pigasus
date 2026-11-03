// batch.go — the step model's own batching rule.
//
// The model emits its tool calls in batches: the calls of one
// assistant message belong to one batch, and Step.Batch numbers them.
// The transcript groups those calls into one block; this file owns the
// question "which of these steps belong to that batch".
//
// A thought belongs to no batch: the reasoning the model streamed
// between two calls is not a call of the batch.
//
// Stdlib-only. No dependency on the TUI host.
package toolsteps

// BatchSteps returns the steps that belong to one batch, in the order
// they were given.
//
// A thought belongs to no batch, so it is left out.
func BatchSteps(steps []Step, batch int) []Step {
	var out []Step
	for _, s := range steps {
		if s.Kind == KindThought {
			continue
		}
		if s.Batch != batch {
			continue
		}
		out = append(out, s)
	}
	return out
}
