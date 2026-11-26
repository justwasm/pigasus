// Package terminal implements the in-process `terminal` tool backed by
// internal/infra/terminalmgr. Its wire declaration is intentionally minimal so
// default context stays small; full usage instructions live in the built-in
// `terminal` skill.
package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/terminalmgr"
)

const (
	// ToolName is the model-facing name of the in-process terminal tool.
	ToolName = "terminal"

	// MinimalDescription is the compact declaration description sent on the wire.
	MinimalDescription = "In-process persistent terminal tool. Read the terminal skill before calling."
)

var minimalWireParams = json.RawMessage(`{"type":"OBJECT","properties":{"action":{"type":"STRING"},"args":{"type":"OBJECT"}},"required":["action"]}`)
var minimalSchema = json.RawMessage(`{"type":"object","properties":{"action":{"type":"string"},"args":{"type":"object"}},"required":["action"]}`)

// MinimalDecl returns the compact backend.ToolDecl advertised to models.
func MinimalDecl() backend.ToolDecl {
	return backend.ToolDecl{
		Name:        ToolName,
		Description: MinimalDescription,
		Parameters:  minimalWireParams,
	}
}

// Tool exposes persistent in-process terminal sessions as a single tool.
type Tool struct {
	Manager terminalmgr.Backend
}

func (t *Tool) Name() string            { return ToolName }
func (t *Tool) Description() string     { return MinimalDescription }
func (t *Tool) Schema() json.RawMessage { return minimalSchema }

type callEnvelope struct {
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args"`
}

type actionArgs struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Title      string   `json:"title"`
	Cwd        string   `json:"cwd"`
	Command    string   `json:"command"`
	Cmd        string   `json:"cmd"`
	Text       string   `json:"text"`
	Input      string   `json:"input"`
	Keys       []string `json:"keys"`
	Paste      bool     `json:"paste"`
	Focus      bool     `json:"focus"`
	WaitMs     *int     `json:"wait_ms"`
	Scrollback int      `json:"scrollback"`
	Raw        bool     `json:"raw"`
	Rows       int      `json:"rows"`
	Cols       int      `json:"cols"`
}

func (t *Tool) Run(ctx context.Context, raw json.RawMessage) (any, error) {
	if t == nil || t.Manager == nil {
		return nil, fmt.Errorf("terminal: manager is not configured")
	}
	var env callEnvelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("terminal: invalid arguments: %w", err)
		}
	}
	action := strings.ToLower(strings.TrimSpace(env.Action))
	if action == "" {
		return nil, fmt.Errorf("terminal: action is required (read the terminal skill for usage)")
	}

	// Support both nested {"action":"...","args":{...}} and flat top-level fields.
	var p actionArgs
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	if len(env.Args) > 0 && string(env.Args) != "null" {
		if err := json.Unmarshal(env.Args, &p); err != nil {
			return nil, fmt.Errorf("terminal: invalid args: %w", err)
		}
	}
	if p.Command == "" && p.Cmd != "" {
		p.Command = p.Cmd
	}
	if p.Text == "" && p.Input != "" {
		p.Text = p.Input
	}

	switch action {
	case "list", "ls":
		sessions := t.Manager.List()
		return formatList(sessions), nil

	case "new", "create":
		res, err := t.Manager.New(ctx, terminalmgr.NewOptions{
			Name:    p.Name,
			Cwd:     p.Cwd,
			Command: p.Command,
			Rows:    p.Rows,
			Cols:    p.Cols,
			Focus:   p.Focus,
			WaitMs:  p.WaitMs,
		})
		if err != nil {
			return nil, err
		}
		return formatCapture("Created terminal session", res), nil

	case "capture", "read", "screen":
		res, err := t.Manager.Capture(ctx, terminalmgr.CaptureOptions{
			ID:         p.ID,
			Scrollback: p.Scrollback,
			Raw:        p.Raw,
		})
		if err != nil {
			return nil, err
		}
		return formatCapture("Captured terminal session", res), nil

	case "send", "send_input", "input":
		res, err := t.Manager.Send(ctx, terminalmgr.SendOptions{
			ID:     p.ID,
			Text:   p.Text,
			Keys:   p.Keys,
			Paste:  p.Paste,
			WaitMs: p.WaitMs,
		})
		if err != nil {
			return nil, err
		}
		return formatCapture("Sent input to terminal session", res), nil

	case "resize":
		if p.Rows <= 0 || p.Cols <= 0 {
			return nil, fmt.Errorf("terminal: resize requires positive rows and cols")
		}
		info, err := t.Manager.Resize(p.ID, p.Rows, p.Cols)
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("Resized terminal %s to %dx%d.", formatSessionLabel(info), info.Rows, info.Cols), nil

	case "switch", "focus", "select":
		info, err := t.Manager.Switch(p.ID)
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("Switched active terminal to %s.", formatSessionLabel(info)), nil

	case "rename", "title":
		title := p.Title
		if title == "" {
			title = p.Name
		}
		info, err := t.Manager.Rename(p.ID, title)
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("Renamed terminal %s to %q.", formatSessionLabel(info), info.Title), nil

	case "kill", "close":
		info, err := t.Manager.Kill(p.ID)
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("Closed terminal %s.", formatSessionLabel(info)), nil

	default:
		return nil, fmt.Errorf("terminal: unknown action %q (read the terminal skill for valid actions)", env.Action)
	}
}

