// Package cortex — types.go: all recovered 1.3.5 v1.3.5 binary struct /
// interface type definitions for the cortex package.
//
// Source: `/workspace/agy/install/antigravity-v1.3.5`
// strings dump: 10,577 hits under `google3/third_party/jetski/cortex/`.
// pclntab entries: ~397 entries under `cortex/cortex.*` (see
// docs/analysis-v1.3.5-official/funcs-v1.3.5-pclntab.txt).
//
// Type inventory (1:1 with v1.3.5 binary pclntab, recovered
// receiver-prefixed methods; from decompiled
// docs/decompiled-v1.3.5/c/google3_third_party_jetski_cortex_cortex/ 33 .c
// files + the pclntab grep `grep -oE 'cortex/cortex\.\(\*[A-Za-z0-9_]+\)'`):
//
//   - CascadeManager          — the central orchestrator (v1.3.5
//     address 0x7fc3e40 .. 0x8000000+, ~150 methods including
//     New/Run/LoadTrajectory/ForkConversation/StartBattleMode/...).
//   - AnnotationsManager      — annotation persistence (0x7fc3f20..).
//   - EndBattleModeError      — error type implementing error + Unwrap.
//   - backgroundRunner        — goroutine registry (Go/CancelAndWait).
//   - jetboxSummariesStoreAdapter — state-sync adapter for jetbox
//     summaries store.
//   - nopPlacer               — no-op workspace placer.
//   - ordinaryExecutor        — standard executor (Run/Send/Close/...).
//   - presenceTracker         — subagent presence acquire/probe/release/remove.
//   - projectLookupImpl       — CITC project id resolver.
//   - providerServerURLMap    — provider URL map (update/get/remove).
//   - sdkExecutor             — SDK executor (Run/Send/Listen/...).
//   - stateSyncSummariesStore — state-sync summaries store
//     (WriteSummary/DeleteSummary).
//
// All types are recovered as Go stubs here. Each one carries the
// v1.3.5 binary address + decompiled .c source filename as a doc
// comment so a downstream verifier can map field-by-field to the
// recovered binary.
package cortex

import (
	"sync"
	"time"
)

// ---- Core orchestrator ----------------------------------------------------

// CascadeManager is the recovered 1.3.5 `(*CascadeManager)` from
// google3/third_party/jetski/cortex/cortex.
//
// pclntab entry: `cortex/cortex.(*CascadeManager).New` (0x7fdb120, the
// constructor) + ~150 methods spanning lifecycle, battle mode, fork,
// trajectory, sensitivity, and subagent management.
//
// This is the **central** orchestrator: it owns the conversation, the
// trajectory store, the workspace URIs, the subagent hub, the
// presence tracker, the executor (sdkExecutor / ordinaryExecutor),
// and the agent script. Everything in the v1.3.5 binary that talks
// to the LLM goes through a CascadeManager.
//
// Stub-only — fields are not yet mapped 1:1 to the v1.3.5 binary
// layout. They will be filled in by future cycles.
type CascadeManager struct {
	mu sync.Mutex

	// Trajectory / store
	storedTrajectory string // cached trajectory id (storedTrajectory 0x7fd8ec0)
	// Executor
	executor Executor // sdkExecutor or ordinaryExecutor
	// Subagent hub (gatedSubagentHub 0x7fe46e0, SubagentHub getter)
	subagentHub SubagentHubLike
	// Workspace
	workspaceURIs []string // resolved workspace URIs
	// Sensitivity
	sensitivityLevel SensitivityLevel
	// Battle mode
	battleMode *BattleModeState // nil when not in battle mode
	// Background runner
	bg *backgroundRunner
	// Presence tracker
	presence *presenceTracker
	// Project lookup
	projectLookup *projectLookupImpl
	// Provider URL map
	providerURLs *providerServerURLMap
	// Annotations
	annotations *AnnotationsManager
	// Summaries store
	summaries stateSyncSummariesStore
	// Settings provider (in-memory CLI settings)
	cliSettings *CLISettingsProvider
	// State
	closed bool
}

// ---- Annotations manager --------------------------------------------------

