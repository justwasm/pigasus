package browser

import (
	"flag"
	"io"
	"testing"

	"github.com/justwasm/pigasus/browsercdp"
)

// browserFlagSet builds a flag set that carries the reference's four
// hidden browser flags, parsed from the given command line.
//
// The flag table itself belongs to the parser (internal/cli/parser_flags.go)
// and its own test pins the names, the types and the defaults; this rig
// carries the four flags the browser policy reads.
func browserFlagSet(t *testing.T, args ...string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool(FlagUseLocalChrome, false, "Whether to use local chrome")
	fs.Bool(FlagHeadless, true, "Whether Chrome runs in headless mode.")
	fs.String(FlagUserDataDir, "", "Chrome user data directory.")
	fs.Int(FlagCDPPort, browsercdp.DefaultCDPPort, "Port for Chrome DevTools Protocol")
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return fs
}

// TestApply_Defaults pins the clone's own policy default: the
// browser it drives is the user's, so it shows a window.
//
// The reference's *flag* table is a different thing and is untouched:
// --local_chrome_headless still defaults to "true" there (internal/cli/
// parser_flags.go), and the flag is honoured as soon as it is given — see
// TestApply_HeadlessIsGiven.
func TestApply_Defaults(t *testing.T) {
	browsercdp.Configure(browsercdp.DefaultConfig())
	t.Cleanup(func() { browsercdp.Configure(browsercdp.DefaultConfig()) })

	Apply(browserFlagSet(t))
	cfg := browsercdp.CurrentConfig()
	if cfg.UseLocalChrome {
		t.Errorf("UseLocalChrome = true, want false (the reference's default)")
	}
	if cfg.Headless {
		t.Errorf("Headless = true, want false (the clone's browser shows a window)")
	}
	if cfg.UserDataDir != "" {
		t.Errorf("UserDataDir = %q, want the clone's own profile", cfg.UserDataDir)
	}
	if cfg.Port != browsercdp.DefaultCDPPort {
		t.Errorf("Port = %d, want %d", cfg.Port, browsercdp.DefaultCDPPort)
	}
}

// TestApply_HeadlessIsGiven pins that the reference's flag
// decides once it is given, in both directions.
func TestApply_HeadlessIsGiven(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{args: []string{"--local_chrome_headless=true"}, want: true},
		{args: []string{"--local_chrome_headless=false"}, want: false},
	} {
		browsercdp.Configure(browsercdp.DefaultConfig())
		Apply(browserFlagSet(t, tc.args...))
		if got := browsercdp.CurrentConfig().Headless; got != tc.want {
			t.Errorf("Headless = %v for %v, want %v", got, tc.args, tc.want)
		}
	}
	browsercdp.Configure(browsercdp.DefaultConfig())
}

// TestApply_ReferenceFlags pins that the four flags the
// reference hides reach the browser client with the values they carry.
func TestApply_ReferenceFlags(t *testing.T) {
	browsercdp.Configure(browsercdp.DefaultConfig())
	t.Cleanup(func() { browsercdp.Configure(browsercdp.DefaultConfig()) })

	Apply(browserFlagSet(t,
		"--use_local_chrome",
		"--local_chrome_headless=false",
		"--local_chrome_user_data_dir=/tmp/pigasus-chrome-profile",
		"--cdp_port=9333",
	))
	cfg := browsercdp.CurrentConfig()
	if !cfg.UseLocalChrome {
		t.Errorf("UseLocalChrome = false, want true")
	}
	if cfg.Headless {
		t.Errorf("Headless = true, want false (--local_chrome_headless=false)")
	}
	if cfg.UserDataDir != "/tmp/pigasus-chrome-profile" {
		t.Errorf("UserDataDir = %q, want the flag's value", cfg.UserDataDir)
	}
	if cfg.Port != 9333 {
		t.Errorf("Port = %d, want 9333", cfg.Port)
	}
}

// TestApply_NilFlagSet pins that a session without a flag
// set keeps the default policy instead of failing.
func TestApply_NilFlagSet(t *testing.T) {
	browsercdp.Configure(browsercdp.DefaultConfig())
	t.Cleanup(func() { browsercdp.Configure(browsercdp.DefaultConfig()) })

	Apply(nil)
	if cfg := browsercdp.CurrentConfig(); cfg.Port != browsercdp.DefaultCDPPort || cfg.Headless {
		t.Errorf("the policy changed without a flag set: %+v", cfg)
	}
}
