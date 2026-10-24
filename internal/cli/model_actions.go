package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

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
	if err := ed.PutModel(provider, entry); err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	fmt.Fprintf(j.Out, "added %s under %s (→ %s)\n", entry.ID, provider, wireModelID(entry))
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
	"provider":           true,
	"model":              true,
	"name":               true,
	"context-window":     true,
	"context_window":     true,
	"default-max-tokens": true,
	"default_max_tokens": true,
	"reasoning-effort":   true,
	"reasoning_effort":   true,
}
