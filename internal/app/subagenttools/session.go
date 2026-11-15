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

	"github.com/justwasm/pigasus/actor"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/features/checkpoint"
)

// SubagentMessage represents a single message in the subagent transcript.
type SubagentMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// Role reports the message's role.
func (m SubagentMessage) GetRole() string { return m.Role }

// gatedTool wraps an actor.Tool with a BeforeToolCallGate.
type gatedTool struct {
	inner actor.Tool
	gate  BeforeToolCallGate
}

func (g *gatedTool) Name() string                       { return g.inner.Name() }
func (g *gatedTool) Description() string                { return g.inner.Description() }
func (g *gatedTool) Definition() actor.ToolDefinition   { return g.inner.Definition() }
func (g *gatedTool) RequiresApproval(input string) bool { return g.inner.RequiresApproval(input) }
func (g *gatedTool) Execute(ctx context.Context, input string) (actor.ToolResult, error) {
	if g.gate != nil {
		decision := g.gate.Decide(ctx, g.inner.Name(), json.RawMessage(input))
		if !decision.Allow {
			return actor.ToolResult{
				Error: decision.Reason,
			}, nil
		}
	}
	return g.inner.Execute(ctx, input)
}

// Invoke runs the subagent synchronously: builds a fresh
// Actor from the parent's deps, sends the prompt, and
// returns when the turn settles or ctx is cancelled.
func Invoke(ctx context.Context, inv Invocation, deps Deps) (Result, error) {
	if strings.TrimSpace(inv.Prompt) == "" {
		return Result{}, errors.New("subagenttools: Invocation.Prompt is required")
	}

	model := invModel(inv, deps)
	if model == "" {
		return Result{}, errors.New("subagenttools: Deps.Model is required")
	}
	provider := deps.Provider
	if provider == nil {
		return Result{}, errors.New("subagenttools: Deps.Provider is required")
	}

	agentDir := deps.AgentDir
	if agentDir == "" {
		agentDir = filepath.Join(os.TempDir(), "subagents")
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

	taskReg := deps.Tasks
	if taskReg != nil {
		taskName := subagentDisplayName(inv)
		taskReg.Register(&taskregistry.Task{
			ID:        subID,
			Name:      taskName,
			Kind:      taskregistry.KindSubagent,
			State:     taskregistry.StateRunning,
			StartedAt: startedAt,
			Prompt:    inv.Prompt,
		})
		defer func() {
			taskReg.Update(subID, func(t *taskregistry.Task) {
				if t.State != taskregistry.StateFailed {
					t.State = taskregistry.StateDone
				}
				t.CurrentTool = ""
				t.EndedAt = time.Now()
			})
		}()
	}

	tools := inv.Tools
	if len(tools) == 0 {
		tools = grantedToolSet(deps, deps.Definitions, inv.Grant)
	}
	var gate BeforeToolCallGate
	if deps.PermissionGateFor != nil {
		gate = deps.PermissionGateFor(cwd)
	}
	gatedTools := make([]actor.Tool, 0, len(tools))
	for _, t := range tools {
		if gate != nil {
			gatedTools = append(gatedTools, &gatedTool{inner: t, gate: gate})
		} else {
			gatedTools = append(gatedTools, t)
		}
	}

	act, err := actor.Spawn(actor.Config{
		ID: subID,
		Default: actor.TurnConfig{
			SystemPrompt: systemPrompt,
			Model:        model,
			Provider:     provider,
			Tools:        gatedTools,
		},
	})
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
	defer act.Close()

	var policy *checkpoint.Policy
	if deps.Backend != nil {
		var defs []actor.ToolDefinition
		for _, t := range gatedTools {
			defs = append(defs, t.Definition())
		}
		decls, _ := json.Marshal(defs)
		policy = checkpoint.New(checkpoint.Config{
			Model:            model,
			SystemPrompt:     systemPrompt,
			ToolDeclarations: decls,
			Backend:          deps.Backend,
		})
		defer policy.Close()
	}

	watchdog := newActivityWatchdog(DefaultWatchdogTimeout, func() {
		_ = act.CancelTurn("watchdog stalled")
		if taskReg != nil {
			taskReg.Update(subID, func(t *taskregistry.Task) {
				t.State = taskregistry.StateFailed
				t.Err = ErrConnectionStalled
				t.EndedAt = time.Now()
			})
		}
	})
	defer watchdog.Stop()

	req := actor.TurnRequest{Prompt: inv.Prompt}
	if policy != nil {
		req.Checkpoint = func(stepCtx context.Context, probe actor.CheckpointProbe) (actor.CheckpointDecision, error) {
			return policy.BeforeStep(stepCtx, act, probe)
		}
	}
	turn, err := act.StartTurn(ctx, req)
	if err != nil {
		return Result{}, err
	}
	events := turn.Events()

	var textBuilder strings.Builder
	for ev := range events {
		switch ev.Type {
		case actor.EventDelta:
			if watchdog != nil {
				watchdog.Ping()
			}
			textBuilder.WriteString(ev.Delta)
			if taskReg != nil {
				taskReg.RecordActivity(subID)
				taskReg.Update(subID, func(t *taskregistry.Task) {
					t.Output = textBuilder.String()
				})
			}
		case actor.EventToolCallStarted:
			if watchdog != nil {
				watchdog.Ping()
			}
			if taskReg != nil {
				taskReg.RecordActivity(subID)
				taskReg.Update(subID, func(t *taskregistry.Task) {
					if ev.ToolCall != nil {
						t.CurrentTool = ev.ToolCall.Name
						// The trajectory fills while the child works;
						// the turn's end replaces the list with
						// usedSteps(msgs).
						t.Steps = append(t.Steps, ev.ToolCall.Name)
					}
				})
			}
		case actor.EventToolCallFinished:
			if watchdog != nil {
				watchdog.Ping()
			}
			if taskReg != nil {
				taskReg.RecordActivity(subID)
				taskReg.Update(subID, func(t *taskregistry.Task) {
					t.CurrentTool = ""
				})
			}
		}
	}

	defer purgeWorktreeArtifacts(worktreePath, subID)

	var runErr error
	if watchdog.IsStalled() {
		runErr = ErrConnectionStalled
	}
	lastTurn := act.Trajectory().LastTurn()
	if lastTurn != nil && lastTurn.Error != "" {
		runErr = errors.New(lastTurn.Error)
	}

	msgs := trajectoryToSubagentMessages(act.Trajectory())
	if subDir != "" {
		persistTranscript(subDir, subID, msgs, runErr)
	}

	outputText := textBuilder.String()
	if lastTurn != nil && lastTurn.FinalOutput != "" {
		outputText = lastTurn.FinalOutput
	}

	if taskReg != nil {
		taskReg.Update(subID, func(t *taskregistry.Task) {
			t.Output = outputText
			t.Tools = usedTools(msgs)
			t.Steps = usedSteps(msgs)
			t.StepCount = len(msgs)
			if t.StepCount < len(t.Steps)+1 {
				t.StepCount = len(t.Steps) + 1
			}
			t.CurrentTool = ""
			if runErr != nil {
				t.State = taskregistry.StateFailed
				t.Err = runErr
			}
		})
	}

	result := Result{
		SubagentID:   subID,
		Text:         outputText,
		Messages:     msgs,
		Duration:     time.Since(startedAt),
		WorktreePath: worktreePath,
	}
	return result, runErr
}

// subagentDisplayName derives the label shown in /tasks and ActiveItems.
func subagentDisplayName(inv Invocation) string {
	typeName := strings.TrimSpace(inv.TypeName)
	role := strings.TrimSpace(inv.Role)
	if typeName != "" && role != "" {
		return typeName + ": " + role
	}
	if typeName != "" {
		return typeName
	}
	if role != "" {
		return role
	}
	name := inv.Prompt
	if len(name) > 60 {
		name = name[:57] + "..."
	}
	return name
}

// usedTools lists the tools a subagent called, in call order and without repeats.
func usedTools(msgs []SubagentMessage) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range msgs {
		if m.ToolName != "" && !seen[m.ToolName] {
			seen[m.ToolName] = true
			out = append(out, m.ToolName)
		}
	}
	return out
}

