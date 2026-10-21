// Command openagy-models is the model registry CLI.
//
// Usage:
//
//	openagy-models list
//	openagy-models add [flags] [name]
//	openagy-models remove <name>
//	openagy-models test <name>
//	openagy-models install-context [lite|strip|passthrough]
//
// The CLI is intentionally small. It does NOT talk to the TUI or the
// shims; it only reads/writes `~/.config/openagy/models.json` and runs
// smoke tests. Everything else (loading the registry, choosing a
// provider, dispatching) is done by the consumer (antigravity, openai-shim,
// anthropic-shim).
//
// Sandbox default: ~/.config/openagy/models.json. Override with
// --config or $OPENAGY_MODELS_CONFIG.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/models"
)

// subcommand constants. We use a simple string switch instead of flag
// sub-commands because the CLI is small enough that adding
// github.com/spf13/cobra would be a larger dep than the rest of the file.
const (
	cmdList          = "list"
	cmdAdd           = "add"
	cmdRemove        = "remove"
	cmdTest          = "test"
	cmdInstallCtx    = "install-context"
	cmdHelp          = "help"
	cmdDefaultConfig = "~/.config/openagy/models.json"
)

func main() {
	// Root flags. Each subcommand has its own FlagSet, but the config
	// path is shared so it can be set globally with --config.
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	// Handle the help / no-arg case at the top so the user always sees
	// the same banner.
	if os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == cmdHelp {
		usage(os.Stdout)
		return
	}

	sub := ""
	args := make([]string, 0, len(os.Args)-1)
	// Pull --config and the subcommand out of the front, in any order.
	// Doing this in a flag.FlagSet would mean defining --config in the
	// global set AND in every subcommand set, which is annoying. The
	// hand-rolled loop is small enough to be obvious.
	cfgPath := envOr("OPENAGY_MODELS_CONFIG", models.DefaultConfigPath())
	for i := 1; i < len(os.Args); i++ {
		a := os.Args[i]
		switch {
		case a == "--config":
			if i+1 < len(os.Args) {
				cfgPath = os.Args[i+1]
				i++
			} else {
				fmt.Fprintln(os.Stderr, "openagy-models: --config requires a value")
				os.Exit(2)
			}
		case strings.HasPrefix(a, "--config="):
			cfgPath = strings.TrimPrefix(a, "--config=")
		case sub == "" && !strings.HasPrefix(a, "-"):
			sub = a
		default:
			args = append(args, a)
		}
	}
	if sub == "" {
		usage(os.Stderr)
		os.Exit(2)
	}
	rest := args

	var err error
	switch sub {
	case cmdList:
		err = runList(cfgPath)
	case cmdAdd:
		err = runAdd(cfgPath, rest)
	case cmdRemove:
		err = runRemove(cfgPath, rest)
	case cmdTest:
		err = runTest(cfgPath, rest)
	case cmdInstallCtx:
		err = runInstallContext(cfgPath, rest)
	default:
		fmt.Fprintf(os.Stderr, "openagy-models: unknown subcommand %q\n\n", sub)
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "openagy-models %s: %v\n", sub, err)
		os.Exit(1)
	}
}

// ---- subcommands -------------------------------------------------------

func runList(cfgPath string) error {
	r, err := loadOrEmpty(cfgPath)
	if err != nil {
		return err
	}
	models := r.List()
	if len(models) == 0 {
		fmt.Printf("(no models configured; add one with `openagy-models add`)\n")
		return nil
	}
	fmt.Printf("%-20s %-12s %-10s %s\n", "NAME", "PROVIDER", "EXT-MODEL", "API-URL")
	for _, m := range models {
		display := m.DisplayName
		if display == "" {
			display = m.Name
		}
		fmt.Printf("%-20s %-12s %-10s %s\n", m.Name, m.Provider, m.ExternalModelName, m.APIURL)
		_ = display // currently unused in output, kept for future "list -v"
	}
	return nil
}

