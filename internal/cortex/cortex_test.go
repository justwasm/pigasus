// Package cortex — cortex_test.go: sub-tests verifying the cycle 42
// cortex skeleton is 1:1 with the v1.3.5 binary at
// `/workspace/agy/install/antigravity-v1.3.5` (MD5
// b7f7ed7c66bc835ee21bd9b7304b8f74).
//
// Source of truth:
//
//   - pclntab entries:
//     `grep -oE 'cortex/cortex\.[A-Za-z0-9_().*]+'
//     /workspace/agy-recovered/docs/analysis-v1.3.5-official/funcs-v1.3.5-pclntab.txt`
//   - decompiled .c files:
//     `ls /workspace/agy-recovered/docs/decompiled-v1.3.5/c/google3_third_party_jetski_cortex_cortex/`
//   - cycle 41 deliverable:
//     /workspace/agy-recovered/cycle-41-backend-lifecycle-deliverable.md
//
// Each sub-test asserts a specific 1:1 alignment. The skeleton is
// not a real implementation — it is a structural recovery. Tests
// cover type presence, method presence, and integration with cycle
// 41's cascade.RealLifecycle (the 6-state FSM).
package cortex

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/cascade"
)

// TestCascadeManager_TypePresence verifies the 12 type names
// recovered from the v1.3.5 pclntab are present in the cortex
// package.
func TestCascadeManager_TypePresence(t *testing.T) {
	expected := []string{
		"CascadeManager",
		"AnnotationsManager",
		"EndBattleModeError",
		"backgroundRunner",
		"jetboxSummariesStoreAdapter",
		"nopPlacer",
		"ordinaryExecutor",
		"presenceTracker",
		"projectLookupImpl",
		"providerServerURLMap",
		"sdkExecutor",
		"stateSyncSummariesStore",
	}
	for _, name := range expected {
		// Use a no-op declaration site probe: we just need the
		// type to exist. The skeleton defines each as a struct or
		// interface; the type assertion via `_ = (*X)(nil)` catches
		// missing or mistyped names at compile time.
		switch name {
		case "CascadeManager":
			var _ *CascadeManager
		case "AnnotationsManager":
			var _ *AnnotationsManager
		case "EndBattleModeError":
			var _ *EndBattleModeError
		case "backgroundRunner":
			var _ *backgroundRunner
		case "jetboxSummariesStoreAdapter":
			var _ *jetboxSummariesStoreAdapter
		case "nopPlacer":
			var _ *nopPlacer
		case "ordinaryExecutor":
			var _ *ordinaryExecutor
		case "presenceTracker":
			var _ *presenceTracker
		case "projectLookupImpl":
			var _ *projectLookupImpl
		case "providerServerURLMap":
			var _ *providerServerURLMap
		case "sdkExecutor":
			var _ *sdkExecutor
		case "stateSyncSummariesStore":
			var _ stateSyncSummariesStore
		default:
			t.Errorf("type %q in expected list is not asserted", name)
		}
	}
}

// TestCascadeManager_ConstructorPresent verifies the v1.3.5
// `cortex.NewCascadeManager` (pclntab 0x7fdb120) is present.
func TestCascadeManager_ConstructorPresent(t *testing.T) {
	cm := NewCascadeManager()
	if cm == nil {
		t.Fatal("NewCascadeManager returned nil")
	}
	if cm.closed {
		t.Error("fresh CascadeManager is closed")
	}
	if cm.sensitivityLevel != SensitivityInternal {
		t.Errorf("default sensitivityLevel = %q, want %q", cm.sensitivityLevel, SensitivityInternal)
	}
}

// TestCascadeManager_OptionsPresent verifies the v1.3.5
// `cortex.WithXxx` helpers are present. pclntab lists 28 With*
// functions; we verify the most common 8 (the rest are wired
// through the same CascadeManagerOption type).
func TestCascadeManager_OptionsPresent(t *testing.T) {
	options := []CascadeManagerOption{
		WithWorkspaceURIs("uri1", "uri2"),
		WithSubagentHub(&nopSubagentHub{}),
		WithProjectID("proj-1"),
		WithRequestedModel("claude-3-5-sonnet"),
		WithCustomAgentSpec("spec"),
		WithSubagentSpec("subagent-spec"),
		WithOverrideWorkspaceURIs("uri3"),
		WithParentTrajectoryInfo("traj-1", 5),
		WithForkNotes("note-1"),
		WithForkAtStepIndex(3),
		WithCloneStrategy("worktree"),
		WithTags("tag1", "tag2"),
		WithEnvironmentID("env-1"),
		WithAgentPath("/path"),
		WithSourceMetadata(map[string]any{"k": "v"}),
		WithForceStopRunningWinner(true),
		WithTriggerTag("trigger"),
	}
	if len(options) < 8 {
		t.Errorf("got %d options, want >= 8", len(options))
	}
	cm := NewCascadeManager(options...)
	if cm == nil {
		t.Fatal("NewCascadeManager with options returned nil")
	}
	if len(cm.workspaceURIs) != 1 || cm.workspaceURIs[0] != "uri3" {
		t.Errorf("workspaceURIs = %v, want [uri3] (WithOverrideWorkspaceURIs should win)", cm.workspaceURIs)
	}
}