// AnnotationsManager is the recovered `(*AnnotationsManager)` from
// google3/third_party/jetski/cortex/cortex.
//
// pclntab entries: NewAnnotationsManager 0x7fc3f20, lockFor 0x7fc4020,
// GetAnnotations 0x7fc4100, UpdateAnnotations 0x7fc4360, Mutate 0x7fc4660,
// save 0x7fc48a0, DeleteAnnotations 0x7fc4d00.
//
// Models the per-conversation file annotation store (think "user
// starred this file" / "this file has comment X"). The 1.3.5 binary
// persists annotations to a per-trajectory JSON file.
type AnnotationsManager struct {
	mu    sync.Mutex
	trajectoryID string
	store  string // backing path (cycle 41 doc: lifecycle.go)
	cache  map[string]Annotation // path -> annotation
}

// Annotation is a single annotation entry (file + key/value tag).
type Annotation struct {
	Path     string
	Key      string
	Value    string
	Created  time.Time
	Modified time.Time
}

// ---- Errors ---------------------------------------------------------------

// EndBattleModeError is the recovered `(*EndBattleModeError)` from
// google3/third_party/jetski/cortex/cortex. pclntab: Error 0x7fc4fa0,
// Unwrap 0x7fc5060.
//
// Returned from CascadeManager.EndBattleMode / ValidateBattleMode when
// the binary decides the in-flight battle mode cannot be safely ended
// (e.g. there are still running subagents or unsaved trajectories).
type EndBattleModeError struct {
	Reason string
	Inner  error // for Unwrap
}

// Error implements `error` (mirrors 1.3.5 (*EndBattleModeError).Error at 0x7fc4fa0).
func (e *EndBattleModeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Inner != nil {
		return "cortex: battle mode end failed: " + e.Reason + ": " + e.Inner.Error()
	}
	return "cortex: battle mode end failed: " + e.Reason
}

// Unwrap returns the inner cause (mirrors 1.3.5 (*EndBattleModeError).Unwrap at 0x7fc5060).
func (e *EndBattleModeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Inner
}

// ---- background runner ----------------------------------------------------

// backgroundRunner is the recovered `(*backgroundRunner)` from
// google3/third_party/jetski/cortex/cortex. pclntab: Go 0x7fd6e00,
// CancelAndWait 0x7fd6e60.
//
// The 1.3.5 binary uses this to track in-flight goroutines so
// CascadeManager.Shutdown can wait for them.
type backgroundRunner struct {
	mu    sync.Mutex
	tasks []bgTask
}

// bgTask is one entry in the runner's task list.
type bgTask struct {
	Name string
	Stop chan struct{}
	Done chan struct{}
}

// ---- jetbox summaries store adapter --------------------------------------

// jetboxSummariesStoreAdapter is the recovered
// `(*jetboxSummariesStoreAdapter)` from
// google3/third_party/jetski/cortex/cortex. pclntab: WriteSummary
// 0x7ff47a0, DeleteSummary 0x7ff4820, GetSummary 0x7ff48a0.
//
// Bridges between the state-sync summaries store and the jetbox
// summaries store (the cycle 25 `internal/language_server_pb/jetbox_summaries_store`).
type jetboxSummariesStoreAdapter struct {
	delegate SummariesStore
	prefix   string
}

// ---- nopPlacer ------------------------------------------------------------

// nopPlacer is the recovered `(*nopPlacer)` from
// google3/third_party/jetski/cortex/cortex. pclntab: NewWorkspaceProject
// 0x7fefde0, AddWorktreeEnvironment 0x7fefe00.
//
// A no-op implementation of the workspace placer interface — used when
// the real placer (CITC / worktree) is disabled.
type nopPlacer struct{}

// NewWorkspaceProject creates a no-op workspace project (mirrors
// nopPlacer.NewWorkspaceProject at 0x7fefde0).
func (n *nopPlacer) NewWorkspaceProject(name string) (WorkspaceProject, error) {
	return WorkspaceProject{Name: name}, nil
}

// AddWorktreeEnvironment is a no-op (mirrors nopPlacer.AddWorktreeEnvironment at 0x7fefe00).
func (n *nopPlacer) AddWorktreeEnvironment(_ WorkspaceProject, _ WorktreeEnv) error {
	return nil
}

