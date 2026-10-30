//go:build !js

package native

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/modular"
	"github.com/justwasm/pigasus/internal/tools"
)

func init() {
	cwd, _ := os.Getwd()
	modular.RegisterToolProvider(NewProvider(cwd))
}

// Provider implements modular.ToolProvider for native environments with full host capabilities.
type Provider struct {
	workspace string
	catalog   *tools.Registry
}

// NewProvider creates a new native tool provider for workspace.
func NewProvider(workspace string) *Provider {
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	return &Provider{
		workspace: workspace,
		catalog:   tools.RuntimeCatalog(workspace),
	}
}

func (p *Provider) Name() string {
	return "native"
}

// Declarations returns the standard reference tool declarations.
func (p *Provider) Declarations() []backend.ToolDecl {
	modelDecls := tools.ModelDeclarations()
	out := make([]backend.ToolDecl, 0, len(modelDecls))
	for _, md := range modelDecls {
		out = append(out, backend.ToolDecl{
			Name:        md.Name,
			Description: md.Description,
			Parameters:  md.Parameters,
		})
	}
	return out
}

// Run executes a tool call using the full native tool registry.
// Unlike WASM, native supports host shell run_command via tools.RuntimeCatalog.
func (p *Provider) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	if _, ok := p.catalog.Get(call.Name); !ok {
		return "", modular.ErrUnknownTool(call.Name)
	}

	rawArgs := strings.TrimSpace(string(call.Arguments))
	if rawArgs == "" || rawArgs == "null" {
		rawArgs = "{}"
	}
	args := json.RawMessage(rawArgs)

	res, err := p.catalog.Dispatch(ctx, call.Name, args)
	if err != nil {
		return fmt.Sprintf("Encountered error in tool %s: %v", call.Name, err), nil
	}
	return res, nil
}
