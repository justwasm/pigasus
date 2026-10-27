package cliapp

import (
	"flag"

	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/cliflags"
)

// applyBrowserFlags hands the browser policy to the browser client.
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
func applyBrowserFlags(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	cfg := browsercdp.CurrentConfig()

	// The flags that were given, as opposed to the ones that only carry
	// their default.
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })

	if fs.Lookup("use_local_chrome") != nil {
		cfg.UseLocalChrome = cliflags.BoolValue(fs, "use_local_chrome")
	}
	if fs.Lookup("local_chrome_headless") != nil && given["local_chrome_headless"] {
		cfg.Headless = cliflags.BoolValue(fs, "local_chrome_headless")
	}
	if fs.Lookup("local_chrome_user_data_dir") != nil {
		cfg.UserDataDir = cliflags.StringValue(fs, "local_chrome_user_data_dir")
	}
	if fs.Lookup("cdp_port") != nil {
		if port := cliflags.IntValue(fs, "cdp_port"); port > 0 {
			cfg.Port = port
		}
	}
	browsercdp.Configure(cfg)
}
