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
	Worktree       string    `json:"worktree"`
	Prompt         string    `json:"prompt"`
	SystemPrompt   string    `json:"systemPrompt"`
	InheritPrompt  bool      `json:"inheritPrompt"`
	State          string    `json:"state"`
	StartedAt      time.Time `json:"startedAt"`
	EndedAt        time.Time `json:"endedAt,omitempty"`
	LastActivityAt time.Time `json:"lastActivityAt"`
	StepCount      int       `json:"stepCount"`
	NestingDepth   int       `json:"nestingDepth"`
	ToolsUsed      []string  `json:"toolsUsed"`
	Error          string    `json:"error,omitempty"`
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

		if deps.ConversationID != "" && meta.ParentID != "" && meta.ParentID != deps.ConversationID {
			continue
		}

		// Reconstitute state: an interrupted "running" task after CLI restart becomes "stopped"
		resumedState := meta.State
		if resumedState == StateRunning {
			resumedState = StateStopped
		}

		childCtx, cancel := context.WithCancel(context.Background())
		_ = childCtx
		child := NewChild(meta.ConversationID, meta.TypeName, meta.Role, meta.Worktree, cancel)
		child.setState(resumedState)
		child.SetSubDir(subDir)

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
			if model == nil {
				model = deps.Model
			}
			go runChild(childRunCtx, child, deps, model, meta.SystemPrompt, meta.Worktree, meta.Worktree, subDir, inv, inv.Tools, meta.StartedAt, initialMsg)
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