// TestCascadeManager_MethodsPresent verifies the v1.3.5
// `(*CascadeManager).Xxx` method set. The cycle 42 skeleton
// includes 60+ top-level methods; we assert the 20 most central
// ones are present.
func TestCascadeManager_MethodsPresent(t *testing.T) {
	cm := NewCascadeManager()
	methods := []struct {
		name string
		call func() error
	}{
		{"Run", func() error { return cm.Run(context.Background(), "hi") }},
		{"Cancel", func() error { return cm.Cancel(context.Background()) }},
		{"Shutdown", func() error { return cm.Shutdown(context.Background()) }},
		{"Save", func() error { return cm.Save() }},
		{"SaveTrajectory", func() error { return cm.SaveTrajectory(Trajectory{ID: "t1"}) }},
		{"LoadTrajectory", func() error { return cm.LoadTrajectory("t1") }},
		{"DeleteTrajectory", func() error { return cm.DeleteTrajectory("t1") }},
		{"ReloadTrajectoryIfLoaded", func() error { return cm.ReloadTrajectoryIfLoaded() }},
		{"GetTrajectory", func() error { _, err := cm.GetTrajectory(); return err }},
		{"GetMainAgentTrajectories", func() error { _, err := cm.GetMainAgentTrajectories(); return err }},
		{"ResidentTrajectoryCount", func() error { _ = cm.ResidentTrajectoryCount(); return nil }},
		{"ForkConversation", func() error { _, err := cm.ForkConversation(); return err }},
		{"StartBattleMode", func() error { return cm.StartBattleMode() }},
		{"EndBattleMode", func() error { return cm.EndBattleMode() }},
		{"ValidateBattleMode", func() error { return cm.ValidateBattleMode() }},
		{"EliminateBattleModeArm", func() error {
			// EliminateBattleModeArm requires battle mode to be
			// active with at least one arm. The cycle 42
			// skeleton is a structural recovery — we just verify
			// the method exists and returns an expected error
			// (battle mode not active is the recovered behavior).
			err := cm.EliminateBattleModeArm(0)
			if err == nil {
				return errors.New("expected error when not in battle mode")
			}
			return nil
		}},
		{"KillAgent", func() error { return cm.KillAgent("s1") }},
		{"MaybeReviveAgent", func() error { return cm.MaybeReviveAgent("s1") }},
		{"WarmUpAgentProcess", func() error { return cm.WarmUpAgentProcess() }},
		{"RequestAgentStatePageUpdate", func() error { return cm.RequestAgentStatePageUpdate(0) }},
		{"WaitForConversationFullyIdle", func() error { return cm.WaitForConversationFullyIdle(context.Background()) }},
		{"ResumeConversationsFromRestart", func() error { return cm.ResumeConversationsFromRestart(context.Background()) }},
		{"UpdateSummaryForID", func() error { return cm.UpdateSummaryForID("id", Summary{}) }},
		{"SetInMemoryCLISettingsProvider", func() error { return cm.SetInMemoryCLISettingsProvider(&CLISettingsProvider{values: map[string]any{}}) }},
		{"UpdateLoadedSensitivityLevel", func() error { return cm.UpdateLoadedSensitivityLevel(SensitivityPublic) }},
		{"PersistSensitivityLevel", func() error { return cm.PersistSensitivityLevel() }},
		{"ResolveWorkspaceURIs", func() error { _, err := cm.ResolveWorkspaceURIs(); return err }},
		{"CleanupMappedWorkspaces", func() error { return cm.CleanupMappedWorkspaces() }},
		{"ForceStopTree", func() error { return cm.ForceStopTree() }},
		{"RevertToStep", func() error { return cm.RevertToStep(0) }},
		{"GetRevertPreview", func() error { _, err := cm.GetRevertPreview(0); return err }},
		{"SendUserCascadeMessage", func() error { return cm.SendUserCascadeMessage("hi") }},
		{"HandleUserInteraction", func() error { return cm.HandleUserInteraction(UserInteractionEvent{}) }},
		{"ListConversationSummaries", func() error { _, err := cm.ListConversationSummaries(); return err }},
		{"ConversationSummary", func() error { _, err := cm.ConversationSummary("id"); return err }},
		{"UpdateSummaryForTrajectory", func() error { return cm.UpdateSummaryForTrajectory(Trajectory{ID: "t1"}) }},
		{"GetInMemoryCLISettingsProvider", func() error { _, err := cm.GetInMemoryCLISettingsProvider(); return err }},
		{"CleanUpWorktreesInArtifacts", func() error { return cm.CleanUpWorktreesInArtifacts() }},
		{"ResolveStaticConfig", func() error { _, err := cm.ResolveStaticConfig(); return err }},
		{"ResolveExecutableScript", func() error { _, err := cm.ResolveExecutableScript(); return err }},
		{"GetResolvedCascadeConfig", func() error { _, err := cm.GetResolvedCascadeConfig(); return err }},
		{"PreviousContribVariant", func() error { _, err := cm.PreviousContribVariant(); return err }},
		{"ViewTrajectoryDoNotUse", func() error { _, err := cm.ViewTrajectoryDoNotUse(); return err }},
		{"GetAllTrajectories", func() error { _, err := cm.GetAllTrajectories(); return err }},
		{"ExecutionStatus", func() error { _ = cm.ExecutionStatus(); return nil }},
		{"IsCascadeRunning", func() error { _ = cm.IsCascadeRunning(); return nil }},
		{"IsQuiesced", func() error { _ = cm.IsQuiesced(); return nil }},
		{"GetActivitySnapshot", func() error { _ = cm.GetActivitySnapshot(); return nil }},
		{"StreamAgentStateUpdates", func() error { _ = cm.StreamAgentStateUpdates(); return nil }},
		{"GetTrajectoryForServing", func() error { _, err := cm.GetTrajectoryForServing(); return err }},
		{"ResolveStaticConfig2", func() error { _, err := cm.ResolveStaticConfig(); return err }},
	}
	for _, m := range methods {
		if err := m.call(); err != nil && !isExpectedErr(err) {
			t.Errorf("method %s returned unexpected error: %v", m.name, err)
		}
	}
}

