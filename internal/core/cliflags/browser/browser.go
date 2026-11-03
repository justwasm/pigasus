// Package browser owns the CLI's four hidden browser flags and the browser
// policy they decide.
//
// The reference CLI hides four flags for the browser 【一手 + 反编译 —
// out/local-macos/FINDINGS.md §2, items 4-7: the "local-Chrome CDP
// control" cluster】:
//
//	--cdp_port=9222                  Port for Chrome DevTools Protocol
//	--local_chrome_headless=true      Whether Chrome runs in headless mode.
//	--local_chrome_user_data_dir=""   Chrome user data directory.
//	--use_local_chrome=false          Whether to use local chrome
//
// The clone's browser client reads the same four values
// (browsercdp.Config), so the flags decide whether the browser
// runs with a window, which profile it uses, which port it is on, and
// whether the Chrome that is already running on the machine is the one
// that gets driven.
//
// One of the four is read differently from the others: the reference's
// flag default for --local_chrome_headless is "true" (headless), but the
// clone's own browser is the user's browser — a window is what makes it
// usable — so the flag is honoured when it is *given*, and the clone's
// visible default stands when it is not.
//
// A flag set without them (the remote-control daemon, the harness) keeps
// the default policy: the clone drives the user's own Chrome when there
// is one, and starts a visible one when there is not.
package browser

import (
	"flag"

	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// The reference's four flag names.
const (
	// FlagCDPPort names the Chrome DevTools Protocol port.
	FlagCDPPort = "cdp_port"
	// FlagHeadless names the flag that decides whether Chrome runs
	// without a window.
	FlagHeadless = "local_chrome_headless"
	// FlagUserDataDir names the Chrome profile the started Chrome
	// uses.
	FlagUserDataDir = "local_chrome_user_data_dir"
	// FlagUseLocalChrome names the flag that decides whether the
	// Chrome that is already running on the machine is driven.
	FlagUseLocalChrome = "use_local_chrome"
)

// Apply hands the browser policy to the browser client.
//
// The flags are read from the set the parser registered, so a session
// without them keeps the default policy.
func Apply(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	cfg := browsercdp.CurrentConfig()

	// The flags that were given, as opposed to the ones that only carry
	// their default.
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })

	if fs.Lookup(FlagUseLocalChrome) != nil {
		cfg.UseLocalChrome = cliflags.BoolValue(fs, FlagUseLocalChrome)
	}
	if fs.Lookup(FlagHeadless) != nil && given[FlagHeadless] {
		cfg.Headless = cliflags.BoolValue(fs, FlagHeadless)
	}
	if fs.Lookup(FlagUserDataDir) != nil {
		cfg.UserDataDir = cliflags.StringValue(fs, FlagUserDataDir)
	}
	if fs.Lookup(FlagCDPPort) != nil {
		if port := cliflags.IntValue(fs, FlagCDPPort); port > 0 {
			cfg.Port = port
		}
	}
	browsercdp.Configure(cfg)
}
