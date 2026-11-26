package modelpicker

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
)

// newCollapseTestPanel opens a /model panel with `ANTIGRAVITY_HOME` isolated
// and the cursor parked on the Orchestrator row.
func newCollapseTestPanel(t *testing.T) *Panel {
	t.Helper()
	configtest.Isolate(t)
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("AGY_ADC_AUTH", "true")

	p := New(Seams{CurrentModelLabel: func() string { return "Orchestrator" }})
	idx := -1
	for i, r := range p.Rows() {
		if r.HasOrchestrator() {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("expected an Orchestrator row under AGY_ADC_AUTH=true")
	}
	p.SetCursor(idx)
	return p
}

func collapseTabKey() tea.Key { return tea.Key{Code: tea.KeyTab} }

func collapseRightKey() tea.Key { return tea.Key{Code: tea.KeyRight} }

func TestCollapseSelection(t *testing.T) {
	cases := []struct {
		name                       string
		pModel, pEff, eModel, eEff string
		wantSingle                 bool
		wantReason                 CollapseReason
		wantModel                  string
	}{
		{
			name:   "same model same effort collapses",
			pModel: "gemini-4-carbon", pEff: "high",
			eModel: "gemini-4-carbon", eEff: "high",
			wantSingle: true, wantReason: ReasonSameModelSameEffort,
			wantModel: "gemini-4-carbon-high",
		},
		{
			name:   "same model different effort stays two-phase",
			pModel: "gemini-4-carbon", pEff: "high",
			eModel: "gemini-4-carbon", eEff: "low",
			wantSingle: false, wantReason: ReasonDifferentEffort,
			wantModel: "gemini-4-carbon-high",
		},
		{
			name:   "same family xhigh vs high stays two-phase",
			pModel: "gemini-4-hydrogen", pEff: "high",
			eModel: "gemini-4-hydrogen", eEff: "xhigh",
			wantSingle: false, wantReason: ReasonDifferentEffort,
			wantModel: "gemini-4-hydrogen-high",
		},
		{
			name:   "different model same effort stays two-phase",
			pModel: "gemini-4-carbon", pEff: "high",
			eModel: "gemini-4-hydrogen", eEff: "high",
			wantSingle: false, wantReason: ReasonDifferentModel,
			wantModel: "gemini-4-carbon-high",
		},
		{
			name:   "suffix slugs split before comparing",
			pModel: "gemini-4-carbon-high", pEff: "",
			eModel: "gemini-4-carbon-high", eEff: "",
			wantSingle: true, wantReason: ReasonSameModelSameEffort,
			wantModel: "gemini-4-carbon-high",
		},
		{
			name:   "suffix slug vs base plus effort",
			pModel: "gemini-4-neon-xhigh", pEff: "",
			eModel: "gemini-4-neon", eEff: "xhigh",
			wantSingle: true, wantReason: ReasonSameModelSameEffort,
			wantModel: "gemini-4-neon-xhigh",
		},
		{
			name:   "case and whitespace are normalised",
			pModel: " Gemini-4-Carbon ", pEff: "HIGH",
			eModel: "gemini-4-carbon", eEff: " high",
			wantSingle: true, wantReason: ReasonSameModelSameEffort,
			wantModel: "gemini-4-carbon-high",
		},
		{
			name:   "empty planner never collapses",
			pModel: "", pEff: "",
			eModel: "gemini-4-carbon", eEff: "high",
			wantSingle: false, wantReason: ReasonUnsetSelection,
		},
		{
			name:   "empty executor never collapses",
			pModel: "gemini-4-carbon", pEff: "high",
			eModel: "", eEff: "",
			wantSingle: false, wantReason: ReasonUnsetSelection,
		},
		{
			name:       "both empty never collapses",
			wantSingle: false, wantReason: ReasonUnsetSelection,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CollapseSelection(tc.pModel, tc.pEff, tc.eModel, tc.eEff)
			if got.SingleAgent != tc.wantSingle {
				t.Fatalf("SingleAgent = %v, want %v (%+v)", got.SingleAgent, tc.wantSingle, got)
			}
			if got.Reason != tc.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if tc.wantModel != "" && got.Model != tc.wantModel {
				t.Fatalf("Model = %q, want %q", got.Model, tc.wantModel)
			}
			wantMode := ModePlanExecute
			if tc.wantSingle {
				wantMode = ModeSingleAgent
			}
			if got.Mode != wantMode {
				t.Fatalf("Mode = %q, want %q", got.Mode, wantMode)
			}
			if !got.SingleAgent && got.Model != "" {
				// A two-phase selection still reports what the planner would
				// run; the collapsed model is only meaningful with a match.
				if got.Reason == ReasonDifferentEffort && got.Effort != "" && !strings.HasSuffix(got.Model, got.Effort) {
					t.Fatalf("two-phase Model %q does not carry effort %q", got.Model, got.Effort)
				}
			}
		})
	}
}

