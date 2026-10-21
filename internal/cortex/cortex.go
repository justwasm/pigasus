// Package cortex — cortex.go: main CascadeManager interface + stub
// implementations, integrated 1:1 with v1.3.5 binary
// `google3/third_party/jetski/cortex/cortex` package.
//
// This file provides:
//
//   - NewCascadeManager (mirrors v1.3.5 cortex.NewCascadeManager
//     at 0x7fdb120, the constructor).
//   - CascadeManager.Run / Cancel / Shutdown / Save / Load (the
//     top-level lifecycle methods, pclntab 0x7fdd380 .Run
//     and 0x7fec1a0 .Shutdown).
//   - CascadeManager.LoadTrajectory / SaveTrajectory /
//     DeleteTrajectory / ForkConversation / GetTrajectory
//     (trajectory IO, pclntab 0x7fdda40, 0x7fddf20, 0x7fea200,
//     0x7fefe20, 0x7fdfd40 .GetTrajectory).
//   - CascadeManager.WarmUpAgentProcess (pclntab 0x7fd9340).
//   - CascadeManager.KillAgent (pclntab 0x7feb060).
//   - CascadeManager.MaybeReviveAgent (pclntab 0x7fe2160).
//   - CascadeManager.StreamAgentStateUpdates (pclntab 0x7fe9e60).
//   - CascadeManager.RequestAgentStatePageUpdate (pclntab 0x7fea1c0).
//   - CascadeManager.SubagentHub / gatedSubagentHub (pclntab
//     0x7fe46e0).
//   - CascadeManager.New (constructor, pclntab 0x7fdb120).
//
// Every method is a **stub** that returns a zero value (or
// `panic("TODO: cortex.<func>")` if the return type cannot be zero).
// Each stub carries a doc comment naming the v1.3.5 binary address,
// the original func name, and the signature. This is the cycle 42
// skeleton — flesh-out comes in later cycles.
//
// Integration with cycle 41 internal/cascade/real_lifecycle.go:
// The 6-state cascader FSM (init -> ready -> execute -> stream ->
// await_approval -> save -> done | error) is exposed in lifecycle.go
// as `Lifecycle` and mirrors the cascade.RealLifecycle API. Callers
// can construct a `cortex.NewCascadeManager(...)` and then
// `lifecycle.NewLifecycle(cm)` to drive it.
package cortex

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Errors used by the cortex package.
var (
	// ErrClosed is returned when an operation is attempted on a
	// closed CascadeManager.
	ErrClosed = errors.New("cortex: CascadeManager is closed")
	// ErrNoTrajectory is returned when a trajectory operation is
	// attempted on a CascadeManager with no stored trajectory.
	ErrNoTrajectory = errors.New("cortex: no stored trajectory")
	// ErrBattleMode is the sentinel for EndBattleModeError.
	ErrBattleMode = errors.New("cortex: battle mode end failed")
)

// NewCascadeManager returns a fresh CascadeManager. Mirrors v1.3.5
// cortex.NewCascadeManager at 0x7fdb120.
//
// The 1.3.5 binary takes a single *CascadeManager and returns a fresh
// one; we take options (zero or more WithXxx helpers below) and
// return a fresh CascadeManager.
func NewCascadeManager(opts ...CascadeManagerOption) *CascadeManager {
	cm := &CascadeManager{
		providerURLs: &providerServerURLMap{urls: map[string]string{}},
		projectLookup: &projectLookupImpl{cache: map[string]string{}},
		presence: &presenceTracker{active: map[string]*presenceEntry{}},
		bg: &backgroundRunner{},
		annotations: &AnnotationsManager{cache: map[string]Annotation{}},
		summaries: stateSyncSummariesStore{},
		cliSettings: &CLISettingsProvider{values: map[string]any{}},
		sensitivityLevel: SensitivityInternal,
	}
	for _, o := range opts {
		o(cm)
	}
	return cm
}

// CascadeManagerOption is a functional option for NewCascadeManager.
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

// ---- Top-level methods ----------------------------------------------------

// New is the recovered `(*CascadeManager).New` constructor
// (pclntab 0x7fdb120, the same address as the package-level
// cortex.NewCascadeManager). It takes a `cm *CascadeManager` (the
// receiver) and returns a fresh `*CascadeManager`.
//
// v1.3.5 binary addr 0x7fdb120, name=New, signature=(cm *CascadeManager) *CascadeManager.
func (cm *CascadeManager) New() *CascadeManager {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return NewCascadeManager()
}

// Run drives the trajectory forward. v1.3.5 binary addr 0x7fd9ce0
// (.processAgentExecution is the actual body; Run is exposed via
// ordinaryExecutor.Run at 0x7fffd80), name=Run, signature=(cm
// *CascadeManager, ctx context.Context, prompt string) error.
func (cm *CascadeManager) Run(ctx context.Context, prompt string) error {
	cm.mu.Lock()
	if cm.closed {
		cm.mu.Unlock()
		return ErrClosed
	}
	cm.mu.Unlock()
	// TODO: cortex.(*CascadeManager).Run — wire executor.Run
	return nil
}

