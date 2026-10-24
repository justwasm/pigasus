package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/models"
)

type addFlagValues struct {
	name        *string
	provider    *string
	apiURL      *string
	extModel    *string
	apiKey      *string
	display     *string
	ptype       *string
	cw          *int
	reasoning   *string
	timeout     *int
	retries     *int
	interactive *bool
}

func bindAddFlags(fs *flag.FlagSet) addFlagValues {
	return addFlagValues{
		name:        fs.String("name", "", "internal name"),
		provider:    fs.String("provider", "", "provider that serves this model (required)"),
		apiURL:      fs.String("api-url", "", "full chat completions / messages URL"),
		extModel:    fs.String("external-model", "", "model id to send upstream"),
		apiKey:      fs.String("api-key", "", "bearer / x-api-key credential"),
		display:     fs.String("display-name", "", "human-readable name (optional)"),
		ptype:       fs.String("type", models.ProviderTypeOpenAI, "wire protocol of the provider: openai|anthropic"),
		cw:          fs.Int("context-window", 0, "context window size (0=unknown)"),
		reasoning:   fs.String("reasoning-effort", "", "OpenAI o-series hint: low|medium|high|default"),
		timeout:     fs.Int("timeout-seconds", 60, "per-request timeout"),
		retries:     fs.Int("max-retries", 1, "max retries on 5xx / network"),
		interactive: fs.Bool("interactive", false, "prompt for missing values"),
	}
}

func runAdd(cfgPath string, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	f := bindAddFlags(fs)
	ordered := reorderArgs(args, fs)
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if *f.name == "" && fs.NArg() > 0 {
		*f.name = fs.Arg(0)
	}
	if *f.interactive {
		if err := promptFor(fs, f.name, f.provider, f.apiURL, f.extModel, f.apiKey); err != nil {
			return err
		}
	}
	providerID := strings.TrimSpace(*f.provider)
	if *f.name == "" || providerID == "" || *f.extModel == "" {
		return fmt.Errorf("add: --name, --provider and --external-model are required (or use --interactive)")
	}

	ed := models.NewEditor(cfgPath)
	providers, err := ed.List()
	if err != nil {
		return err
	}
	set := passedFlags(fs)
	p, err := resolveProvider(providerID, f, providers, set)
	if err != nil {
		return err
	}
	if err := ed.PutProvider(providerID, p); err != nil {
		return err
	}
	entry := resolveModelEntry(*f.name, providerID, f, providers, set)
	if err := ed.PutModel(providerID, entry); err != nil {
		return err
	}
	fmt.Printf("✓ added %q under provider %q (%s)\n", entry.ID, providerID, p.Endpoint())
	return nil
}

func resolveProvider(providerID string, f addFlagValues, providers map[string]models.ProviderConfig, set map[string]bool) (models.ProviderConfig, error) {
	p, known := providers[providerID]
	if !known {
		if strings.TrimSpace(*f.apiURL) == "" {
			return p, fmt.Errorf("add: --api-url is required for a new provider (or add the provider first)")
		}
		p = models.ProviderConfig{
			ID:             providerID,
			Type:           *f.ptype,
			APIURL:         *f.apiURL,
			APIKey:         *f.apiKey,
			TimeoutSeconds: *f.timeout,
			MaxRetries:     *f.retries,
		}
	} else {
		applyProviderOverrides(&p, f, set)
	}
	if p.Endpoint() == "" {
		return p, fmt.Errorf("add: the provider %q has no endpoint", providerID)
	}
	return p, nil
}

func applyProviderOverrides(p *models.ProviderConfig, f addFlagValues, set map[string]bool) {
	if set["api-url"] {
		p.APIURL = *f.apiURL
	}
	if set["api-key"] {
		p.APIKey = *f.apiKey
	}
	if set["type"] {
		p.Type = *f.ptype
	}
	if set["timeout-seconds"] {
		p.TimeoutSeconds = *f.timeout
	}
	if set["max-retries"] {
		p.MaxRetries = *f.retries
	}
}

func resolveModelEntry(name, providerID string, f addFlagValues, providers map[string]models.ProviderConfig, set map[string]bool) models.ProviderModel {
	entry := models.ProviderModel{ID: name}
	for _, m := range providers[providerID].Models {
		if m.ID == entry.ID {
			entry = m
			break
		}
	}
	if set["external-model"] {
		entry.Model = *f.extModel
	}
	if set["display-name"] {
		entry.Name = *f.display
	}
	if set["context-window"] {
		entry.ContextWindow = *f.cw
	}
	if set["reasoning-effort"] {
		entry.ReasoningEffort = *f.reasoning
	}
	return entry
}

func passedFlags(fs *flag.FlagSet) map[string]bool {
	out := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { out[f.Name] = true })
	return out
}

func promptFor(fs *flag.FlagSet, name, provider, apiURL, extModel, apiKey *string) error {
	fields := []struct {
		val    *string
		prompt string
	}{
		{name, "name"},
		{provider, "provider (openai|anthropic|ollama|openrouter|google|custom)"},
		{apiURL, "api-url (full URL)"},
		{extModel, "external-model"},
		{apiKey, "api-key"},
	}
	for _, field := range fields {
		if *field.val == "" {
			v, err := ask(field.prompt)
			if err != nil {
				return err
			}
			*field.val = v
		}
	}
	_ = fs
	return nil
}

func ask(prompt string) (string, error) {
	fmt.Printf("%s: ", prompt)
	rd := bufio.NewReader(os.Stdin)
	line, err := rd.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			return strings.TrimSpace(line), nil
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func reorderArgs(args []string, fs *flag.FlagSet) []string {
	known := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) { known[f.Name] = true })
	var flags, pos []string
	for i := 0; i < len(args); {
		a := args[i]
		name, hasValue, val, isFlag := parseArgFlag(a)
		if !isFlag || !known[name] {
			pos = append(pos, a)
			i++
			continue
		}
		flagDef := fs.Lookup(name)
		isBool := flagDef != nil && isBoolFlag(flagDef.Value)
		flags, i = appendReorderedFlag(flags, args, i, name, val, hasValue, isBool)
	}
	return append(flags, pos...)
}

func parseArgFlag(a string) (string, bool, string, bool) {
	if strings.HasPrefix(a, "--") {
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			return strings.TrimPrefix(a[:eq], "--"), true, a[eq+1:], true
		}
		return strings.TrimPrefix(a, "--"), false, "", true
	}
	if strings.HasPrefix(a, "-") && len(a) > 1 {
		return strings.TrimPrefix(a, "-"), false, "", true
	}
	return "", false, "", false
}

func appendReorderedFlag(flags, args []string, i int, name, val string, hasValue, isBool bool) ([]string, int) {
	if hasValue {
		flags = append(flags, "--"+name+"="+val)
		return flags, i + 1
	}
	if isBool {
		flags = append(flags, "--"+name+"=true")
		return flags, i + 1
	}
	if i+1 < len(args) {
		flags = append(flags, "--"+name+"="+args[i+1])
		return flags, i + 2
	}
	flags = append(flags, "--"+name)
	return flags, i + 1
}

func isBoolFlag(v flag.Value) bool {
	bv, ok := v.(interface{ IsBoolFlag() bool })
	return ok && bv.IsBoolFlag()
}

func makeAddFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	bindAddFlags(fs)
	return fs
}
