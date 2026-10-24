// Command openagy-models is the model registry CLI.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/models"
	// Termux/Android: /etc/resolv.conf points at a dead loopback
	// nameserver, so `model test` would never resolve the provider.
	_ "github.com/justwasm/pigasus/internal/dns"
)

const (
	cmdList          = "list"
	cmdAdd           = "add"
	cmdRemove        = "remove"
	cmdTest          = "test"
	cmdInstallCtx    = "install-context"
	cmdHelp          = "help"
	cmdDefaultConfig = "~/.config/openagy/providers.json"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	if os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == cmdHelp {
		usage(os.Stdout)
		return
	}

	cfgPath, sub, rest := parseGlobalArgs(os.Args[1:])
	if sub == "" {
		usage(os.Stderr)
		os.Exit(2)
	}

	if err := executeSubcommand(cfgPath, sub, rest); err != nil {
		fmt.Fprintf(os.Stderr, "openagy-models %s: %v\n", sub, err)
		os.Exit(1)
	}
}

func parseGlobalArgs(args []string) (string, string, []string) {
	cfgPath := envOr("OPENAGY_PROVIDERS_CONFIG", models.ProvidersPath())
	sub := ""
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config":
			if i+1 < len(args) {
				cfgPath = args[i+1]
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
			rest = append(rest, a)
		}
	}
	return cfgPath, sub, rest
}

func executeSubcommand(cfgPath, sub string, rest []string) error {
	switch sub {
	case cmdList:
		return runList(cfgPath)
	case cmdAdd:
		return runAdd(cfgPath, rest)
	case cmdRemove:
		return runRemove(cfgPath, rest)
	case cmdTest:
		return runTest(cfgPath, rest)
	case cmdInstallCtx:
		return runInstallContext(cfgPath, rest)
	default:
		fmt.Fprintf(os.Stderr, "openagy-models: unknown subcommand %q\n\n", sub)
		usage(os.Stderr)
		os.Exit(2)
		return nil
	}
}

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
		fmt.Printf("%-20s %-12s %-10s %s\n", m.Name, m.Provider, m.ExternalModelName, m.APIURL)
	}
	return nil
}

func runRemove(cfgPath string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("remove: <name> required")
	}
	ed := models.NewEditor(cfgPath)
	for _, name := range args {
		_, found, err := ed.DeleteModel(strings.TrimSpace(name))
		if err != nil {
			return err
		}
		if found {
			fmt.Printf("✓ removed %q\n", name)
			continue
		}
		fmt.Printf("✗ %q not found\n", name)
	}
	return nil
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

func usage(w io.Writer) {
	fmt.Fprintf(w, `openagy-models — manage custom LLM providers

Usage:
  openagy-models list
  openagy-models add [flags] [name]
  openagy-models remove <name> [<name> ...]
  openagy-models test <name> [<name> ...]
  openagy-models install-context [lite|strip|passthrough]

Flags (common):
  --config <path>   path to providers.json (default: `+cmdDefaultConfig+`,
                    env: OPENAGY_PROVIDERS_CONFIG)

Flags (add):
  --provider <p>        the provider that serves this model (required);
                        created from the flags below when it does not exist
  --api-url <url>       full chat completions / messages URL
  --api-key <k>         bearer / x-api-key
  --type <t>            wire protocol of the provider: openai|anthropic
  --external-model <m>  model id to send upstream
  --display-name <n>    human-readable name
  --context-window <n>  context size (0=unknown)
  --reasoning-effort <e> low|medium|high|default
  --timeout-seconds <n> per-request timeout (default 60)
  --max-retries <n>     retries on 5xx / network (default 1)
  --interactive         prompt for missing values

Examples:
  openagy-models add minimax-m3 \
      --provider=gateway \
      --api-url=https://gateway.example.com/v1/chat/completions \
      --external-model=MiniMax-M3 \
      --api-key=-
  openagy-models test minimax-m3
  openagy-models install-context strip
`)
}
