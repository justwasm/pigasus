package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
)

// ---- model_actions.go ----

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
	m, err := reg.ResolveModel(strings.TrimSpace(args[0]))
	if err != nil {
		var ambErr *models.AmbiguousModelError
		if errors.As(err, &ambErr) {
			return errExitCodeMsg(1, fmt.Sprintf("Error: %s", ambErr.Error()))
		}
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
	if provider == "" && strings.Contains(id, "/") {
		parts := strings.SplitN(id, "/", 2)
		provider = parts[0]
		id = parts[1]
	}
	if provider == "" {
		return errExitCodeMsg(1, "Error: model add: --provider is required (or use <provider>/<model-id>)")
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
	if catModel, ok := models.FindCatwalkModel(provider, id); ok {
		if entry.Name == "" {
			entry.Name = catModel.Name
		}
		if entry.ContextWindow == 0 {
			entry.ContextWindow = catModel.ContextWindow
		}
		if entry.DefaultMaxTokens == 0 {
			entry.DefaultMaxTokens = catModel.DefaultMaxTokens
		}
		if entry.ReasoningEffort == "" {
			entry.ReasoningEffort = catModel.ReasoningEffort
		}
		if len(entry.ReasoningEfforts) == 0 {
			entry.ReasoningEfforts = catModel.ReasoningEfforts
		}
		if entry.SupportsImages == nil {
			entry.SupportsImages = catModel.SupportsImages
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
	if want == "" && strings.Contains(id, "/") {
		parts := strings.SplitN(id, "/", 2)
		want = parts[0]
		id = parts[1]
	}

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
		reg, err := models.LoadAll()
		if err == nil {
			if _, err := reg.ResolveModel(id); err != nil {
				var ambErr *models.AmbiguousModelError
				if errors.As(err, &ambErr) {
					return false, fmt.Errorf("Error: %s", ambErr.Error())
				}
			}
		}
		_, found, err := ed.DeleteModel(id)
		return found, err
	}
	return ed.DeleteModelFromProvider(wantProvider, id)
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

// ---- model_table.go ----

// modelTable renders the model catalogue the way the reference renders it.
//
// # 【一手】 The reference renders the list two ways, and the difference is
// only visible in a terminal.
//
// stdout is a terminal:
//
//	gemini-3.8-flash-medium   Gemini 3.8 Flash (Medium)
//	claude-opus-4-6-thinking  Claude Opus 4.6 (Thinking)
//
// stdout is a pipe or a file:
//
//	gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)
//	claude-opus-4-6-thinking\tClaude Opus 4.6 (Thinking)
//
// The aligned form is a text/tabwriter with minwidth 0, tabwidth 8 and
// padding 2. The widest catalogue id is 24 characters ("claude-opus-4-6-
// thinking"), so every label starts at column 26 — measured off a pty
// capture of agy 1.4.5, not inferred.
//
// The tab separated form is the machine readable one: one row per model,
// id and label joined by a single tab, no padding. It is what the reference
// writes when stdout is not a terminal, so a caller that pipes `agy models`
// into cut/awk sees the same bytes the reference produces.
func modelTable(w io.Writer, entries []catalog.ModelEntry, aligned bool) error {
	if !aligned {
		for _, m := range entries {
			if _, err := fmt.Fprintf(w, "%s\t%s\n", m.ID, m.Label); err != nil {
				return err
			}
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	for _, m := range entries {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", m.ID, m.Label); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// writeModelTable renders the model catalogue for the writer it is given:
// space aligned when that writer is a terminal, tab separated otherwise.
//
// The terminal check is on the writer, not on os.Stdout: the clone's
// subcommands can be pointed at a buffer or a pipe, and the reference's own
// rule is about the stream the catalogue is written to.
func writeModelTable(w io.Writer, entries []catalog.ModelEntry) error {
	return modelTable(w, entries, isTerminalWriter(w))
}

// ---- provider_actions.go ----

// runProviderList lists the configured providers and their models.
func runProviderList(j *JetskiCLI, args []string) error {
	ed := models.ProviderEditor()
	providers, err := ed.List()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	fmt.Fprintf(j.Out, "providers: %s\n", ed.Path())
	models.RenderProviderTable(j.Out, providers)
	return nil
}

// runProviderShow shows one provider in detail.
func runProviderShow(j *JetskiCLI, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return usageFail(j, usageProviderShow(j.Name), "Error: usage: provider show <id>")
	}
	ed := models.ProviderEditor()
	providers, err := ed.List()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	id := strings.TrimSpace(args[0])
	p, ok := providers[id]
	if !ok {
		return errExitCodeMsg(1, fmt.Sprintf("Error: no provider named %q", id))
	}
	models.RenderProviderDetail(j.Out, id, p)
	return nil
}

type providerAddFlags struct {
	name    string
	ptype   string
	baseURL string
	apiURL  string
	apiKey  string
	timeout int
	retries int
	headers providerHeaderList
	specs   providerModelList
}

func newProviderAddFlagSet(errOut io.Writer, f *providerAddFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("provider add", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&f.name, "name", "", "display name")
	fs.StringVar(&f.ptype, "type", models.ProviderTypeOpenAI, "wire protocol: openai|anthropic")
	fs.StringVar(&f.baseURL, "base-url", "", "API base")
	fs.StringVar(&f.baseURL, "base_url", "", "API base")
	fs.StringVar(&f.apiURL, "api-url", "", "full endpoint")
	fs.StringVar(&f.apiURL, "api_url", "", "full endpoint")
	fs.StringVar(&f.apiKey, "api-key", "", "credential")
	fs.StringVar(&f.apiKey, "api_key", "", "credential")
	fs.IntVar(&f.timeout, "timeout-seconds", 0, "per-request timeout")
	fs.IntVar(&f.timeout, "timeout_seconds", 0, "per-request timeout")
	fs.IntVar(&f.retries, "max-retries", 0, "retry count for 5xx and network failures")
	fs.IntVar(&f.retries, "max_retries", 0, "retry count for 5xx and network failures")
	fs.Var(&f.headers, "header", "extra request header, as name=value (repeatable)")
	fs.Var(&f.specs, "model", "model to serve, as id or id=upstream-id (repeatable)")
	return fs
}

func applyProviderFlags(p *models.ProviderConfig, f *providerAddFlags, set map[string]bool) error {
	if set["name"] {
		p.Name = f.name
	}
	if set["type"] {
		p.Type = f.ptype
	}
	if set["base-url"] || set["base_url"] {
		p.BaseURL = f.baseURL
	}
	if set["api-url"] || set["api_url"] {
		p.APIURL = f.apiURL
	}
	if set["api-key"] || set["api_key"] {
		p.APIKey = f.apiKey
	}
	if set["timeout-seconds"] || set["timeout_seconds"] {
		p.TimeoutSeconds = f.timeout
	}
	if set["max-retries"] || set["max_retries"] {
		p.MaxRetries = f.retries
	}
	if set["header"] {
		p.ExtraHeaders = map[string]string{}
		for _, h := range f.headers {
			k, v, ok := strings.Cut(h, "=")
			if !ok {
				return fmt.Errorf("Error: --header %q is not name=value", h)
			}
			p.ExtraHeaders[strings.TrimSpace(k)] = v
		}
	}
	return nil
}

// runProviderAdd creates or updates a provider.
func runProviderAdd(j *JetskiCLI, args []string) error {
	var f providerAddFlags
	fs := newProviderAddFlagSet(j.ErrOut, &f)
	id, rest := splitPositionalID(args, providerAddValueFlags)
	if err := fs.Parse(rest); err != nil {
		return errExitCode(1)
	}
	if id == "" {
		return usageFail(j, usageProviderAdd(j.Name), "Error: usage: provider add <id> [flags]")
	}

	ed := models.ProviderEditor()
	providers, err := ed.List()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	p := providers[id]
	p.ID = id
	if p.Endpoint() == "" {
		if preset, ok := models.PresetProviderConfig(id); ok {
			if p.Name == "" {
				p.Name = preset.Name
			}
			if p.Type == "" || p.Type == models.ProviderTypeOpenAI {
				p.Type = preset.Type
			}
			p.BaseURL = preset.BaseURL
			p.APIURL = preset.APIURL
		}
	}
	if err := applyProviderFlags(&p, &f, flagSetFlags(fs)); err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	if p.Endpoint() == "" {
		return errExitCodeMsg(1, "Error: provider add: --base-url or --api-url is required")
	}
	if err := ed.PutProvider(id, p); err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	fmt.Fprintf(j.Out, "added provider %s (type=%s endpoint=%s)\n", id, p.WireType(), p.Endpoint())
	for _, spec := range f.specs {
		if err := ed.PutModel(id, spec); err != nil {
			return errExitCodeMsg(1, err.Error())
		}
		fmt.Fprintf(j.Out, "added model %s under %s (→ %s)\n", spec.ID, id, wireModelID(spec))
	}
	if len(f.specs) == 0 && len(providers[id].Models) == 0 {
		if preset, ok := models.PresetProviderConfig(id); ok && len(preset.Models) > 0 {
			for _, m := range preset.Models {
				_ = ed.PutModel(id, m)
			}
			fmt.Fprintf(j.Out, "  preloaded %d models from Catwalk catalog\n", len(preset.Models))
		}
	}
	if p.APIKey == "" {
		method := models.CatwalkAuthMethod(id)
		switch method {
		case "browser":
			fmt.Fprintf(j.Out, "  Note: this provider typically authenticates via browser login / OAuth.\n")
		case "both":
			fmt.Fprintf(j.Out, "  Note: no API key set; you can supply --api-key or authenticate via browser.\n")
		default:
			fmt.Fprintf(j.Out, "  Note: no API key set; set one with --api-key.\n")
		}
	}
	return nil
}

// runProviderPresets lists preset providers from the embedded Catwalk catalog.
func runProviderPresets(j *JetskiCLI, args []string) error {
	all := models.CatwalkProviders()
	fmt.Fprintf(j.Out, "available preset providers (%d):\n", len(all))
	for _, p := range all {
		auth := models.CatwalkAuthMethod(string(p.ID))
		fmt.Fprintf(j.Out, "  %-16s %-24s (auth: %s, models: %d)\n", p.ID, p.Name, auth, len(p.Models))
	}
	return nil
}

// runProviderRemove removes a provider and the models it serves.
func runProviderRemove(j *JetskiCLI, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return usageFail(j, usageProviderRemove(j.Name), "Error: usage: provider remove <id>")
	}
	id := strings.TrimSpace(args[0])
	ed := models.ProviderEditor()
	if err := ed.DeleteProvider(id); err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	fmt.Fprintf(j.Out, "removed provider %s\n", id)
	return nil
}

// runProviderTest probes the provider's endpoint and credential.
//
// This is the headless twin of the TUI panel's verification: one request,
// one line of feedback, and an exit code that says whether it worked.
func runProviderTest(j *JetskiCLI, args []string) error {
	fs := flag.NewFlagSet("provider test", flag.ContinueOnError)
	fs.SetOutput(j.ErrOut)
	var modelID string
	fs.StringVar(&modelID, "model", "", "model to probe")
	fs.StringVar(&modelID, "model_id", "", "model to probe")

	id, rest := splitPositionalID(args, providerTestValueFlags)
	if err := fs.Parse(rest); err != nil {
		return errExitCode(1)
	}
	if id == "" {
		return usageFail(j, usageProviderTest(j.Name), "Error: usage: provider test <id>")
	}
	ed := models.ProviderEditor()
	providers, err := ed.List()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	p, ok := providers[id]
	if !ok {
		return errExitCodeMsg(1, fmt.Sprintf("Error: no provider named %q", id))
	}
	probe := strings.TrimSpace(modelID)
	if probe == "" {
		probe = firstProviderWireModel(p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), models.VerifyTimeout)
	defer cancel()
	result := models.VerifyProvider(ctx, p, p.APIKey, probe)
	if result.OK {
		fmt.Fprintf(j.Out, "%s %s: %s\n", "✓", id, result.Detail)
		return nil
	}
	fmt.Fprintf(j.Out, "%s %s: %s\n", "✗", id, result.Detail)
	return errExitCode(1)
}

// firstProviderWireModel is the wire id of the provider's first model —
// the one the probe talks to when the caller did not name one.
func firstProviderWireModel(p models.ProviderConfig) string {
	for _, m := range p.Models {
		wire := strings.TrimSpace(m.Model)
		if wire == "" {
			wire = strings.TrimSpace(m.ID)
		}
		if wire != "" {
			return wire
		}
	}
	return ""
}

// providerHeaderList collects a repeatable --header flag.
type providerHeaderList []string

func (h *providerHeaderList) String() string { return strings.Join(*h, ",") }

func (h *providerHeaderList) Set(v string) error {
	*h = append(*h, v)
	return nil
}

// providerModelList collects a repeatable --model flag. Each value is
// "id" or "id=upstream-id".
type providerModelList []models.ProviderModel

func (l *providerModelList) String() string { return "" }

func (l *providerModelList) Set(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return fmt.Errorf("model: an empty model id")
	}
	id, upstream, hasUpstream := strings.Cut(v, "=")
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("model: %q has no model id", v)
	}
	entry := models.ProviderModel{ID: id}
	if hasUpstream {
		entry.Model = strings.TrimSpace(upstream)
	}
	*l = append(*l, entry)
	return nil
}

// providerAddValueFlags and providerTestValueFlags list the flags that take a
// value. splitPositionalID needs them so the identifier is not read out
// of a flag's value.
var providerAddValueFlags = map[string]bool{
	"name":            true,
	"type":            true,
	"base-url":        true,
	"base_url":        true,
	"api-url":         true,
	"api_url":         true,
	"api-key":         true,
	"api_key":         true,
	"timeout-seconds": true,
	"timeout_seconds": true,
	"max-retries":     true,
	"max_retries":     true,
	"header":          true,
	"model":           true,
}

var providerTestValueFlags = map[string]bool{
	"model":    true,
	"model_id": true,
}

// ---- provider_cmds.go ----

// provider_cmds.go — `agy provider …` and `agy model …`.
//
// # Why a separate command
//
// `agy models` prints the catalogue the reference prints, and its output
// is byte-pinned: the ids and labels, tab separated when the output is a
// pipe and space aligned when it is a terminal. The commands that *change*
// the configuration therefore live under their own names, so the pinned
// output is never at risk.
//
// Both commands are hidden: `agy --help` lists the reference's twelve
// visible subcommands and nothing else (see internal/cli/parser_test.go).
//
// # What they write
//
// Every write goes through models.Editor, which rewrites the field it
// changes and leaves the rest of the file alone (see internal/models/edit.go).
// The wrapper's `agy wrapper providers …` writes the same file through the
// same layer.
//
// # Merge, not replace
//
// `provider add` and `model add` merge: a flag the user did not pass keeps
// the value the file already had. `agy provider add gateway --base-url X`
// changes the endpoint and leaves the credential, the header and the
// models alone. Clearing a field is an explicit act (`--api-key ""`).

// ---- usage ---------------------------------------------------------------

func usageProvider(n string) string {
	return "Usage: " + n + " provider <subcommand> [flags]\n" +
		"\n" +
		"Manage the configured model providers\n" +
		"\n" +
		"Subcommands:\n" +
		"  list     List the configured providers and their models\n" +
		"  presets  List available preset providers from the Catwalk catalog\n" +
		"  show     Show one provider in detail\n" +
		"  add      Create or update a provider\n" +
		"  remove   Remove a provider and its models\n" +
		"\n" +
		"The configuration file is " + models.ProvidersPath() + "\n"
}

func usageProviderPresets(n string) string {
	return "Usage: " + n + " provider presets\n" +
		"\n" +
		"List available preset providers from the Catwalk catalog\n"
}

func usageProviderShow(n string) string {
	return "Usage: " + n + " provider show <id>\n" +
		"\n" +
		"Show one provider in detail\n"
}

func usageProviderAdd(n string) string {
	return "Usage: " + n + " provider add <id> [flags]\n" +
		"\n" +
		"Create or update a provider\n" +
		"\n" +
		"Flags:\n" +
		"  --name              Display name of the provider\n" +
		"  --type              Wire protocol: openai or anthropic\n" +
		"  --base-url          API base; the endpoint is derived from it\n" +
		"  --api-url           Full endpoint, used verbatim\n" +
		"  --api-key           Credential; $NAME reads it from the environment\n" +
		"  --timeout-seconds   Per-request timeout\n" +
		"  --max-retries       Retry count for 5xx and network failures\n" +
		"  --header            Extra request header, as name=value (repeatable)\n" +
		"  --model             Model to serve, as id or id=upstream-id (repeatable)\n" +
		"\n" +
		"Notes:\n" +
		"  A flag that is not passed keeps the value the file already had.\n" +
		"  The endpoint is derived from --base-url by protocol: openai appends\n" +
		"  /chat/completions, anthropic appends /v1/messages. --api-url overrides\n" +
		"  both and is used verbatim.\n"
}

func usageProviderRemove(n string) string {
	return "Usage: " + n + " provider remove <id>\n" +
		"\n" +
		"Remove a provider and the models it serves\n"
}

func usageProviderTest(n string) string {
	return "Usage: " + n + " provider test <id> [--model <id>]\n" +
		"\n" +
		"Probe the provider's endpoint and credential\n" +
		"\n" +
		"Flags:\n" +
		"  --model   Model to probe; the provider's first model by default\n" +
		"\n" +
		"Notes:\n" +
		"  The probe is a minimal chat completion when a model is known, and a\n" +
		"  model listing otherwise. It makes one request and reports what the\n" +
		"  endpoint answered. The exit code is 1 when the probe fails.\n"
}

func usageModel(n string) string {
	return "Usage: " + n + " model <subcommand> [flags]\n" +
		"\n" +
		"Manage the configured models\n" +
		"\n" +
		"Subcommands:\n" +
		"  list     List the configured models\n" +
		"  show     Show one model in detail\n" +
		"  add      Create or update a model\n" +
		"  remove   Remove a model\n" +
		"\n" +
		"The configuration file is " + models.ProvidersPath() + "\n"
}

func usageModelShow(n string) string {
	return "Usage: " + n + " model show <id>\n" +
		"\n" +
		"Show one model in detail\n"
}

func usageModelAdd(n string) string {
	return "Usage: " + n + " model add <id> --provider <provider> [flags]\n" +
		"\n" +
		"Create or update a model\n" +
		"\n" +
		"Flags:\n" +
		"  --provider                 Provider that serves this model (required)\n" +
		"  --model                    Model id sent upstream\n" +
		"  --name                     Display name\n" +
		"  --context-window           Declared context window in tokens\n" +
		"  --default-max-tokens       Declared output cap in tokens\n" +
		"  --reasoning-effort         low, medium, high or default\n" +
		"  --reasoning-efforts        The reasoning tiers the model offers,\n" +
		"                             comma separated: low, medium, high,\n" +
		"                             xhigh, max\n" +
		"  --supports-images          The model takes pictures\n" +
		"  --supported-mime-types     Media types the model accepts, comma separated\n" +
		"  --disabled                 The model is out of service\n" +
		"  --quota-reset-time         When the model's quota comes back (RFC 3339)\n" +
		"  --quota-remaining-fraction How much of the model's quota is left\n" +
		"  --supports-thought-circulation  The model circulates its thoughts\n" +
		"  --model-url                Link to the model's own page\n" +
		"  --tag-title                The picker's tag line under the model's name\n" +
		"  --tag-description          The picker's tag line, in full\n" +
		"  --effort-supported         The model exposes a reasoning level\n" +
		"\n" +
		"Notes:\n" +
		"  A flag that is not passed keeps the value the file already had.\n" +
		"  --supports-images=false states the opposite; without the flag the\n" +
		"  catalogue's family table decides.\n" +
		"  --supported-mime-types wins over --supports-images: it is the fuller\n" +
		"  answer, and it names the media types the model takes.\n" +
		"  The flags the Web UI's model picker reads are --disabled, --quota-reset-time,\n" +
		"  --quota-remaining-fraction, --supported-mime-types, --tag-title,\n" +
		"  --tag-description, --supports-thought-circulation and --model-url.\n" +
		"  A model that declares --reasoning-efforts is listed once per tier, the way\n" +
		"  the reference lists its own (Gemini 3.8 Flash (High)). The tier rides the\n" +
		"  model's id, and --reasoning-effort says which one it runs at.\n"
}

func usageModelRemove(n string) string {
	return "Usage: " + n + " model remove <id> [--provider <provider>]\n" +
		"\n" +
		"Remove a model\n"
}

// ---- parent dispatchers --------------------------------------------------

// runProviderParent handles `agy provider` with no subcommand.
func runProviderParent(j *JetskiCLI, args []string) error {
	if len(args) > 0 {
		return errExitCodeMsg(1, fmt.Sprintf("Error: unknown command: %s", args[0]))
	}
	fmt.Fprint(j.ErrOut, usageProvider(j.Name))
	return errExitCode(1)
}

// runModelParent handles `agy model` with no subcommand.
func runModelParent(j *JetskiCLI, args []string) error {
	if len(args) > 0 {
		return errExitCodeMsg(1, fmt.Sprintf("Error: unknown command: %s", args[0]))
	}
	fmt.Fprint(j.ErrOut, usageModel(j.Name))
	return errExitCode(1)
}

// ---- shared helpers ------------------------------------------------------

// wireModelID returns the model id sent upstream: the explicit one when
// there is one, the catalogue id otherwise.
func wireModelID(m models.ProviderModel) string {
	if strings.TrimSpace(m.Model) != "" {
		return m.Model
	}
	return m.ID
}

// splitPositionalID pulls the identifier out of args and returns the rest
// for the flag parser.
//
// The identifier may appear before or after the flags, so the scan has to
// know which flags take a value: without that, `provider add --model a=b`
// would read "a=b" as the identifier.
func splitPositionalID(args []string, takesValue map[string]bool) (string, []string) {
	id := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			rest = append(rest, a)
			name := strings.TrimLeft(a, "-")
			if strings.ContainsRune(name, '=') {
				continue
			}
			if takesValue[name] && i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
			continue
		}
		if id == "" {
			id = a
			continue
		}
		rest = append(rest, a)
	}
	return id, rest
}

// flagSetFlags returns the names of the flags the user actually passed.
func flagSetFlags(fs *flag.FlagSet) map[string]bool {
	out := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { out[f.Name] = true })
	return out
}

