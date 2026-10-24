package rehydration

// Trajectory is a read-only view over a stored conversation.
//
// The reference's `cortex/rehydration` package wraps the trajectory in
// two views — a mutable `rehydratingTrajectory` and a read-only
// `rehydratingReadOnlyTrajectory` (【反编译】 LS 0x100f40940 and
// 0x100f40fa0) — and both expose the same index helpers over the
// steps: `StepIndicesByType` and `StepIndicesExcludingStatuses`.
//
// The clone's convstore already owns the stored steps, so the
// trajectory here is a thin view: it exists so the rehydration pass
// and its callers walk the steps the same way, and so the index
// helpers have one implementation.
//
// The reference's view carries more than the clone needs: the
// trajectory id, the cascade id, the source, the trajectory type, the
// parent references and the battle-mode infos. None of them is state
// an agent's world is derived from, so the clone's view does not
// carry them.
type Trajectory struct {
	steps []Step
}

// NewTrajectory returns a view over the given steps.
func NewTrajectory(steps []Step) *Trajectory {
	return &Trajectory{steps: steps}
}

// NumSteps returns the number of steps in the trajectory.
func (t *Trajectory) NumSteps() int {
	if t == nil {
		return 0
	}
	return len(t.steps)
}

// Step returns the step at index i.
func (t *Trajectory) Step(i int) (Step, bool) {
	if t == nil || i < 0 || i >= len(t.steps) {
		return Step{}, false
	}
	return t.steps[i], true
}

// Steps returns every step of the trajectory.
func (t *Trajectory) Steps() []Step {
	if t == nil {
		return nil
	}
	return t.steps
}

// StepsSlice returns the steps in [from, to). The bounds are clamped to
// the trajectory.
func (t *Trajectory) StepsSlice(from, to int) []Step {
	if t == nil {
		return nil
	}
	if from < 0 {
		from = 0
	}
	if to > len(t.steps) {
		to = len(t.steps)
	}
	if from >= to {
		return nil
	}
	return t.steps[from:to]
}

// StepIndicesByType returns the positions of the steps of the given
// type. A type of zero matches every step that has no type set.
func (t *Trajectory) StepIndicesByType(stepType int64) []int64 {
	if t == nil {
		return nil
	}
	var out []int64
	for _, s := range t.steps {
		if s.Type == stepType {
			out = append(out, s.Idx)
		}
	}
	return out
}

// StepIndicesExcludingStatuses returns the positions of the steps whose
// status is not in the excluded set.
//
// The reference's trajectory asks the store for this list
// (【反编译】 `dbtrajectory.(*sqliteStore).StepIndicesExcludingStatuses`,
// CLI 0x1011919a0) so the rehydration pass can walk the steps that
// actually ran.
func (t *Trajectory) StepIndicesExcludingStatuses(statuses ...int64) []int64 {
	if t == nil {
		return nil
	}
	excluded := make(map[int64]bool, len(statuses))
	for _, s := range statuses {
		excluded[s] = true
	}
	var out []int64
	for _, s := range t.steps {
		if !excluded[s.Status] {
			out = append(out, s.Idx)
		}
	}
	return out
}

// ToolCallSteps returns the steps that are tool calls.
func (t *Trajectory) ToolCallSteps() []Step {
	if t == nil {
		return nil
	}
	var out []Step
	for _, s := range t.steps {
		if s.Type == StepToolCall || (s.Type == 0 && s.ToolName != "") {
			out = append(out, s)
		}
	}
	return out
}
