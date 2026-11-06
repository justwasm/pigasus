package agyproxy

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/features/models"
	agybin "github.com/justwasm/pigasus/internal/infra/proxy/bin"
)

// ---- capture -------------------------------------------------------------

func cmdCapture(opts AdminOptions, args []string) error {
	if len(args) == 0 {
		c := loadConfig()
		s := resolveSettings(c)
		fmt.Fprintf(opts.Out, "capture: %s\n", onOff(s.CaptureOn))
		fmt.Fprintf(opts.Out, "dir: %s\n", emptyAs(s.CaptureDir, "(disabled)"))
		fmt.Fprintf(opts.Out, "max: %s\n", captureMaxDetail(s.CaptureMaxBytes))
		fmt.Fprintf(opts.Out, "config: %s\n", configPath())
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
		fmt.Fprintf(opts.Out, "capture: %s\n", onOff(on))
		if on {
			fmt.Fprintf(opts.Out, "dir: %s\n", resolveSettings(c).CaptureDir)
		}
		return nil
	case "dir":
		if len(args) < 2 {
			fmt.Fprintln(opts.Out, resolveSettings(c).CaptureDir)
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
		fmt.Fprintf(opts.Out, "dir: %s\n", dir)
		return nil
	case "max":
		if len(args) < 2 {
			fmt.Fprintf(opts.Out, "max: %s\n", captureMaxDetail(resolveSettings(c).CaptureMaxBytes))
			return nil
		}
		n, err := parseBytes(args[1])
		if err != nil {
			return fmt.Errorf("capture max: %w", err)
		}
		c.CaptureMaxBytes = &n
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Fprintf(opts.Out, "max: %s\n", captureMaxDetail(n))
		return nil
	case "purge":
		dir := resolveSettings(c).CaptureDir
		if dir == "" {
			return errors.New("capture purge: the capture directory is disabled")
		}
		files, freed, err := purgeCaptures(dir)
		if err != nil {
			return fmt.Errorf("capture purge: %w", err)
		}
		fmt.Fprintf(opts.Out, "purged %d file%s, freed %s\n", files, plural(files), humanBytes(freed))
		return nil
	default:
		return fmt.Errorf("capture: unknown subcommand %q (want on|off|dir|max|purge)", args[0])
	}
}

// captureMaxText renders the capture ceiling for the doctor line.
func captureMaxText(n int64) string {
	if n <= 0 {
		return "unlimited"
	}
	return humanBytes(n)
}

// captureMaxDetail renders the capture ceiling with its exact byte count.
// The human form alone is misleading when the ceiling was set in decimal
// ("250MB" reads back as "238.4 MiB"), and the byte count is the number the
// rotation actually compares against.
func captureMaxDetail(n int64) string {
	if n <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%s (%d bytes)", humanBytes(n), n)
}

// ---- balance -------------------------------------------------------------

func cmdBalance(opts AdminOptions, args []string) error {
	c := loadConfig()
	if len(args) == 0 {
		s := resolveSettings(c)
		fmt.Fprintf(opts.Out, "balance: %s\n", onOff(s.Balance))
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "on", "enable", "enabled", "1", "true":
		b := true
		c.Balance = &b
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Fprintln(opts.Out, "balance: on")
		return nil
	case "off", "disable", "disabled", "0", "false":
		b := false
		c.Balance = &b
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Fprintln(opts.Out, "balance: off")
		return nil
	default:
		return fmt.Errorf("balance: unknown subcommand %q (want on|off)", args[0])
	}
}

// ---- aggregate -----------------------------------------------------------

func cmdAggregate(opts AdminOptions, args []string) error {
	c := loadConfig()
	if len(args) == 0 {
		s := resolveSettings(c)
		fmt.Fprintf(opts.Out, "aggregate: %s\n", onOff(s.Aggregate))
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "on", "enable", "enabled", "1", "true":
		b := true
		c.Aggregate = &b
		// Enabling aggregate automatically enables balance
		c.Balance = &b
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Fprintln(opts.Out, "aggregate: on (virtual unified account across pool)")
		return nil
	case "off", "disable", "disabled", "0", "false":
		b := false
		c.Aggregate = &b
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Fprintln(opts.Out, "aggregate: off")
		return nil
	default:
		return fmt.Errorf("aggregate: unknown subcommand %q (want on|off)", args[0])
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

// ---- doctor --------------------------------------------------------------

func cmdDoctor(opts AdminOptions, args []string) error {
	fs := newFlagSet("doctor", opts.Err)
	probeTimeout := fs.Duration("probe-timeout", 5*time.Second, "upstream reachability timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Fprintf(opts.Out, "proxy       %s (cycle 274)\n", Version)
	doctorPrintAgy(opts)
	reg, regErr := loadRegistry()
	doctorPrintRegistry(opts, reg, regErr)

	cfg := loadConfig()
	set := resolveSettings(cfg)
	fmt.Fprintf(opts.Out, "config      %s\n", configPath())
	fmt.Fprintf(opts.Out, "log         %s\n", logSink())
	fmt.Fprintf(opts.Out, "upstream    %s\n", emptyAs(set.Upstream, endpoints.Current().CloudCode))

	var configured []models.Model
	if regErr == nil {
		configured = reg.List()
	}
	doctorPrintServer(opts, set, configured)
	doctorPrintProbe(opts, set, *probeTimeout)
	return nil
}

func doctorPrintAgy(opts AdminOptions) {
	real, err := agybin.Path(os.Getenv(agybin.EnvRealBin))
	if err != nil {
		fmt.Fprintf(opts.Out, "agy         NOT FOUND: %v\n", err)
		return
	}
	ver, verr := agybin.Version(real)
	if verr != nil {
		fmt.Fprintf(opts.Out, "agy         %s (version: %v)\n", real, verr)
		return
	}
	fmt.Fprintf(opts.Out, "agy         %s (%s)\n", real, ver)
	if ver != "1.4.2" {
		fmt.Fprintf(opts.Out, "note        built against 1.4.2; CLOUD_CODE_URL support is verified there\n")
	}
}

func doctorPrintRegistry(opts AdminOptions, reg *models.Registry, regErr error) {
	if regErr != nil {
		fmt.Fprintf(opts.Out, "registry    %s: %v\n", models.ProvidersPath(), regErr)
		return
	}
	fmt.Fprintf(opts.Out, "registry    %s (%d model%s)\n", models.ProvidersPath(), reg.Len(), plural(reg.Len()))
	for _, m := range reg.List() {
		display := m.DisplayName
		if display == "" {
			display = m.Name
		}
		fmt.Fprintf(opts.Out, "            %-16s %-20s %-10s → %-20s %s\n",
			m.Name, display, m.Provider, m.ExternalModelName, Placeholder(m))
	}
}

func doctorPrintServer(opts AdminOptions, set settings, configured []models.Model) {
	maxBytes := set.CaptureMaxBytes
	srv, err := New(Config{
		Addr:            set.Addr,
		Upstream:        set.Upstream,
		Models:          configured,
		CaptureDir:      set.CaptureDir,
		CaptureMaxBytes: &maxBytes,
		Logf:            func(string, ...any) {},
		Balance:         set.Balance,
	})
	if err != nil {
		fmt.Fprintf(opts.Out, "port        BIND FAILED: %v\n", err)
	} else if addr, err := srv.Start(); err != nil {
		fmt.Fprintf(opts.Out, "port        BIND FAILED: %v\n", err)
	} else {
		fmt.Fprintf(opts.Out, "port        %s (ephemeral; CLOUD_CODE_URL=http://%s)\n", set.Addr, addr)
		_ = srv.Close()
	}
	fmt.Fprintf(opts.Out, "capture     %s (%s)\n", emptyAs(set.CaptureDir, "(disabled)"), onOff(set.CaptureOn && set.CaptureDir != ""))
	if set.CaptureDir != "" {
		fmt.Fprintf(opts.Out, "            %d file%s, %s (max %s)\n",
			countCaptureFiles(set.CaptureDir), plural(countCaptureFiles(set.CaptureDir)),
			dirSize(set.CaptureDir), captureMaxText(set.CaptureMaxBytes))
	}
	fmt.Fprintf(opts.Out, "balance     %s\n", onOff(set.Balance))
	if set.Balance {
		fmt.Fprintf(opts.Out, "aggregate   %s\n", onOff(set.Aggregate))
	}
	if pol := endpoints.CurrentAccountPool(); pol != nil {
		strategy := pol.Strategy
		if strategy == "" {
			strategy = pol.Normalised().Strategy
		}
		fmt.Fprintf(opts.Out, "pool        %s (strategy: %s, threshold: %.1f%%)\n",
			onOff(pol.Enabled), strategy, pol.Threshold*100)
	}
}

func doctorPrintProbe(opts AdminOptions, set settings, probeTimeout time.Duration) {
	upstream := emptyAs(set.Upstream, endpoints.Current().CloudCode)
	if status, elapsed, perr := probeUpstream(upstream, probeTimeout); perr != nil {
		fmt.Fprintf(opts.Out, "probe       %s: %v\n", upstream, perr)
	} else {
		fmt.Fprintf(opts.Out, "probe       %s: HTTP %d in %s\n", upstream, status, elapsed.Round(time.Millisecond))
	}
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

// ---- version -------------------------------------------------------------

func cmdVersion(opts AdminOptions, args []string) error {
	fmt.Fprintf(opts.Out, "agy proxy %s (cycle 274)\n", Version)
	real, err := agybin.Path(os.Getenv(agybin.EnvRealBin))
	if err != nil {
		fmt.Fprintf(opts.Out, "agy         not found (%v)\n", err)
		return nil
	}
	ver, verr := agybin.Version(real)
	if verr != nil {
		fmt.Fprintf(opts.Out, "agy         %s (version: %v)\n", real, verr)
		return nil
	}
	fmt.Fprintf(opts.Out, "agy         %s (%s)\n", real, ver)
	return nil
}

// ---- serve ---------------------------------------------------------------

func cmdServe(opts AdminOptions, args []string) error {
	fileCfg := loadConfig()
	set := resolveSettings(fileCfg)
	maxBytes := set.CaptureMaxBytes

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(opts.Err)
	addrFlag := fs.String("addr", firstNonEmpty(set.Addr, "127.0.0.1:19999"), "HTTP listen address")
	balanceFlag := fs.Bool("balance", set.Balance, "enable multi-account load balancing")
	aggregateFlag := fs.Bool("aggregate", set.Aggregate, "enable virtual unified pool account aggregation")
	captureFlag := fs.Bool("capture", set.CaptureOn, "enable request/response capture")
	captureDirFlag := fs.String("capture-dir", set.CaptureDir, "directory for request/response capture")
	if err := fs.Parse(args); err != nil {
		return err
	}

	capDir := *captureDirFlag
	if !*captureFlag {
		capDir = ""
	}

	cfg := Config{
		Addr:            *addrFlag,
		Upstream:        set.Upstream,
		CaptureDir:      capDir,
		CaptureMaxBytes: &maxBytes,
		Balance:         *balanceFlag,
		Aggregate:       *aggregateFlag,
		Logf: func(format string, a ...any) {
			fmt.Fprintf(opts.Out, "[proxy] "+format+"\n", a...)
		},
	}
	srv, err := New(cfg)
	if err != nil {
		return err
	}
	boundAddr, err := srv.Start()
	if err != nil {
		return err
	}
	defer srv.Close()

	fmt.Fprintf(opts.Out, "OpenAGY Gateway listening on http://%s\n", boundAddr)
	fmt.Fprintf(opts.Out, "  OpenAI endpoint:    http://%s/v1/chat/completions\n", boundAddr)
	fmt.Fprintf(opts.Out, "  Anthropic endpoint: http://%s/v1/messages\n", boundAddr)
	fmt.Fprintf(opts.Out, "  Responses endpoint: http://%s/v1/responses (WebSocket + HTTP)\n", boundAddr)
	fmt.Fprintf(opts.Out, "  Models endpoint:    http://%s/v1/models\n", boundAddr)
	fmt.Fprintf(opts.Out, "  CloudCode upstream: %s\n", srv.upstream)
	fmt.Fprintf(opts.Out, "  Aggregate mode:     %v (balancing: %v)\n", srv.aggregateActive(), srv.balanceActive())
	fmt.Fprintf(opts.Out, "\nReady for local agents (Claude Code, Codex, Cursor, Cline, Aider). Press Ctrl+C to stop.\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	fmt.Fprintf(opts.Out, "\nShutting down...\n")
	return nil
}
