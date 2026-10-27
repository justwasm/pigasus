package agyproxy

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/agybin"
	"github.com/justwasm/pigasus/internal/models"
)

// ---- capture -------------------------------------------------------------

func cmdCapture(opts AdminOptions, args []string) error {
	if len(args) == 0 {
		c := loadConfig()
		s := resolveSettings(c)
		fmt.Fprintf(opts.Out, "capture: %s\n", onOff(s.CaptureOn))
		fmt.Fprintf(opts.Out, "dir: %s\n", emptyAs(s.CaptureDir, "(disabled)"))
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
	srv, err := New(Config{
		Addr:       set.Addr,
		Upstream:   set.Upstream,
		Models:     configured,
		CaptureDir: set.CaptureDir,
		Logf:       func(string, ...any) {},
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
		fmt.Fprintf(opts.Out, "            %d file%s, %s\n", countCaptureFiles(set.CaptureDir), plural(countCaptureFiles(set.CaptureDir)), dirSize(set.CaptureDir))
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
