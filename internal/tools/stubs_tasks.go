package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/justwasm/pigasus/internal/subagent"
)

// Wait — Wait duration. v4 32 #23.
type Wait struct{ Workspace string }

func (s *Wait) Name() string { return "wait" }
func (s *Wait) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"seconds":{"type":"integer"},"duration":{"type":"string"}}}`)
}
func (s *Wait) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Seconds  int    `json:"seconds"`
		Duration string `json:"duration"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	var d time.Duration
	switch {
	case p.Seconds > 0:
		d = time.Duration(p.Seconds) * time.Second
	case p.Duration != "":
		parsed, err := time.ParseDuration(p.Duration)
		if err != nil {
			return nil, fmt.Errorf("wait: invalid duration %q: %w", p.Duration, err)
		}
		d = parsed
	default:
		return nil, errors.New("wait: either seconds or duration is required")
	}
	select {
	case <-time.After(d):
		return fmt.Sprintf("ok: waited %s", d), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CreateTask — Spawn subagent. v4 32 #27.
type CreateTask struct{ Workspace string }

func (s *CreateTask) Name() string { return "create_task" }
func (s *CreateTask) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"agentName":{"type":"string"},"prompt":{"type":"string"},"title":{"type":"string"}},"required":["agentName","prompt"]}`)
}
func (s *CreateTask) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		AgentName string `json:"agent_name"`
		Prompt    string `json:"prompt"`
		Title     string `json:"title"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.AgentName == "" || p.Prompt == "" {
		return nil, errors.New("create_task: agent_name and prompt are required")
	}
	sa := subagent.NewSubagent(p.AgentName, p.Prompt)
	sa.SetTitle(p.Title)
	defaultSubagentManager.Add(sa)
	defaultTaskRegistry.AttachSubagent(sa, "")
	if err := sa.Start(); err != nil {
		return nil, fmt.Errorf("create_task: start: %w", err)
	}
	if err := sa.Wait(ctx); err != nil {
		return nil, fmt.Errorf("create_task: wait: %w", err)
	}
	return map[string]any{
		"id":         sa.ID,
		"name":       sa.Name,
		"title":      p.Title,
		"state":      sa.State,
		"result":     sa.Result,
		"agent_name": p.AgentName,
	}, nil
}

// SendMessage — Message subagent. v4 32 #28.
type SendMessage struct{ Workspace string }

func (s *SendMessage) Name() string { return "send_message" }
func (s *SendMessage) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"},"content":{"type":"string"}},"required":["taskId","content"]}`)
}
func (s *SendMessage) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		TaskID  string `json:"task_id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.TaskID == "" || p.Content == "" {
		return nil, errors.New("send_message: task_id and content are required")
	}
	sa, ok := defaultSubagentManager.Get(p.TaskID)
	if !ok {
		return nil, fmt.Errorf("send_message: subagent %q not found (was create_task ever called?)", p.TaskID)
	}
	if err := sa.Send(p.Content); err != nil {
		return nil, fmt.Errorf("send_message: send: %w", err)
	}
	return fmt.Sprintf("ok: message sent to %s (state=%s)", p.TaskID, sa.State), nil
}

// NotifyUser — Notify user. v4 32 #7.
type NotifyUser struct{ Workspace string }

func (s *NotifyUser) Name() string { return "notify_user" }
func (s *NotifyUser) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"},"priority":{"type":"string","enum":["low","medium","high"]}},"required":["message"]}`)
}
func (s *NotifyUser) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Message  string `json:"message"`
		Priority string `json:"priority"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Message == "" {
		return nil, errors.New("notify_user: message is required")
	}
	if p.Priority == "" {
		p.Priority = "medium"
	}
	fmt.Printf("[notify_user][%s] %s\n", p.Priority, p.Message)
	marker := filepath.Join(resolveDir(s.Workspace), ".notified")
	entry := fmt.Sprintf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), p.Priority, p.Message)
	f, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return nil, err
	}
	return "ok: notified user", nil
}

// TaskBoundary — Mark task boundary. v4 32 #9.
type TaskBoundary struct{ Workspace string }

func (s *TaskBoundary) Name() string { return "task_boundary" }
func (s *TaskBoundary) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"},"type":{"type":"string","enum":["start","end"]},"id":{"type":"string"}},"required":["label","type"]}`)
}
func (s *TaskBoundary) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Label string `json:"label"`
		Type  string `json:"type"`
		ID    string `json:"id"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Label == "" || p.Type == "" {
		return nil, errors.New("task_boundary: label and type are required")
	}
	if p.ID == "" {
		p.ID = fmt.Sprintf("%s-%d", p.Type, time.Now().UnixNano())
	}
	marker := filepath.Join(resolveDir(s.Workspace), fmt.Sprintf(".task_boundary_%s", p.ID))
	body := fmt.Sprintf("label=%s\ntype=%s\nat=%s\n", p.Label, p.Type, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(marker, []byte(body), 0644); err != nil {
		return nil, err
	}
	return fmt.Sprintf("ok: task boundary %s -> %s (marker: %s)", p.Type, p.Label, marker), nil
}

// Unzip — Extract zip archive. v4 32 #11.
type Unzip struct{ Workspace string }

func (s *Unzip) Name() string { return "unzip" }
func (s *Unzip) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"destination":{"type":"string"}},"required":["archive"]}`)
}
func (s *Unzip) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Archive     string `json:"archive"`
		Destination string `json:"destination"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Archive == "" {
		return nil, errors.New("unzip: archive is required")
	}
	dst := p.Destination
	if dst == "" {
		dst = "."
	}
	if !filepath.IsAbs(p.Archive) && s.Workspace != "" {
		p.Archive = filepath.Join(s.Workspace, p.Archive)
	}
	if !filepath.IsAbs(dst) && s.Workspace != "" {
		dst = filepath.Join(s.Workspace, dst)
	}
	out, err := execRun(ctx, "", "unzip", "-o", p.Archive, "-d", dst)
	return trim(out), err
}

// GenerateImage — Image generation (reference name `generate_image`).
type GenerateImage struct {
	Workspace string
	Provider  func(ctx context.Context, prompt, size string) (string, error)
}

func (s *GenerateImage) Name() string { return "generate_image" }
func (s *GenerateImage) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"Prompt":{"type":"string"},"ImageName":{"type":"string"}},"required":["Prompt","ImageName"]}`)
}
func (s *GenerateImage) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Prompt    string `json:"prompt"`
		Size      string `json:"size"`
		ImageName string `json:"image_name"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Prompt == "" {
		return nil, errors.New("generate_image: prompt is required")
	}
	if s.Provider != nil {
		out, err := s.Provider(ctx, p.Prompt, p.Size)
		if err != nil {
			return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
		}
		return resultStamp(start) + out, nil
	}
	return resultStamp(start) + "Encountered error in tool execution: no response in generate content result", nil
}