// Cancel cancels an in-flight turn. v1.3.5 binary addr 0x7fdd480-area
// (.Cancel / .beginCreation is the entry), name=Cancel,
// signature=(cm *CascadeManager, ctx context.Context) error.
func (cm *CascadeManager) Cancel(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// CancelSteps cancels specific steps. v1.3.5 binary addr (per pclntab
// .CancelSteps area), name=CancelSteps, signature=(cm
// *CascadeManager, ctx context.Context, steps []int) error.
func (cm *CascadeManager) CancelSteps(ctx context.Context, steps []int) error {
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// AcknowledgeCodeActionStep acknowledges a code-action step. v1.3.5
// binary addr (per pclntab .AcknowledgeCodeActionStep area),
// name=AcknowledgeCodeActionStep, signature=(cm *CascadeManager, ctx
// context.Context, step int) error.
func (cm *CascadeManager) AcknowledgeCodeActionStep(ctx context.Context, step int) error {
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// Shutdown tears down the CascadeManager. v1.3.5 binary addr
// 0x7fec1a0, name=Shutdown, signature=(cm *CascadeManager, ctx
// context.Context) error.
func (cm *CascadeManager) Shutdown(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return nil
	}
	cm.closed = true
	if cm.executor != nil {
		_ = cm.executor.Close()
	}
	return nil
}

// Save persists the current trajectory. v1.3.5 binary addr 0x7fddf20
// (.SaveTrajectory), name=SaveTrajectory, signature=(cm
// *CascadeManager) error.
func (cm *CascadeManager) Save() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	if cm.storedTrajectory == "" {
		return ErrNoTrajectory
	}
	return nil
}

// SaveTrajectory persists a trajectory. v1.3.5 binary addr 0x7fddf20,
// name=SaveTrajectory, signature=(cm *CascadeManager, traj
// Trajectory) error.
func (cm *CascadeManager) SaveTrajectory(traj Trajectory) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	cm.storedTrajectory = traj.ID
	return nil
}

// LoadTrajectory loads a trajectory by id. v1.3.5 binary addr
// 0x7fdda40, name=LoadTrajectory, signature=(cm *CascadeManager, id
// string) error.
func (cm *CascadeManager) LoadTrajectory(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	cm.storedTrajectory = id
	return nil
}

// ReloadTrajectoryIfLoaded reloads a trajectory if loaded. v1.3.5
// binary addr 0x7fddc00, name=ReloadTrajectoryIfLoaded,
// signature=(cm *CascadeManager) error.
func (cm *CascadeManager) ReloadTrajectoryIfLoaded() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// DeleteTrajectory deletes a trajectory. v1.3.5 binary addr
// 0x7fea200, name=DeleteTrajectory, signature=(cm *CascadeManager,
// id string) error.
func (cm *CascadeManager) DeleteTrajectory(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// GetTrajectory returns the current trajectory. v1.3.5 binary addr
// (per pclntab .GetTrajectory), name=GetTrajectory,
// signature=(cm *CascadeManager) (Trajectory, error).
func (cm *CascadeManager) GetTrajectory() (Trajectory, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return Trajectory{}, ErrClosed
	}
	if cm.storedTrajectory == "" {
		return Trajectory{}, ErrNoTrajectory
	}
	return Trajectory{ID: cm.storedTrajectory}, nil
}

// GetTrajectoryForServing returns the trajectory for serving. v1.3.5
// binary addr (per pclntab .GetTrajectoryForServing),
// name=GetTrajectoryForServing, signature=(cm *CascadeManager) (Trajectory, error).
func (cm *CascadeManager) GetTrajectoryForServing() (Trajectory, error) {
	return cm.GetTrajectory()
}

// GetMainAgentTrajectories returns the main agent trajectories. v1.3.5
// binary addr 0x7fe10e0, name=GetMainAgentTrajectories,
// signature=(cm *CascadeManager) ([]Trajectory, error).
func (cm *CascadeManager) GetMainAgentTrajectories() ([]Trajectory, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return nil, ErrClosed
	}
	return nil, nil
}

// GetAllTrajectories returns all trajectories. v1.3.5 binary addr
// (per pclntab .GetAllTrajectories), name=GetAllTrajectories,
// signature=(cm *CascadeManager) ([]Trajectory, error).
func (cm *CascadeManager) GetAllTrajectories() ([]Trajectory, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return nil, ErrClosed
	}
	return nil, nil
}

// ResidentTrajectoryCount returns the count of resident trajectories.
// v1.3.5 binary addr (per pclntab .ResidentTrajectoryCount),
// name=ResidentTrajectoryCount, signature=(cm *CascadeManager) int.
func (cm *CascadeManager) ResidentTrajectoryCount() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.storedTrajectory == "" {
		return 0
	}
	return 1
}

// ForkConversation forks the current conversation. v1.3.5 binary addr
// 0x7fefe20, name=ForkConversation, signature=(cm *CascadeManager,
// opts ...ForkOption) (string, error).
func (cm *CascadeManager) ForkConversation(opts ...CascadeManagerOption) (string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return "", ErrClosed
	}
	return "fork-" + cm.storedTrajectory, nil
}

// ---- Battle mode ----------------------------------------------------------

// StartBattleMode starts a battle-mode run. v1.3.5 binary addr
// 0x7fc7f60, name=StartBattleMode, signature=(cm *CascadeManager,
// opts ...BattleModeOption) error.
func (cm *CascadeManager) StartBattleMode(opts ...CascadeManagerOption) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	cm.battleMode = &BattleModeState{StartedAt: time.Now()}
	return nil
}

