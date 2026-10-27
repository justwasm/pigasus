// Package modelcatalog handles model catalogue and --model/--effort selection parity.
package modelcatalog

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/models"
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
//
// It is the models package's closed set — the same five words the Web UI's
// dispatcher accepts, and the ones a configured model may declare as its
// reasoning tiers.
var ValidEfforts = models.EffortLevels

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
		} else if tiers, ok := configuredTiers(model); ok {
			// A model the user configured, with the tiers its own
			// configuration states. The reference's own rule applies:
			// a tier the model does not offer is refused, and the
			// answer is the model's id with the tier spelled into it.
			if !containsStr(tiers, effort) {
				return "", invalidModelSelection(model, effort,
					fmt.Sprintf("%s has no %q effort (available: %s)", model, effort, strings.Join(tiers, ", ")))
			}
			m, _ := configuredModel(model)
			return models.TierRowID(m.Name, effort), nil
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
	// A model the user configured, with tiers: its bare id stands for
	// the model's default tier — the reference's own rule ("--model
	// <name>" without "--effort" picks the model's server default).
	if m, ok := configuredModel(model); ok {
		if tier := models.DefaultEffort(m); tier != "" {
			return models.TierRowID(m.Name, tier), nil
		}
	}
	return "", invalidModelSelection(model, "",
		"model "+model+" is not recognized as a known model or custom model in settings\n"+availableModelsBlock())
}

// configuredModel returns the configured model a name names — by its
// catalogue id or by its display label — and whether it names one at all.
func configuredModel(name string) (models.Model, bool) {
	reg := models.CachedRegistry()
	if reg == nil {
		return models.Model{}, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Model{}, false
	}
	for _, m := range reg.List() {
		if m.Name == name || strings.TrimSpace(m.DisplayName) == name {
			return m, true
		}
	}
	return models.Model{}, false
}

// configuredTiers returns the reasoning tiers a configured model offers,
// and whether the name names a configured model with tiers at all.
//
// A model that declares no tiers is not "a model with tiers": it is one
// row, and the reference's --effort refuses to override it.
func configuredTiers(name string) ([]string, bool) {
	m, ok := configuredModel(name)
	if !ok {
		return nil, false
	}
	tiers := models.NormalizeEfforts(m.ReasoningEfforts)
	if len(tiers) == 0 {
		return nil, false
	}
	return tiers, true
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
	return models.BaseModelID(id)
}

// ModelEffortFromID returns the effort a catalogue id encodes, or "".
func ModelEffortFromID(id string) string {
	return models.TierFromID(id)
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
