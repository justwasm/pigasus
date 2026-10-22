package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

// DeleteFile — Source: tools.delete_file (v4 32 #20). The only fs tool
// that survived the v4 rename with the same name.
type DeleteFile struct{ Workspace string }

func (d *DeleteFile) Name() string { return "delete_file" }
func (d *DeleteFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (d *DeleteFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := workspacePath(d.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	return nil, os.Remove(full)
}

// RenameFile — Source: tools.rename_file. Backward-compat (not in v4 32
// list, but kept for 1.3.3 callers).
type RenameFile struct{ Workspace string }

func (r *RenameFile) Name() string { return "rename_file" }
func (r *RenameFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"]}`)
}
func (r *RenameFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	from, err := workspacePath(r.Workspace, p.From)
	if err != nil {
		return nil, err
	}
	to, err := workspacePath(r.Workspace, p.To)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return nil, err
	}
	return nil, os.Rename(from, to)
}

// CopyFile — Source: tools.copy_file. Backward-compat (not in v4 32 list,
// but kept for 1.3.3 callers).
type CopyFile struct{ Workspace string }

func (c *CopyFile) Name() string { return "copy_file" }
func (c *CopyFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"]}`)
}
func (c *CopyFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	from, err := workspacePath(c.Workspace, p.From)
	if err != nil {
		return nil, err
	}
	to, err := workspacePath(c.Workspace, p.To)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return nil, err
	}
	return nil, os.WriteFile(to, data, 0644)
}

// ListDir — Source: tools.list_dir (v4 32 #12). Renamed from
// 1.3.3 list_directory to match v4 naming.
type ListDir struct{ Workspace string }

func (l *ListDir) Name() string { return "list_dir" }
func (l *ListDir) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"recursive":{"type":"boolean"}}}`)
}
func (l *ListDir) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := readPath(l.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	if p.Recursive {
		var entries []string
		err := filepath.Walk(full, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if path == full {
				return nil
			}
			rel, _ := filepath.Rel(full, path)
			if info.IsDir() {
				entries = append(entries, rel+"/")
			} else {
				entries = append(entries, rel)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return entries, nil
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	return out, nil
}
