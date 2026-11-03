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
	"fmt"
)

// Tool is the recovered tool interface. Source: agent.Tool.
type Tool interface {
	Name() string
	Schema() json.RawMessage
	Run(ctx context.Context, args json.RawMessage) (any, error)
}

// Registry is the recovered tool registry. Source: agent.Registry.
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

// Describer is the optional interface a Tool implements to expose the
// model-facing description the reference advertises for it. Tool does not
// require it — the description is not needed by the executor path — but
// anything that re-advertises the registry to a model (the pig loop bridge,
// a future Anthropic-compatible shim) does need it.
//
// The description text is the reference's, captured with the rest of the
// advertised surface; see the provenance note at the top of this file.
type Describer interface {
	Description() string
}

// Describe returns the tool's advertised description, or "" when it carries
// none. Callers that need a non-empty string should fall back to Name().
func Describe(t Tool) string {
	if d, ok := t.(Describer); ok {
		return d.Description()
	}
	return ""
}

// Dispatch runs a named tool with raw JSON arguments and renders its result as
// the text fed back to the model. Unknown tools and errors are reported as
// text rather than Go errors so the loop always has a result to feed back.
//
// Bash and Read tool results are additionally run through StripCitationFootnotes
// before being returned to the model. AGY 1.2.10 (cycle 230): search-result
// citation extraction was running on every tool result, so Bash / Read output
// was getting stray `[N] https://...` footnotes appended. The model never
// asked for them and they polluted the transcript.
func (r *Registry) Dispatch(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	out, err := t.Run(ctx, args)
	if err != nil {
		return "", err
	}
	var resText string
	switch v := out.(type) {
	case nil:
		return "", nil
	case string:
		if ShouldStripCitations(name) {
			v = StripCitationFootnotes(v)
		}
		resText = v
	default:
		b, mErr := json.Marshal(v)
		if mErr != nil {
			resText = fmt.Sprint(v)
		} else {
			s := string(b)
			if ShouldStripCitations(name) {
				s = StripCitationFootnotes(s)
			}
			resText = s
		}
	}
	budget := ApplyOutputBudget(resText, "", DefaultMaxResultTokens)
	return budget.Content, nil
}
