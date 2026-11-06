// Package agenttools advertises the restricted tool surface a markdown custom
// agent gets.
//
// A 1.4.7 `--agent foo -p` request captured through the clone's own proxy
// carries exactly seven declarations, in this order:
//
//	send_message, view_file, read_url_content, search_web, schedule,
//	generate_image, manage_task
//
// They are the subagent/read-only surface: no run_command, no file writes, no
// subagent management. The declarations (descriptions and parameter schemas)
// are reproduced byte for byte from that request in tools_1.4.7.json; the
// executors are the clone's existing registry tools of the same name.
package agenttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/tools"
)

//go:embed tools_1.4.7.json
var declarationsJSON []byte

// Declaration is one captured function declaration.
type Declaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

var (
	once         sync.Once
	declarations []Declaration
	parseErr     error
)

// Declarations returns the seven captured declarations in request order.
func Declarations() ([]Declaration, error) {
	once.Do(func() {
		parseErr = json.Unmarshal(declarationsJSON, &declarations)
	})
	return declarations, parseErr
}

// Names returns the advertised names in request order.
func Names() []string {
	decls, err := Declarations()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(decls))
	for _, d := range decls {
		out = append(out, d.Name)
	}
	return out
}

// Tools adapts the captured declarations onto the clone's executors. A
// declaration with no executor still advertises (and fails with the registry's
// unknown-tool error at call time), keeping the wire surface exact.
func Tools(registry *tools.Registry) []actor.Tool {
	decls, err := Declarations()
	if err != nil {
		return nil
	}
	out := make([]actor.Tool, 0, len(decls))
	for _, d := range decls {
		var inner tools.Tool
		if registry != nil {
			inner, _ = registry.Get(d.Name)
		}
		out = append(out, &tool{decl: d, inner: inner})
	}
	return out
}

type tool struct {
	decl  Declaration
	inner tools.Tool
}

func (t *tool) Name() string        { return t.decl.Name }
func (t *tool) Description() string { return t.decl.Description }
func (t *tool) RequiresApproval(_ string) bool { return false }

func (t *tool) Definition() actor.ToolDefinition {
	return actor.ToolDefinition{
		Name:        t.decl.Name,
		Description: t.decl.Description,
		Parameters:  t.decl.Parameters,
	}
}

func (t *tool) Execute(ctx context.Context, input string) (actor.ToolResult, error) {
	if t.inner == nil {
		return actor.ToolResult{Error: fmt.Sprintf("unknown tool %q", t.decl.Name)}, fmt.Errorf("unknown tool %q", t.decl.Name)
	}
	out, err := t.inner.Run(ctx, json.RawMessage(input))
	if err != nil {
		return actor.ToolResult{Error: err.Error()}, err
	}
	return actor.ToolResult{
		Output: render(out),
	}, nil
}

func render(out any) string {
	switch v := out.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	case json.RawMessage:
		return string(v)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Sprint(out)
	}
	return string(b)
}

// Registry returns a tools.Registry holding only the captured surface, with
// the captured descriptions and schemas. The TUI advertises tools through
// backend.ToolDecl, which only carries Name+Parameters; wrapping the registry
// keeps the parameter schemas exact there too.
func Registry(source *tools.Registry) *tools.Registry {
	out := tools.NewRegistry()
	decls, err := Declarations()
	if err != nil {
		return out
	}
	for _, d := range decls {
		var inner tools.Tool
		if source != nil {
			inner, _ = source.Get(d.Name)
		}
		out.Register(&registryTool{decl: d, inner: inner})
	}
	return out
}

type registryTool struct {
	decl  Declaration
	inner tools.Tool
}

func (t *registryTool) Name() string { return t.decl.Name }

func (t *registryTool) Description() string { return t.decl.Description }

func (t *registryTool) Schema() json.RawMessage {
	b, err := json.Marshal(t.decl.Parameters)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return b
}

func (t *registryTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	if t.inner == nil {
		return nil, fmt.Errorf("unknown tool %q", t.decl.Name)
	}
	return t.inner.Run(ctx, args)
}
