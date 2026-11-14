package subagenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

// CheckpointMeta captures the complete persistent state of a subagent actor.
type CheckpointMeta struct {
	ConversationID string    `json:"conversationId"`
	ParentID       string    `json:"parentId"`
	TypeName       string    `json:"typeName"`
	Role           string    `json:"role"`
	Model          string    `json:"model,omitempty"`
	Effort         string    `json:"effort,omitempty"`
	Worktree       string    `json:"worktree"`
	Prompt         string    `json:"prompt"`
	SystemPrompt   string    `json:"systemPrompt"`
	InheritPrompt  bool      `json:"inheritPrompt"`
	State          string    `json:"state"`
	StartedAt      time.Time `json:"startedAt"`
	// SpawnStepIndex is the global step the launch came from; a checkpoint
	// reports it as spawnStepIndex.
	SpawnStepIndex int32     `json:"spawnStepIndex,omitempty"`
	EndedAt        time.Time `json:"endedAt,omitempty"`
	LastActivityAt time.Time `json:"lastActivityAt"`
	StepCount      int       `json:"stepCount"`
	NestingDepth   int       `json:"nestingDepth"`
	ToolsUsed      []string  `json:"toolsUsed"`
	// Output is the child's answer — what the subagent detail renders under
	// its "Answer" heading. Without it a restored child shows
	// "(no output)" even though its trajectory holds the answer.
	Output string `json:"output,omitempty"`
	// Steps is the child's trajectory, in order — the same list the
	// subagent detail's "Trajectory — <name> (<N> steps)" section
	// renders. It is persisted so a restored subagent keeps its
	// trajectory instead of reporting "No steps yet"; see
	// rehydrateSteps for the fallback that rebuilds it from the
	// transcript when the field is absent.
	Steps []string `json:"steps,omitempty"`
	Error string   `json:"error,omitempty"`
}

// saveCheckpoint atomically writes the subagent's metadata to <subDir>/<id>.meta.json.
func saveCheckpoint(subDir string, meta CheckpointMeta) error {
	if subDir == "" || meta.ConversationID == "" {
		return nil
	}
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(subDir, meta.ConversationID+".meta.json")
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// loadCheckpoint reads the subagent's metadata from <subDir>/<id>.meta.json.
func loadCheckpoint(subDir, conversationID string) (*CheckpointMeta, error) {
	if subDir == "" || conversationID == "" {
		return nil, os.ErrNotExist
	}
	path := filepath.Join(subDir, conversationID+".meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta CheckpointMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint meta: %w", err)
	}
	return &meta, nil
}

// rehydrateSteps returns the restored child's trajectory.
//
// The checkpoint carries it, so a subagent restored after a restart keeps
// the "Trajectory — <name> (<N> steps)" section its detail view renders.
// A checkpoint written before the field existed has none; the transcript
// beside it does, and the steps are the same derivation the live run
// used (usedSteps over the persisted messages), so they are rebuilt from
// there rather than left empty.
func rehydrateSteps(subDir string, meta *CheckpointMeta) []string {
	if meta == nil {
		return nil
	}
	if len(meta.Steps) > 0 {
		return append([]string(nil), meta.Steps...)
	}
	if subDir == "" || meta.ConversationID == "" {
		return nil
	}
	msgs, err := loadTranscript(subDir, meta.ConversationID)
	if err != nil || len(msgs) == 0 {
		return nil
	}
	return usedSteps(msgs)
}

// rehydrateOutput returns the restored child's answer.
//
// The checkpoint carries it; a checkpoint written before the field existed
// has only the transcript beside it, and the answer is the same derivation
// the live run used (assistantText over the persisted messages).
func rehydrateOutput(subDir string, meta *CheckpointMeta) string {
	if meta == nil {
		return ""
	}
	if meta.Output != "" {
		return meta.Output
	}
	if subDir == "" || meta.ConversationID == "" {
		return ""
	}
	msgs, err := loadTranscript(subDir, meta.ConversationID)
	if err != nil || len(msgs) == 0 {
		return ""
	}
	return assistantText(msgs)
}

// isRecoverableLimitError detects rate limit, token exhaustion, or quota errors.
func isRecoverableLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "429") ||
		strings.Contains(s, "resourceexhausted") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "token limit") ||
		strings.Contains(s, "quota") ||
		strings.Contains(s, "context length") ||
		strings.Contains(s, "max_tokens")
}