// EndBattleMode ends a battle-mode run. v1.3.5 binary addr 0x7fcbb00,
// name=EndBattleMode, signature=(cm *CascadeManager) error.
func (cm *CascadeManager) EndBattleMode() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	cm.battleMode = nil
	return nil
}

// ValidateBattleMode validates a battle-mode run. v1.3.5 binary addr
// 0x7fc78c0, name=ValidateBattleMode, signature=(cm *CascadeManager)
// error.
func (cm *CascadeManager) ValidateBattleMode() error {
	return nil
}

// EnsureBattleModePreconditions ensures battle-mode preconditions.
// v1.3.5 binary addr 0x7fc77a0, name=EnsureBattleModePreconditions,
// signature=(cm *CascadeManager) error.
func (cm *CascadeManager) EnsureBattleModePreconditions() error {
	return nil
}

// EliminateBattleModeArm eliminates a battle-mode arm. v1.3.5 binary
// addr 0x7fd28a0, name=EliminateBattleModeArm, signature=(cm
// *CascadeManager, index int) error.
func (cm *CascadeManager) EliminateBattleModeArm(index int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.battleMode == nil {
		return errors.New("cortex: not in battle mode")
	}
	if index < 0 || index >= len(cm.battleMode.Arms) {
		return fmt.Errorf("cortex: arm index %d out of range [0,%d)", index, len(cm.battleMode.Arms))
	}
	cm.battleMode.Arms[index].Status = "eliminated"
	return nil
}

// ---- Sensitivity level ----------------------------------------------------

// UpdateLoadedSensitivityLevel updates the loaded sensitivity level.
// v1.3.5 binary addr 0x7fda920, name=UpdateLoadedSensitivityLevel,
// signature=(cm *CascadeManager, level SensitivityLevel) error.
func (cm *CascadeManager) UpdateLoadedSensitivityLevel(level SensitivityLevel) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	cm.sensitivityLevel = level
	return nil
}

// PersistSensitivityLevel persists the sensitivity level. v1.3.5 binary
// addr 0x7fdac20, name=PersistSensitivityLevel, signature=(cm
// *CascadeManager) error.
func (cm *CascadeManager) PersistSensitivityLevel() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// loadedSensitivityLevel returns the loaded sensitivity level. v1.3.5
// binary addr 0x7fdf720, name=loadedSensitivityLevel,
// signature=(cm *CascadeManager) SensitivityLevel.
func (cm *CascadeManager) loadedSensitivityLevel() SensitivityLevel {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.sensitivityLevel
}

// ---- Annotations ----------------------------------------------------------

// GetAnnotations returns the annotations for a path. v1.3.5 binary
// addr 0x7fc4100, name=GetAnnotations, signature=(cm
// *CascadeManager, path string) (Annotation, bool).
func (cm *CascadeManager) GetAnnotations(path string) (Annotation, bool) {
	cm.annotations.mu.Lock()
	defer cm.annotations.mu.Unlock()
	a, ok := cm.annotations.cache[path]
	return a, ok
}

// UpdateAnnotations updates annotations for a path. v1.3.5 binary
// addr 0x7fc4360, name=UpdateAnnotations, signature=(cm
// *CascadeManager, a Annotation) error.
func (cm *CascadeManager) UpdateAnnotations(a Annotation) error {
	cm.annotations.mu.Lock()
	defer cm.annotations.mu.Unlock()
	a.Modified = time.Now()
	cm.annotations.cache[a.Path] = a
	return nil
}

// Mutate atomically mutates annotations. v1.3.5 binary addr 0x7fc4660,
// name=Mutate, signature=(cm *CascadeManager, fn func(map[string]Annotation)) error.
func (cm *CascadeManager) Mutate(fn func(map[string]Annotation)) error {
	cm.annotations.mu.Lock()
	defer cm.annotations.mu.Unlock()
	fn(cm.annotations.cache)
	return nil
}

// DeleteAnnotations deletes annotations for a path. v1.3.5 binary
// addr 0x7fc4d00, name=DeleteAnnotations, signature=(cm
// *CascadeManager, path string) error.
func (cm *CascadeManager) DeleteAnnotations(path string) error {
	cm.annotations.mu.Lock()
	defer cm.annotations.mu.Unlock()
	delete(cm.annotations.cache, path)
	return nil
}

// ---- Subagent hub ---------------------------------------------------------

// SubagentHub returns the (un-gated) subagent hub. v1.3.5 binary
// addr (per pclntab .SubagentHub), name=SubagentHub, signature=(cm
// *CascadeManager) SubagentHubLike.
func (cm *CascadeManager) SubagentHub() SubagentHubLike {
	return cm.subagentHub
}

// gatedSubagentHub returns the gated subagent hub. v1.3.5 binary
// addr 0x7fe46e0, name=gatedSubagentHub, signature=(cm
// *CascadeManager) SubagentGate.
func (cm *CascadeManager) gatedSubagentHub() SubagentGate {
	if cm.subagentHub == nil {
		return nil
	}
	return cm.subagentHub.Gate()
}

// ---- Agent state / killing / reviving ------------------------------------

// KillAgent kills a subagent. v1.3.5 binary addr 0x7feb060,
// name=KillAgent, signature=(cm *CascadeManager, subagentID string) error.
func (cm *CascadeManager) KillAgent(subagentID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	if cm.presence != nil {
		cm.presence.remove(subagentID)
	}
	return nil
}