func runAdd(cfgPath string, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	name := fs.String("name", "", "internal name (e.g. agw, openai). If empty, the first positional is used.")
	provider := fs.String("provider", "", "provider: openai|anthropic|ollama|openrouter|agw|google|custom")
	apiURL := fs.String("api-url", "", "full chat completions / messages URL")
	extModel := fs.String("external-model", "", "model id to send upstream (e.g. MiniMax-M3, gpt-4o-mini)")
	apiKey := fs.String("api-key", "", "bearer / x-api-key credential (use - for agw dev sentinel)")
	display := fs.String("display-name", "", "human-readable name (optional)")
	cw := fs.Int("context-window", 0, "context window size (0=unknown)")
	reasoning := fs.String("reasoning-effort", "", "OpenAI o-series hint: low|medium|high|default")
	timeout := fs.Int("timeout-seconds", 60, "per-request timeout")
	retries := fs.Int("max-retries", 1, "max retries on 5xx / network")
	interactive := fs.Bool("interactive", false, "prompt for missing values")
	// Reorder args so all flags come before positionals. Go's stdlib
	// flag.Parse stops at the first non-flag token, which breaks the
	// common `add agw --provider=...` invocation. We do a single pass
	// here that re-emits flags first, positionals after.
	ordered := reorderArgs(args, fs)
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	// Positional name is allowed for the common "openagy-models add agw"
	// invocation.
	if *name == "" && fs.NArg() > 0 {
		*name = fs.Arg(0)
	}
	// Interactive: read any missing field from stdin.
	if *interactive {
		if err := promptFor(fs, name, provider, apiURL, extModel, apiKey); err != nil {
			return err
		}
	}
	if *name == "" || *provider == "" || *apiURL == "" || *extModel == "" {
		return fmt.Errorf("add: --name, --provider, --api-url, --external-model are required (or use --interactive)")
	}
	m := models.Model{
		Name:              *name,
		DisplayName:       *display,
		Provider:          *provider,
		APIKey:            *apiKey,
		APIURL:            *apiURL,
		ExternalModelName: *extModel,
		ContextWindow:     *cw,
		ReasoningEffort:   *reasoning,
		TimeoutSeconds:    *timeout,
		MaxRetries:        *retries,
	}
	r, err := loadOrEmpty(cfgPath)
	if err != nil {
		return err
	}
	if err := r.Add(m); err != nil {
		return err
	}
	if err := models.SaveToFile(cfgPath, r); err != nil {
		return err
	}
	fmt.Printf("✓ added %q -> %s %s\n", m.Name, m.Provider, m.APIURL)
	return nil
}

func runRemove(cfgPath string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("remove: <name> required")
	}
	r, err := loadOrEmpty(cfgPath)
	if err != nil {
		return err
	}
	for _, name := range args {
		if r.Remove(name) {
			fmt.Printf("✓ removed %q\n", name)
		} else {
			fmt.Printf("✗ %q not found\n", name)
		}
	}
	return models.SaveToFile(cfgPath, r)
}

func runTest(cfgPath string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("test: <name> required")
	}
	r, err := loadOrEmpty(cfgPath)
	if err != nil {
		return err
	}
	for _, name := range args {
		if err := smokeTest(r, name); err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s: %v\n", name, err)
			continue
		}
	}
	return nil
}

func runInstallContext(cfgPath string, args []string) error {
	mode := models.ModeLite
	if len(args) > 0 {
		mode = models.Mode(strings.ToLower(args[0]))
	}
	if err := models.SetMode(string(mode)); err != nil {
		return err
	}
	dir := filepath.Dir(cfgPath)
	paths, err := models.InstallContext(dir, mode)
	if err != nil {
		return err
	}
	for _, p := range paths {
		fmt.Printf("✓ wrote %s\n", p)
	}
	fmt.Printf("mode: %s\n", models.Banner())
	return nil
}

// ---- helpers -----------------------------------------------------------

// smokeTest runs a single Chat call against the named model. The smoke
// input is a fixed "ping" string; the test passes if the provider returns
// a non-empty content field. We deliberately don't assert semantic
// correctness — provider behavior is opaque from here, and we'd rather
// have a CI test that says "I got a reply" than one that flakes when
// an upstream model changes its tone.
func smokeTest(r *models.Registry, name string) error {
	m, ok := r.GetByName(name)
	if !ok {
		return fmt.Errorf("not configured")
	}
	prov, err := models.NewProvider(*m)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.Timeout())*time.Second)
	defer cancel()
	fmt.Printf("[test %s] provider=%s model=%s url=%s ...\n", name, prov.Name(), m.ExternalModelName, m.APIURL)
	resp, err := prov.Chat(ctx, models.ChatRequest{
		Messages: []models.ChatMessage{{Role: "user", Content: "ping"}},
		MaxTokens: 50,
	})
	if err != nil {
		return err
	}
	if resp.Content == "" {
		return fmt.Errorf("empty content")
	}
	if strings.Contains(resp.Content, "mock reply") {
		return fmt.Errorf("got 'mock reply' (provider misrouted to mock?)")
	}
	fmt.Printf("✓ %s (%d tokens): %q\n", name, resp.TotalTokens, truncate(resp.Content, 120))
	if len(resp.Raw) > 0 {
		// Show one parsed field to confirm the wire shape, but cap to a few
		// hundred bytes so the test output stays readable.
		var asMap map[string]any
		if err := json.Unmarshal(resp.Raw, &asMap); err == nil {
			if id, _ := asMap["id"].(string); id != "" {
				fmt.Printf("  upstream id: %s\n", id)
			}
			if model, _ := asMap["model"].(string); model != "" {
				fmt.Printf("  upstream model: %s\n", model)
			}
		}
	}
	return nil
}

func loadOrEmpty(path string) (*models.Registry, error) {
	if path == "" {
		return &models.Registry{}, nil
	}
	return models.LoadFromFile(path)
}

