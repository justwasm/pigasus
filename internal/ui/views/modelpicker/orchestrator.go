package modelpicker

import (
	"strings"

	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/palette"
)

// OrchestratorStop identifies which control inside the Orchestrator composite
// row currently has keyboard focus.
type OrchestratorStop int

const (
	StopPlanner OrchestratorStop = iota
	StopPlannerEffort
	StopExecutor
	StopExecutorEffort
)

// OrchestratorRoleUI holds the interactive family and effort indices for one
// Orchestrator role (Planner or Executor).
type OrchestratorRoleUI struct {
	Families  []catalog.OrchestratorFamily
	FamilyIdx int
	EffortIdx int
}

// OrchestratorRowState holds the interactive state for the Orchestrator
// composite row in /model.
type OrchestratorRowState struct {
	Planner  OrchestratorRoleUI
	Executor OrchestratorRoleUI
	Focus    OrchestratorStop
}

// NewOrchestratorRowState builds interactive UI state from a resolved
// OrchestratorConfig without mutating settings.json.
func NewOrchestratorRowState(cfg catalog.OrchestratorConfig) *OrchestratorRowState {
	return &OrchestratorRowState{
		Planner:  newOrchestratorRoleUI(cfg.Planner),
		Executor: newOrchestratorRoleUI(cfg.Executor),
		Focus:    StopPlanner,
	}
}

func newOrchestratorRoleUI(role catalog.OrchestratorRoleState) OrchestratorRoleUI {
	famIdx := 0
	for i, f := range role.Families {
		if strings.EqualFold(f.BaseID, role.BaseID) {
			famIdx = i
			break
		}
	}
	effIdx := 0
	if famIdx >= 0 && famIdx < len(role.Families) {
		efforts := role.Families[famIdx].Efforts
		want := role.Effort
		if want == "" {
			want = catalog.DefaultOrchestratorEffort(efforts)
		}
		for i, e := range efforts {
			if strings.EqualFold(e, want) {
				effIdx = i
				break
			}
		}
	}
	return OrchestratorRoleUI{
		Families:  role.Families,
		FamilyIdx: famIdx,
		EffortIdx: effIdx,
	}
}

