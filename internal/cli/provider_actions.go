package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/justwasm/pigasus/internal/models"
)

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