// MaybeReviveAgent revives a subagent if it can be revived. v1.3.5
// binary addr 0x7fe2160, name=MaybeReviveAgent, signature=(cm
// *CascadeManager, subagentID string) error.
func (cm *CascadeManager) MaybeReviveAgent(subagentID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// WarmUpAgentProcess warms up the agent process. v1.3.5 binary addr
// 0x7fd9340, name=WarmUpAgentProcess, signature=(cm *CascadeManager) error.
func (cm *CascadeManager) WarmUpAgentProcess() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// StreamAgentStateUpdates streams agent state updates. v1.3.5 binary
// addr 0x7fe9e60, name=StreamAgentStateUpdates, signature=(cm
// *CascadeManager) <-chan AgentStateUpdate.
func (cm *CascadeManager) StreamAgentStateUpdates() <-chan AgentStateUpdate {
	ch := make(chan AgentStateUpdate, 16)
	return ch
}

// RequestAgentStatePageUpdate requests an agent state page update.
// v1.3.5 binary addr 0x7fea1c0, name=RequestAgentStatePageUpdate,
// signature=(cm *CascadeManager, page int) error.
func (cm *CascadeManager) RequestAgentStatePageUpdate(page int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// IsCascadeRunning reports whether the cascade is running. v1.3.5
// binary addr (per pclntab .IsCascadeRunning), name=IsCascadeRunning,
// signature=(cm *CascadeManager) bool.
func (cm *CascadeManager) IsCascadeRunning() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.executor != nil
}

// IsQuiesced reports whether the cascade is quiesced. v1.3.5 binary
// addr 0x7fecd80, name=IsQuiesced, signature=(cm *CascadeManager) bool.
func (cm *CascadeManager) IsQuiesced() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.closed
}

// ExecutionStatus returns the execution status. v1.3.5 binary addr
// (per pclntab .ExecutionStatus), name=ExecutionStatus,
// signature=(cm *CascadeManager) ExecutorLoopStatus.
func (cm *CascadeManager) ExecutionStatus() ExecutorLoopStatus {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return LoopStatusQuiesced
	}
	if cm.executor == nil {
		return LoopStatusIdle
	}
	return LoopStatusBusy
}

// GetActivitySnapshot returns the activity snapshot. v1.3.5 binary
// addr 0x7fecde0, name=GetActivitySnapshot, signature=(cm
// *CascadeManager) ActivitySnapshot.
func (cm *CascadeManager) GetActivitySnapshot() ActivitySnapshot {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return ActivitySnapshot{
		TrajectoryID:  cm.storedTrajectory,
		RunningCount:  cm.presence.count(),
		BattleMode:    cm.battleMode != nil,
		Sensitivity:   cm.sensitivityLevel,
		LastUpdateUTC: time.Now().UTC(),
	}
}

// ActivitySnapshot is a snapshot of the cascade's activity (mirrors
// the v1.3.5 (*CascadeManager).GetActivitySnapshot return value).
type ActivitySnapshot struct {
	TrajectoryID  string
	RunningCount  int
	BattleMode    bool
	Sensitivity   SensitivityLevel
	LastUpdateUTC time.Time
}

// WaitForConversationFullyIdle blocks until the conversation is idle.
// v1.3.5 binary addr 0x7febc20, name=WaitForConversationFullyIdle,
// signature=(cm *CascadeManager, ctx context.Context) error.
func (cm *CascadeManager) WaitForConversationFullyIdle(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// ResumeConversationsFromRestart resumes conversations after a
// restart. v1.3.5 binary addr 0x7fdd380, name=ResumeConversationsFromRestart,
// signature=(cm *CascadeManager, ctx context.Context) error.
func (cm *CascadeManager) ResumeConversationsFromRestart(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// ForceStopTree force-stops the trajectory tree. v1.3.5 binary addr
// (per pclntab .ForceStopTree), name=ForceStopTree, signature=(cm
// *CascadeManager) error.
func (cm *CascadeManager) ForceStopTree() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// RevertToStep reverts the trajectory to a step. v1.3.5 binary addr
// (per pclntab .RevertToStep), name=RevertToStep, signature=(cm
// *CascadeManager, step int) error.
func (cm *CascadeManager) RevertToStep(step int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// GetRevertPreview returns the revert preview. v1.3.5 binary addr
// (per pclntab .GetRevertPreview), name=GetRevertPreview,
// signature=(cm *CascadeManager, step int) (RevertPreview, error).
func (cm *CascadeManager) GetRevertPreview(step int) (RevertPreview, error) {
	return RevertPreview{StepIndex: step}, nil
}

// RevertPreview is the recovered revert preview.
type RevertPreview struct {
	StepIndex  int
	Diff       string
	Affected   []string
}

// SendUserCascadeMessage sends a user message to the cascade. v1.3.5
// binary addr (per pclntab .SendUserCascadeMessage),
// name=SendUserCascadeMessage, signature=(cm *CascadeManager, msg
// string) error.
func (cm *CascadeManager) SendUserCascadeMessage(msg string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// HandleUserInteraction handles a user interaction. v1.3.5 binary
// addr (per pclntab .HandleUserInteraction),
// name=HandleUserInteraction, signature=(cm *CascadeManager, ev
// UserInteractionEvent) error.
func (cm *CascadeManager) HandleUserInteraction(ev UserInteractionEvent) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// UserInteractionEvent is the recovered user interaction event.
type UserInteractionEvent struct {
	Kind    string
	SubagentID string
	Payload map[string]any
}

// ---- Conversation summaries -----------------------------------------------

// ListConversationSummaries lists conversation summaries. v1.3.5
// binary addr 0x7fed0a0, name=ListConversationSummaries,
// signature=(cm *CascadeManager) ([]Summary, error).
func (cm *CascadeManager) ListConversationSummaries() ([]Summary, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return nil, ErrClosed
	}
	return nil, nil
}

// ConversationSummary returns a single conversation summary. v1.3.5
// binary addr 0x7fed660, name=ConversationSummary, signature=(cm
// *CascadeManager, id string) (Summary, error).
func (cm *CascadeManager) ConversationSummary(id string) (Summary, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return Summary{}, ErrClosed
	}
	return Summary{TrajectoryID: id}, nil
}

// UpdateSummaryForID updates the summary for an id. v1.3.5 binary addr
// 0x7fda520, name=UpdateSummaryForID, signature=(cm *CascadeManager,
// id string, summary Summary) error.
func (cm *CascadeManager) UpdateSummaryForID(id string, summary Summary) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return cm.summaries.WriteSummary(id, summary)
}

// UpdateSummaryForTrajectory updates the summary for a trajectory.
// v1.3.5 binary addr 0x7fda820, name=UpdateSummaryForTrajectory,
// signature=(cm *CascadeManager, traj Trajectory) error.
func (cm *CascadeManager) UpdateSummaryForTrajectory(traj Trajectory) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// ---- CLI settings ---------------------------------------------------------

// SetInMemoryCLISettingsProvider sets the in-memory CLI settings
// provider. v1.3.5 binary addr 0x7fdaec0,
// name=SetInMemoryCLISettingsProvider, signature=(cm
// *CascadeManager, p *CLISettingsProvider) error.
func (cm *CascadeManager) SetInMemoryCLISettingsProvider(p *CLISettingsProvider) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cliSettings = p
	return nil
}