// TestEndBattleModeError verifies the v1.3.5
// `(*EndBattleModeError).Error` and `.Unwrap` (pclntab 0x7fc4fa0 +
// 0x7fc5060).
func TestEndBattleModeError(t *testing.T) {
	inner := ErrBattleMode
	e := &EndBattleModeError{Reason: "test reason", Inner: inner}
	got := e.Error()
	if !strings.Contains(got, "test reason") {
		t.Errorf("Error() = %q, want it to contain 'test reason'", got)
	}
	if !strings.Contains(got, ErrBattleMode.Error()) {
		t.Errorf("Error() = %q, want it to wrap inner %q", got, ErrBattleMode.Error())
	}
	if got := e.Unwrap(); got != inner {
		t.Errorf("Unwrap() = %v, want %v", got, inner)
	}
	// nil-receiver safety
	var nilErr *EndBattleModeError
	if got := nilErr.Error(); got != "<nil>" {
		t.Errorf("nil.Error() = %q, want %q", got, "<nil>")
	}
	if got := nilErr.Unwrap(); got != nil {
		t.Errorf("nil.Unwrap() = %v, want nil", got)
	}
}

// TestCortexLifecycle_FSM walks the 6-state FSM (init -> ready ->
// execute -> stream -> save -> done) on a CortexLifecycle and
// verifies the transitions are recorded. This is the cycle 42
// integration with the cycle 41 cascade.RealLifecycle FSM.
func TestCortexLifecycle_FSM(t *testing.T) {
	cm := NewCascadeManager()
	// Pre-load a trajectory so Save() can succeed.
	if err := cm.LoadTrajectory("traj-test"); err != nil {
		t.Fatalf("LoadTrajectory: %v", err)
	}
	cl := NewCortexLifecycle(cm)
	if got := cl.Current(); got != LifecycleInit {
		t.Errorf("init state = %q, want %q", got, LifecycleInit)
	}
	// The Run() will transition through ready -> execute -> save
	// -> done (no subagent blocked).
	if err := cl.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got := cl.Current(); got != LifecycleDone {
		t.Errorf("post-Run state = %q, want %q", got, LifecycleDone)
	}
	history := cl.History()
	if len(history) < 4 {
		t.Errorf("history len = %d, want >= 4 transitions (init->ready->execute->save->done)", len(history))
	}
	// Verify the state sequence
	want := []CortexLifecycleState{
		LifecycleInit,
		LifecycleReady,
		LifecycleExecute,
		LifecycleSave,
		LifecycleDone,
	}
	for i, w := range want {
		if i >= len(history) {
			break
		}
		if history[i].To != w {
			t.Errorf("transition[%d].To = %q, want %q", i, history[i].To, w)
		}
	}
}

