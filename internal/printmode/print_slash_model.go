package printmode

import (
	"flag"
	"strings"

	"github.com/justwasm/pigasus/internal/modelcatalog"
)

// PrintSlashModels prints the effective model as "<slug>\t<label>".
func PrintSlashModels(fs *flag.FlagSet) string {
	return printSlashModels(fs)
}

func printSlashModels(fs *flag.FlagSet) string {
	id, label, err := printSlashModelID(fs)
	if err != nil {
		return ""
	}
	return id + "\t" + label + "\n"
}

// printSlashModelID resolves the model `/model` reports, as a slug and a
// display label.
//
// Cycle 114: --effort alone also selects a model. Captured from the reference:
//
//	agy -p=/model --effort=low    -> gemini-3.8-flash-low  Gemini 3.8 Flash (Low)
//	agy -p=/model --effort=xhigh  -> rc 1, "gemini-3.8-flash has no \"xhigh\"
//	                                 effort (available: low, medium, high)"
//
// so the (--model, --effort) pair is validated on the slash path too, and a
// failure becomes an ERROR envelope.
func printSlashModelID(fs *flag.FlagSet) (id, label string, err error) {
	model := flagStringValue(fs, "model")
	effort := flagStringValue(fs, "effort")
	if model != "" || effort != "" {
		id, err := modelcatalog.ResolveModelSelection(model, effort)
		if err != nil {
			return "", "", err
		}
		return id, modelcatalog.CatalogueLabel(id), nil
	}
	name, _ := modelcatalog.ResolveEffectiveModel("")
	return modelSlug(name), name, nil
}

// jsonModelInfo is the `/model` command payload. Field order is the
// reference's; a map would marshal alphabetically (effort, id, is_default,
// label) and not match.
type jsonModelInfo struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Effort    string `json:"effort"`
	IsDefault bool   `json:"is_default"`
}

// printSlashModelData renders the `/model` command payload.
//
// `is_default` is false in every capture (with and without --model, for three
// different models), so the clone emits false; the flag's real meaning is
// unrecovered.
func printSlashModelData(id, label string) any {
	return jsonModelInfo{
		ID:        id,
		Label:     label,
		Effort:    modelEffortFromID(id),
		IsDefault: false,
	}
}

// modelBaseSlug strips a trailing effort suffix from a catalogue id:
// "gemini-3.8-flash-high" -> "gemini-3.8-flash". Ids that carry no effort
// suffix (claude-sonnet-4-6, claude-opus-4-6-thinking) are returned unchanged.
func modelBaseSlug(id string) string {
	for _, e := range modelcatalog.ValidEfforts {
		if strings.HasSuffix(id, "-"+e) {
			return strings.TrimSuffix(id, "-"+e)
		}
	}
	return id
}

// modelEffortFromID returns the effort a catalogue id encodes, or "".
func modelEffortFromID(id string) string {
	base := modelBaseSlug(id)
	if base == id {
		return ""
	}
	return strings.TrimPrefix(id, base+"-")
}

// jsonEffortInfo is the `/effort` command payload, in the reference's field
// order (a map would sort adjustable, available, current).
type jsonEffortInfo struct {
	Adjustable bool     `json:"adjustable"`
	Current    string   `json:"current"`
	Available  []string `json:"available"`
}

// printSlashEffortData renders the `/effort` command payload. `current` is the
// same word the text answer carries; `available` and `adjustable` come from the
// resolved model's effort levels.
func printSlashEffortData(id, current string) any {
	levels := []string{}
	adjustable := false
	if spec, ok := modelcatalog.BaseModelEfforts[modelBaseSlug(id)]; ok {
		levels = spec.Levels
		adjustable = len(spec.Levels) > 1
	}
	return jsonEffortInfo{Adjustable: adjustable, Current: current, Available: levels}
}

// modelSlug turns a display model name into the reference's slug form:
// "Gemini 3.8 Flash (High)" -> "gemini-3.8-flash-high". Cycle 101: the
// catalogue is consulted first, because the mechanical transform disagrees with
// the reference for Claude ("Claude Sonnet 4.6 (Thinking)" is
// "claude-sonnet-4-6", not "claude-sonnet-4.6-thinking").
func modelSlug(name string) string {
	if id, ok := modelcatalog.CatalogueID(name); ok {
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

// printSlashEffort prints the current reasoning effort, which the reference
// reports as a bare word ("high" on a default install).
func printSlashEffort(fs *flag.FlagSet) string {
	if e := flagStringValue(fs, "effort"); e != "" {
		return e + "\n"
	}
	return "high\n"
}

// PrintSlashEffort prints the current reasoning effort.
func PrintSlashEffort(fs *flag.FlagSet) string {
	return printSlashEffort(fs)
}