// GetInMemoryCLISettingsProvider returns the in-memory CLI settings
// provider. v1.3.5 binary addr 0x7fdb040,
// name=GetInMemoryCLISettingsProvider, signature=(cm
// *CascadeManager) (*CLISettingsProvider, error).
func (cm *CascadeManager) GetInMemoryCLISettingsProvider() (*CLISettingsProvider, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.cliSettings == nil {
		return nil, errors.New("cortex: no in-memory CLI settings provider")
	}
	return cm.cliSettings, nil
}

// ---- Workspace ------------------------------------------------------------

// ResolveWorkspaceURIs resolves workspace URIs. v1.3.5 binary addr
// 0x7fd72e0, name=ResolveWorkspaceURIs, signature=(cm
// *CascadeManager) ([]string, error).
func (cm *CascadeManager) ResolveWorkspaceURIs() ([]string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return append([]string(nil), cm.workspaceURIs...), nil
}

// CleanupMappedWorkspaces cleans up mapped workspaces. v1.3.5 binary
// addr 0x7fd73e0, name=CleanupMappedWorkspaces, signature=(cm
// *CascadeManager) error.
func (cm *CascadeManager) CleanupMappedWorkspaces() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.workspaceURIs = nil
	return nil
}

// CleanUpWorkspaces cleans up workspaces. v1.3.5 binary addr
// 0x7fd74e0, name=CleanUpWorkspaces, signature=(cm *CascadeManager)
// error.
func (cm *CascadeManager) CleanUpWorkspaces() error {
	return cm.CleanupMappedWorkspaces()
}

// CleanUpWorktreesInArtifacts cleans up worktrees in artifacts. v1.3.5
// binary addr 0x7fd7bc0, name=CleanUpWorktreesInArtifacts,
// signature=(cm *CascadeManager) error.
func (cm *CascadeManager) CleanUpWorktreesInArtifacts() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return nil
}

// ---- Configuration --------------------------------------------------------

// ResolveStaticConfig resolves the static config. v1.3.5 binary addr
// 0x7fd91c0, name=ResolveStaticConfig, signature=(cm
// *CascadeManager) (StaticConfig, error).
func (cm *CascadeManager) ResolveStaticConfig() (StaticConfig, error) {
	return StaticConfig{}, nil
}

// ResolveExecutableScript resolves the executable script. v1.3.5
// binary addr 0x7fd9140, name=ResolveExecutableScript, signature=(cm
// *CascadeManager) (string, error).
func (cm *CascadeManager) ResolveExecutableScript() (string, error) {
	return "", nil
}

// GetResolvedCascadeConfig returns the resolved cascade config. v1.3.5
// binary addr (per pclntab .GetResolvedCascadeConfig),
// name=GetResolvedCascadeConfig, signature=(cm *CascadeManager)
// (CascadeConfig, error).
func (cm *CascadeManager) GetResolvedCascadeConfig() (CascadeConfig, error) {
	return CascadeConfig{}, nil
}

// PreviousContribVariant returns the previous contrib variant. v1.3.5
// binary addr 0x7fd9240, name=PreviousContribVariant, signature=(cm
// *CascadeManager) (string, error).
func (cm *CascadeManager) PreviousContribVariant() (string, error) {
	return "", nil
}

