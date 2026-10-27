// Package tools implements the recovered Antigravity tool surface.
//
// The advertised surface is the catalogue in catalog.go: it holds the
// reference's own sixty names (1.4.7) and, for each of them, the
// executor the clone implements — or a pendingTool when the reference
// advertises the name but the clone has no implementation yet.
//
// Cycle 346: this file used to carry a **second** registry —
// `coreTools` + `stubTools` + `RegisterAll`, 42 entries built from the
// v4 tool set — which nothing but one test ever called. Its executor
// set was a parallel copy of the catalogue's (a second `Finish`, a
// second `Wait`, a second `SendMessage`, …) plus nine `git_*` and
// twenty-eight v4 stub tools the reference does not advertise. All of
// it is gone; the catalogue is the one registry.
//
// The package exposes Tool (Name/Schema/Run) and Registry
// (Register/Get/List/Size).
package tools

import (
	"context"
	"encoding/json"
	"errors"
)

// Tool is the recovered tool interface. Source: agent.tools.Tool.
type Tool interface {
	Name() string
	Schema() json.RawMessage
	Run(ctx context.Context, args json.RawMessage) (any, error)
}

// Registry is the recovered tool registry. Source: agent.tools.Registry.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

func (r *Registry) Register(t Tool) { r.tools[t.Name()] = t }
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

// Size returns the number of registered tools.
func (r *Registry) Size() int { return len(r.tools) }

// ErrNotImplemented is returned by tools whose real implementation
// is not yet available in the recovered build. Cycle 50: the error
// string no longer uses the "placeholder" prefix (matches the
// v1.3.5 binary convention; the prefix was a cycle-25 stub marker
// the v1.3.5 binary does not emit).
var ErrNotImplemented = errors.New("tool not implemented in recovered build")

// PlaceholderStr describes the placeholder behaviour.
const PlaceholderStr = "recovered v4 placeholder; sandbox denies real fs/git/browser ops in CI"