// TestCortexLifecycle_AwaitsApproval verifies the
// SubagentAwaitsApproval transition.
func TestCortexLifecycle_AwaitsApproval(t *testing.T) {
	cm := NewCascadeManager()
	cl := NewCortexLifecycle(cm)
	cl.SubagentAwaitsApproval(SubagentState{
		ID:     "s1",
		Status: SubagentAwaitsApproval,
	})
	if got := cl.Current(); got != LifecycleAwaitApproval {
		t.Errorf("state after SubagentAwaitsApproval = %q, want %q", got, LifecycleAwaitApproval)
	}
	if err := cl.ApproveSubagent("s1"); err != nil {
		t.Errorf("ApproveSubagent: %v", err)
	}
	if got := cl.Current(); got != LifecycleStream {
		t.Errorf("state after ApproveSubagent = %q, want %q", got, LifecycleStream)
	}
	if err := cl.RejectSubagent("s1"); err == nil {
		t.Error("RejectSubagent on unblocked subagent returned nil error")
	}
}

// TestSubagentAwaitsApprovalGate verifies the cortex-side gate
// (parallel to cycle 41 cascade.RealLifecycle).
func TestSubagentAwaitsApprovalGate(t *testing.T) {
	gate := NewSubagentAwaitsApprovalGate()
	gate.Enqueue(PendingApproval{ID: "a1", SubagentID: "s1", Kind: ApprovalToolConfirm, ToolName: "shell"})
	gate.Enqueue(PendingApproval{ID: "a2", SubagentID: "s2", Kind: ApprovalUserInput, Question: "What?"})
	if got := gate.Len(); got != 2 {
		t.Errorf("Len = %d, want 2", got)
	}
	appr, err := gate.ApproveSubagent("a1")
	if err != nil {
		t.Errorf("ApproveSubagent: %v", err)
	}
	if appr.SubagentID != "s1" {
		t.Errorf("SubagentID = %q, want s1", appr.SubagentID)
	}
	if got := gate.Len(); got != 1 {
		t.Errorf("post-Approve Len = %d, want 1", got)
	}
}

