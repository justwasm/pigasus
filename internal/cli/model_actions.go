package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/models"
)

// runModelList lists the configured models.
func runModelList(j *JetskiCLI, args []string) error {
	fs := flag.NewFlagSet("model list", flag.ContinueOnError)
	fs.SetOutput(j.ErrOut)
	provider := fs.String("provider", "", "only list the models of this provider")
	if err := fs.Parse(args); err != nil {
		return errExitCode(1)
	}
	want := strings.TrimSpace(*provider)
	reg, err := models.LoadAll()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	entries := reg.List()
	if want != "" {
		filtered := entries[:0]
		for _, m := range entries {
			if m.ProviderID == want || m.Provider == want {
				filtered = append(filtered, m)
			}
		}
		entries = filtered
	}
	fmt.Fprintf(j.Out, "models: %s\n", models.ProvidersPath())
	models.RenderModelTable(j.Out, entries)
	return nil
}

// runModelShow shows one model in detail.
func runModelShow(j *JetskiCLI, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return usageFail(j, usageModelShow(j.Name), "Error: usage: model show <id>")
	}
	reg, err := models.LoadAll()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	m, ok := reg.GetByName(strings.TrimSpace(args[0]))
	if !ok {
		return errExitCodeMsg(1, fmt.Sprintf("Error: no model named %q", args[0]))
	}
	// The flattened model carries the *resolved* credential, which is empty
	// when the environment reference is not set. The provider's own entry
	// carries the reference the user wrote, which is what they want to
	// see: "$OPENAGY_GATEWAY_KEY" names the variable, a literal is masked.
	if pid := strings.TrimSpace(m.ProviderID); pid != "" {
		if providers, err := models.ProviderEditor().List(); err == nil {
			if p, ok := providers[pid]; ok && p.APIKey != "" {
				m.APIKey = p.APIKey
			}
		}
	}
	models.RenderModelDetail(j.Out, *m)
	return nil
}

type modelAddFlags struct {
	provider  string
	external  string
	name      string
	ctxWindow int
	maxTokens int
	effort    string
	// reasoningEfforts are the reasoning tiers the model offers — the
	// levels it can be selected at. The list is the closed set the Web
	// UI's dispatcher accepts (low|medium|high|xhigh|max).
	reasoningEfforts string
	// supportsImages is the model's own word about pictures. It is
	// tri-state through the flag set: a flag that is not passed leaves
	// the entry's own value alone, and a passed flag says yes or no
	// (`--supports-images=false`).
	supportsImages bool
	// mimeTypes is the model's own list of accepted media types — the
	// fuller answer than the `supports_images` marker. The list wins.
	mimeTypes string
	// disabled takes the model out of the Web UI's picker.
	disabled bool
	// quotaResetTime and quotaRemainingFraction are the model's quota
	// state: when the quota comes back, and how much of it is left.
	quotaResetTime         string
	quotaRemainingFraction float64
	// thoughtCirculation says the model circulates its thoughts.
	thoughtCirculation bool
	// modelURL is a link to the model's own page.
	modelURL string
	// tagTitle and tagDescription are the picker's tag line.
	tagTitle       string
	tagDescription string
	// effortSupported says the model exposes a reasoning level.
	effortSupported bool
}

func newModelAddFlagSet(errOut io.Writer, f *modelAddFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("model add", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&f.provider, "provider", "", "provider that serves this model")
	fs.StringVar(&f.external, "model", "", "model id sent upstream")
	fs.StringVar(&f.name, "name", "", "display name")
	fs.IntVar(&f.ctxWindow, "context-window", 0, "declared context window in tokens")
	fs.IntVar(&f.ctxWindow, "context_window", 0, "declared context window in tokens")
	fs.IntVar(&f.maxTokens, "default-max-tokens", 0, "declared output cap in tokens")
	fs.IntVar(&f.maxTokens, "default_max_tokens", 0, "declared output cap in tokens")
	fs.StringVar(&f.effort, "reasoning-effort", "", "low|medium|high|default")
	fs.StringVar(&f.effort, "reasoning_effort", "", "low|medium|high|default")
	fs.StringVar(&f.reasoningEfforts, "reasoning-efforts", "",
		"comma separated reasoning tiers the model offers: low,medium,high,xhigh,max")
	fs.StringVar(&f.reasoningEfforts, "reasoning_efforts", "",
		"comma separated reasoning tiers the model offers: low,medium,high,xhigh,max")
	fs.BoolVar(&f.supportsImages, "supports-images", false, "the model takes pictures")
	fs.BoolVar(&f.supportsImages, "supports_images", false, "the model takes pictures")
	fs.StringVar(&f.mimeTypes, "supported-mime-types", "", "comma separated media types the model accepts")
	fs.StringVar(&f.mimeTypes, "supported_mime_types", "", "comma separated media types the model accepts")
	fs.BoolVar(&f.disabled, "disabled", false, "the model is out of service")
	fs.StringVar(&f.quotaResetTime, "quota-reset-time", "", "when the model's quota comes back (RFC 3339)")
	fs.StringVar(&f.quotaResetTime, "quota_reset_time", "", "when the model's quota comes back (RFC 3339)")
	fs.Float64Var(&f.quotaRemainingFraction, "quota-remaining-fraction", 0, "how much of the model's quota is left")
	fs.Float64Var(&f.quotaRemainingFraction, "quota_remaining_fraction", 0, "how much of the model's quota is left")
	fs.BoolVar(&f.thoughtCirculation, "supports-thought-circulation", false, "the model circulates its thoughts")
	fs.BoolVar(&f.thoughtCirculation, "supports_thought_circulation", false, "the model circulates its thoughts")
	fs.StringVar(&f.modelURL, "model-url", "", "link to the model's own page")
	fs.StringVar(&f.modelURL, "model_url", "", "link to the model's own page")
	fs.StringVar(&f.tagTitle, "tag-title", "", "the picker's tag line under the model's name")
	fs.StringVar(&f.tagTitle, "tag_title", "", "the picker's tag line under the model's name")
	fs.StringVar(&f.tagDescription, "tag-description", "", "the picker's tag line, in full")
	fs.StringVar(&f.tagDescription, "tag_description", "", "the picker's tag line, in full")
	fs.BoolVar(&f.effortSupported, "effort-supported", false, "the model exposes a reasoning level")
	fs.BoolVar(&f.effortSupported, "effort_supported", false, "the model exposes a reasoning level")
	return fs
}