// usedSteps lists the subagent's trajectory steps in order.
func usedSteps(msgs []SubagentMessage) []string {
	var out []string
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		if m.ToolName != "" {
			stepStr := formatToolStep(m.ToolName, m.Content)
			out = append(out, stepStr)
		} else if line := firstLine(m.Content); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// usedOutputs lists the output for each tool call in usedSteps in corresponding order.
func usedOutputs(msgs []SubagentMessage) []string {
	var outputs []string
	toolResults := make(map[string]string)
	var orderedResults []string
	for _, m := range msgs {
		if m.Role == "toolResult" {
			if m.ToolCallID != "" {
				toolResults[m.ToolCallID] = m.Content
			}
			orderedResults = append(orderedResults, m.Content)
		}
	}
	resultIdx := 0
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		if m.ToolName != "" {
			out := ""
			if m.ToolCallID != "" && toolResults[m.ToolCallID] != "" {
				out = toolResults[m.ToolCallID]
			} else if resultIdx < len(orderedResults) {
				out = orderedResults[resultIdx]
				resultIdx++
			}
			outputs = append(outputs, out)
		}
	}
	return outputs
}

func formatToolStep(toolName, content string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return ""
	}
	args := extractToolArgs(toolName, content)
	if args != "" {
		return fmt.Sprintf("%s(%s)", toolName, args)
	}
	return toolName
}

