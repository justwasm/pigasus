package cliapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
)

// runProxyCLI executes one invocation through the clone's parser — the one
// with the hidden `proxy` group appended, exactly as setupCLIParser builds
// it — and returns (stdout, stderr, exitCode).
//
// The parser is built here rather than through setupCLIParser so the test
// does not install the production bridges (the TUI, the daemon) as global
// state.
func runProxyCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	p := cli.NewParser(cli.StandardFlags(),
		append(cli.StandardSubcommands(), proxySubcommand()),
		"pigasus", "recovered", "recovered")
	p.Root().RootRun = func(args []string) error { return runRoot(args, p) }
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	_ = p.ExecuteArgs(args)
	return out.String(), errb.String(), p.ExitCode()
}

// TestProxySubcommandIsVisible pins the superset rule: `pigasus --help` lists
// the reference subcommands plus openagy's superset, including proxy.
func TestProxySubcommandIsVisible(t *testing.T) {
	specs := append(cli.StandardSubcommands(), proxySubcommand())
	visible := 0
	found := false
	for _, s := range specs {
		if s.Name == "proxy" {
			found = true
			if s.Hidden {
				t.Fatal("the proxy subcommand must be visible")
			}
		}
		if !s.Hidden {
			visible++
		}
	}
	if !found {
		t.Fatal("the proxy subcommand is missing")
	}
	if visible != 17 {
		t.Fatalf("visible subcommands = %d, want 17 (12 ref + 5 superset)", visible)
	}
}

// TestProxyCaptureRoundTrip drives `pigasus proxy capture` through the
// parser and checks that the command's output lands on the parser's
// stream, not on os.Stdout.
func TestProxyCaptureRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAGY_WRAPPER_CONFIG", filepath.Join(dir, "wrapper.json"))
	t.Setenv("OPENAGY_CAPTURE_DIR", "")
	t.Setenv("HOME", dir) // keep the default capture dir inside the sandbox

	out, _, code := runProxyCLI(t, "proxy", "capture", "off")
	if code != 0 {
		t.Fatalf("proxy capture off = %d, want 0", code)
	}
	if !strings.Contains(out, "capture: off") {
		t.Fatalf("proxy capture off wrote %q, want the capture line", out)
	}

	out, _, code = runProxyCLI(t, "proxy", "capture", "on")
	if code != 0 {
		t.Fatalf("proxy capture on = %d, want 0", code)
	}
	if !strings.Contains(out, "capture: on") {
		t.Fatalf("proxy capture on wrote %q, want the capture line", out)
	}
}

// TestProxyUsageOnParentCommand checks the parent command's behaviour: no
// subcommand prints the usage on stderr and exits 1, like every other
// parent command in the clone.
func TestProxyUsageOnParentCommand(t *testing.T) {
	_, errOut, code := runProxyCLI(t, "proxy")
	if code != 1 {
		t.Fatalf("bare proxy = %d, want 1", code)
	}
	if !strings.Contains(errOut, "Usage: pigasus proxy") {
		t.Fatalf("bare proxy wrote %q, want the usage block", errOut)
	}
}

// TestProxyUnknownSubCommand checks the rejection path.
func TestProxyUnknownSubCommand(t *testing.T) {
	_, errOut, code := runProxyCLI(t, "proxy", "nope")
	if code != 1 {
		t.Fatalf("proxy nope = %d, want 1", code)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Fatalf("proxy nope wrote %q, want the unknown-command line", errOut)
	}
}

// TestRegistryStaysWithTheClone pins the flattening: the provider registry
// is the clone's own command family, not a proxy subcommand. `pigasus
// providers` (the alias) and `pigasus provider list` are the same handler.
func TestRegistryStaysWithTheClone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // providers.json lands in the sandbox
	t.Setenv("HOME", dir)

	aliasOut, _, code := runProxyCLI(t, "providers", "list")
	if code != 0 {
		t.Fatalf("providers list = %d, want 0", code)
	}
	cloneOut, _, code := runProxyCLI(t, "provider", "list")
	if code != 0 {
		t.Fatalf("provider list = %d, want 0", code)
	}
	if aliasOut != cloneOut {
		t.Fatalf("providers wrote %q, provider list wrote %q", aliasOut, cloneOut)
	}
	if !strings.Contains(cloneOut, "providers:") {
		t.Fatalf("provider list wrote %q, want the registry path line", cloneOut)
	}
}

// TestProxyLaunchIsNotReachedByTheParser documents the split between the
// two entry points: `pigasus agy` is dispatched in main() before the parser
// exists (the vendor binary must see its command line untouched), while
// `pigasus proxy …` is an ordinary subcommand.
func TestProxyLaunchIsNotReachedByTheParser(t *testing.T) {
	if os.Args[0] == "" {
		t.Fatal("os.Args[0] is empty")
	}
	out, errOut, code := runProxyCLI(t, "agy", "--help")
	if code == 0 {
		t.Fatalf("the parser must not know the agy subcommand (code %d, out %q, err %q)", code, out, errOut)
	}
}