// ---- ordinaryExecutor -----------------------------------------------------

// ordinaryExecutor is the recovered `(*ordinaryExecutor)` from
// google3/third_party/jetski/cortex/cortex. pclntab: Run 0x7fffd80,
// Send 0x7fffe20, Listen 0x7fffc60, Close 0x7fff860, MaybeRevive
// 0x7fffce0, GetTrajectory 0x7fffaa0, GetAgentState 0x7fff8c0,
// GetExecutorLoopStatus 0x7fff920,
// GetExecutorLoopStatusAndLastIdleTime 0x7fff980, GetRunStateInternal
// 0x7fff9a0, HandleExecutionError 0x7fffbc0, SendBookmark 0x7fffea0,
// WaitAndFlushSnapshot 0x7ffff20.
//
// The "ordinary" executor wraps the in-process agent loop (vs. the
// sdkExecutor which forks a process). It is what the cycle 41
// RealLifecycle drives.
type ordinaryExecutor struct {
	mu       sync.Mutex
	trajectoryID string
	closed   bool
}

// ---- presenceTracker ------------------------------------------------------

// presenceTracker is the recovered `(*presenceTracker)` from
// google3/third_party/jetski/cortex/cortex. pclntab: newPresenceTracker
// 0x7fedc80, acquire 0x7fedd20, probe 0x7fedfc0, release 0x7fee2a0,
// remove 0x7fee600.
//
// Tracks the liveness of all subagents across the trajectory tree.
// Used by StartBattleMode to verify no other subagent is running.
type presenceTracker struct {
	mu     sync.Mutex
	active map[string]*presenceEntry
}

// presenceEntry is one tracked subagent presence.
type presenceEntry struct {
	SubagentID string
	AcquiredAt time.Time
	Holder     string
}

// ---- projectLookupImpl ----------------------------------------------------

// projectLookupImpl is the recovered `(*projectLookupImpl)` from
// google3/third_party/jetski/cortex/cortex. pclntab:
// GetProjectIDForConversationID 0x7feb960.
//
// Maps conversation id to CITC project id.
type projectLookupImpl struct {
	mu sync.Mutex
	cache map[string]string
}

// ---- providerServerURLMap -------------------------------------------------

// providerServerURLMap is the recovered `(*providerServerURLMap)` from
// google3/third_party/jetski/cortex/cortex. pclntab: get 0x7fec5e0,
// update 0x7fec480, remove 0x7fec540 (inferred from pclntab).
//
// A thread-safe map from provider name (e.g. "anthropic", "openai")
// to the server URL.
type providerServerURLMap struct {
	mu  sync.RWMutex
	urls map[string]string
}

// ---- sdkExecutor ----------------------------------------------------------

// sdkExecutor is the recovered `(*sdkExecutor)` from
// google3/third_party/jetski/cortex/cortex. pclntab: Run 0x7fff640,
// Send 0x7fec700, Listen 0x7fff480, Close 0x7ffedc0, MaybeRevive
// 0x7fff560, GetTrajectory 0x7fff240, GetAgentState 0x7ffee80,
// GetExecutorLoopStatus 0x7ffef40,
// GetExecutorLoopStatusAndLastIdleTime 0x7fff000, GetRunStateInternal
// 0x7fff020, HandleExecutionError 0x7fff3a0, SendBookmark 0x7fff720,
// WaitAndFlushSnapshot 0x7fff800, WarmUpAgentProcess 0x7fd9400,
// surfaceAgentProcessError 0x7fd9900, updateSdkExecutableStatus
// 0x7fda160, updateSdkExecutableStatusLocked 0x7fda2e0, executeScript
// 0x7fd8600, getDefaultWorkspaceCwd 0x7fd6ea0, getWorkspaceCwd
// 0x7fd7040.
//
// The "sdk" executor forks a child process to run the agent — vs. the
// ordinaryExecutor which runs in-process.
type sdkExecutor struct {
	mu       sync.Mutex
	cmd      *sdkProcess // child process handle
	trajectoryID string
	closed   bool
}

// sdkProcess is the recovered child-process handle. The 1.3.5 binary
// uses os/exec.Cmd; the stub here captures the same surface.
type sdkProcess struct {
	Path string
	Args []string
	Env  []string
}

