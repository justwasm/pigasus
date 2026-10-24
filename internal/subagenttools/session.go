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

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/extension/builtin"
	"github.com/MichaelKinsy/PiG/coding/extension/factoryload"
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
		systemPrompt = deps.SystemPrompt
	}

	subID := newSubagentID()
	subDir := filepath.Join(agentDir, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		subDir = ""
	}

	cwd, worktreePath := resolveSubagentCWD(agentDir, subID, inv.Worktree, deps)
	startedAt := time.Now()

	rt, session, err := createSubagentSession(ctx, deps, model, systemPrompt, cwd)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = rt.Close() }()
	defer func() { _ = session.Close() }()

	defer purgeWorktreeArtifacts(worktreePath, subID)

	msgs, runErr := session.Send(ctx, inv.Prompt)
	return buildInvokeResult(subID, startedAt, worktreePath, msgs, runErr, subDir, session)
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

func createSubagentSession(ctx context.Context, deps Deps, model *pi.Model, systemPrompt, cwd string) (*coding.Runtime, *coding.Session, error) {
	exts, extRuntime := minimalBuiltinExtensions()
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
		ExtraTools:       nil,
		BeforeToolCall:   deps.beforeToolCallHooks(),
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
	path := filepath.Join(subDir, subID+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
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
