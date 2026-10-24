package agyproxy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/justwasm/pigasus/internal/agybin"
	"github.com/justwasm/pigasus/internal/models"
)

// Launch starts the proxy, points CLOUD_CODE_URL at it, and execs the
// real agy. Everything about the child (stdin/stdout/stderr, working
// directory, argv) is untouched; only the environment gains one variable.
//
// The clone calls this for `pigasus agy`; see cmd/pigasus/main.go.
func Launch(args []string) int {
	real, err := agybin.Path(os.Getenv(agybin.EnvRealBin))
	if err != nil {
		fmt.Fprintf(os.Stderr, "agy proxy: %v\n", err)
		return 127
	}
	reg, err := loadRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agy proxy: registry: %v\n", err)
		return 1
	}

	logf, closeLog := openLog()
	defer closeLog()

	srv, addr, err := startProxyServer(reg.List(), logf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agy proxy: proxy: %v\n", err)
		return 1
	}
	defer srv.Close()

	return runWrappedChild(real, args, addr)
}

func startProxyServer(configured []models.Model, logf func(string, ...any)) (*Server, string, error) {
	cfg := resolveSettings(loadConfig())
	srv, err := New(Config{
		Addr:       cfg.Addr,
		Upstream:   cfg.Upstream,
		Models:     configured,
		CaptureDir: cfg.CaptureDir,
		Logf:       logf,
	})
	if err != nil {
		return nil, "", err
	}
	addr, err := srv.Start()
	if err != nil {
		return nil, "", err
	}
	return srv, addr, nil
}

func runWrappedChild(real string, args []string, addr string) int {
	cmd := exec.Command(real, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = withEnv(os.Environ(), "CLOUD_CODE_URL", "http://"+addr)

	cleanupSignals := forwardSignals(cmd)
	defer cleanupSignals()

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
		fmt.Fprintf(os.Stderr, "agy proxy: exec %s: %v\n", real, err)
		return 127
	}
	return 0
}

func forwardSignals(cmd *exec.Cmd) func() {
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for s := range sigs {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		}
	}()
	return func() { signal.Stop(sigs) }
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
