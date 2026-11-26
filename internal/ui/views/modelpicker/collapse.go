// collapse.go — the Orchestrator's single-agent collapse decision.
//
// The reference collapses the composite Orchestrator row to a plain
// single-agent run when Planner and Executor resolve to the *same* model at
// the *same* thinking effort (agy 1.5.17: "picking the same model and
// thinking Effort for both Planner and Executor runs single-agent directly
// on that model"). Distinct effort levels of the same family keep the
// two-phase plan-and-execute loop.
//
// The decision is a pure function over the four inputs so it can be
// table-tested without a panel, a catalogue or settings.json. The panel only
// reads it: it owns no decision of its own here.

package modelpicker

import (
	"strings"

	"github.com/justwasm/pigasus/internal/features/models"
)

// SelectionMode names how an Orchestrator selection runs.
type SelectionMode string

const (
	// ModeSingleAgent is a single agent running on the collapsed model.
	ModeSingleAgent SelectionMode = "single-agent"
	// ModePlanExecute keeps the two-phase plan-and-execute loop.
	ModePlanExecute SelectionMode = "plan-and-execute"
)

// CollapseReason is the *why* behind a SelectionCollapse, kept so callers
// (and tests) can tell an unset selection from a genuinely distinct one.
type CollapseReason string

const (
	// ReasonSameModelSameEffort collapses: one model, one effort.
	ReasonSameModelSameEffort CollapseReason = "same-model-same-effort"
	// ReasonDifferentModel keeps two-phase: Planner and Executor differ.
	ReasonDifferentModel CollapseReason = "different-model"
	// ReasonDifferentEffort keeps two-phase: same family, distinct effort.
	ReasonDifferentEffort CollapseReason = "different-effort"
	// ReasonUnsetSelection keeps two-phase: a role has no model yet.
	ReasonUnsetSelection CollapseReason = "unset-selection"
)

// SelectionCollapse is the outcome of comparing the Planner and Executor
// selections.
//
// SingleAgent is the answer; Model is the model to run when it is true (the
// catalogue slug, e.g. "gemini-4-carbon-high"); BaseID and Effort are the
// split halves so a caller can hand them to the status line, /btw or
// /context without re-parsing the slug.
type SelectionCollapse struct {
	SingleAgent bool
	Mode        SelectionMode
	Reason      CollapseReason
	BaseID      string
	Effort      string
	Model       string
}

// CollapseSelection decides whether a Planner/Executor selection is
// effectively single-agent.
//
// The four inputs are what the Orchestrator row persists: a family base id
// (or a full "<base>-<effort>" slug) and an effort string per role. A model
// that already carries its effort suffix is split first, and an explicit
// effort argument wins over the suffix. Comparison is case-insensitive and
// whitespace-trimmed; an unset model on either side never collapses, because
// there is no model to run.
func CollapseSelection(plannerModel, plannerEffort, executorModel, executorEffort string) SelectionCollapse {
	pBase, pEffort := splitModelEffort(plannerModel, plannerEffort)
	eBase, eEffort := splitModelEffort(executorModel, executorEffort)

	// An unset role has nothing to run single-agent on: the reference keeps
	// the default plan-and-execute shape until both roles resolve.
	if pBase == "" || eBase == "" {
		return SelectionCollapse{
			Mode:   ModePlanExecute,
			Reason: ReasonUnsetSelection,
		}
	}

	if pBase != eBase {
		return SelectionCollapse{
			Mode:   ModePlanExecute,
			Reason: ReasonDifferentModel,
			BaseID: pBase,
			Effort: pEffort,
			Model:  joinModelEffort(pBase, pEffort),
		}
	}

	// Same family: only an identical effort collapses. "high" vs "xhigh" on
	// the same base is two-phase plan-and-execute.
	if !strings.EqualFold(pEffort, eEffort) {
		return SelectionCollapse{
			Mode:   ModePlanExecute,
			Reason: ReasonDifferentEffort,
			BaseID: pBase,
			Effort: pEffort,
			Model:  joinModelEffort(pBase, pEffort),
		}
	}

	return SelectionCollapse{
		SingleAgent: true,
		Mode:        ModeSingleAgent,
		Reason:      ReasonSameModelSameEffort,
		BaseID:      pBase,
		Effort:      pEffort,
		Model:       joinModelEffort(pBase, pEffort),
	}
}