// Clone returns a shallow copy of the OrchestratorRowState.
func (s *OrchestratorRowState) Clone() *OrchestratorRowState {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

// CurrentFamily returns the currently selected family for the role.
func (r OrchestratorRoleUI) CurrentFamily() catalog.OrchestratorFamily {
	if len(r.Families) == 0 {
		return catalog.OrchestratorFamily{}
	}
	idx := r.FamilyIdx
	if idx < 0 || idx >= len(r.Families) {
		idx = 0
	}
	return r.Families[idx]
}

// CurrentEffort returns the currently selected effort for the role.
func (r OrchestratorRoleUI) CurrentEffort() string {
	fam := r.CurrentFamily()
	if len(fam.Efforts) == 0 {
		return ""
	}
	idx := r.EffortIdx
	if idx < 0 || idx >= len(fam.Efforts) {
		return catalog.DefaultOrchestratorEffort(fam.Efforts)
	}
	return fam.Efforts[idx]
}

// CurrentSlug returns the full model slug (e.g. "gemini-4-carbon-high").
func (r OrchestratorRoleUI) CurrentSlug() string {
	fam := r.CurrentFamily()
	if fam.BaseID == "" {
		return ""
	}
	eff := r.CurrentEffort()
	if eff == "" {
		return fam.BaseID
	}
	return fam.BaseID + "-" + eff
}

// StepFamily cycles the selected family by delta and resets effort to the
// role's default ("high" or highest offered if "high" is unavailable).
func (r *OrchestratorRoleUI) StepFamily(delta int) {
	if len(r.Families) == 0 {
		return
	}
	r.FamilyIdx = (r.FamilyIdx + delta + len(r.Families)) % len(r.Families)
	fam := r.CurrentFamily()
	def := catalog.DefaultOrchestratorEffort(fam.Efforts)
	r.EffortIdx = 0
	for i, e := range fam.Efforts {
		if strings.EqualFold(e, def) {
			r.EffortIdx = i
			break
		}
	}
}

// StepEffort cycles the selected effort within the current family's offered
// effort levels.
func (r *OrchestratorRoleUI) StepEffort(delta int) {
	fam := r.CurrentFamily()
	if len(fam.Efforts) <= 1 {
		return
	}
	r.EffortIdx = (r.EffortIdx + delta + len(fam.Efforts)) % len(fam.Efforts)
}

func (s *OrchestratorRowState) focusableStops() []OrchestratorStop {
	stops := []OrchestratorStop{StopPlanner}
	if len(s.Planner.CurrentFamily().Efforts) > 1 {
		stops = append(stops, StopPlannerEffort)
	}
	stops = append(stops, StopExecutor)
	if len(s.Executor.CurrentFamily().Efforts) > 1 {
		stops = append(stops, StopExecutorEffort)
	}
	return stops
}

// CycleFocus moves focus between Planner, Planner Effort, Executor, and
// Executor Effort.
func (s *OrchestratorRowState) CycleFocus(delta int) {
	if s == nil {
		return
	}
	stops := s.focusableStops()
	if len(stops) == 0 {
		return
	}
	cur := 0
	for i, st := range stops {
		if st == s.Focus {
			cur = i
			break
		}
	}
	next := (cur + delta + len(stops)) % len(stops)
	s.Focus = stops[next]
}

// StepFocused steps the currently focused family or effort by delta.
func (s *OrchestratorRowState) StepFocused(delta int) {
	if s == nil {
		return
	}
	switch s.Focus {
	case StopPlanner:
		s.Planner.StepFamily(delta)
	case StopPlannerEffort:
		s.Planner.StepEffort(delta)
	case StopExecutor:
		s.Executor.StepFamily(delta)
	case StopExecutorEffort:
		s.Executor.StepEffort(delta)
	}
}

// ToSettings converts the interactive state into settings.OrchestratorSettings.
func (s *OrchestratorRowState) ToSettings() settings.OrchestratorSettings {
	if s == nil {
		return settings.OrchestratorSettings{}
	}
	return settings.OrchestratorSettings{
		Planner:  s.Planner.CurrentSlug(),
		Executor: s.Executor.CurrentSlug(),
	}
}

func renderOrchestratorDetailLines(s *OrchestratorRowState) []string {
	if s == nil {
		return nil
	}
	return []string{
		renderOrchestratorRoleLine("Planner ", s.Planner, s.Focus == StopPlanner, s.Focus == StopPlannerEffort),
		renderOrchestratorRoleLine("Executor", s.Executor, s.Focus == StopExecutor, s.Focus == StopExecutorEffort),
	}
}

func renderOrchestratorRoleLine(label string, role OrchestratorRoleUI, focusFamily, focusEffort bool) string {
	fam := role.CurrentFamily()
	famName := fam.Label
	if famName == "" {
		famName = fam.ShortLabel
	}
	famText := famName
	if focusFamily {
		famText = palette.BlueOpen + famName + palette.BlueClose
	}
	line := "  " + palette.DimGrayOpen + label + "  ◂ " + palette.DimGrayClose +
		famText +
		palette.DimGrayOpen + " ▸" + palette.DimGrayClose

	if eff := role.CurrentEffort(); eff != "" {
		// The focused effort is bracketed, the way the reference marks the
		// field Tab lands on: `[high]`. The brackets ride with the focus, so
		// Tab moves them between Planner and Executor.
		effText := eff
		if focusEffort {
			effText = palette.BlueOpen + "[" + eff + "]" + palette.BlueClose
		}
		line += "   " + palette.DimGrayOpen + "Effort  ◂ " + palette.DimGrayClose +
			effText +
			palette.DimGrayOpen + " ▸" + palette.DimGrayClose
	}
	return line
}

// renderOrchestratorCollapseLines renders the selection's effective mode
// under the Planner/Executor controls.
//
// A collapsed selection (same model, same effort) is what actually runs, so
// the panel says so instead of leaving the two-phase shape implied.
func renderOrchestratorCollapseLines(s *OrchestratorRowState) []string {
	d := s.Collapse()
	note := renderCollapseNote(d)
	if note == "" {
		return nil
	}
	return []string{note}
}