// ---- stateSyncSummariesStore ----------------------------------------------

// stateSyncSummariesStore is the recovered
// `(*stateSyncSummariesStore)` from
// google3/third_party/jetski/cortex/cortex. pclntab: WriteSummary
// 0x7ff4680, DeleteSummary 0x7ff4700.
//
// Implements SummariesStore (cycle 25: internal/language_server_pb/jetbox_summaries_store).
type stateSyncSummariesStore struct {
	mu      sync.Mutex
	backend SummariesStore
}

// ---- supporting types -----------------------------------------------------

// SensitivityLevel is the recovered "sensitivity level" enum used by
// CascadeManager.PersistSensitivityLevel (0x7fdac20),
// UpdateLoadedSensitivityLevel (0x7fda920), and
// loadedSensitivityLevel (0x7fdf720).
//
// The 1.3.5 binary has 3 levels (PUBLIC / INTERNAL / RESTRICTED); we
// model them as a string for forward compatibility.
type SensitivityLevel string

const (
	// SensitivityPublic — non-sensitive, can be cached publicly.
	SensitivityPublic SensitivityLevel = "PUBLIC"
	// SensitivityInternal — Google-internal, can be cached internally.
	SensitivityInternal SensitivityLevel = "INTERNAL"
	// SensitivityRestricted — restricted, must be fetched fresh.
	SensitivityRestricted SensitivityLevel = "RESTRICTED"
)

// BattleModeState is the recovered "battle mode" internal state used
// by StartBattleMode (0x7fc7f60), EndBattleMode (0x7fcbb00),
// EliminateBattleModeArm (0x7fd28a0), and the helpers
// cleanupBattleMode (0x7fca8a0) / applyBattleModeWinner (0x7fcd500).
type BattleModeState struct {
	Arms       []BattleModeArm
	Winner     string
	StartedAt  time.Time
	Forks      []string // trajectory ids of the forked arms
}

// BattleModeArm is one arm in a battle-mode run.
type BattleModeArm struct {
	Index       int
	TrajectoryID string
	Model       string
	Status      string // "running" | "done" | "eliminated"
}

// SubagentHubLike is the recovered "subagent hub" interface. The 1.3.5
// binary has a `(*CascadeManager).SubagentHub` getter (pclntab 0x7fe3xxx
// area) and a `(*CascadeManager).gatedSubagentHub` (0x7fe46e0) — both
// return this kind of thing.
//
// SubagentHub is the per-trajectory subagent registry.
type SubagentHubLike interface {
	// Register a subagent
	Register(id, parent string) error
	// Lookup a subagent by id
	Lookup(id string) (SubagentState, bool)
	// List all subagents for a parent
	ListForParent(parent string) []SubagentState
	// Unregister a subagent
	Unregister(id string) error
	// Gate is the "gated" subagent hub — only allows registration
	// when the parent's lifecycle is in the right state.
	Gate() SubagentGate
}

// SubagentGate is the gate returned by gatedSubagentHub.
type SubagentGate interface {
	Allow(id string) bool
}