func applyModelFlags(entry *models.ProviderModel, f *modelAddFlags, set map[string]bool) {
	if set["model"] {
		entry.Model = f.external
	}
	if set["name"] {
		entry.Name = f.name
	}
	if set["context-window"] || set["context_window"] {
		entry.ContextWindow = f.ctxWindow
	}
	if set["default-max-tokens"] || set["default_max_tokens"] {
		entry.DefaultMaxTokens = f.maxTokens
	}
	if set["reasoning-effort"] || set["reasoning_effort"] {
		entry.ReasoningEffort = f.effort
	}
	if set["reasoning-efforts"] || set["reasoning_efforts"] {
		entry.ReasoningEfforts = models.NormalizeEfforts(splitCommaList(f.reasoningEfforts))
	}
	if set["supports-images"] || set["supports_images"] {
		stated := f.supportsImages
		entry.SupportsImages = &stated
	}
	if set["supported-mime-types"] || set["supported_mime_types"] {
		entry.SupportedMimeTypes = splitCommaList(f.mimeTypes)
	}
	if set["disabled"] {
		entry.Disabled = f.disabled
	}
	if set["quota-reset-time"] || set["quota_reset_time"] {
		entry.QuotaInfo = quotaInfoWithReset(entry.QuotaInfo, f.quotaResetTime)
	}
	if set["quota-remaining-fraction"] || set["quota_remaining_fraction"] {
		entry.QuotaInfo = quotaInfoWithFraction(entry.QuotaInfo, f.quotaRemainingFraction)
	}
	if set["supports-thought-circulation"] || set["supports_thought_circulation"] {
		stated := f.thoughtCirculation
		entry.SupportsThoughtCirculation = &stated
	}
	if set["model-url"] || set["model_url"] {
		entry.ModelURL = f.modelURL
	}
	if set["tag-title"] || set["tag_title"] {
		entry.TagTitle = f.tagTitle
	}
	if set["tag-description"] || set["tag_description"] {
		entry.TagDescription = f.tagDescription
	}
	if set["effort-supported"] || set["effort_supported"] {
		stated := f.effortSupported
		entry.EffortSupported = &stated
	}
}

// splitCommaList splits a comma separated list into its parts, and
// drops the empty ones.
func splitCommaList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// quotaInfoWithReset sets the quota's reset time, or clears the quota
// when the time is empty.
func quotaInfoWithReset(q *models.QuotaInfo, raw string) *models.QuotaInfo {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if q == nil {
		q = &models.QuotaInfo{}
	} else {
		copied := *q
		q = &copied
	}
	q.ResetTime = raw
	return q
}

// quotaInfoWithFraction sets how much of the quota is left.
func quotaInfoWithFraction(q *models.QuotaInfo, fraction float64) *models.QuotaInfo {
	if q == nil {
		q = &models.QuotaInfo{}
	} else {
		copied := *q
		q = &copied
	}
	value := fraction
	q.RemainingFraction = &value
	return q
}