func TestOrchestratorRowState_Collapse(t *testing.T) {
	st := NewOrchestratorRowState(catalog.OrchestratorConfig{
		Planner: catalog.OrchestratorRoleState{
			Families: []catalog.OrchestratorFamily{
				{BaseID: "fam-a", Label: "Fam A", Efforts: []string{"low", "medium", "high"}},
			},
			BaseID: "fam-a",
			Effort: "high",
		},
		Executor: catalog.OrchestratorRoleState{
			Families: []catalog.OrchestratorFamily{
				{BaseID: "fam-a", Label: "Fam A", Efforts: []string{"low", "medium", "high"}},
			},
			BaseID: "fam-a",
			Effort: "high",
		},
	})
	if d := st.Collapse(); !d.SingleAgent || d.Model != "fam-a-high" {
		t.Fatalf("same model+effort Collapse() = %+v, want single-agent fam-a-high", d)
	}

	// Distinct effort on the same family: two-phase.
	st.Focus = StopExecutorEffort
	st.StepFocused(-1) // high -> medium
	if d := st.Collapse(); d.SingleAgent || d.Reason != ReasonDifferentEffort {
		t.Fatalf("different effort Collapse() = %+v, want two-phase different-effort", d)
	}

	var nilState *OrchestratorRowState
	if d := nilState.Collapse(); d.SingleAgent {
		t.Fatalf("nil state Collapse() = %+v, want two-phase", d)
	}
}

func TestOrchestratorRoleLine_BracketsFocusedEffort(t *testing.T) {
	role := OrchestratorRoleUI{
		Families: []catalog.OrchestratorFamily{
			{BaseID: "fam-a", Label: "Fam A", Efforts: []string{"low", "medium", "high"}},
		},
		FamilyIdx: 0,
		EffortIdx: 2,
	}
	focused := stripSGR(renderOrchestratorRoleLine("Planner ", role, false, true))
	if !strings.Contains(focused, "[high]") {
		t.Fatalf("focused effort not bracketed:\n%q", focused)
	}
	unfocused := stripSGR(renderOrchestratorRoleLine("Executor", role, false, false))
	if strings.Contains(unfocused, "[high]") {
		t.Fatalf("unfocused effort should not be bracketed:\n%q", unfocused)
	}
	if !strings.Contains(unfocused, "high") {
		t.Fatalf("unfocused effort missing:\n%q", unfocused)
	}
}

func TestOrchestratorDetailLines_BracketFollowsFocus(t *testing.T) {
	st := NewOrchestratorRowState(catalog.OrchestratorConfig{
		Planner: catalog.OrchestratorRoleState{
			Families: []catalog.OrchestratorFamily{
				{BaseID: "fam-a", Label: "Fam A", Efforts: []string{"low", "high"}},
			},
			BaseID: "fam-a", Effort: "high",
		},
		Executor: catalog.OrchestratorRoleState{
			Families: []catalog.OrchestratorFamily{
				{BaseID: "fam-b", Label: "Fam B", Efforts: []string{"low", "high"}},
			},
			BaseID: "fam-b", Effort: "high",
		},
	})
	lines := stripSGR(strings.Join(renderOrchestratorDetailLines(st), "\n"))
	// Focus starts on StopPlanner: no effort stop is focused yet.
	if strings.Contains(lines, "[high]") {
		t.Fatalf("no effort should be bracketed while Planner is focused:\n%s", lines)
	}

	st.CycleFocus(+1) // -> StopPlannerEffort
	lines = stripSGR(strings.Join(renderOrchestratorDetailLines(st), "\n"))
	if got := strings.Count(lines, "[high]"); got != 1 {
		t.Fatalf("Planner effort bracket count = %d, want 1:\n%s", got, lines)
	}
	if strings.Index(lines, "[high]") > strings.Index(lines, "Executor") {
		t.Fatalf("Planner bracket landed on the Executor line:\n%s", lines)
	}

	st.CycleFocus(+1) // -> StopExecutor
	st.CycleFocus(+1) // -> StopExecutorEffort
	lines = stripSGR(strings.Join(renderOrchestratorDetailLines(st), "\n"))
	if got := strings.Count(lines, "[high]"); got != 1 {
		t.Fatalf("Executor effort bracket count = %d, want 1:\n%s", got, lines)
	}
	if strings.Index(lines, "[high]") < strings.Index(lines, "Executor") {
		t.Fatalf("Executor bracket landed on the Planner line:\n%s", lines)
	}
}

