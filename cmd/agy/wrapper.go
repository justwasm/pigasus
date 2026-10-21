package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/justwasm/pigasus/devutils/agybin"
	"github.com/justwasm/pigasus/devutils/agyproxy"
	"github.com/justwasm/pigasus/internal/models"
)

// wrapperVersion is the wrapper's own version, independent of agy's.
const wrapperVersion = "0.1.0"

// wrapperUsage is the help text. Kept as one string so `wrapper help` and a
// bare `wrapper` print the same thing.
const wrapperUsage = `usage: agy wrapper <command> [args]

  models                     list configured models
  models add <name> [flags]  add or replace a model
  models remove <name>       delete a model
  capture on|off             toggle request/response capture
  capture dir [path]         show or set the capture directory
  log [-n N]                 tail the proxy log (default 40 lines)
  doctor                     agy path/version, registry, port, capture, upstream
  version                    wrapper and agy versions
  help                       this text

Everything else on the command line is passed to agy untouched:
  agy -p "hello"             run agy with the proxy attached
  agy models                 the vendor model listing

environment:
  AGY_REAL_BIN               path to the real agy binary
  OPENAGY_WRAPPER_ADDR       listen address (default 127.0.0.1:0)
  OPENAGY_CAPTURE_DIR        capture directory
  OPENAGY_UPSTREAM           upstream base URL
  OPENAGY_WRAPPER_CONFIG     wrapper config file
  OPENAGY_WRAPPER_LOG        proxy log sink: a file path (default
                             ~/.openagy/wrapper.log), "stderr", or "off".
                             stderr smears agy's TUI — debugging only.
`

// runWrapper dispatches the admin subcommands. Any error is printed once and
// turned into exit status 1; nothing here touches the network for long.
func runWrapper(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stdout, wrapperUsage)
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(os.Stdout, wrapperUsage)
		return 0
	case "version", "-v", "--version":
		return wrapErr(cmdVersion(args[1:]))
	case "models":
		return wrapErr(cmdModels(args[1:]))
	case "capture":
		return wrapErr(cmdCapture(args[1:]))
	case "log":
		return wrapErr(cmdLog(args[1:]))
	case "doctor":
		return wrapErr(cmdDoctor(args[1:]))
	default:
		return wrapErr(fmt.Errorf("unknown command %q (try: agy wrapper help)", args[0]))
	}
}

func wrapErr(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	fmt.Fprintf(os.Stderr, "agy wrapper: %v\n", err)
	return 1
}

// ---- models ------------------------------------------------------------

func cmdModels(args []string) error {
	if len(args) == 0 {
		reg, err := ensureRegistry(models.DefaultConfigPath())
		if err != nil {
			return err
		}
		printModels(reg)
		return nil
	}
	switch args[0] {
	case "list", "ls":
		reg, err := ensureRegistry(models.DefaultConfigPath())
		if err != nil {
			return err
		}
		printModels(reg)
		return nil
	case "add":
		return cmdModelsAdd(args[1:])
	case "remove", "rm", "delete":
		return cmdModelsRemove(args[1:])
	default:
		return fmt.Errorf("models: unknown subcommand %q (want add|remove|list)", args[0])
	}
}

func printModels(reg *models.Registry) {
	path := reg.Path()
	if path == "" {
		path = models.DefaultConfigPath()
	}
	list := reg.List()
	fmt.Printf("registry: %s\n", path)
	if len(list) == 0 {
		fmt.Println("no models configured")
		return
	}
	for _, m := range list {
		display := m.DisplayName
		if display == "" {
			display = m.Name
		}
		fmt.Printf("%-20s %-24s provider=%-10s model=%-20s placeholder=%s slug=%s\n",
			m.Name, display, m.Provider, m.ExternalModelName,
			agyproxy.Placeholder(m), agyproxy.Slug(m))
		if m.ContextWindow > 0 {
			fmt.Printf("%-20s context_window=%d\n", "", m.ContextWindow)
		}
	}
}