func extractToolArgs(toolName, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		var s string
		if err2 := json.Unmarshal([]byte(content), &s); err2 == nil {
			_ = json.Unmarshal([]byte(s), &m)
		}
		if len(m) == 0 {
			s = strings.Trim(content, "\"")
			s = strings.ReplaceAll(s, `\"`, `"`)
			return s
		}
	}
	cleanVal := func(v any) string {
		if v == nil {
			return ""
		}
		str := fmt.Sprint(v)
		str = strings.TrimSpace(str)
		if strings.HasPrefix(str, "\"") && strings.HasSuffix(str, "\"") && len(str) >= 2 {
			var unquoted string
			if err := json.Unmarshal([]byte(str), &unquoted); err == nil {
				str = unquoted
			} else {
				str = str[1 : len(str)-1]
			}
		}
		str = strings.ReplaceAll(str, `\"`, `"`)
		if idx := strings.Index(str, "\n"); idx != -1 {
			str = strings.TrimSpace(str[:idx])
			if !strings.HasSuffix(str, "...") {
				str += " ..."
			}
		}
		if len([]rune(str)) > 80 {
			runes := []rune(str)
			str = string(runes[:77]) + "..."
		}
		return str
	}

	switch strings.ToLower(toolName) {
	case "run_command", "bash":
		for _, k := range []string{"CommandLine", "command_line", "command", "cmd"} {
			if v, ok := m[k]; ok {
				return cleanVal(v)
			}
		}
	case "search_web", "websearch", "web_search":
		for _, k := range []string{"query", "Query", "q"} {
			if v, ok := m[k]; ok {
				return cleanVal(v)
			}
		}
	case "send_message", "message":
		recip := cleanVal(m["Recipient"])
		if recip == "" {
			recip = cleanVal(m["recipient"])
		}
		msg := cleanVal(m["Message"])
		if msg == "" {
			msg = cleanVal(m["message"])
		}
		fl := firstLine(msg)
		if len(fl) > 60 {
			fl = fl[:57] + "..."
		}
		if recip != "" && fl != "" {
			return recip + ": " + fl
		}
		if fl != "" {
			return fl
		}
		if recip != "" {
			return recip
		}
	case "read_file", "view_file", "write_file", "edit_file":
		for _, k := range []string{"Path", "path", "AbsolutePath", "TargetFile", "file"} {
			if v, ok := m[k]; ok {
				return cleanVal(v)
			}
		}
	case "manage_task":
		action := cleanVal(m["action"])
		if action == "" {
			action = cleanVal(m["Action"])
		}
		id := cleanVal(m["id"])
		if id == "" {
			id = cleanVal(m["TaskId"])
		}
		if action != "" && id != "" {
			return action + " " + id
		}
		if action != "" {
			return action
		}
		if id != "" {
			return id
		}
	case "read_url_content":
		for _, k := range []string{"Url", "url", "URL"} {
			if v, ok := m[k]; ok {
				return cleanVal(v)
			}
		}
	}
	for _, v := range m {
		if s := cleanVal(v); s != "" && len(s) < 100 {
			return s
		}
	}
	return ""
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

