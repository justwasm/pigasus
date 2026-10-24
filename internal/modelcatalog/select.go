// Package modelcatalog handles model catalogue and --model/--effort selection parity.
package modelcatalog

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
)

// BaseModelSpec is a base model slug's thinking-level table: the levels it
// exposes and the level the server returns when --effort is absent.
type BaseModelSpec struct {
	Levels []string
	Def    string
}

// BaseModelEfforts maps a base model slug to its BaseModelSpec.
var BaseModelEfforts = map[string]BaseModelSpec{
	"gemini-3.8-flash": {Levels: []string{"low", "medium", "high"}, Def: "high"},
	"gpt-oss-120b":     {Levels: []string{"medium"}, Def: "medium"},
}

// ResolveDefaultEffort returns the effort the CLI should pick when
// --model <base> is given without --effort.
func ResolveDefaultEffort(spec BaseModelSpec) string {
	if spec.Def != "" && containsStr(spec.Levels, spec.Def) {
		return spec.Def
	}
	if containsStr(spec.Levels, "medium") {
		return "medium"
	}
	if len(spec.Levels) > 0 {
		return spec.Levels[0]
	}
	return ""
}

// ValidEfforts is the global vocabulary the reference accepts for --effort.
var ValidEfforts = []string{"low", "medium", "high", "xhigh", "max"}

type modelSelectionError struct{ msg string }

func (e *modelSelectionError) Error() string { return e.msg }

func invalidModelSelection(model, effort, reason string) error {
	return &modelSelectionError{msg: fmt.Sprintf(
		"error: invalid model selection (--model %q --effort %q): %s", model, effort, reason)}
}

// CatalogueIDBySlug resolves a full model slug to its catalogue ID.
func CatalogueIDBySlug(name string) (string, bool) {
	for _, m := range cli.ModelCatalogue() {
		if m.ID == name {
			return m.ID, true
		}
	}
	return "", false
}

// CatalogueID resolves a model name to the catalogue ID.
func CatalogueID(name string) (string, bool) {
	for _, m := range cli.ModelCatalogue() {
		if m.ID == name || m.Label == name {
			return m.ID, true
		}
	}
	return "", false
}

// CatalogueLabel returns the display label for a catalogue ID.
func CatalogueLabel(id string) string {
	for _, m := range cli.ModelCatalogue() {
		if m.ID == id {
			return m.Label
		}
	}
	return id
}

func gatedModel(name string) (cli.GatedModel, bool) {
	for _, g := range cli.ReferenceGatedModels() {
		if g.ID == name || g.Label == name {
			return g, true
		}
	}
	return cli.GatedModel{}, false
}

// AvailableModelsBlock returns the reference's available models list.
func AvailableModelsBlock() string {
	return availableModelsBlock()
}

func availableModelsBlock() string {
	var b strings.Builder
	b.WriteString("Available models:")
	for _, m := range cli.ModelCatalogue() {
		b.WriteString("\n  ")
		b.WriteString(m.Label)
	}
	return b.String()
}

// ResolveModelSelection reproduces the reference's --model/--effort validation.
func ResolveModelSelection(model, effort string) (string, error) {
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)

	if effort != "" && !containsStr(ValidEfforts, effort) {
		return "", invalidModelSelection(model, effort,
			fmt.Sprintf("invalid --effort %q (valid: low, medium, high, xhigh, max)", effort))
	}
	if g, ok := gatedModel(model); ok {
		return "", invalidModelSelection(model, effort,
			fmt.Sprintf("%q is unavailable: %s\n%s", g.Label, g.Reason, availableModelsBlock()))
	}
	if effort != "" {
		return resolveModelWithEffort(model, effort)
	}
	return resolveModelWithoutEffort(model)
}

func resolveModelWithEffort(model, effort string) (string, error) {
	base := "gemini-3.8-flash"
	if model != "" {
		if _, ok := BaseModelEfforts[model]; ok {
			base = model
		} else if _, ok := CatalogueIDBySlug(model); ok {
			return "", invalidModelSelection(model, effort,
				fmt.Sprintf("--model %s conflicts with --effort=%s", model, effort))
		} else {
			return "", invalidModelSelection(model, effort,
				fmt.Sprintf("--effort is not supported for model %q", model))
		}
	}
	spec := BaseModelEfforts[base]
	if !containsStr(spec.Levels, effort) {
		return "", invalidModelSelection(model, effort,
			fmt.Sprintf("%s has no %q effort (available: %s)", base, effort, strings.Join(spec.Levels, ", ")))
	}
	return base + "-" + effort, nil
}

func resolveModelWithoutEffort(model string) (string, error) {
	if model == "" {
		return "", nil
	}
	if spec, ok := BaseModelEfforts[model]; ok {
		if def := ResolveDefaultEffort(spec); def != "" {
			return model + "-" + def, nil
		}
	}
	if id, ok := CatalogueID(model); ok {
		return id, nil
	}
	return "", invalidModelSelection(model, "",
		"model "+model+" is not recognized as a known model or custom model in settings\n"+availableModelsBlock())
}

// ParseEffortWord normalises a user-typed effort string to canonical effort.
func ParseEffortWord(w string) string {
	w = strings.ToLower(strings.TrimSpace(w))
	for _, v := range ValidEfforts {
		if w == v {
			return v
		}
	}
	return ""
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ModelBaseSlug strips a trailing effort suffix from a catalogue id:
// "gemini-3.8-flash-high" -> "gemini-3.8-flash". Ids that carry no effort
// suffix (claude-sonnet-4-6, claude-opus-4-6-thinking) are returned unchanged.
func ModelBaseSlug(id string) string {
	for _, e := range ValidEfforts {
		if strings.HasSuffix(id, "-"+e) {
			return strings.TrimSuffix(id, "-"+e)
		}
	}
	return id
}

// ModelEffortFromID returns the effort a catalogue id encodes, or "".
func ModelEffortFromID(id string) string {
	base := ModelBaseSlug(id)
	if base == id {
		return ""
	}
	return strings.TrimPrefix(id, base+"-")
}

// ModelSlug turns a display model name into the reference's slug form:
// "Gemini 3.8 Flash (High)" -> "gemini-3.8-flash-high". Cycle 101: the
// catalogue is consulted first, because the mechanical transform disagrees with
// the reference for Claude ("Claude Sonnet 4.6 (Thinking)" is
// "claude-sonnet-4-6", not "claude-sonnet-4.6-thinking").
func ModelSlug(name string) string {
	if id, ok := CatalogueID(name); ok {
		return id
	}
	s := strings.ToLower(name)
	// Two passes: the parens become spaces first, then the spaces become
	// dashes. strings.Replacer is single-pass, so it cannot do both at once.
	s = strings.NewReplacer("(", " ", ")", " ", "_", "-").Replace(s)
	s = strings.ReplaceAll(s, " ", "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
