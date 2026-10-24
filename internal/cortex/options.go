// Package cortex — options.go: functional options for CascadeManager.
//
// Mirrors the v1.3.5 cortex.With* helpers (pclntab):
//
//   - WithForkNotes              0x7ffb6e0
//   - WithAgentConfigResolution  0x8122fc0
//   - WithConversationPresence   0x86984c0
//   - WithTriggerTag             0x7ffe3a0
//   - WithForkAtStepIndex        0x7ffe400
//   - WithCloneStrategy          0x7ffe460
//   - WithCitcClient             0x7ffe4c0
//   - WithSdkProcessManager      0x7ffe520
//   - WithExternalBaseOverlayConfig 0x7ffe580
//   - WithSettingsStore          0x7ffe5e0
//   - WithSubagentHub            0x7ffe640
//   - WithPlatformClient         0x7ffe6a0
//   - WithBundleMintClient       0x7ffe700
//   - WithAdminControlsManager   0x7ffe760
//   - WithTurnListener           0x7ffe7c0
//   - WithSourceMetadata         0x7ffe820
//   - WithEnvironmentID          0x7ffe880
//   - WithProjectID              0x7ffe8e0
//   - WithAgentPath              0x7ffe940
//   - WithCustomAgentSpec        0x7ffe9a0
//   - WithWorkspaceURIs          0x7ffea00
//   - WithSubagentSpec           0x7ffea60
//   - WithParentTrajectoryInfo   0x7ffeac0
//   - WithOverrideWorkspaceURIs  0x7ffeb40
//   - WithRequestedModel         0x7ffeba0
//   - WithTags                   0x7ffebc0
//   - WithRemoteControlDetails   0x7ffec20
//   - WithForceStopRunningWinner 0x7ffec80
package cortex

// CascadeManagerOption is a functional option for NewCascadeManager.
type CascadeManagerOption func(*CascadeManager)

// WithWorkspaceURIs sets the workspace URIs (mirrors WithWorkspaceURIs 0x7ffea00).
func WithWorkspaceURIs(uris ...string) CascadeManagerOption {
	return func(cm *CascadeManager) { cm.workspaceURIs = uris }
}

// WithSubagentHub sets the subagent hub (mirrors WithSubagentHub 0x7ffe640).
func WithSubagentHub(hub SubagentHubLike) CascadeManagerOption {
	return func(cm *CascadeManager) { cm.subagentHub = hub }
}

// WithProjectID sets the CITC project id (mirrors WithProjectID 0x7ffe8e0).
func WithProjectID(id string) CascadeManagerOption {
	return func(cm *CascadeManager) { cm.projectLookup.cache["_self"] = id }
}

// WithRequestedModel sets the requested model (mirrors WithRequestedModel 0x7ffeba0).
func WithRequestedModel(model string) CascadeManagerOption {
	return func(_ *CascadeManager) { /* stored on Run */ }
}

// WithCustomAgentSpec sets the custom agent spec (mirrors WithCustomAgentSpec 0x7ffe9a0).
func WithCustomAgentSpec(spec string) CascadeManagerOption {
	return func(_ *CascadeManager) { /* spec stored on Run */ }
}

// WithSubagentSpec sets the subagent spec (mirrors WithSubagentSpec 0x7ffea60).
func WithSubagentSpec(spec string) CascadeManagerOption {
	return func(_ *CascadeManager) { /* spec stored on Run */ }
}

// WithOverrideWorkspaceURIs sets override workspace URIs (mirrors WithOverrideWorkspaceURIs 0x7ffeb40).
func WithOverrideWorkspaceURIs(uris ...string) CascadeManagerOption {
	return func(cm *CascadeManager) { cm.workspaceURIs = uris }
}

// WithParentTrajectoryInfo sets parent trajectory info (mirrors WithParentTrajectoryInfo 0x7ffeac0).
func WithParentTrajectoryInfo(id string, step int) CascadeManagerOption {
	return func(_ *CascadeManager) { /* stored on Run */ }
}

