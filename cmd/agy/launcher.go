package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/justwasm/pigasus/devutils/agybin"
	"github.com/justwasm/pigasus/devutils/agyproxy"
	"github.com/justwasm/pigasus/internal/models"
)

// runLauncher starts the proxy, points CLOUD_CODE_URL at it, and execs the
// real agy. Everything about the child (stdin/stdout/stderr, working
// directory, argv) is untouched; only the environment gains one variable.
func runLauncher(args []string) int {
	real, err := agybin.Path(os.Getenv(agybin.EnvRealBin))
	if err != nil {
		fmt.Fprintf(os.Stderr, "agy wrapper: %v\n", err)
		return 127
	}

	regPath := models.DefaultConfigPath()
	reg, err := ensureRegistry(regPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agy wrapper: registry: %v\n", err)
		return 1
	}
	configured := reg.List()

	// The proxy logs one line per RPC. Those lines must not go to fd 2: fd 2
	// is the terminal agy's TUI owns, and interleaving with it smears the
	// screen. Default sink is ~/.openagy/wrapper.log (see log.go).
	logf, closeLog := openLog()
	defer closeLog()

	cfg := resolveSettings(loadConfig())
	srv, err := agyproxy.New(agyproxy.Config{
		Addr:       cfg.Addr,
		Upstream:   cfg.Upstream,
		Models:     configured,
		CaptureDir: cfg.CaptureDir,
		Logf:       logf,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agy wrapper: proxy: %v\n", err)
		return 1
	}
	addr, err := srv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agy wrapper: proxy: %v\n", err)
		return 1
	}
	defer srv.Close()

	cmd := exec.Command(real, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = withEnv(os.Environ(), "CLOUD_CODE_URL", "http://"+addr)

	// Forward termination signals so Ctrl-C reaches agy exactly once. The
	// proxy is closed by the deferred Close on the way out.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	go func() {
		for s := range sigs {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		}
	}()

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if code := exitErr.ExitCode(); code >= 0 {
				return code
			}
			if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				return 128 + int(ws.Signal())
			}
			return 1
		}
		fmt.Fprintf(os.Stderr, "agy wrapper: exec %s: %v\n", real, err)
		return 127
	}
	return 0
}

// withEnv returns env with key=value appended, dropping any earlier definition
// so the child sees exactly one CLOUD_CODE_URL (a stale one inherited from a
// parent wrapper would otherwise win on some platforms).
func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, prefix+value)
}