// ---- accounts_cmds.go ----

// accounts_cmds.go — `agy accounts …`.
//
// # Why a separate command
//
// The reference (`agy`) keeps exactly one credential, so it has no
// account registry and no command for one. The clone's registry is a
// superset: the accounts the user signed in to, and the switch between
// them.
//
// The command is hidden, like the other openagy-only subcommands:
// `agy --help` lists the reference's twelve visible subcommands and
// nothing else (see internal/cli/parser_test.go).
//
// # What a switch writes
//
// `agy accounts switch <email>` writes the account's token to the shared
// credential locations — the macOS keychain entry the reference reads
// (service "gemini", account "antigravity") and the three on-disk
// paths — so the installed `agy` runs as that account from its next
// start-up. See internal/features/accounts/core.

// ---- usage ---------------------------------------------------------------

func usageAccounts(n string) string {
	return "Usage: " + n + " accounts <subcommand> [flags]\n" +
		"\n" +
		"Manage the signed-in Google accounts\n" +
		"\n" +
		"Subcommands:\n" +
		"  list     List the known accounts\n" +
		"  switch   Make one account the active one\n" +
		"  remove   Forget one account\n" +
		"\n" +
		"The registry is " + accounts.Path() + "\n"
}

func usageAccountsSwitch(n string) string {
	return "Usage: " + n + " accounts switch <email>\n" +
		"\n" +
		"Make one account the active one\n"
}

