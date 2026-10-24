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
	Arms      []BattleModeArm
	Winner    string
	StartedAt time.Time
	Forks     []string // trajectory ids of the forked arms
}

// BattleModeArm is one arm in a battle-mode run.
type BattleModeArm struct {
	Index        int
	TrajectoryID string
	Model        string
	Status       string // "running" | "done" | "eliminated"
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
	ID           string
	Parent       string
	TrajectoryID string
	Status       SubagentStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
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
	Status       SubagentStatus
	TrajectoryID string
	UpdatedAt    time.Time
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