// TrajectoryStoreManager returns the trajectory store manager. v1.3.5
// binary addr 0x7fecea0, name=TrajectoryStoreManager, signature=(cm
// *CascadeManager) TrajectoryStoreManager.
func (cm *CascadeManager) TrajectoryStoreManager() *TrajectoryStoreManagerHandle {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return &TrajectoryStoreManagerHandle{cm: cm}
}

// TrajectoryStoreManagerHandle is a placeholder for the v1.3.5
// returned object (the decompiled body is opaque in the
// `(*CascadeManager).TrajectoryStoreManager` .c file).
type TrajectoryStoreManagerHandle struct {
	cm *CascadeManager
}

// ViewTrajectoryDoNotUse is a debug-only trajectory viewer. v1.3.5
// binary addr (per pclntab .ViewTrajectoryDoNotUse),
// name=ViewTrajectoryDoNotUse, signature=(cm *CascadeManager) (string, error).
func (cm *CascadeManager) ViewTrajectoryDoNotUse() (string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.storedTrajectory, nil
}

// ---- Static configuration types ------------------------------------------

// CascadeConfig is the recovered cascade config (mirrors v1.3.5
// `CascadeConfig` proto, see decompiled cortex_pb/cortex_go_proto
// at 0x68323a0).
type CascadeConfig struct {
	AgentAPIConfig *AgentAPIConfig
	Model          string
	Tools          []ToolConfig
}

// AgentAPIConfig is the recovered agent API config.
type AgentAPIConfig struct {
	URL    string
	APIKey string
	Model  string
}

// ToolConfig is the recovered tool config.
type ToolConfig struct {
	Name     string
	Enabled  bool
	Settings map[string]any
}

// StaticConfig is the recovered static config (mirrors the v1.3.5
// `(*CascadeManager).ResolveStaticConfig` return).
type StaticConfig struct {
	Models  []string
	Plugins []string
	Flags   map[string]any
}

// ---- presenceTracker impl -------------------------------------------------

// count returns the number of active presences.
func (p *presenceTracker) count() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.active)
}

// remove removes a presence by id.
func (p *presenceTracker) remove(id string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.active, id)
}

// ---- backgroundRunner impl -----------------------------------------------

// Go runs a function in the background. v1.3.5 binary addr 0x7fd6e00,
// name=Go, signature=(bg *backgroundRunner, name string, fn func()).
func (bg *backgroundRunner) Go(name string, fn func()) {
	bg.mu.Lock()
	task := bgTask{Name: name, Stop: make(chan struct{}), Done: make(chan struct{})}
	bg.tasks = append(bg.tasks, task)
	bg.mu.Unlock()
	go func() {
		defer close(task.Done)
		fn()
	}()
}

// CancelAndWait cancels all background tasks and waits. v1.3.5 binary
// addr 0x7fd6e60, name=CancelAndWait, signature=(bg
// *backgroundRunner) error.
func (bg *backgroundRunner) CancelAndWait() error {
	bg.mu.Lock()
	tasks := append([]bgTask(nil), bg.tasks...)
	bg.mu.Unlock()
	for _, t := range tasks {
		close(t.Stop)
		<-t.Done
	}
	return nil
}

// ---- stateSyncSummariesStore impl ---------------------------------------

// WriteSummary writes a summary. v1.3.5 binary addr 0x7ff4680,
// name=WriteSummary, signature=(s stateSyncSummariesStore, id string,
// summary Summary) error.
func (s *stateSyncSummariesStore) WriteSummary(id string, summary Summary) error {
	if s.backend == nil {
		return nil
	}
	return s.backend.WriteSummary(id, summary)
}

// DeleteSummary deletes a summary. v1.3.5 binary addr 0x7ff4700,
// name=DeleteSummary, signature=(s stateSyncSummariesStore, id string) error.
func (s *stateSyncSummariesStore) DeleteSummary(id string) error {
	if s.backend == nil {
		return nil
	}
	return s.backend.DeleteSummary(id)
}

// GetSummary returns a summary. (Not in pclntab directly but
// SummariesStore interface requires it.)
func (s *stateSyncSummariesStore) GetSummary(id string) (Summary, bool) {
	if s.backend == nil {
		return Summary{}, false
	}
	return s.backend.GetSummary(id)
}

// ---- jetboxSummariesStoreAdapter impl -----------------------------------

// WriteSummary writes a summary. v1.3.5 binary addr 0x7ff47a0,
// name=WriteSummary, signature=(j *jetboxSummariesStoreAdapter, id
// string, summary Summary) error.
func (j *jetboxSummariesStoreAdapter) WriteSummary(id string, summary Summary) error {
	return j.delegate.WriteSummary(j.prefix + ":" + id, summary)
}

// DeleteSummary deletes a summary. v1.3.5 binary addr 0x7ff4820,
// name=DeleteSummary, signature=(j *jetboxSummariesStoreAdapter, id
// string) error.
func (j *jetboxSummariesStoreAdapter) DeleteSummary(id string) error {
	return j.delegate.DeleteSummary(j.prefix + ":" + id)
}

// GetSummary returns a summary. v1.3.5 binary addr 0x7ff48a0,
// name=GetSummary, signature=(j *jetboxSummariesStoreAdapter, id
// string) (Summary, bool).
func (j *jetboxSummariesStoreAdapter) GetSummary(id string) (Summary, bool) {
	return j.delegate.GetSummary(j.prefix + ":" + id)
}

// ---- providerServerURLMap impl -----------------------------------------

