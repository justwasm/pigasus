package subagenttools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/extension/builtin"
	"github.com/MichaelKinsy/PiG/coding/extension/factoryload"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

// Invoke runs the subagent synchronously: builds a fresh
// `coding.Session` from the parent's services, sends the prompt, and
// returns when the session settles or ctx is cancelled.
//
// The function is concurrency-safe across Deps; each call spins up
// its own session, and PiG's session manager serialises per-cwd
// persistence. The Deps fields themselves are not mutated.
func Invoke(ctx context.Context, inv Invocation, deps Deps) (Result, error) {
	if deps.Services == nil {
		return Result{}, errors.New("subagenttools: Deps.Services is required")
	}
	if strings.TrimSpace(inv.Prompt) == "" {
		return Result{}, errors.New("subagenttools: Invocation.Prompt is required")
	}

	model := invModel(inv, deps)
	if model == nil {
		return Result{}, errors.New("subagenttools: no model resolved (set Deps.Model or Invocation.Model)")
	}

	agentDir := deps.AgentDir
	if agentDir == "" {
		agentDir = deps.Services.AgentDir()
	}

	systemPrompt := inv.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = deps.parentPrompt()
	}

	subID := newSubagentID()
	subDir := filepath.Join(agentDir, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		subDir = ""
	}

	cwd, worktreePath := resolveSubagentCWD(agentDir, subID, inv.Worktree, deps)
	startedAt := time.Now()

	// Register in taskregistry if available so /tasks and ActiveItems preview reflect the running subagent.
	taskReg := deps.Tasks
	if taskReg != nil {
		taskName := inv.Prompt
		if len(taskName) > 60 {
			taskName = taskName[:57] + "..."
		}
		taskReg.Register(&taskregistry.Task{
			ID:        subID,
			Name:      taskName,
			Kind:      taskregistry.KindSubagent,
			State:     taskregistry.StateRunning,
			StartedAt: startedAt,
			// The prompt and the answer are what the /tasks detail
			// view shows for a subagent row (the reference's
			// "Prompt" and "Answer" sections). The prompt is known
			// up front; the answer is filled in once the run ends.
			Prompt: inv.Prompt,
		})
		defer func() {
			taskReg.Update(subID, func(t *taskregistry.Task) {
				// A failed run already recorded the failure; do not
				// overwrite it with "done" on the way out.
				if t.State != taskregistry.StateFailed {
					t.State = taskregistry.StateDone
				}
				t.EndedAt = time.Now()
			})
		}()
	}

	rt, session, err := createSubagentSession(ctx, deps, model, systemPrompt, cwd, inv.Tools, parentContextOf(deps, inv))
	if err != nil {
		if taskReg != nil {
			taskReg.Update(subID, func(t *taskregistry.Task) {
				t.State = taskregistry.StateFailed
				t.Err = err
				t.EndedAt = time.Now()
			})
		}
		return Result{}, err
	}
	defer func() { _ = rt.Close() }()
	defer func() { _ = session.Close() }()

	defer purgeWorktreeArtifacts(worktreePath, subID)

	msgs, runErr := session.Send(ctx, inv.Prompt)
	if runErr != nil && taskReg != nil {
		taskReg.Update(subID, func(t *taskregistry.Task) {
			t.State = taskregistry.StateFailed
			t.Err = runErr
			t.EndedAt = time.Now()
		})
	}
	result, buildErr := buildInvokeResult(subID, startedAt, worktreePath, msgs, runErr, subDir, session)
	if taskReg != nil {
		taskReg.Update(subID, func(t *taskregistry.Task) {
			t.Output = result.Text
			t.Tools = usedTools(msgs)
			t.Steps = usedSteps(msgs)
		})
	}
	return result, buildErr
}

// usedTools lists the tools a subagent called, in call order and
// without repeats. The /tasks detail view renders them under the
// reference's "Tools" heading.
func usedTools(msgs []piagent.AgentMessage) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range msgs {
		if m.Assistant == nil {
			continue
		}
		for _, block := range m.Assistant.Content {
			call, ok := block.(pi.ToolCall)
			if !ok || call.Name == "" || seen[call.Name] {
				continue
			}
			seen[call.Name] = true
			out = append(out, call.Name)
		}
	}
	return out
}

// usedSteps lists the subagent's trajectory steps, in order: one per
// assistant content block — a tool call by its name, a text block by
// its first line.
//
// The /tasks detail view renders them under the reference's
// "Trajectory — <name> (<N> steps)" heading, one numbered row per
// step. The reference walks the messages of its own trajectory there;
// the clone's PiG session hands back the same conversation, and each
// assistant block of it is one step.
func usedSteps(msgs []piagent.AgentMessage) []string {
	var out []string
	for _, m := range msgs {
		if m.Assistant == nil {
			continue
		}
		for _, block := range m.Assistant.Content {
			switch b := block.(type) {
			case pi.ToolCall:
				if b.Name != "" {
					out = append(out, b.Name)
				}
			case pi.TextContent:
				if line := firstLine(b.Text); line != "" {
					out = append(out, line)
				}
			}
		}
	}
	return out
}

// firstLine returns a text block's first non-empty line, trimmed.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func resolveSubagentCWD(agentDir, subID, invWorktree string, deps Deps) (string, string) {
	cwd := strings.TrimSpace(invWorktree)
	if cwd != "" {
		return cwd, cwd
	}
	worktreePath, err := allocateSubagentWorktreeDir(agentDir, subID)
	if err != nil {
		fallback := strings.TrimSpace(deps.CWD)
		if fallback == "" && deps.Services != nil {
			fallback = deps.Services.CWD()
		}
		worktreePath = fallback
	}
	return worktreePath, worktreePath
}

