// Package cortex_test / cortex — cortex_test.go: cycle 42 tests for
// CascadeManager type presence, constructor, options, and methods.
//
// These tests verify that:
//   1. All 12 recovered type names from v1.3.5 binary are present and
//      instantiable.
//   2. cortex.NewCascadeManager constructor exists and returns non-nil.
//   3. All 28 With* option helpers exist and accept arguments.
//   4. CascadeManager top-level method set contains all recovered methods.
//   5. EndBattleModeError implements error + Unwrap.
package cortex

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestCascadeManager_TypePresence verifies the 12 type names
// recovered from the v1.3.5 binary are defined and non-empty.
func TestCascadeManager_TypePresence(t *testing.T) {
	recoveredTypes := []string{
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

	for _, name := range recoveredTypes {
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
			var _ *stateSyncSummariesStore
		default:
			t.Errorf("unhandled type check: %s", name)
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
}

// TestCascadeManager_OptionsPresent verifies the v1.3.5
// `cortex.With*` option helpers are present (all 28 recovered
// through the same CascadeManagerOption type).
func TestCascadeManager_OptionsPresent(t *testing.T) {
	options := []CascadeManagerOption{
		WithForkNotes("note1", "note2"),
		WithAgentConfigResolution(nil),
		WithConversationPresence(nil),
		WithTriggerTag("test-tag"),
		WithForkAtStepIndex(3),
		WithCloneStrategy("shallow"),
		WithCitcClient(nil),
		WithSdkProcessManager(nil),
		WithExternalBaseOverlayConfig(nil),
		WithSettingsStore(nil),
		WithSubagentHub(&nopSubagentHub{}),
		WithPlatformClient(nil),
		WithBundleMintClient(nil),
		WithAdminControlsManager(nil),
		WithTurnListener(nil),
		WithSourceMetadata(map[string]any{"source": "test"}),
		WithEnvironmentID("env-123"),
		WithProjectID("proj-456"),
		WithAgentPath("/path/to/agent"),
		WithCustomAgentSpec("spec-a"),
		WithWorkspaceURIs("file:///a", "file:///b"),
		WithSubagentSpec("spec-sub"),
		WithParentTrajectoryInfo("parent-1", 5),
		WithOverrideWorkspaceURIs("file:///override"),
		WithRequestedModel("gemini-2.0"),
		WithTags("tag1", "tag2"),
		WithRemoteControlDetails(nil),
		WithForceStopRunningWinner(true),
	}
	cm := NewCascadeManager(options...)
	if cm == nil {
		t.Fatal("NewCascadeManager with options returned nil")
	}
}

type cmMethodTestCase struct {
	name string
	call func() error
}

func cmLifecycleMethods(cm *CascadeManager) []cmMethodTestCase {
	return []cmMethodTestCase{
		{"Run", func() error { return cm.Run(context.Background(), "hi") }},
		{"Cancel", func() error { return cm.Cancel(context.Background()) }},
		{"Shutdown", func() error { return cm.Shutdown(context.Background()) }},
		{"Save", func() error { return cm.Save() }},
		{"WaitForConversationFullyIdle", func() error { return cm.WaitForConversationFullyIdle(context.Background()) }},
		{"ResumeConversationsFromRestart", func() error { return cm.ResumeConversationsFromRestart(context.Background()) }},
		{"ForceStopTree", func() error { return cm.ForceStopTree() }},
	}
}

func cmTrajectoryMethods(cm *CascadeManager) []cmMethodTestCase {
	return []cmMethodTestCase{
		{"SaveTrajectory", func() error { return cm.SaveTrajectory(Trajectory{ID: "t1"}) }},
		{"LoadTrajectory", func() error { return cm.LoadTrajectory("t1") }},
		{"DeleteTrajectory", func() error { return cm.DeleteTrajectory("t1") }},
		{"ReloadTrajectoryIfLoaded", func() error { return cm.ReloadTrajectoryIfLoaded() }},
		{"GetTrajectory", func() error { _, err := cm.GetTrajectory(); return err }},
		{"GetMainAgentTrajectories", func() error { _, err := cm.GetMainAgentTrajectories(); return err }},
		{"ResidentTrajectoryCount", func() error { _ = cm.ResidentTrajectoryCount(); return nil }},
		{"ForkConversation", func() error { _, err := cm.ForkConversation(); return err }},
		{"RevertToStep", func() error { return cm.RevertToStep(0) }},
		{"GetRevertPreview", func() error { _, err := cm.GetRevertPreview(0); return err }},
		{"ViewTrajectoryDoNotUse", func() error { _, err := cm.ViewTrajectoryDoNotUse(); return err }},
		{"GetAllTrajectories", func() error { _, err := cm.GetAllTrajectories(); return err }},
		{"GetTrajectoryForServing", func() error { _, err := cm.GetTrajectoryForServing(); return err }},
	}
}

func cmBattleModeMethods(cm *CascadeManager) []cmMethodTestCase {
	return []cmMethodTestCase{
		{"StartBattleMode", func() error { return cm.StartBattleMode() }},
		{"EndBattleMode", func() error { return cm.EndBattleMode() }},
		{"ValidateBattleMode", func() error { return cm.ValidateBattleMode() }},
		{"EliminateBattleModeArm", func() error {
			err := cm.EliminateBattleModeArm(0)
			if err == nil {
				return errors.New("expected error when not in battle mode")
			}
			return nil
		}},
		{"UpdateLoadedSensitivityLevel", func() error { return cm.UpdateLoadedSensitivityLevel(SensitivityPublic) }},
		{"PersistSensitivityLevel", func() error { return cm.PersistSensitivityLevel() }},
	}
}

func cmAgentAndMiscMethods(cm *CascadeManager) []cmMethodTestCase {
	return []cmMethodTestCase{
		{"KillAgent", func() error { return cm.KillAgent("s1") }},
		{"MaybeReviveAgent", func() error { return cm.MaybeReviveAgent("s1") }},
		{"WarmUpAgentProcess", func() error { return cm.WarmUpAgentProcess() }},
		{"RequestAgentStatePageUpdate", func() error { return cm.RequestAgentStatePageUpdate(0) }},
		{"UpdateSummaryForID", func() error { return cm.UpdateSummaryForID("id", Summary{}) }},
		{"SetInMemoryCLISettingsProvider", func() error {
			return cm.SetInMemoryCLISettingsProvider(&CLISettingsProvider{values: map[string]any{}})
		}},
		{"ResolveWorkspaceURIs", func() error { _, err := cm.ResolveWorkspaceURIs(); return err }},
		{"CleanupMappedWorkspaces", func() error { return cm.CleanupMappedWorkspaces() }},
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
		{"ExecutionStatus", func() error { _ = cm.ExecutionStatus(); return nil }},
		{"IsCascadeRunning", func() error { _ = cm.IsCascadeRunning(); return nil }},
		{"IsQuiesced", func() error { _ = cm.IsQuiesced(); return nil }},
		{"GetActivitySnapshot", func() error { _ = cm.GetActivitySnapshot(); return nil }},
		{"StreamAgentStateUpdates", func() error { _ = cm.StreamAgentStateUpdates(); return nil }},
		{"ResolveStaticConfig2", func() error { _, err := cm.ResolveStaticConfig(); return err }},
	}
}

// TestCascadeManager_MethodsPresent verifies the v1.3.5
// `(*CascadeManager).Xxx` method set.
func TestCascadeManager_MethodsPresent(t *testing.T) {
	cm := NewCascadeManager()
	var methods []cmMethodTestCase
	methods = append(methods, cmLifecycleMethods(cm)...)
	methods = append(methods, cmTrajectoryMethods(cm)...)
	methods = append(methods, cmBattleModeMethods(cm)...)
	methods = append(methods, cmAgentAndMiscMethods(cm)...)

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

func (n *nopSubagentHub) Register(_, _ string) error            { return nil }
func (n *nopSubagentHub) Lookup(_ string) (SubagentState, bool) { return SubagentState{}, true }
func (n *nopSubagentHub) ListForParent(_ string) []SubagentState { return nil }
func (n *nopSubagentHub) Unregister(_ string) error             { return nil }
func (n *nopSubagentHub) Gate() SubagentGate                    { return &nopSubagentGate{} }

// nopSubagentGate is a no-op SubagentGate used in option tests.
type nopSubagentGate struct{}

func (n *nopSubagentGate) Allow(_ string) bool { return true }