// SubagentState is the recovered subagent runtime state. The 1.3.5
// binary has these in the `(*CascadeManager).backfillSubagentsIfEmpty`
// path (0x7fdc8c0).
type SubagentState struct {
	ID        string
	Parent    string
	TrajectoryID string
	Status    SubagentStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SubagentStatus is the recovered subagent status enum.
type SubagentStatus string

const (
	// SubagentPending — created, not yet running.
	SubagentPending SubagentStatus = "pending"
	// SubagentRunning — actively executing.
	SubagentRunning SubagentStatus = "running"
	// SubagentAwaitsApproval — waiting on user (recovered as
	// SubagentAwaitsApproval in store at 0x8aa6f20 +
	// 0x8aa3f80 + 0x8aa7da0).
	SubagentAwaitsApproval SubagentStatus = "awaits_approval"
	// SubagentDone — finished OK.
	SubagentDone SubagentStatus = "done"
	// SubagentFailed — finished with error.
	SubagentFailed SubagentStatus = "failed"
	// SubagentKilled — killed by KillAgent.
	SubagentKilled SubagentStatus = "killed"
)

// Executor is the recovered executor interface. The 1.3.5 binary has
// two implementations: sdkExecutor and ordinaryExecutor, both
// implementing the same surface.
type Executor interface {
	Run(ctx ExecContext, prompt string) error
	Send(msg AgentMessage) error
	Listen() <-chan AgentStateUpdate
	Close() error
	MaybeRevive() error
	GetTrajectory() (Trajectory, error)
	GetAgentState() AgentState
	GetExecutorLoopStatus() ExecutorLoopStatus
	GetExecutorLoopStatusAndLastIdleTime() (ExecutorLoopStatus, time.Time)
	GetRunStateInternal() interface{}
	HandleExecutionError(err error) error
	SendBookmark(b Bookmark) error
	WaitAndFlushSnapshot() error
}

// ExecContext is the recovered executor context. The 1.3.5 binary
// uses a per-trajectory context.
type ExecContext struct {
	TrajectoryID string
	Parent       string
	Cwd          string
}

// AgentMessage is the recovered agent message.
type AgentMessage struct {
	Role    string
	Content string
	Name    string
}

// AgentState is the recovered agent state.
type AgentState struct {
	Status    SubagentStatus
	TrajectoryID string
	UpdatedAt time.Time
}

// AgentStateUpdate is one update on the executor's Listen channel.
type AgentStateUpdate struct {
	State AgentState
	Seq   int64
}

// Bookmark is a trajectory bookmark.
type Bookmark struct {
	StepIndex int
	Label     string
}

// Trajectory is the recovered trajectory (one conversation's history).
type Trajectory struct {
	ID    string
	Steps []TrajectoryStep
}

// TrajectoryStep is one step in a trajectory.
type TrajectoryStep struct {
	Index    int
	Role     string
	Content  string
	ToolName string
	ToolArgs map[string]any
}

// ExecutorLoopStatus is the recovered loop status.
type ExecutorLoopStatus string

const (
	// LoopStatusIdle — executor idle, ready to accept work.
	LoopStatusIdle ExecutorLoopStatus = "idle"
	// LoopStatusBusy — executor busy with a turn.
	LoopStatusBusy ExecutorLoopStatus = "busy"
	// LoopStatusQuiesced — executor shut down, no more work.
	LoopStatusQuiesced ExecutorLoopStatus = "quiesced"
)

// SummariesStore is the recovered summaries store interface. Implemented
// by stateSyncSummariesStore and jetboxSummariesStoreAdapter.
type SummariesStore interface {
	WriteSummary(id string, summary Summary) error
	DeleteSummary(id string) error
	GetSummary(id string) (Summary, bool)
}

// Summary is a one-line summary of a trajectory.
type Summary struct {
	TrajectoryID string
	Title        string
	UpdatedAt    time.Time
}

// WorkspaceProject is the recovered workspace project (a single
// workspace, possibly with multiple worktrees).
type WorkspaceProject struct {
	Name      string
	URI       string
	Worktrees []Worktree
}

// Worktree is one worktree in a workspace.
type Worktree struct {
	Branch string
	Path   string
}

// WorktreeEnv is the recovered worktree environment.
type WorktreeEnv struct {
	Branch string
	Base   string
}

// CLISettingsProvider is the recovered in-memory CLI settings provider
// (set via CascadeManager.SetInMemoryCLISettingsProvider 0x7fdaec0).
type CLISettingsProvider struct {
	mu     sync.Mutex
	values map[string]any
}

// ForkInfo is the recovered fork metadata. The 1.3.5 binary uses this
// in `(*CascadeManager).ForkConversation` (0x7fefe20) and the helpers
// (forkNameFor, forkSnapshotVersion, etc.).
type ForkInfo struct {
	ForkedFrom string
	ForkedAt   time.Time
	ForkedStep int
	Notes      []string
}

// AgentScript is the recovered agent script (a list of steps to run).
// The 1.3.5 binary uses this in applyAgentScriptToMetadata
// (0x7fe96c0).
type AgentScript struct {
	Items []AgentScriptItem
}

// AgentScriptItem is one step in an agent script.
type AgentScriptItem struct {
	Kind    string // "tool" | "message" | "control"
	Payload map[string]any
}