func createSubagentSession(ctx context.Context, deps Deps, model *pi.Model, systemPrompt, cwd string, extraTools []piagent.AgentTool, pctx *ParentContext) (*coding.Runtime, *coding.Session, error) {
	exts, extRuntime := minimalBuiltinExtensions()
	exts = loadParentContextExtension(exts, extRuntime, pctx)
	rt, err := coding.NewRuntime(coding.RuntimeOptions{
		Services:         deps.Services,
		NewExtensions:    exts,
		ExtensionRuntime: extRuntime,
		AbortContext:     ctx,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("subagenttools: new runtime: %w", err)
	}

	session, err := rt.New(coding.SessionStartOptions{
		Model:            model,
		SystemPrompt:     systemPrompt,
		CWDOverride:      stringPtr(cwd),
		NoSession:        true,
		SkipBuiltinTools: true,
		ExtraTools:       extraTools,
		BeforeToolCall:   deps.beforeToolCallHooks(cwd),
	})
	if err != nil {
		_ = rt.Close()
		return nil, nil, fmt.Errorf("subagenttools: new session: %w", err)
	}

	if err := session.BindExtensions(ctx, coding.ExtensionBindings{}); err != nil {
		_ = session.Close()
		_ = rt.Close()
		return nil, nil, fmt.Errorf("subagenttools: bind extensions: %w", err)
	}
	return rt, session, nil
}

func buildInvokeResult(subID string, startedAt time.Time, worktreePath string, msgs []piagent.AgentMessage, runErr error, subDir string, session *coding.Session) (Result, error) {
	result := Result{
		SubagentID:   subID,
		Duration:     time.Since(startedAt),
		Messages:     msgs,
		WorktreePath: worktreePath,
	}
	if mgr := session.SessionManager(); mgr != nil {
		result.SessionID = sessionIDOf(mgr)
	}
	if runErr != nil {
		persistTranscript(subDir, subID, msgs, runErr)
		return result, runErr
	}
	result.Text = assistantText(msgs)
	if subDir != "" {
		persistTranscript(subDir, subID, msgs, nil)
	}
	return result, nil
}

func invModel(inv Invocation, deps Deps) *pi.Model {
	if id := strings.TrimSpace(inv.Model); id != "" && deps.Model != nil {
		copy := *deps.Model
		copy.ID = id
		return &copy
	}
	return deps.Model
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

func assistantText(msgs []piagent.AgentMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Assistant == nil {
			continue
		}
		if m.Assistant.StopReason == pi.StopReasonError || m.Assistant.StopReason == pi.StopReasonAborted {
			continue
		}
		for _, block := range m.Assistant.Content {
			if text, ok := block.(pi.TextContent); ok {
				b.WriteString(text.Text)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func sessionIDOf(mgr *coding.SessionManager) string {
	if mgr == nil {
		return ""
	}
	sm, ok := any(mgr).(interface{ SessionDir() string })
	if !ok {
		return ""
	}
	dir := sm.SessionDir()
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var newest os.DirEntry
	var newestMod int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().UnixNano() >= newestMod {
			newestMod = info.ModTime().UnixNano()
			newest = e
		}
	}
	if newest == nil {
		return ""
	}
	name := newest.Name()
	if idx := strings.Index(name, "_"); idx >= 0 {
		return strings.TrimSuffix(name[idx+1:], ".jsonl")
	}
	return strings.TrimSuffix(name, ".jsonl")
}

func persistTranscript(subDir, subID string, msgs []piagent.AgentMessage, runErr error) {
	if subDir == "" || subID == "" {
		return
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for i, msg := range msgs {
		line := map[string]any{
			"subagent": subID,
			"index":    i,
			"role":     msg.Role(),
			"message":  msg,
		}
		_ = encoder.Encode(line)
	}
	if runErr != nil {
		_ = encoder.Encode(map[string]any{
			"subagent": subID,
			"role":     "error",
			"error":    runErr.Error(),
		})
	}
	path := filepath.Join(subDir, subID+".jsonl")
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o644); err == nil {
		_ = os.Rename(tmpPath, path)
	} else {
		_ = os.WriteFile(path, buf.Bytes(), 0o644)
	}
}

// loadTranscript reads a persisted subagent JSONL transcript from disk,
// reconstructing the sequence of AgentMessages so execution can be resumed without losing progress.
func loadTranscript(subDir, subID string) ([]piagent.AgentMessage, error) {
	if subDir == "" || subID == "" {
		return nil, nil
	}
	path := filepath.Join(subDir, subID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var msgs []piagent.AgentMessage
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record struct {
			Subagent string          `json:"subagent"`
			Index    int             `json:"index"`
			Role     string          `json:"role"`
			Message  json.RawMessage `json:"message"`
			Error    string          `json:"error"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		if record.Role == "error" || len(record.Message) == 0 {
			continue
		}
		var msg piagent.AgentMessage
		if err := json.Unmarshal(record.Message, &msg); err == nil && msg.Role() != "" {
			msgs = append(msgs, msg)
		}
	}
	return msgs, scanner.Err()
}

func minimalBuiltinExtensions() ([]extension.Extension, *extension.ExtensionRuntime) {
	extRuntime := extension.CreateExtensionRuntime()
	var out []extension.Extension
	for _, entry := range builtin.All(builtin.Options{}) {
		ext, err := factoryload.LoadExtensionFromFactory(entry.Factory, "", nil, extRuntime, entry.Path())
		if err != nil {
			continue
		}
		out = append(out, ext)
	}
	return out, extRuntime
}