func usageAccountsRemove(n string) string {
	return "Usage: " + n + " accounts remove <email>\n" +
		"\n" +
		"Forget one account\n"
}

// ---- handlers ------------------------------------------------------------

// runAccountsList prints the accounts the clone knows about and marks
// the active one.
func runAccountsList(j *JetskiCLI, args []string) error {
	st, err := accounts.Load()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	active := ""
	if _, email := accounts.Current(); email != "" {
		active = email
	} else if st.Active != "" {
		active = st.Active
	}
	fmt.Fprintf(j.Out, "accounts: %s\n", accounts.Path())
	if len(st.Accounts) == 0 {
		fmt.Fprintln(j.Out, "no accounts signed in")
		return nil
	}
	for _, acc := range st.Accounts {
		marker := ""
		if strings.EqualFold(acc.Email, active) {
			marker = "  (active)"
		}
		tier := acc.PlanTier
		if strings.TrimSpace(tier) == "" {
			tier = "-"
		}
		weekly, fiveHour := accountsQuotaColumns(acc)
		fmt.Fprintf(j.Out, "%-40s %-18s %-16s %-16s%s\n", acc.Email, tier, weekly, fiveHour, marker)
	}
	return nil
}

// accountsQuotaColumns renders an account's cached quota: the weekly
// and the five-hour remaining, the lowest across the model groups.
//
// The numbers come from the registry's cache, so the command never
// queries the server: the /accounts panel is what refreshes them.
//
// The percentages are right-aligned in a fixed-width column, so they
// line up down the list.
func accountsQuotaColumns(acc accounts.Account) (string, string) {
	return acc.QuotaColumns()
}

