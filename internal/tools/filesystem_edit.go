package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// resultStamp renders the Created At / Completed At header the reference puts
// in front of every tool result (captured for run_command in cycle 275
// and for read_file / write_file / edit_file in cycle 293).
func resultStamp(start time.Time) string {
	return "Created At: " + start.Format(time.RFC3339) + "\nCompleted At: " + time.Now().Format(time.RFC3339) + "\n"
}

// CreateFile — Source: tools.create_file (v4 32 #14). The new primary
// file-write tool. Renamed from 1.3.3 write_file to match v4 naming.
type CreateFile struct{ Workspace string }

func (c *CreateFile) Name() string { return "create_file" }
func (c *CreateFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}
func (c *CreateFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := workspacePath(c.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(full); statErr == nil && !p.Overwrite {
		return resultStamp(start) + fmt.Sprintf("Encountered error in tool execution: %s already exists. To overwrite it, retry with Overwrite=true. To edit it, view its contents first and use a code edit tool. Otherwise, choose a different file name", full), nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(full, []byte(p.Content), 0644); err != nil {
		return nil, err
	}
	return resultStamp(start) + "Created file.", nil
}

// WriteFile — Alias of CreateFile. The original 1.3.3 name for the file-
// write tool, preserved as an alias struct so that 1.3.3 callers that
// still invoke "write_file" keep working after the v4 rename.
type WriteFile struct{ Workspace string }

func (w *WriteFile) Name() string { return "write_file" }
func (w *WriteFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}
func (w *WriteFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Append bool `json:"append"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Append {
		start := time.Now()
		if _, err := (&AppendFile{Workspace: w.Workspace}).Run(ctx, args); err != nil {
			return nil, err
		}
		return resultStamp(start) + "Created file.", nil
	}
	return (&CreateFile{Workspace: w.Workspace}).Run(ctx, args)
}

// EditFile — Source: tools.edit_file. Backward-compat (not in v4 32 list,
// but kept for 1.3.3 callers). Applies a search/replace edit.
type EditFile struct{ Workspace string }

func (e *EditFile) Name() string { return "edit_file" }
func (e *EditFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"oldString":{"type":"string"},"newString":{"type":"string"}},"required":["path","oldString","newString"]}`)
}

func executeEdit(content, oldStr, newStr, full string, start time.Time) (string, error) {
	if !strings.Contains(content, oldStr) {
		return resultStamp(start) + "Encountered error in tool execution: target content not found in the file; the Target must exactly match existing file text, including whitespace", nil
	}
	line := strings.Count(content[:strings.Index(content, oldStr)], "\n") + 1
	updated := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(full, []byte(updated), 0644); err != nil {
		return "", err
	}
	return resultStamp(start) + fmt.Sprintf("Made 1 replacement in %s starting at line %d (+%d/-%d lines).",
		full, line, countTextLines(newStr), countTextLines(oldStr)), nil
}

func (e *EditFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Path        string `json:"path"`
		OldString   string `json:"oldString"`
		NewString   string `json:"newString"`
		Target      string `json:"target"`
		Replacement string `json:"replacement"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.OldString == "" {
		p.OldString = p.Target
	}
	if p.NewString == "" {
		p.NewString = p.Replacement
	}
	full, err := workspacePath(e.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if p.OldString == "" {
		return nil, fmt.Errorf("edit_file: Target is required")
	}
	return executeEdit(string(data), p.OldString, p.NewString, full, start)
}

func countTextLines(s string) int {
	if s == "" {
		return 0
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Count(s, "\n") + 1
}

// AppendFile — Source: tools.append_file. Backward-compat (not in v4 32
// list, but kept for 1.3.3 callers).
type AppendFile struct{ Workspace string }

func (a *AppendFile) Name() string { return "append_file" }
func (a *AppendFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}
func (a *AppendFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := workspacePath(a.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	_, err = f.WriteString(p.Content)
	return nil, err
}