// RehydrateSubagents scans subDir for persisted subagent checkpoints and restores their
// actors in Registry and task.Registry, enabling seamless resumption after a CLI restart.
func RehydrateSubagents(subDir string, reg *Registry, taskReg *taskregistry.Registry, deps Deps) ([]*Child, error) {
	if subDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(subDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var restored []*Child
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".meta.json")
		meta, err := loadCheckpoint(subDir, id)
		if err != nil || meta == nil {
			continue
		}

		// A child belongs to exactly one conversation. The checkpoint
		// records that owner, so a conversation only adopts its own
		// children: without the check, opening any conversation in a
		// process that shares the subagents directory lists another
		// conversation's (finished) subagents in /tasks.
		//
		// A checkpoint written before the parent id was recorded
		// carries no owner. It cannot be attributed, so it is not
		// adopted: guessing would put a stranger's subagent — and an
		// empty trajectory for it — in the panel.
		if deps.ConversationID != "" && meta.ParentID != deps.ConversationID {
			continue
		}

		// A child that is still live in this process owns its own task
		// row: the checkpoint beside it is the snapshot from its last
		// finished turn, and adopting that snapshot would show the child
		// as idle with its latest turn's answer missing.
		if reg != nil {
			if _, live := reg.Get(meta.ConversationID); live {
				continue
			}
		}

		// Reconstitute state: an interrupted "running" task after CLI restart becomes "stopped"
		resumedState := meta.State
		if resumedState == StateRunning {
			resumedState = StateStopped
		}

		childCtx, cancel := context.WithCancel(context.Background())
		_ = childCtx
		child := NewChild(meta.ConversationID, meta.TypeName, meta.Role, meta.Worktree, cancel)
		child.ParentID = meta.ParentID
		child.Model = meta.Model
		child.Effort = meta.Effort
		child.SpawnStepIndex = meta.SpawnStepIndex
		child.StartedAt = meta.StartedAt
		child.setState(resumedState)
		child.SetSubDir(subDir)
		child.LogAbsoluteURI = fileURI(filepath.Join(subDir, meta.ConversationID+".jsonl"))

		inv := Invocation{
			TypeName:      meta.TypeName,
			Role:          meta.Role,
			Prompt:        meta.Prompt,
			SystemPrompt:  meta.SystemPrompt,
			InheritPrompt: meta.InheritPrompt,
			Worktree:      meta.Worktree,
		}

		// Rebuild the launchFn so the child can be revived via Revive() or manage_subagents action=resume
		launchFn := func(runCtx context.Context, initialMsg *Message) {
			childRunCtx, runCancel := context.WithCancel(context.WithoutCancel(runCtx))
			child.SetCancel(runCancel)
			child.setState(StateRunning)
			model := invModel(inv, deps)
			if model == "" {
				model = deps.Model
			}
			go runChild(childRunCtx, child, deps, model, deps.Provider, meta.SystemPrompt, meta.Worktree, meta.Worktree, subDir, inv, inv.Tools, meta.StartedAt, initialMsg)
		}
		child.SetLaunch(launchFn)

		if reg != nil {
			reg.Add(child)
		}

		if taskReg != nil {
			taskState := taskregistry.State(resumedState)
			t := &taskregistry.Task{
				ID:             meta.ConversationID,
				Name:           subagentDisplayName(inv),
				Kind:           taskregistry.KindSubagent,
				State:          taskState,
				StartedAt:      meta.StartedAt,
				EndedAt:        meta.EndedAt,
				LastActivityAt: meta.LastActivityAt,
				Prompt:         meta.Prompt,
				Tools:          meta.ToolsUsed,
				Steps:          rehydrateSteps(subDir, meta),
				Output:         rehydrateOutput(subDir, meta),
				Model:          meta.Model,
				Effort:         meta.Effort,
			}
			if meta.Error != "" {
				t.Err = errors.New(meta.Error)
			}
			taskReg.Register(t)
			taskReg.RegisterHandle(meta.ConversationID, &taskregistry.TaskHandle{
				ID:     meta.ConversationID,
				Cancel: cancel,
				Abort:  func() { child.Stop() },
				Resume: func(ctx context.Context) error {
					if !child.Revive(ctx, nil) {
						return taskregistry.ErrTaskNotResumable
					}
					return nil
				},
			})
		}

		restored = append(restored, child)
	}

	return restored, nil
}