func cmdModelsAdd(args []string) error {
	fs := flag.NewFlagSet("models add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		display  string
		provider string
		apiURL   string
		apiKey   string
		external string
		ctxWin   int
		effort   string
	)
	// Both spellings map to the same variable: shell users type dashes,
	// JSON-shaped copy/paste carries underscores.
	for _, name := range []string{"display-name", "display_name"} {
		fs.StringVar(&display, name, "", "display name shown by agy")
	}
	fs.StringVar(&provider, "provider", "openai", "openai|anthropic|ollama|google|openrouter|agw|custom")
	for _, name := range []string{"api-url", "api_url"} {
		fs.StringVar(&apiURL, name, "", "chat completions (or messages) endpoint")
	}
	for _, name := range []string{"api-key", "api_key"} {
		fs.StringVar(&apiKey, name, "", "credential; agw uses the literal -")
	}
	for _, name := range []string{"external-model", "external_model_name"} {
		fs.StringVar(&external, name, "", "model id sent upstream")
	}
	for _, name := range []string{"context-window", "context_window"} {
		fs.IntVar(&ctxWin, name, 0, "declared context window in tokens")
	}
	fs.StringVar(&effort, "reasoning-effort", "", "low|medium|high|default")

	// The name may appear before or after the flags.
	var name string
	var rest []string
	for _, a := range args {
		if name == "" && !strings.HasPrefix(a, "-") {
			name = a
			continue
		}
		rest = append(rest, a)
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if name == "" {
		return errors.New("models add: name is required (agy wrapper models add NAME -provider …)")
	}
	m := models.Model{
		Name:              name,
		DisplayName:       display,
		Provider:          provider,
		APIKey:            apiKey,
		APIURL:            apiURL,
		ExternalModelName: external,
		ContextWindow:     ctxWin,
		ReasoningEffort:   effort,
	}
	if m.ExternalModelName == "" {
		m.ExternalModelName = name
	}
	if err := m.Validate(); err != nil {
		return err
	}
	reg, err := ensureRegistry(models.DefaultConfigPath())
	if err != nil {
		return err
	}
	if err := reg.Add(m); err != nil {
		return err
	}
	if err := models.SaveToFile(models.DefaultConfigPath(), reg); err != nil {
		return err
	}
	fmt.Printf("added %s (%s → %s, placeholder %s)\n", m.Name, m.Provider, m.ExternalModelName, agyproxy.Placeholder(m))
	return nil
}

func cmdModelsRemove(args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return errors.New("models remove: name is required")
	}
	name := strings.TrimSpace(args[0])
	path := models.DefaultConfigPath()
	reg, err := ensureRegistry(path)
	if err != nil {
		return err
	}
	if !reg.Remove(name) {
		return fmt.Errorf("models remove: no model named %q", name)
	}
	if err := models.SaveToFile(path, reg); err != nil {
		return err
	}
	fmt.Printf("removed %s\n", name)
	return nil
}

// ---- capture -----------------------------------------------------------