func invModel(inv Invocation, deps Deps) string {
	m := strings.TrimSpace(inv.Model)
	if m == "" || strings.EqualFold(m, "inherit") {
		m = strings.TrimSpace(deps.Model)
	}
	switch strings.ToLower(m) {
	case "pro":
		return "gemini-4-argon"
	case "flash", "flash_lite":
		return "gemini-3.8-flash"
	default:
		return m
	}
}

func resolveSubagentCWD(agentDir, subID, invWorktree string, deps Deps) (string, string) {
	cwd := strings.TrimSpace(invWorktree)
	if cwd != "" {
		return cwd, cwd
	}
	worktreePath, err := allocateSubagentWorktreeDir(agentDir, subID)
	if err != nil {
		fallback := strings.TrimSpace(deps.CWD)
		if fallback == "" {
			fallback, _ = os.Getwd()
		}
		worktreePath = fallback
	}
	return worktreePath, worktreePath
}

func trajectoryToSubagentMessages(tr *actor.Trajectory) []SubagentMessage {
	if tr == nil {
		return nil
	}
	var out []SubagentMessage
	for _, turn := range tr.Turns() {
		if turn.Input != "" {
			out = append(out, SubagentMessage{
				Role:    "user",
				Content: turn.Input,
			})
		}
		for _, step := range turn.Steps {
			if step.Delta != "" {
				out = append(out, SubagentMessage{
					Role:    "assistant",
					Content: step.Delta,
				})
			}
			for _, tc := range step.ToolCalls {
				out = append(out, SubagentMessage{
					Role:       "assistant",
					ToolName:   tc.Name,
					ToolCallID: tc.ID,
					Content:    tc.Input,
				})
			}
			for _, tr := range step.ToolResults {
				content := tr.Output
				if tr.Error != "" {
					content = tr.Error
				}
				out = append(out, SubagentMessage{
					Role:       "toolResult",
					ToolCallID: tr.ToolCallID,
					Content:    content,
				})
			}
		}
	}
	return out
}

func persistTranscript(subDir, subID string, msgs []SubagentMessage, runErr error) {
	if subDir == "" || subID == "" {
		return
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for i, msg := range msgs {
		line := map[string]any{
			"subagent": subID,
			"index":    i,
			"role":     msg.Role,
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

	brainLogDir := filepath.Join(confhome.Antigravity(), "brain", subID, ".system_generated", "logs")
	if err := os.MkdirAll(brainLogDir, 0o755); err == nil {
		_ = os.WriteFile(filepath.Join(brainLogDir, "transcript.jsonl"), buf.Bytes(), 0o644)
	}
}

// loadTranscript reads a persisted subagent JSONL transcript from disk.
func loadTranscript(subDir, subID string) ([]SubagentMessage, error) {
	if subDir == "" || subID == "" {
		return nil, errors.New("subagenttools: subDir and subID are required")
	}
	path := filepath.Join(subDir, subID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var msgs []SubagentMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		role, _ := raw["role"].(string)
		if role == "error" {
			continue
		}
		var msg SubagentMessage
		if mBytes, err := json.Marshal(raw["message"]); err == nil {
			_ = json.Unmarshal(mBytes, &msg)
		}
		if msg.Role == "" {
			msg.Role = role
		}
		msgs = append(msgs, msg)
	}
	return msgs, scanner.Err()
}