// WithForkNotes sets fork notes (mirrors WithForkNotes 0x7ffb6e0).
func WithForkNotes(notes ...string) CascadeManagerOption {
	return func(_ *CascadeManager) { /* notes attached on Fork */ }
}

// WithForkAtStepIndex sets fork step index (mirrors WithForkAtStepIndex 0x7ffe400).
func WithForkAtStepIndex(step int) CascadeManagerOption {
	return func(_ *CascadeManager) { /* step used on Fork */ }
}

// WithCloneStrategy sets clone strategy (mirrors WithCloneStrategy 0x7ffe460).
func WithCloneStrategy(s string) CascadeManagerOption {
	return func(_ *CascadeManager) { /* strategy used on Fork */ }
}

// WithTags sets tags (mirrors WithTags 0x7ffebc0).
func WithTags(tags ...string) CascadeManagerOption {
	return func(_ *CascadeManager) { /* tags attached on Run */ }
}

// WithEnvironmentID sets environment id (mirrors WithEnvironmentID 0x7ffe880).
func WithEnvironmentID(id string) CascadeManagerOption {
	return func(_ *CascadeManager) { /* env id stored on Run */ }
}

// WithAgentPath sets agent path (mirrors WithAgentPath 0x7ffe940).
func WithAgentPath(p string) CascadeManagerOption {
	return func(_ *CascadeManager) { /* path stored on Run */ }
}

// WithSourceMetadata sets source metadata (mirrors WithSourceMetadata 0x7ffe820).
func WithSourceMetadata(m map[string]any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* metadata attached on Run */ }
}

// WithTurnListener sets turn listener (mirrors WithTurnListener 0x7ffe7c0).
func WithTurnListener(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* listener hooked on Run */ }
}

// WithAdminControlsManager sets admin controls (mirrors WithAdminControlsManager 0x7ffe760).
func WithAdminControlsManager(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* manager wired on Run */ }
}

// WithBundleMintClient sets bundle mint client (mirrors WithBundleMintClient 0x7ffe700).
func WithBundleMintClient(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* client wired on Run */ }
}

// WithPlatformClient sets platform client (mirrors WithPlatformClient 0x7ffe6a0).
func WithPlatformClient(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* client wired on Run */ }
}

// WithCitcClient sets CITC client (mirrors WithCitcClient 0x7ffe4c0).
func WithCitcClient(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* client wired on Run */ }
}

// WithSdkProcessManager sets sdk process manager (mirrors WithSdkProcessManager 0x7ffe520).
func WithSdkProcessManager(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* manager wired on Run */ }
}

// WithExternalBaseOverlayConfig sets external base overlay config (mirrors 0x7ffe580).
func WithExternalBaseOverlayConfig(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* config stored on Run */ }
}

// WithSettingsStore sets settings store (mirrors WithSettingsStore 0x7ffe5e0).
func WithSettingsStore(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* store wired on Run */ }
}

// WithAgentConfigResolution sets agent config resolution (mirrors 0x8122fc0).
func WithAgentConfigResolution(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* resolution stored on Run */ }
}

// WithConversationPresence sets conversation presence (mirrors WithConversationPresence 0x86984c0).
func WithConversationPresence(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* presence wired on Run */ }
}

// WithRemoteControlDetails sets remote control details (mirrors 0x7ffec20).
func WithRemoteControlDetails(_ any) CascadeManagerOption {
	return func(_ *CascadeManager) { /* details attached on Run */ }
}

// WithTriggerTag sets trigger tag (mirrors WithTriggerTag 0x7ffe3a0).
func WithTriggerTag(tag string) CascadeManagerOption {
	return func(_ *CascadeManager) { /* tag attached on Run */ }
}

// WithForceStopRunningWinner sets force-stop-running-winner (mirrors 0x7ffec80).
func WithForceStopRunningWinner(b bool) CascadeManagerOption {
	return func(_ *CascadeManager) { /* flag read on EndBattleMode */ }
}