func cmdCapture(args []string) error {
	if len(args) == 0 {
		c := loadConfig()
		s := resolveSettings(c)
		fmt.Printf("capture: %s\n", onOff(s.CaptureOn))
		fmt.Printf("dir: %s\n", emptyAs(s.CaptureDir, "(disabled)"))
		fmt.Printf("config: %s\n", configPath())
		return nil
	}
	c := loadConfig()
	switch args[0] {
	case "on", "off":
		on := args[0] == "on"
		c.Capture = &on
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Printf("capture: %s\n", onOff(on))
		if on {
			fmt.Printf("dir: %s\n", resolveSettings(c).CaptureDir)
		}
		return nil
	case "dir":
		if len(args) < 2 {
			fmt.Println(resolveSettings(c).CaptureDir)
			return nil
		}
		dir, err := expandUserPath(args[1])
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		c.CaptureDir = dir
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Printf("dir: %s\n", dir)
		return nil
	default:
		return fmt.Errorf("capture: unknown subcommand %q (want on|off|dir)", args[0])
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func emptyAs(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// ---- doctor ------------------------------------------------------------

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	probeTimeout := fs.Duration("probe-timeout", 5*time.Second, "upstream reachability timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Printf("wrapper     %s (cycle 274)\n", wrapperVersion)

	// agy path + version. A missing binary is reported, not fatal: doctor is
	// diagnostic and should still print the rest.
	real, err := agybin.Path(os.Getenv(agybin.EnvRealBin))
	if err != nil {
		fmt.Printf("agy         NOT FOUND: %v\n", err)
	} else {
		ver, verr := agybin.Version(real)
		switch {
		case verr != nil:
			fmt.Printf("agy         %s (version: %v)\n", real, verr)
		default:
			fmt.Printf("agy         %s (%s)\n", real, ver)
			if ver != "1.4.2" {
				fmt.Printf("note        built against 1.4.2; CLOUD_CODE_URL support is verified there\n")
			}
		}
	}

	// Registry (creates the default MiniMax M3 entry on first run).
	regPath := models.DefaultConfigPath()
	reg, regErr := ensureRegistry(regPath)
	if regErr != nil {
		fmt.Printf("registry    %s: %v\n", regPath, regErr)
	} else {
		fmt.Printf("registry    %s (%d model%s)\n", regPath, reg.Len(), plural(reg.Len()))
		for _, m := range reg.List() {
			display := m.DisplayName
			if display == "" {
				display = m.Name
			}
			fmt.Printf("            %-16s %-20s %-10s → %-20s %s\n",
				m.Name, display, m.Provider, m.ExternalModelName, agyproxy.Placeholder(m))
		}
	}

	cfg := loadConfig()
	set := resolveSettings(cfg)
	fmt.Printf("config      %s\n", configPath())
	fmt.Printf("log         %s\n", logSink())
	fmt.Printf("upstream    %s\n", emptyAs(set.Upstream, agyproxy.DefaultUpstream))

	// Bind the proxy for real so the line shows a port that works, then close.
	var configured []models.Model
	if regErr == nil {
		configured = reg.List()
	}
	srv, err := agyproxy.New(agyproxy.Config{
		Addr:       set.Addr,
		Upstream:   set.Upstream,
		Models:     configured,
		CaptureDir: set.CaptureDir,
		Logf:       func(string, ...any) {}, // doctor stays quiet
	})
	if err != nil {
		fmt.Printf("port        BIND FAILED: %v\n", err)
	} else if addr, err := srv.Start(); err != nil {
		fmt.Printf("port        BIND FAILED: %v\n", err)
	} else {
		fmt.Printf("port        %s (ephemeral; CLOUD_CODE_URL=http://%s)\n", set.Addr, addr)
		_ = srv.Close()
	}

	fmt.Printf("capture     %s (%s)\n", emptyAs(set.CaptureDir, "(disabled)"), onOff(set.CaptureOn && set.CaptureDir != ""))
	if set.CaptureDir != "" {
		fmt.Printf("            %d file%s, %s\n", countCaptureFiles(set.CaptureDir), plural(countCaptureFiles(set.CaptureDir)), dirSize(set.CaptureDir))
	}

	// Upstream reachability. Any HTTP answer — including 401/404 — proves DNS
	// and TLS work; only transport errors are interesting.
	upstream := emptyAs(set.Upstream, agyproxy.DefaultUpstream)
	if status, elapsed, perr := probeUpstream(upstream, *probeTimeout); perr != nil {
		fmt.Printf("probe       %s: %v\n", upstream, perr)
	} else {
		fmt.Printf("probe       %s: HTTP %d in %s\n", upstream, status, elapsed.Round(time.Millisecond))
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func probeUpstream(base string, timeout time.Duration) (int, time.Duration, error) {
	client := &http.Client{Timeout: timeout}
	started := time.Now()
	resp, err := client.Get(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, time.Since(started), nil
}

func countCaptureFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

func dirSize(dir string) string {
	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "0 B"
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() {
			total += info.Size()
		}
	}
	return humanBytes(total)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// expandUserPath expands a leading ~ so `capture dir ~/x` does what it says.
func expandUserPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "~/") {
		if p == "" {
			return "", errors.New("empty path")
		}
		return filepath.Abs(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[2:]), nil
}

// ---- version -----------------------------------------------------------

func cmdVersion(args []string) error {
	fmt.Printf("agy wrapper %s (cycle 274)\n", wrapperVersion)
	real, err := agybin.Path(os.Getenv(agybin.EnvRealBin))
	if err != nil {
		fmt.Printf("agy         not found (%v)\n", err)
		return nil
	}
	ver, verr := agybin.Version(real)
	if verr != nil {
		fmt.Printf("agy         %s (version: %v)\n", real, verr)
		return nil
	}
	fmt.Printf("agy         %s (%s)\n", real, ver)
	return nil
}