// get returns the URL for a provider. v1.3.5 binary addr 0x7fec5e0,
// name=get, signature=(m *providerServerURLMap, provider string) string.
func (m *providerServerURLMap) get(provider string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.urls[provider]
}

// update sets the URL for a provider. v1.3.5 binary addr 0x7fec480,
// name=update, signature=(m *providerServerURLMap, provider string,
// url string).
func (m *providerServerURLMap) update(provider, url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.urls[provider] = url
}

// remove removes a provider. v1.3.5 binary addr (inferred from pclntab),
// name=remove, signature=(m *providerServerURLMap, provider string).
func (m *providerServerURLMap) remove(provider string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.urls, provider)
}

// ---- projectLookupImpl impl --------------------------------------------

// GetProjectIDForConversationID returns the project id for a
// conversation. v1.3.5 binary addr 0x7feb960,
// name=GetProjectIDForConversationID, signature=(p
// *projectLookupImpl, convID string) string.
func (p *projectLookupImpl) GetProjectIDForConversationID(convID string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cache[convID]
}

// ---- Executors (stub) ---------------------------------------------------

// Close closes the ordinary executor. v1.3.5 binary addr 0x7fff860,
// name=Close, signature=(e *ordinaryExecutor) error.
func (e *ordinaryExecutor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

// Run runs the ordinary executor. v1.3.5 binary addr 0x7fffd80,
// name=Run, signature=(e *ordinaryExecutor, ctx ExecContext, prompt
// string) error.
func (e *ordinaryExecutor) Run(_ ExecContext, _ string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	return nil
}

// Send sends a message. v1.3.5 binary addr 0x7fffe20, name=Send,
// signature=(e *ordinaryExecutor, msg AgentMessage) error.
func (e *ordinaryExecutor) Send(_ AgentMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	return nil
}

// Listen returns the agent state update channel. v1.3.5 binary addr
// 0x7fffc60, name=Listen, signature=(e *ordinaryExecutor) <-chan
// AgentStateUpdate.
func (e *ordinaryExecutor) Listen() <-chan AgentStateUpdate {
	return make(chan AgentStateUpdate)
}

// MaybeRevive revives the executor. v1.3.5 binary addr 0x7fffce0,
// name=MaybeRevive, signature=(e *ordinaryExecutor) error.
func (e *ordinaryExecutor) MaybeRevive() error {
	return nil
}

// GetTrajectory returns the trajectory. v1.3.5 binary addr 0x7fffaa0,
// name=GetTrajectory, signature=(e *ordinaryExecutor) (Trajectory, error).
func (e *ordinaryExecutor) GetTrajectory() (Trajectory, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return Trajectory{ID: e.trajectoryID}, nil
}

// GetAgentState returns the agent state. v1.3.5 binary addr 0x7fff8c0,
// name=GetAgentState, signature=(e *ordinaryExecutor) AgentState.
func (e *ordinaryExecutor) GetAgentState() AgentState {
	return AgentState{Status: SubagentRunning, TrajectoryID: e.trajectoryID}
}

// GetExecutorLoopStatus returns the loop status. v1.3.5 binary addr
// 0x7fff920, name=GetExecutorLoopStatus, signature=(e
// *ordinaryExecutor) ExecutorLoopStatus.
func (e *ordinaryExecutor) GetExecutorLoopStatus() ExecutorLoopStatus {
	return LoopStatusIdle
}

// GetExecutorLoopStatusAndLastIdleTime returns the loop status and last
// idle time. v1.3.5 binary addr 0x7fff980,
// name=GetExecutorLoopStatusAndLastIdleTime, signature=(e
// *ordinaryExecutor) (ExecutorLoopStatus, time.Time).
func (e *ordinaryExecutor) GetExecutorLoopStatusAndLastIdleTime() (ExecutorLoopStatus, time.Time) {
	return LoopStatusIdle, time.Now()
}

// GetRunStateInternal returns the run state. v1.3.5 binary addr
// 0x7fff9a0, name=GetRunStateInternal, signature=(e
// *ordinaryExecutor) interface{}.
func (e *ordinaryExecutor) GetRunStateInternal() interface{} {
	return nil
}

// HandleExecutionError handles an execution error. v1.3.5 binary addr
// 0x7fffbc0, name=HandleExecutionError, signature=(e
// *ordinaryExecutor, err error) error.
func (e *ordinaryExecutor) HandleExecutionError(err error) error {
	return err
}

// SendBookmark sends a bookmark. v1.3.5 binary addr 0x7fffea0,
// name=SendBookmark, signature=(e *ordinaryExecutor, b Bookmark) error.
func (e *ordinaryExecutor) SendBookmark(_ Bookmark) error {
	return nil
}

// WaitAndFlushSnapshot waits for the snapshot to flush. v1.3.5 binary
// addr 0x7ffff20, name=WaitAndFlushSnapshot, signature=(e
// *ordinaryExecutor) error.
func (e *ordinaryExecutor) WaitAndFlushSnapshot() error {
	return nil
}

// Close closes the sdk executor. v1.3.5 binary addr 0x7ffedc0,
// name=Close, signature=(e *sdkExecutor) error.
func (e *sdkExecutor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

// Run runs the sdk executor. v1.3.5 binary addr 0x7fff640, name=Run,
// signature=(e *sdkExecutor, ctx ExecContext, prompt string) error.
func (e *sdkExecutor) Run(_ ExecContext, _ string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	return nil
}

// Send sends a message. v1.3.5 binary addr 0x7fec700, name=Send,
// signature=(e *sdkExecutor, msg AgentMessage) error.
func (e *sdkExecutor) Send(_ AgentMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	return nil
}

// Listen returns the agent state update channel. v1.3.5 binary addr
// 0x7fff480, name=Listen, signature=(e *sdkExecutor) <-chan
// AgentStateUpdate.
func (e *sdkExecutor) Listen() <-chan AgentStateUpdate {
	return make(chan AgentStateUpdate)
}

// MaybeRevive revives the sdk executor. v1.3.5 binary addr 0x7fff560,
// name=MaybeRevive, signature=(e *sdkExecutor) error.
func (e *sdkExecutor) MaybeRevive() error {
	return nil
}

// GetTrajectory returns the trajectory. v1.3.5 binary addr 0x7fff240,
// name=GetTrajectory, signature=(e *sdkExecutor) (Trajectory, error).
func (e *sdkExecutor) GetTrajectory() (Trajectory, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return Trajectory{ID: e.trajectoryID}, nil
}

// GetAgentState returns the agent state. v1.3.5 binary addr 0x7ffee80,
// name=GetAgentState, signature=(e *sdkExecutor) AgentState.
func (e *sdkExecutor) GetAgentState() AgentState {
	return AgentState{Status: SubagentRunning, TrajectoryID: e.trajectoryID}
}

// GetExecutorLoopStatus returns the loop status. v1.3.5 binary addr
// 0x7ffef40, name=GetExecutorLoopStatus, signature=(e *sdkExecutor)
// ExecutorLoopStatus.
func (e *sdkExecutor) GetExecutorLoopStatus() ExecutorLoopStatus {
	return LoopStatusIdle
}

// GetExecutorLoopStatusAndLastIdleTime returns the loop status and last
// idle time. v1.3.5 binary addr 0x7fff000,
// name=GetExecutorLoopStatusAndLastIdleTime, signature=(e
// *sdkExecutor) (ExecutorLoopStatus, time.Time).
func (e *sdkExecutor) GetExecutorLoopStatusAndLastIdleTime() (ExecutorLoopStatus, time.Time) {
	return LoopStatusIdle, time.Now()
}

// GetRunStateInternal returns the run state. v1.3.5 binary addr
// 0x7fff020, name=GetRunStateInternal, signature=(e *sdkExecutor)
// interface{}.
func (e *sdkExecutor) GetRunStateInternal() interface{} {
	return nil
}

// HandleExecutionError handles an execution error. v1.3.5 binary addr
// 0x7fff3a0, name=HandleExecutionError, signature=(e *sdkExecutor,
// err error) error.
func (e *sdkExecutor) HandleExecutionError(err error) error {
	return err
}

// SendBookmark sends a bookmark. v1.3.5 binary addr 0x7fff720,
// name=SendBookmark, signature=(e *sdkExecutor, b Bookmark) error.
func (e *sdkExecutor) SendBookmark(_ Bookmark) error {
	return nil
}

// WaitAndFlushSnapshot waits for the snapshot to flush. v1.3.5 binary
// addr 0x7fff800, name=WaitAndFlushSnapshot, signature=(e
// *sdkExecutor) error.
func (e *sdkExecutor) WaitAndFlushSnapshot() error {
	return nil
}

// WarmUpAgentProcess warms up the agent process. v1.3.5 binary addr
// 0x7fd9400, name=WarmUpAgentProcess, signature=(e *sdkExecutor) error.
func (e *sdkExecutor) WarmUpAgentProcess() error {
	return nil
}

// executeScript executes a script. v1.3.5 binary addr 0x7fd8600,
// name=executeScript, signature=(e *sdkExecutor, script string) error.
func (e *sdkExecutor) executeScript(_ string) error {
	return nil
}

// getDefaultWorkspaceCwd returns the default workspace cwd. v1.3.5
// binary addr 0x7fd6ea0, name=getDefaultWorkspaceCwd, signature=(e
// *sdkExecutor) string.
func (e *sdkExecutor) getDefaultWorkspaceCwd() string {
	return ""
}

// getWorkspaceCwd returns the workspace cwd. v1.3.5 binary addr
// 0x7fd7040, name=getWorkspaceCwd, signature=(e *sdkExecutor, ws
// WorkspaceProject) string.
func (e *sdkExecutor) getWorkspaceCwd(_ WorkspaceProject) string {
	return ""
}

// surfaceAgentProcessError surfaces an agent process error. v1.3.5
// binary addr 0x7fd9900, name=surfaceAgentProcessError, signature=(e
// *sdkExecutor, err error) error.
func (e *sdkExecutor) surfaceAgentProcessError(err error) error {
	return err
}

// updateSdkExecutableStatus updates the sdk executable status. v1.3.5
// binary addr 0x7fda160, name=updateSdkExecutableStatus,
// signature=(e *sdkExecutor, status string) error.
func (e *sdkExecutor) updateSdkExecutableStatus(_ string) error {
	return nil
}

// updateSdkExecutableStatusLocked updates the sdk executable status
// (caller holds lock). v1.3.5 binary addr 0x7fda2e0,
// name=updateSdkExecutableStatusLocked, signature=(e *sdkExecutor,
// status string) error.
func (e *sdkExecutor) updateSdkExecutableStatusLocked(_ string) error {
	return nil
}