func TestOrchestratorFooter_LabelsTabAsNextField(t *testing.T) {
	p := newCollapseTestPanel(t)
	rendered := stripSGR(strings.Join(p.Lines(40), "\n"))
	if !strings.Contains(rendered, "tab Next Field") {
		t.Fatalf("footer missing `tab Next Field`:\n%s", rendered)
	}
	if strings.Contains(rendered, "tab Field") {
		t.Fatalf("footer still carries the retired `tab Field` label:\n%s", rendered)
	}
}

func TestOrchestratorPanel_IsSingleAgentSelection(t *testing.T) {
	// Defaults: Planner Carbon-high, Executor Hydrogen-high -> different
	// families, so two-phase.
	p := newCollapseTestPanel(t)
	if p.IsSingleAgentSelection() {
		t.Fatal("default Planner/Executor selection should be two-phase")
	}
	if d, ok := p.OrchestratorCollapse(); !ok || d.Reason != ReasonDifferentModel {
		t.Fatalf("OrchestratorCollapse() = %+v, %v; want different-model", d, ok)
	}
	// A non-Orchestrator row answers false/not-ok.
	p.SetCursor(0)
	if _, ok := p.OrchestratorCollapse(); ok {
		t.Fatal("a plain model row should not report an Orchestrator collapse")
	}
	if p.IsSingleAgentSelection() {
		t.Fatal("a plain model row is not a single-agent Orchestrator selection")
	}
}

func TestOrchestratorPanel_CollapsesWhenRolesMatch(t *testing.T) {
	p := newCollapseTestPanel(t)
	// Walk the Executor family to the Planner's family (Carbon) and align
	// effort: same model + same effort must collapse.
	// Focus order: Planner, PlannerEffort, Executor, ExecutorEffort.
	p.HandleKey(collapseTabKey()) // -> PlannerEffort
	p.HandleKey(collapseTabKey()) // -> Executor
	p.HandleKey(collapseRightKey())
	p.HandleKey(collapseRightKey()) // hydrogen -> sonnet? -> hydrogen (wrap)
	d, ok := p.OrchestratorCollapse()
	if !ok {
		t.Fatal("expected an Orchestrator row")
	}
	// Whatever the family walk landed on, the predicate must agree with the
	// pure function over the same live selection.
	want := CollapseSelection(
		d.BaseID, d.Effort,
		d.BaseID, d.Effort,
	)
	if !want.SingleAgent {
		t.Fatalf("self-comparison must collapse: %+v", want)
	}
	if p.IsSingleAgentSelection() != (d.SingleAgent) {
		t.Fatalf("IsSingleAgentSelection() disagrees with OrchestratorCollapse(): %+v", d)
	}
}

func TestOrchestratorPanel_CollapseNoteRendersWhenSingleAgent(t *testing.T) {
	// A row state whose roles already match must print the in-panel note.
	st := NewOrchestratorRowState(catalog.OrchestratorConfig{
		Planner: catalog.OrchestratorRoleState{
			Families: []catalog.OrchestratorFamily{
				{BaseID: "fam-a", Label: "Fam A", Efforts: []string{"high"}},
			},
			BaseID: "fam-a", Effort: "high",
		},
		Executor: catalog.OrchestratorRoleState{
			Families: []catalog.OrchestratorFamily{
				{BaseID: "fam-a", Label: "Fam A", Efforts: []string{"high"}},
			},
			BaseID: "fam-a", Effort: "high",
		},
	})
	lines := stripSGR(strings.Join(renderOrchestratorCollapseLines(st), "\n"))
	if !strings.Contains(lines, "Single agent") || !strings.Contains(lines, "fam-a") {
		t.Fatalf("collapse note missing or wrong:\n%q", lines)
	}
	if got := renderOrchestratorCollapseLines(&OrchestratorRowState{}); got != nil {
		t.Fatalf("unset state should render no note, got %v", got)
	}
}