// runModelAdd creates or updates a model.
func runModelAdd(j *JetskiCLI, args []string) error {
	var f modelAddFlags
	fs := newModelAddFlagSet(j.ErrOut, &f)
	id, rest := splitPositionalID(args, modelAddValueFlags)
	if err := fs.Parse(rest); err != nil {
		return errExitCode(1)
	}
	if id == "" {
		return usageFail(j, usageModelAdd(j.Name), "Error: usage: model add <id> --provider <provider>")
	}
	provider := strings.TrimSpace(f.provider)
	if provider == "" {
		return errExitCodeMsg(1, "Error: model add: --provider is required")
	}

	ed := models.ProviderEditor()
	providers, err := ed.List()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	p, ok := providers[provider]
	if !ok {
		return errExitCodeMsg(1, fmt.Sprintf(
			"Error: no provider named %q (create it with: %s provider add %s --base-url …)",
			provider, j.Name, provider))
	}
	entry := models.ProviderModel{ID: id}
	for _, m := range p.Models {
		if m.ID == id {
			entry = m
			break
		}
	}
	applyModelFlags(&entry, &f, flagSetFlags(fs))
	if err := validateModelFlags(&entry, &f, flagSetFlags(fs)); err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	if err := ed.PutModel(provider, entry); err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	fmt.Fprintf(j.Out, "added %s under %s (→ %s)\n", entry.ID, provider, wireModelID(entry))
	return nil
}

// validateModelFlags checks the values the flags carry.
//
// The quota's reset time is a timestamp the picker shows, so a typo in
// it is worth catching here rather than at the picker's screen. The
// fraction is a share of the quota — one is a full quota, zero is none.
func validateModelFlags(entry *models.ProviderModel, f *modelAddFlags, set map[string]bool) error {
	if err := models.ValidateEfforts(entry.ReasoningEfforts); err != nil {
		return fmt.Errorf("Error: model add: --reasoning-efforts %w", err)
	}
	if entry.QuotaInfo != nil {
		if raw := strings.TrimSpace(entry.QuotaInfo.ResetTime); raw != "" {
			if _, err := time.Parse(time.RFC3339, raw); err != nil {
				return fmt.Errorf("Error: model add: --quota-reset-time is not an RFC 3339 timestamp: %q", raw)
			}
		}
	}
	if set["quota-remaining-fraction"] || set["quota_remaining_fraction"] {
		if f.quotaRemainingFraction < 0 || f.quotaRemainingFraction > 1 {
			return fmt.Errorf("Error: model add: --quota-remaining-fraction is a share of the quota: %v is not between 0 and 1",
				f.quotaRemainingFraction)
		}
	}
	return nil
}

// runModelRemove removes a model.
func runModelRemove(j *JetskiCLI, args []string) error {
	fs := flag.NewFlagSet("model remove", flag.ContinueOnError)
	fs.SetOutput(j.ErrOut)
	provider := fs.String("provider", "", "only remove from this provider")
	id, rest := splitPositionalID(args, map[string]bool{"provider": true})
	if err := fs.Parse(rest); err != nil {
		return errExitCode(1)
	}
	if id == "" {
		return usageFail(j, usageModelRemove(j.Name), "Error: usage: model remove <id> [--provider <provider>]")
	}
	want := strings.TrimSpace(*provider)

	ed := models.ProviderEditor()
	removed, err := deleteModelTarget(ed, id, want)
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	if !removed {
		return errExitCodeMsg(1, fmt.Sprintf("Error: no model named %q", id))
	}
	fmt.Fprintf(j.Out, "removed %s\n", id)
	return nil
}

func deleteModelTarget(ed *models.Editor, id, wantProvider string) (bool, error) {
	if wantProvider == "" {
		_, found, err := ed.DeleteModel(id)
		return found, err
	}
	providers, err := ed.List()
	if err != nil {
		return false, err
	}
	p, ok := providers[wantProvider]
	if !ok {
		return false, fmt.Errorf("Error: no provider named %q", wantProvider)
	}
	for _, m := range p.Models {
		if m.ID != id {
			continue
		}
		if _, _, err := ed.DeleteModel(id); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, fmt.Errorf("Error: no model named %q under %q", id, wantProvider)
}

var modelAddValueFlags = map[string]bool{
	"provider":                     true,
	"model":                        true,
	"name":                         true,
	"context-window":               true,
	"context_window":               true,
	"default-max-tokens":           true,
	"default_max_tokens":           true,
	"reasoning-effort":             true,
	"reasoning_effort":             true,
	"reasoning-efforts":            true,
	"reasoning_efforts":            true,
	"supports-images":              true,
	"supports_images":              true,
	"supported-mime-types":         true,
	"supported_mime_types":         true,
	"disabled":                     true,
	"quota-reset-time":             true,
	"quota_reset_time":             true,
	"quota-remaining-fraction":     true,
	"quota_remaining_fraction":     true,
	"supports-thought-circulation": true,
	"supports_thought_circulation": true,
	"model-url":                    true,
	"model_url":                    true,
	"tag-title":                    true,
	"tag_title":                    true,
	"tag-description":              true,
	"tag_description":              true,
	"effort-supported":             true,
	"effort_supported":             true,
}
