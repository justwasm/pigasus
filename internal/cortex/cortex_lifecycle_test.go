// Package cortex — cortex_lifecycle_test.go: cycle 42 tests for
// CortexLifecycle, gates, subagent FSM, and integrations.
package cortex

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/cascade"
)

// TestCortexLifecycle_FSM walks the 6-state FSM (init -> ready ->
// execute -> stream -> save -> done) on a CortexLifecycle and
// verifies the transitions are recorded.
func TestCortexLifecycle_FSM(t *testing.T) {
	cm := NewCascadeManager()
	if err := cm.LoadTrajectory("traj-test"); err != nil {
		t.Fatalf("LoadTrajectory: %v", err)
	}
	cl := NewCortexLifecycle(cm)
	if got := cl.Current(); got != LifecycleInit {
		t.Errorf("init state = %q, want %q", got, LifecycleInit)
	}
	if err := cl.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got := cl.Current(); got != LifecycleDone {
		t.Errorf("post-Run state = %q, want %q", got, LifecycleDone)
	}
	history := cl.History()
	if len(history) < 4 {
		t.Errorf("history len = %d, want >= 4 transitions", len(history))
	}
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

	_ = hub.Register("s1", "p1")
	gate.Enqueue(PendingApproval{ID: "a1", SubagentID: "s1", Kind: ApprovalToolConfirm, ToolName: "shell"})

	_, st, err := driver.Approve("a1")
	if err != nil {
		t.Errorf("Approve: %v", err)
	}
	if st.Status != SubagentDone {
		t.Errorf("post-Approve Status = %q, want %q", st.Status, SubagentDone)
	}

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

func mapCortexHistoryToCascadeStates(history []CortexLifecycleTransition) []cascade.CascaderState {
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
			cascaderStates = append(cascaderStates, cascade.CascaderExecute)
		}
	}
	return cascaderStates
}

// TestIntegrationWithCycle41_CascadeFSM verifies the cortex
// CortexLifecycle produces a 6-state sequence compatible with the
// cycle 41 cascade.RealLifecycle FSM.
func TestIntegrationWithCycle41_CascadeFSM(t *testing.T) {
	cm := NewCascadeManager()
	if err := cm.LoadTrajectory("traj-int"); err != nil {
		t.Fatalf("LoadTrajectory: %v", err)
	}
	cl := NewCortexLifecycle(cm)
	if err := cl.Run(context.Background(), "ping"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	cascaderStates := mapCortexHistoryToCascadeStates(cl.History())
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
// CortexLifecycle does not race.
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

// TestAnnotatedTypes_AnnotationsManager verifies AnnotationsManager operations.
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

// TestExecutors_StubPresent verifies executor implementations.
func TestExecutors_StubPresent(t *testing.T) {
	var oe Executor = &ordinaryExecutor{}
	var se Executor = &sdkExecutor{}
	if oe == nil || se == nil {
		t.Error("executor stubs are nil")
	}
	if err := oe.Close(); err != nil {
		t.Errorf("ordinaryExecutor.Close: %v", err)
	}
	if err := se.Close(); err != nil {
		t.Errorf("sdkExecutor.Close: %v", err)
	}
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
