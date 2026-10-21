// model_select.go — cycle 101: --model/--effort selection parity.
//
// The 1.3.6 binary validates the (--model, --effort) pair *before* it creates a
// session, but only on the headless path: `agy --model Gemini` starts the TUI
// (and only fails there because there is no TTY), while
// `agy -p /model --model Gemini` exits 1 with
//
//	error: invalid model selection (--model "Gemini" --effort ""): model Gemini
//	is not recognized as a known model or custom model in settings
//	Available models:
//	  Gemini 3.8 Flash (High)
//	  …
//
// Every message below is captured verbatim from the reference.
package main

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
)

// baseModelSpec is a *base* model slug's thinking-level table: the levels it
// exposes and the level the server returns when --effort is absent.
type baseModelSpec struct {
	levels []string
	def    string
}

// baseModelEfforts maps a *base* model slug to its baseModelSpec. Verified
// against 1.3.6:
//
//	--model gemini-3.8-flash                -> gemini-3.8-flash-high
//	--model gemini-3.8-flash --effort low   -> gemini-3.8-flash-low
//	--model gpt-oss-120b                    -> gpt-oss-120b-medium
//	--model gpt-oss-120b --effort high      -> gpt-oss-120b has no "high" effort (available: medium)
var baseModelEfforts = map[string]baseModelSpec{
	"gemini-3.8-flash": {levels: []string{"low", "medium", "high"}, def: "high"},
	"gpt-oss-120b":     {levels: []string{"medium"}, def: "medium"},
}

// resolveDefaultEffort returns the effort the CLI should pick when
// --model <base> is given without --effort, applying the AGY 1.3.4
// changelog chain (line 26 of internal/cli/testdata/changelog_reference.txt):
//
//	"the CLI now picks the model's default thinking level (the server's
//	 default for that model, otherwise medium, otherwise the lowest
//	 available)."
//
// The chain in code:
//
//  1. spec.def, when set and present in spec.levels (the server's default)
//  2. "medium", when present in spec.levels
//  3. The first element of spec.levels (the lowest available)
//
// Returns "" when spec.levels is empty — the caller treats that as "this
// base model has no effort tier to pick" and falls through to the
// catalogue (claude-sonnet-4-6, claude-opus-4-6-thinking, MiniMax M3,
// and — when claude55Available() is true — claude-opus-5-5 and
// claude-sonnet-5-5).
func resolveDefaultEffort(spec baseModelSpec) string {
	if spec.def != "" && containsStr(spec.levels, spec.def) {
		return spec.def
	}
	if containsStr(spec.levels, "medium") {
		return "medium"
	}
	if len(spec.levels) > 0 {
		return spec.levels[0]
	}
	return ""
}

// validEfforts is the global vocabulary the reference accepts for --effort,
// even when the resolved base model does not expose every level: `--effort
// xhigh` parses, then fails as *unavailable* for gemini-3.8-flash rather than
// as an unknown effort name.
var validEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// modelSelectionError carries the reference's exact stderr text (exit code 1).
type modelSelectionError struct{ msg string }

func (e *modelSelectionError) Error() string { return e.msg }

func invalidModelSelection(model, effort, reason string) error {
	return &modelSelectionError{msg: fmt.Sprintf(
		"error: invalid model selection (--model %q --effort %q): %s", model, effort, reason)}
}

// catalogueIDBySlug resolves a full model *slug* to its catalogue ID. Labels do
// not match: the reference distinguishes the two when --effort is also given
// (`--model gemini-3.8-flash-high --effort low` says "conflicts with" while
// `--model "Gemini 3.8 Flash (High)" --effort low` says "is not supported").
func catalogueIDBySlug(name string) (string, bool) {
	for _, m := range cli.ReferenceModels() {
		if m.ID == name {
			return m.ID, true
		}
	}
	return "", false
}

// catalogueID resolves a model name — base slug, full slug or display label —
// to the catalogue ID `agy models` prints.
func catalogueID(name string) (string, bool) {
	for _, m := range cli.ReferenceModels() {
		if m.ID == name || m.Label == name {
			return m.ID, true
		}
	}
	return "", false
}

// catalogueLabel returns the display label for a catalogue ID.
func catalogueLabel(id string) string {
	for _, m := range cli.ReferenceModels() {
		if m.ID == id {
			return m.Label
		}
	}
	return id
}

// availableModelsBlock is the list the reference appends to the unknown-model
// error.
func availableModelsBlock() string {
	var b strings.Builder
	b.WriteString("Available models:")
	for _, m := range cli.ReferenceModels() {
		b.WriteString("\n  ")
		b.WriteString(m.Label)
	}
	return b.String()
}

// resolveModelSelection reproduces the reference's --model/--effort validation
// and returns the resolved catalogue ID ("" when neither flag was given).
func resolveModelSelection(model, effort string) (string, error) {
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)

	if effort != "" && !containsStr(validEfforts, effort) {
		return "", invalidModelSelection(model, effort,
			fmt.Sprintf("invalid --effort %q (valid: low, medium, high, xhigh, max)", effort))
	}

	if effort != "" {
		base := "gemini-3.8-flash"
		if model != "" {
			if _, ok := baseModelEfforts[model]; ok {
				base = model
			} else if _, ok := catalogueIDBySlug(model); ok {
				return "", invalidModelSelection(model, effort,
					fmt.Sprintf("--model %s conflicts with --effort=%s", model, effort))
			} else {
				return "", invalidModelSelection(model, effort,
					fmt.Sprintf("--effort is not supported for model %q", model))
			}
		}
		spec := baseModelEfforts[base]
		if !containsStr(spec.levels, effort) {
			return "", invalidModelSelection(model, effort,
				fmt.Sprintf("%s has no %q effort (available: %s)", base, effort, strings.Join(spec.levels, ", ")))
		}
		return base + "-" + effort, nil
	}

	if model == "" {
		return "", nil
	}
	if spec, ok := baseModelEfforts[model]; ok {
		// AGY 1.3.4 changelog line 26: --model <base> without --effort picks
		// the model's default thinking level via the server default →
		// medium → lowest-available chain in resolveDefaultEffort.
		if def := resolveDefaultEffort(spec); def != "" {
			return model + "-" + def, nil
		}
		// spec had no levels to pick — fall through to the catalogue so a
		// future base model entry can't trap the user in an empty slug.
	}
	if id, ok := catalogueID(model); ok {
		return id, nil
	}
	return "", invalidModelSelection(model, effort,
		"model "+model+" is not recognized as a known model or custom model in settings\n"+availableModelsBlock())
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