func promptFor(fs *flag.FlagSet, name, provider, apiURL, extModel, apiKey *string) error {
	// Read from stdin one line at a time. We only ask for fields that
	// are still empty. Existing flags win.
	if *name == "" {
		v, err := ask("name")
		if err != nil {
			return err
		}
		*name = v
	}
	if *provider == "" {
		v, err := ask("provider (openai|anthropic|ollama|openrouter|agw|google|custom)")
		if err != nil {
			return err
		}
		*provider = v
	}
	if *apiURL == "" {
		v, err := ask("api-url (full URL)")
		if err != nil {
			return err
		}
		*apiURL = v
	}
	if *extModel == "" {
		v, err := ask("external-model")
		if err != nil {
			return err
		}
		*extModel = v
	}
	if *apiKey == "" {
		v, err := ask("api-key (use - for agw dev sentinel)")
		if err != nil {
			return err
		}
		*apiKey = v
	}
	_ = fs // currently unused, but kept for future expansion
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

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// reorderArgs returns a new slice with all flag-style tokens moved to the
// front. The set of "known flags" comes from the FlagSet. This lets
// `add agw --provider=agw` work even though stdlib's flag.Parse stops
// at the first non-flag token. Bool flags without a value are emitted
// as `--name=true` so the parser still recognizes them.
func reorderArgs(args []string, fs *flag.FlagSet) []string {
	known := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		known[f.Name] = true
	})
	flags := make([]string, 0, len(args))
	pos := make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		a := args[i]
		var name string
		var hasValue bool
		var value string
		switch {
		case strings.HasPrefix(a, "--"):
			eq := strings.IndexByte(a, '=')
			if eq >= 0 {
				name = strings.TrimPrefix(a[:eq], "--")
				hasValue = true
				value = a[eq+1:]
			} else {
				name = strings.TrimPrefix(a, "--")
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			name = strings.TrimPrefix(a, "-")
		default:
			pos = append(pos, a)
			i++
			continue
		}
		if !known[name] {
			// Unknown — treat as positional so flag.Parse can complain
			// about it (more useful than silently dropping).
			pos = append(pos, a)
			i++
			continue
		}
		flag := fs.Lookup(name)
		isBool := false
		if flag != nil {
			// flag.Value tells us if it's a bool (the zero value
			// defaults to false for bool flags, and isBoolFlag is the
			// canonical check).
			if bv, ok := flag.Value.(interface{ IsBoolFlag() bool }); ok && bv.IsBoolFlag() {
				isBool = true
			}
		}
		if hasValue {
			flags = append(flags, "--"+name+"="+value)
		} else if isBool {
			flags = append(flags, "--"+name+"=true")
		} else if i+1 < len(args) {
			flags = append(flags, "--"+name+"="+args[i+1])
			i++
		} else {
			// No value supplied; emit as-is and let flag.Parse error.
			flags = append(flags, "--"+name)
		}
		i++
	}
	return append(flags, pos...)
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `openagy-models — manage custom LLM providers (cycle 33+)

Usage:
  openagy-models list
  openagy-models add [flags] [name]
  openagy-models remove <name> [<name> ...]
  openagy-models test <name> [<name> ...]
  openagy-models install-context [lite|strip|passthrough]

Flags (common):
  --config <path>   path to models.json (default: `+cmdDefaultConfig+`,
                    env: OPENAGY_MODELS_CONFIG)

Flags (add):
  --provider <p>        one of: openai|anthropic|ollama|openrouter|agw|google|custom
  --api-url <url>       full chat completions / messages URL
  --external-model <m>  model id to send upstream
  --api-key <k>         bearer / x-api-key (use - for agw dev sentinel)
  --display-name <n>    human-readable name
  --context-window <n>  context size (0=unknown)
  --reasoning-effort <e> low|medium|high|default
  --timeout-seconds <n> per-request timeout (default 60)
  --max-retries <n>     retries on 5xx / network (default 1)
  --interactive         prompt for missing values

Examples:
  openagy-models add agw \
      --provider=agw \
      --api-url=https://agw.k0s.io/v1/chat/completions \
      --external-model=MiniMax-M3 \
      --api-key=-
  openagy-models test agw
  openagy-models install-context strip
`)
}

// ---- test-only helpers (used by main_test.go) -------------------------

// makeAddFlagSet builds the same FlagSet the `add` subcommand uses.
// Exported to the same package only.
func makeAddFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.String("name", "", "")
	fs.String("provider", "", "")
	fs.String("api-url", "", "")
	fs.String("external-model", "", "")
	fs.String("api-key", "", "")
	fs.String("display-name", "", "")
	fs.Int("context-window", 0, "")
	fs.String("reasoning-effort", "", "")
	fs.Int("timeout-seconds", 60, "")
	fs.Int("max-retries", 1, "")
	fs.Bool("interactive", false, "")
	return fs
}

// osReadFile is a tiny alias so test code doesn't need to import os
// directly (some linters complain about file I/O in test files).
func osReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
