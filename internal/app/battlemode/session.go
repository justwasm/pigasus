package battlemode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type armRuntime struct {
	state  ArmState
	cancel context.CancelFunc
}

// Session orchestrates a single Battle Mode run across N model arms.
type Session struct {
	mu          sync.RWMutex
	task        string
	baseDir     string
	repoRoot    string
	baseCommit  string
	isGit       bool
	tempDir     string
	baseCopyDir string
	arms        []*armRuntime
	winnerIndex int
	startedAt   time.Time
	finishedAt  time.Time
	running     bool
	onUpdate    func(SessionSnapshot)
}

// NewSession creates a new Battle Mode session.
func NewSession() *Session {
	return &Session{
		winnerIndex: -1,
	}
}

// OnUpdate registers a callback invoked whenever session or arm state changes.
func (s *Session) OnUpdate(fn func(SessionSnapshot)) {
	s.mu.Lock()
	s.onUpdate = fn
	s.mu.Unlock()
}

// PrepareWorkspaces creates isolated workspaces for each arm. If baseDir is
// inside a git repository with a valid HEAD commit, it records the base commit
// and creates detached git worktrees under a temporary directory. Otherwise it
// falls back to copying workspace files into isolated temporary directories.
func (s *Session) PrepareWorkspaces(ctx context.Context, baseDir string, arms []ArmSpec) error {
	if len(arms) == 0 {
		return errors.New("battlemode: at least one arm is required")
	}
	if strings.TrimSpace(baseDir) == "" {
		return errors.New("battlemode: baseDir is required")
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("battlemode: resolve baseDir: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(absBase); err == nil {
		absBase = resolved
	}
	info, err := os.Stat(absBase)
	if err != nil {
		return fmt.Errorf("battlemode: stat baseDir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("battlemode: baseDir %q is not a directory", absBase)
	}

	// Clean up any prior workspaces on this session before preparing new ones.
	if err := s.Cleanup(); err != nil {
		return fmt.Errorf("battlemode: cleanup prior workspaces: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "pigasus-battle-*")
	if err != nil {
		return fmt.Errorf("battlemode: create temp dir: %w", err)
	}

	repoRoot, headCommit, isGit := detectGitRepo(ctx, absBase)
	runtimes := make([]*armRuntime, len(arms))

	if isGit {
		if resolvedRoot, err := filepath.EvalSymlinks(repoRoot); err == nil {
			repoRoot = resolvedRoot
		}
		rel, err := filepath.Rel(repoRoot, absBase)
		if err != nil || strings.HasPrefix(rel, "..") {
			_ = os.RemoveAll(tempDir)
			return fmt.Errorf("battlemode: relative path from repo root: %w", err)
		}
		var createdWorktrees []string
		for i, spec := range arms {
			wtDir := filepath.Join(tempDir, fmt.Sprintf("arm-%d", i))
			if _, err := runGit(ctx, repoRoot, "worktree", "add", "--detach", wtDir, headCommit); err != nil {
				for _, created := range createdWorktrees {
					_, _ = runGit(context.Background(), repoRoot, "worktree", "remove", "--force", created)
				}
				_ = os.RemoveAll(tempDir)
				return fmt.Errorf("battlemode: create worktree for arm %d: %w", i, err)
			}
			createdWorktrees = append(createdWorktrees, wtDir)

			ws := wtDir
			if rel != "." && rel != "" {
				ws = filepath.Join(wtDir, rel)
			}
			norm := normalizeArmSpec(i, spec)
			runtimes[i] = &armRuntime{
				state: ArmState{
					Spec:         norm,
					Workspace:    ws,
					WorktreeRoot: wtDir,
					Status:       StatusPending,
				},
			}
		}
	} else {
		baseCopyDir := filepath.Join(tempDir, "base")
		if err := copyDir(absBase, baseCopyDir); err != nil {
			_ = os.RemoveAll(tempDir)
			return fmt.Errorf("battlemode: snapshot baseDir: %w", err)
		}
		for i, spec := range arms {
			armDir := filepath.Join(tempDir, fmt.Sprintf("arm-%d", i))
			if err := copyDir(baseCopyDir, armDir); err != nil {
				_ = os.RemoveAll(tempDir)
				return fmt.Errorf("battlemode: copy workspace for arm %d: %w", i, err)
			}
			norm := normalizeArmSpec(i, spec)
			runtimes[i] = &armRuntime{
				state: ArmState{
					Spec:      norm,
					Workspace: armDir,
					Status:    StatusPending,
				},
			}
		}
		s.mu.Lock()
		s.baseCopyDir = baseCopyDir
		s.mu.Unlock()
	}

	s.mu.Lock()
	s.baseDir = absBase
	s.repoRoot = repoRoot
	s.baseCommit = headCommit
	s.isGit = isGit
	s.tempDir = tempDir
	s.arms = runtimes
	s.winnerIndex = -1
	s.startedAt = time.Time{}
	s.finishedAt = time.Time{}
	s.running = false
	s.mu.Unlock()

	s.notify()
	return nil
}

func normalizeArmSpec(index int, spec ArmSpec) ArmSpec {
	spec.Index = index
	if spec.ID == "" {
		spec.ID = fmt.Sprintf("arm-%d", index)
	}
	if spec.Label == "" {
		spec.Label = spec.Model
	}
	return spec
}

// Run executes all non-eliminated arms concurrently in their isolated workspaces.
func (s *Session) Run(ctx context.Context, task string, runner ArmRunner) error {
	if runner == nil {
		return errors.New("battlemode: runner is required")
	}

	s.mu.Lock()
	if len(s.arms) == 0 {
		s.mu.Unlock()
		return errors.New("battlemode: no workspaces prepared")
	}
	if s.running {
		s.mu.Unlock()
		return errors.New("battlemode: session is already running")
	}
	s.task = task
	s.startedAt = time.Now()
	s.finishedAt = time.Time{}
	s.running = true
	isGit := s.isGit
	baseCommit := s.baseCommit
	baseCopyDir := s.baseCopyDir
	armCount := len(s.arms)
	s.mu.Unlock()

	s.notify()

	var wg sync.WaitGroup
	for i := 0; i < armCount; i++ {
		s.mu.Lock()
		arm := s.arms[i]
		if arm.state.Status == StatusEliminated {
			s.mu.Unlock()
			continue
		}
		armCtx, cancel := context.WithCancel(ctx)
		arm.cancel = cancel
		arm.state.Status = StatusRunning
		arm.state.StartedAt = time.Now()
		arm.state.FinishedAt = time.Time{}
		arm.state.Duration = 0
		arm.state.Error = ""
		spec := arm.state.Spec
		workspace := arm.state.Workspace
		wtRoot := arm.state.WorktreeRoot
		s.mu.Unlock()

		s.notify()

		wg.Add(1)
		go func(idx int, a *armRuntime, aCtx context.Context, cancelFn context.CancelFunc) {
			defer wg.Done()
			defer cancelFn()

			out, runErr := runner(aCtx, spec, workspace, task)
			finished := time.Now()

			// Compute diff even if the arm errored so partial edits remain inspectable.
			diffCtx := context.Background()
			var diffText string
			var changedFiles []string
			if isGit && wtRoot != "" {
				diffText, changedFiles, _ = computeGitDiff(diffCtx, wtRoot, baseCommit)
			} else if baseCopyDir != "" {
				diffText, changedFiles, _ = computeDirDiff(diffCtx, baseCopyDir, workspace)
			}

			s.mu.Lock()
			a.cancel = nil
			a.state.FinishedAt = finished
			a.state.Duration = finished.Sub(a.state.StartedAt)
			a.state.Response = out.Response
			a.state.Usage = out.Usage
			a.state.Diff = diffText
			a.state.ChangedFiles = changedFiles
			if a.state.Status != StatusEliminated {
				if runErr != nil {
					a.state.Status = StatusError
					a.state.Error = runErr.Error()
				} else {
					a.state.Status = StatusDone
				}
			}
			s.mu.Unlock()

			s.notify()
		}(i, arm, armCtx, cancel)
	}

	wg.Wait()

	s.mu.Lock()
	s.running = false
	s.finishedAt = time.Now()
	s.mu.Unlock()

	s.notify()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// EliminateArm marks the arm at index as eliminated and cancels it if running.
func (s *Session) EliminateArm(index int) error {
	s.mu.Lock()
	if index < 0 || index >= len(s.arms) {
		n := len(s.arms)
		s.mu.Unlock()
		return fmt.Errorf("battlemode: arm index %d out of range [0,%d)", index, n)
	}
	if s.winnerIndex == index {
		s.mu.Unlock()
		return fmt.Errorf("battlemode: cannot eliminate winning arm %d", index)
	}
	arm := s.arms[index]
	cancel := arm.cancel
	arm.state.Status = StatusEliminated
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.notify()
	return nil
}

// PickWinner applies the winning arm's diff/changes back to baseDir and marks
// the arm as the session winner.
func (s *Session) PickWinner(ctx context.Context, index int) (ApplyResult, error) {
	s.mu.Lock()
	if index < 0 || index >= len(s.arms) {
		n := len(s.arms)
		s.mu.Unlock()
		return ApplyResult{}, fmt.Errorf("battlemode: arm index %d out of range [0,%d)", index, n)
	}
	arm := s.arms[index]
	switch arm.state.Status {
	case StatusEliminated:
		s.mu.Unlock()
		return ApplyResult{}, fmt.Errorf("battlemode: arm %d is eliminated", index)
	case StatusRunning, StatusPending:
		status := arm.state.Status
		s.mu.Unlock()
		return ApplyResult{}, fmt.Errorf("battlemode: arm %d is %s", index, status)
	}
	isGit := s.isGit
	repoRoot := s.repoRoot
	baseDir := s.baseDir
	baseCommit := s.baseCommit
	baseCopyDir := s.baseCopyDir
	wtRoot := arm.state.WorktreeRoot
	workspace := arm.state.Workspace
	model := arm.state.Spec.Model
	s.mu.Unlock()

	var result ApplyResult
	result.WinnerIndex = index
	result.WinnerModel = model

	if isGit && wtRoot != "" {
		diffText, changedFiles, err := computeGitDiff(ctx, wtRoot, baseCommit)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("battlemode: compute winner diff: %w", err)
		}
		result.Diff = diffText
		result.AppliedFiles = changedFiles
		if strings.TrimSpace(diffText) == "" {
			result.Method = "noop"
		} else {
			if _, err := runGitWithInput(ctx, repoRoot, diffText, "apply", "--whitespace=nowarn"); err != nil {
				return ApplyResult{}, fmt.Errorf("battlemode: apply winner diff: %w", err)
			}
			result.Method = "git-apply"
		}
	} else {
		diffText, _, err := computeDirDiff(ctx, baseCopyDir, workspace)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("battlemode: compute winner dir diff: %w", err)
		}
		applied, err := syncDirChanges(baseCopyDir, workspace, baseDir)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("battlemode: sync winner files: %w", err)
		}
		result.Diff = diffText
		result.AppliedFiles = applied
		if len(applied) == 0 {
			result.Method = "noop"
		} else {
			result.Method = "file-sync"
		}
	}

	s.mu.Lock()
	s.winnerIndex = index
	for i, a := range s.arms {
		a.state.Winner = (i == index)
	}
	s.arms[index].state.Diff = result.Diff
	s.arms[index].state.ChangedFiles = append([]string(nil), result.AppliedFiles...)
	s.mu.Unlock()

	s.notify()
	return result, nil
}

// Cleanup removes temporary git worktrees and temporary workspace directories.
func (s *Session) Cleanup() error {
	s.mu.Lock()
	isGit := s.isGit
	repoRoot := s.repoRoot
	tempDir := s.tempDir
	var worktrees []string
	var cancels []context.CancelFunc
	for _, a := range s.arms {
		if a.cancel != nil {
			cancels = append(cancels, a.cancel)
			a.cancel = nil
		}
		if a.state.WorktreeRoot != "" {
			worktrees = append(worktrees, a.state.WorktreeRoot)
			a.state.WorktreeRoot = ""
		}
	}
	s.tempDir = ""
	s.baseCopyDir = ""
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	var errs []string
	if isGit && repoRoot != "" {
		ctx := context.Background()
		for _, wt := range worktrees {
			if _, err := runGit(ctx, repoRoot, "worktree", "remove", "--force", wt); err != nil {
				// If directory was already removed, prune will clean metadata.
				if _, statErr := os.Stat(wt); statErr == nil {
					errs = append(errs, err.Error())
				}
			}
		}
		_, _ = runGit(ctx, repoRoot, "worktree", "prune")
	}

	if tempDir != "" {
		if err := os.RemoveAll(tempDir); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("battlemode: cleanup errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Snapshot returns a thread-safe copy of the current session state.
func (s *Session) Snapshot() SessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	arms := make([]ArmState, len(s.arms))
	for i, a := range s.arms {
		st := a.state
		if len(a.state.ChangedFiles) > 0 {
			st.ChangedFiles = append([]string(nil), a.state.ChangedFiles...)
		}
		arms[i] = st
	}
	return SessionSnapshot{
		Task:        s.task,
		BaseDir:     s.baseDir,
		RepoRoot:    s.repoRoot,
		BaseCommit:  s.baseCommit,
		IsGit:       s.isGit,
		Arms:        arms,
		WinnerIndex: s.winnerIndex,
		StartedAt:   s.startedAt,
		FinishedAt:  s.finishedAt,
		Running:     s.running,
	}
}

func (s *Session) notify() {
	s.mu.RLock()
	fn := s.onUpdate
	s.mu.RUnlock()
	if fn != nil {
		fn(s.Snapshot())
	}
}