// runAccountsSwitch makes one account the active one.
func runAccountsSwitch(j *JetskiCLI, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return usageFail(j, usageAccountsSwitch(j.Name), "Error: usage: accounts switch <email>")
	}
	email := strings.TrimSpace(args[0])
	acc, err := accounts.Activate(context.Background(), email)
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	fmt.Fprintf(j.Out, "active account: %s\n", acc.Email)
	return nil
}

// runAccountsRemove forgets one account.
func runAccountsRemove(j *JetskiCLI, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return usageFail(j, usageAccountsRemove(j.Name), "Error: usage: accounts remove <email>")
	}
	email := strings.TrimSpace(args[0])
	acc, active, err := accounts.Remove(email)
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	if acc.Email == "" {
		return errExitCodeMsg(1, fmt.Sprintf("Error: no account named %q", email))
	}
	if active {
		fmt.Fprintf(j.Out, "removed %s (signed out)\n", acc.Email)
		return nil
	}
	fmt.Fprintf(j.Out, "removed %s\n", acc.Email)
	return nil
}

// runAccountsParent is the no-subcommand form: it prints the list.
func runAccountsParent(j *JetskiCLI, args []string) error {
	if len(args) == 0 {
		return runAccountsList(j, args)
	}
	return usageFail(j, usageAccounts(j.Name), "Error: unknown accounts subcommand")
}

