package agyproxy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"

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

	filteredArgs, balanceFlag, aggregateFlag := parseProxyFlags(args)

	srv, addr, err := startProxyServer(reg.List(), logf, balanceFlag, aggregateFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agy proxy: proxy: %v\n", err)
		return 1
	}
	defer srv.Close()

	return runWrappedChild(real, filteredArgs, addr)
}

// parseProxyFlags extracts proxy-level flags (e.g. --balance, --aggregate) before arguments
// reach the vendor agy binary. It returns the arguments to pass to vendor agy,
// and whether balance/aggregate was explicitly enabled or disabled (*bool, nil if unset).
func parseProxyFlags(args []string) ([]string, *bool, *bool) {
	filtered := make([]string, 0, len(args))
	var balance *bool
	var aggregate *bool
	pastDashDash := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		if pastDashDash {
			filtered = append(filtered, a)
			continue
		}
		if a == "--" {
			pastDashDash = true
			filtered = append(filtered, a)
			continue
		}

		// Check for --no-balance / -no-balance
		if a == "--no-balance" || a == "-no-balance" {
			f := false
			balance = &f
			continue
		}

		// Check for --no-aggregate / -no-aggregate
		if a == "--no-aggregate" || a == "-no-aggregate" {
			f := false
			aggregate = &f
			continue
		}

		// Check for --balance=... / -balance=...
		if strings.HasPrefix(a, "--balance=") || strings.HasPrefix(a, "-balance=") {
			val := a[strings.IndexByte(a, '=')+1:]
			if isTruthy(val) {
				t := true
				balance = &t
			} else if isFalsy(val) {
				f := false
				balance = &f
			} else {
				t := true
				balance = &t
			}
			continue
		}

		// Check for --aggregate=... / -aggregate=...
		if strings.HasPrefix(a, "--aggregate=") || strings.HasPrefix(a, "-aggregate=") {
			val := a[strings.IndexByte(a, '=')+1:]
			if isTruthy(val) {
				t := true
				aggregate = &t
			} else if isFalsy(val) {
				f := false
				aggregate = &f
			} else {
				t := true
				aggregate = &t
			}
			continue
		}

		// Check for bare --balance or -balance
		if a == "--balance" || a == "-balance" {
			if i+1 < len(args) {
				next := strings.ToLower(strings.TrimSpace(args[i+1]))
				if next == "true" || next == "on" || next == "yes" || next == "enable" || next == "enabled" {
					t := true
					balance = &t
					i++
					continue
				}
				if next == "false" || next == "off" || next == "no" || next == "disable" || next == "disabled" {
					f := false
					balance = &f
					i++
					continue
				}
			}
			t := true
			balance = &t
			continue
		}

		// Check for bare --aggregate or -aggregate
		if a == "--aggregate" || a == "-aggregate" {
			if i+1 < len(args) {
				next := strings.ToLower(strings.TrimSpace(args[i+1]))
				if next == "true" || next == "on" || next == "yes" || next == "enable" || next == "enabled" {
					t := true
					aggregate = &t
					i++
					continue
				}
				if next == "false" || next == "off" || next == "no" || next == "disable" || next == "disabled" {
					f := false
					aggregate = &f
					i++
					continue
				}
			}
			t := true
			aggregate = &t
			continue
		}

		filtered = append(filtered, a)
	}
	return filtered, balance, aggregate
}

func startProxyServer(configured []models.Model, logf func(string, ...any), balanceFlag, aggregateFlag *bool) (*Server, string, error) {
	cfg := resolveSettings(loadConfig())
	balance := cfg.Balance
	if balanceFlag != nil {
		balance = *balanceFlag
	}
	aggregate := cfg.Aggregate
	if aggregateFlag != nil {
		aggregate = *aggregateFlag
	}
	if aggregate {
		balance = true
	}
	srv, err := New(Config{
		Addr:       cfg.Addr,
		Upstream:   cfg.Upstream,
		Models:     configured,
		CaptureDir: cfg.CaptureDir,
		Logf:       logf,
		Balance:    balance,
		Aggregate:  aggregate,
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
			return exitStatus(exitErr)
		}
		fmt.Fprintf(os.Stderr, "agy proxy: exec %s: %v\n", real, err)
		return 127
	}
	return 0
}

func forwardSignals(cmd *exec.Cmd) func() {
	sigs := make(chan os.Signal, 4)
	notifyProxySignals(sigs)
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