// Ensure Tool satisfies tools.Tool and tools.Describer at compile time.
var (
	_ tools.Tool      = (*Tool)(nil)
	_ tools.Describer = (*Tool)(nil)
)

func formatSessionLabel(info terminalmgr.SessionInfo) string {
	if info.Name != "" {
		return fmt.Sprintf("%s (%s)", info.Name, info.ID)
	}
	return info.ID
}

func formatList(sessions []terminalmgr.SessionInfo) string {
	if len(sessions) == 0 {
		return "No active terminal sessions."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Terminal sessions (%d):\n", len(sessions))
	for _, s := range sessions {
		fmt.Fprintf(&b, "- id=%s", s.ID)
		if s.Name != "" {
			fmt.Fprintf(&b, " name=%s", s.Name)
		}
		if s.Title != "" {
			fmt.Fprintf(&b, " title=%q", s.Title)
		}
		if s.Cwd != "" {
			fmt.Fprintf(&b, " cwd=%s", s.Cwd)
		}
		if s.Active {
			b.WriteString(" active=true")
		}
		if s.Self {
			b.WriteString(" self=true")
		}
		fmt.Fprintf(&b, " size=%dx%d cursor=(%d,%d) status=%q scrollback=%d\n",
			s.Rows, s.Cols, s.CursorX, s.CursorY, s.Status, s.ScrollbackLines)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatCapture(header string, res terminalmgr.CaptureResult) string {
	s := res.Session
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", header, formatSessionLabel(s))
	fmt.Fprintf(&b, "status=%s size=%dx%d cursor=(%d,%d)", s.Status, s.Rows, s.Cols, s.CursorX, s.CursorY)
	if s.Active {
		b.WriteString(" active=true")
	}
	if s.Self {
		b.WriteString(" self=true")
	}
	if s.Cwd != "" {
		fmt.Fprintf(&b, " cwd=%s", s.Cwd)
	}
	if s.Title != "" {
		fmt.Fprintf(&b, " title=%q", s.Title)
	}
	b.WriteByte('\n')

	if len(res.Scrollback) > 0 {
		fmt.Fprintf(&b, "--- scrollback (%d lines) ---\n", len(res.Scrollback))
		for _, line := range res.Scrollback {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	b.WriteString("--- screen ---\n")
	if len(res.Screen) == 0 {
		b.WriteString("(empty)\n")
	} else {
		for _, line := range res.Screen {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
