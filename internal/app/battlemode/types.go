// Package battlemode orchestrates multi-model parallel execution in isolated
// workspaces (git worktrees or copied directories), captures per-arm diffs and
// metrics, supports arm elimination, and applies the winning arm's changes back
// to the base workspace.
package battlemode

import (
	"context"
	"time"
)

// Arm status constants.
const (
	StatusPending    = "pending"
	StatusRunning    = "running"
	StatusDone       = "done"
	StatusError      = "error"
	StatusEliminated = "eliminated"
)

// ArmSpec describes one competing model arm in a Battle Mode run.
type ArmSpec struct {
	Index    int
	ID       string
	Model    string
	Label    string
	Provider string
}

// DisplayName returns the human-readable label for the arm.
func (a ArmSpec) DisplayName() string {
	if a.Label != "" {
		return a.Label
	}
	if a.Model != "" {
		return a.Model
	}
	if a.ID != "" {
		return a.ID
	}
	return "arm"
}

// TokenUsage records token consumption for an arm execution.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// ArmOutput is the result returned by an ArmRunner for a single arm.
type ArmOutput struct {
	Response string
	Usage    TokenUsage
	Metadata map[string]string
}

// ArmRunner executes a task for a single arm inside its isolated workspace.
type ArmRunner func(ctx context.Context, arm ArmSpec, workspace string, task string) (ArmOutput, error)

// ArmState captures the live and final state of one arm in a Battle Mode session.
type ArmState struct {
	Spec         ArmSpec
	Workspace    string
	WorktreeRoot string
	Status       string
	StartedAt    time.Time
	FinishedAt   time.Time
	Duration     time.Duration
	Usage        TokenUsage
	Response     string
	Diff         string
	ChangedFiles []string
	Error        string
	Winner       bool
}

// ApplyResult describes the outcome of applying a winning arm's changes to baseDir.
type ApplyResult struct {
	WinnerIndex  int
	WinnerModel  string
	AppliedFiles []string
	Diff         string
	Method       string // "git-apply", "file-sync", or "noop"
}

// SessionSnapshot is an immutable point-in-time view of a Battle Mode session.
type SessionSnapshot struct {
	Task        string
	BaseDir     string
	RepoRoot    string
	BaseCommit  string
	IsGit       bool
	Arms        []ArmState
	WinnerIndex int
	StartedAt   time.Time
	FinishedAt  time.Time
	Running     bool
}