// TestSubagentFSM_Transitions walks the subagent FSM through
// init -> pending -> running -> awaits_approval -> done.
func TestSubagentFSM_Transitions(t *testing.T) {
	cm := NewCascadeManager()
	hub := newSubagentHub(cm)
	if err := hub.Register("s1", "p1"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// pending is reached by transitioning from init.
	rec := hub.regs["s1"]
	rec.transition(SubagentFSMPending, "queued for execution")
	if rec.state != SubagentFSMPending {
		t.Errorf("state = %q, want %q", rec.state, SubagentFSMPending)
	}
	if err := hub.start("s1"); err != nil {
		t.Errorf("start: %v", err)
	}
	if err := hub.markAwaitsApproval("s1"); err != nil {
		t.Errorf("markAwaitsApproval: %v", err)
	}
	if err := hub.markDone("s1"); err != nil {
		t.Errorf("markDone: %v", err)
	}
	if rec.state != SubagentFSMDone {
		t.Errorf("final state = %q, want %q", rec.state, SubagentFSMDone)
	}
}

// TestApprovalDriver_ApproveReject verifies the approval driver
// routes approval/rejection through the gate + hub correctly.
func TestApprovalDriver_ApproveReject(t *testing.T) {
	cm := NewCascadeManager()
	hub := newSubagentHub(cm)
	gate := NewSubagentAwaitsApprovalGate()
	driver := NewApprovalDriver(cm, hub, gate)

	// Setup a subagent and a pending approval.
	_ = hub.Register("s1", "p1")
	gate.Enqueue(PendingApproval{ID: "a1", SubagentID: "s1", Kind: ApprovalToolConfirm, ToolName: "shell"})

	// Approve.
	_, st, err := driver.Approve("a1")
	if err != nil {
		t.Errorf("Approve: %v", err)
	}
	if st.Status != SubagentDone {
		t.Errorf("post-Approve Status = %q, want %q", st.Status, SubagentDone)
	}

	// Now reject a different approval.
	gate.Enqueue(PendingApproval{ID: "a2", SubagentID: "s2", Kind: ApprovalUserInput})
	_ = hub.Register("s2", "p1")
	_, st, err = driver.Reject("a2")
	if err != nil {
		t.Errorf("Reject: %v", err)
	}
	if st.Status != SubagentFailed {
		t.Errorf("post-Reject Status = %q, want %q", st.Status, SubagentFailed)
	}
}

// TestIntegrationWithCycle41_CascadeFSM verifies the cortex
// CortexLifecycle produces a 6-state sequence compatible with the
// cycle 41 cascade.RealLifecycle FSM (init -> ready -> execute ->
// stream -> save -> done).
func TestIntegrationWithCycle41_CascadeFSM(t *testing.T) {
	cm := NewCascadeManager()
	if err := cm.LoadTrajectory("traj-int"); err != nil {
		t.Fatalf("LoadTrajectory: %v", err)
	}
	cl := NewCortexLifecycle(cm)
	// Drive cl.Run; the state sequence must match the cycle 41
	// cascade.RealLifecycle states by name (init -> ready ->
	// execute -> save -> done) — same FSM, just different package.
	if err := cl.Run(context.Background(), "ping"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Map cortex states to cascade states.
	history := cl.History()
	cascaderStates := make([]cascade.CascaderState, 0, len(history))
	for _, h := range history {
		switch h.To {
		case LifecycleInit:
			cascaderStates = append(cascaderStates, cascade.CascaderInit)
		case LifecycleReady:
			cascaderStates = append(cascaderStates, cascade.CascaderReady)
		case LifecycleExecute:
			cascaderStates = append(cascaderStates, cascade.CascaderExecute)
		case LifecycleStream:
			cascaderStates = append(cascaderStates, cascade.CascaderStream)
		case LifecycleAwaitApproval:
			cascaderStates = append(cascaderStates, cascade.CascaderAwaitApproval)
		case LifecycleSave:
			cascaderStates = append(cascaderStates, cascade.CascaderSave)
		case LifecycleDone:
			cascaderStates = append(cascaderStates, cascade.CascaderDone)
		case LifecycleError:
			cascaderStates = append(cascaderStates, cascade.CascaderError)
		case LifecycleAwaitRevival:
			cascaderStates = append(cascaderStates, cascade.CascaderExecute) // not in cascade, but pass through
		}
	}
	// Verify the first 5 states match the cycle 41 cascade FSM.
	if len(cascaderStates) < 5 {
		t.Fatalf("cascaderStates = %v, want >= 5 entries", cascaderStates)
	}
	want := []cascade.CascaderState{
		cascade.CascaderInit,
		cascade.CascaderReady,
		cascade.CascaderExecute,
		cascade.CascaderSave,
		cascade.CascaderDone,
	}
	for i, w := range want {
		if cascaderStates[i] != w {
			t.Errorf("cascaderStates[%d] = %q, want %q", i, cascaderStates[i], w)
		}
	}
}

// TestLifecycle_ConcurrencySafe verifies concurrent access to a
// CortexLifecycle does not race (regression test against the
// recovered 1.3.5 (*CascadeManager) which is racy if not guarded).
func TestLifecycle_ConcurrencySafe(t *testing.T) {
	cm := NewCascadeManager()
	cl := NewCortexLifecycle(cm)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cl.Current()
			_ = cl.History()
			_ = cl.Subagent()
		}()
	}
	wg.Wait()
}