// ---- recovered_catalog.go ----

// RecoveredTools lists the tool names recovered from the reference binary.
var RecoveredTools = []string{
	"run_command", "create_file", "edit_file", "read_file", "delete_file",
	"list_dir", "search_files", "grep_search", "codesearch", "web_search",
	"web_fetch", "git_status", "git_diff", "git_commit", "git_push",
	"git_pull", "git_checkout", "git_branch", "git_merge", "git_log",
	"todo_write", "todo_read", "spawn_agent", "kill_agent", "list_agents",
	"mcp_call", "mcp_list_tools", "ask_user", "show_diff", "apply_patch",
	"run_tests", "lint_code",
}

// RecoveredServices lists the gRPC service names recovered from the reference binary.
var RecoveredServices = []string{
	"CortexService/GenerateChat", "CortexService/StreamGenerateChat",
	"CortexService/FetchConfig", "CortexService/FetchLicenses",
	"CortexService/RefreshToken", "LanguageServerService/SelectLicense",
	"LanguageServerService/SelfAssignLicense", "ToolsService/Invoke",
	"ToolsService/ListTools", "WorkspaceService/Init",
	"WorkspaceService/Status", "ProjectService/Get",
	"ProjectService/List", "OAuthService/StartDeviceFlow",
	"OAuthService/PollDeviceCode", "OAuthService/RefreshToken",
	"RemoteControlService/Start", "RemoteControlService/Status",
	"RemoteControlService/Stop", "PluginService/Install",
	"PluginService/Uninstall", "MCPService/AddServer",
}