// splitModelEffort normalises one role's (model, effort) pair.
//
// effortLevelTokens holds the trailing slug tokens that are effort levels
// rather than part of a family name, so "gemini-4-carbon-high" splits into
// ("gemini-4-carbon", "high") while "gemini-4-carbon" stays whole.
func splitModelEffort(model, effort string) (base, lvl string) {
	base = strings.ToLower(strings.TrimSpace(model))
	lvl = strings.ToLower(strings.TrimSpace(effort))
	if base == "" {
		return "", lvl
	}
	if lvl != "" {
		return base, lvl
	}
	for _, tok := range effortLevelTokens() {
		suffix := "-" + tok
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix), tok
		}
	}
	return base, ""
}

// effortLevelTokens is the catalogue's effort vocabulary plus the extended
// levels the Orchestrator families offer (xhigh, max).
func effortLevelTokens() []string {
	tokens := make([]string, 0, len(models.EffortLevels)+2)
	seen := make(map[string]bool, len(models.EffortLevels)+2)
	add := func(t string) {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			return
		}
		seen[t] = true
		tokens = append(tokens, t)
	}
	for _, t := range models.EffortLevels {
		add(t)
	}
	add("xhigh")
	add("max")
	return tokens
}

// joinModelEffort rebuilds the catalogue slug from its halves.
func joinModelEffort(base, effort string) string {
	if base == "" {
		return ""
	}
	if effort == "" {
		return base
	}
	return base + "-" + effort
}

// Collapse reports whether this row state's current selection is effectively
// single-agent, and on which model.
func (s *OrchestratorRowState) Collapse() SelectionCollapse {
	if s == nil {
		return CollapseSelection("", "", "", "")
	}
	return CollapseSelection(
		s.Planner.CurrentFamily().BaseID, s.Planner.CurrentEffort(),
		s.Executor.CurrentFamily().BaseID, s.Executor.CurrentEffort(),
	)
}

// OrchestratorCollapse reports the collapse decision for a row that carries
// composite Orchestrator state; ok is false for every other row.
func (r Row) OrchestratorCollapse() (SelectionCollapse, bool) {
	if !r.HasOrchestrator() {
		return SelectionCollapse{}, false
	}
	return r.Orchestrator.Collapse(), true
}

// OrchestratorCollapse reports whether the selection under the cursor is
// effectively single-agent, and on which model.
//
// The live interaction state wins over the row's snapshot, so a caller that
// asks after a ←/→ or Tab sees the current selection rather than the one the
// panel opened with.
func (p *Panel) OrchestratorCollapse() (SelectionCollapse, bool) {
	if p == nil {
		return SelectionCollapse{}, false
	}
	row, ok := p.currentRow()
	if !ok || !row.HasOrchestrator() {
		return SelectionCollapse{}, false
	}
	if p.orchestrator != nil {
		return p.orchestrator.Collapse(), true
	}
	return row.Orchestrator.Collapse(), true
}

// IsSingleAgentSelection answers the caller's question in one call: true when
// the cursor sits on the Orchestrator row and its Planner/Executor selection
// collapses to a single agent.
func (p *Panel) IsSingleAgentSelection() bool {
	d, ok := p.OrchestratorCollapse()
	return ok && d.SingleAgent
}

// renderCollapseNote is the in-panel echo of a collapsed selection: the
// /model footer says what the row will actually run.
func renderCollapseNote(d SelectionCollapse) string {
	if !d.SingleAgent {
		return ""
	}
	name := d.BaseID
	if d.Effort != "" {
		name += " · " + d.Effort
	}
	return "  \x1b[2mSingle agent · " + name + "\x1b[0m"
}