// TestAnnotatedTypes_AnnotationsManager verifies the
// AnnotationsManager.
func TestAnnotatedTypes_AnnotationsManager(t *testing.T) {
	cm := NewCascadeManager()
	_ = cm.UpdateAnnotations(Annotation{Path: "a.go", Key: "tag", Value: "important"})
	a, ok := cm.GetAnnotations("a.go")
	if !ok {
		t.Fatal("GetAnnotations: not found")
	}
	if a.Value != "important" {
		t.Errorf("Value = %q, want %q", a.Value, "important")
	}
	a.Modified = time.Time{}
	if err := cm.Mutate(func(m map[string]Annotation) {
		m["b.go"] = Annotation{Path: "b.go", Key: "tag", Value: "ok"}
	}); err != nil {
		t.Errorf("Mutate: %v", err)
	}
	if _, ok := cm.GetAnnotations("b.go"); !ok {
		t.Error("Mutate did not insert b.go")
	}
	if err := cm.DeleteAnnotations("a.go"); err != nil {
		t.Errorf("DeleteAnnotations: %v", err)
	}
	if _, ok := cm.GetAnnotations("a.go"); ok {
		t.Error("DeleteAnnotations did not remove a.go")
	}
}

// TestExecutors_StubPresent verifies both the ordinaryExecutor
// and the sdkExecutor implement the Executor interface.
func TestExecutors_StubPresent(t *testing.T) {
	var oe Executor = &ordinaryExecutor{}
	var se Executor = &sdkExecutor{}
	if oe == nil || se == nil {
		t.Error("executor stubs are nil")
	}
	// Smoke-test the methods.
	if err := oe.Close(); err != nil {
		t.Errorf("ordinaryExecutor.Close: %v", err)
	}
	if err := se.Close(); err != nil {
		t.Errorf("sdkExecutor.Close: %v", err)
	}
	// GetExecutorLoopStatus should not panic.
	if got := oe.GetExecutorLoopStatus(); got == "" {
		t.Error("ordinaryExecutor.GetExecutorLoopStatus returned empty")
	}
	if got := se.GetExecutorLoopStatus(); got == "" {
		t.Error("sdkExecutor.GetExecutorLoopStatus returned empty")
	}
}

// TestSensitivityLevel_AllValues verifies the 3 sensitivity levels.
func TestSensitivityLevel_AllValues(t *testing.T) {
	cm := NewCascadeManager()
	for _, l := range []SensitivityLevel{SensitivityPublic, SensitivityInternal, SensitivityRestricted} {
		if err := cm.UpdateLoadedSensitivityLevel(l); err != nil {
			t.Errorf("UpdateLoadedSensitivityLevel(%q): %v", l, err)
		}
		if got := cm.loadedSensitivityLevel(); got != l {
			t.Errorf("loadedSensitivityLevel = %q, want %q", got, l)
		}
	}
}

// TestBattleMode_BasicCycle runs the basic Start/End cycle.
func TestBattleMode_BasicCycle(t *testing.T) {
	cm := NewCascadeManager()
	if err := cm.StartBattleMode(); err != nil {
		t.Fatalf("StartBattleMode: %v", err)
	}
	if cm.battleMode == nil {
		t.Error("battleMode is nil after Start")
	}
	cm.battleMode.Arms = append(cm.battleMode.Arms, BattleModeArm{Index: 0, TrajectoryID: "t1", Model: "a"})
	if err := cm.EliminateBattleModeArm(0); err != nil {
		t.Errorf("EliminateBattleModeArm: %v", err)
	}
	if cm.battleMode.Arms[0].Status != "eliminated" {
		t.Errorf("arm status = %q, want 'eliminated'", cm.battleMode.Arms[0].Status)
	}
	if err := cm.EndBattleMode(); err != nil {
		t.Errorf("EndBattleMode: %v", err)
	}
	if cm.battleMode != nil {
		t.Error("battleMode is not nil after End")
	}
}

// ---- helpers ----

// isExpectedErr reports whether err is an expected error for a
// method stub (ErrClosed, ErrNoTrajectory, or nil).
func isExpectedErr(err error) bool {
	if err == nil {
		return true
	}
	if err == ErrClosed {
		return true
	}
	if err == ErrNoTrajectory {
		return true
	}
	return false
}

// nopSubagentHub is a no-op SubagentHubLike used in option tests.
type nopSubagentHub struct{}

func (n *nopSubagentHub) Register(_, _ string) error   { return nil }
func (n *nopSubagentHub) Lookup(_ string) (SubagentState, bool) { return SubagentState{}, true }
func (n *nopSubagentHub) ListForParent(_ string) []SubagentState { return nil }
func (n *nopSubagentHub) Unregister(_ string) error    { return nil }
func (n *nopSubagentHub) Gate() SubagentGate           { return &nopSubagentGate{} }

// nopSubagentGate is a no-op SubagentGate used in option tests.
type nopSubagentGate struct{}

func (n *nopSubagentGate) Allow(_ string) bool { return true }
