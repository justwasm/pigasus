package browsercdp

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// DefaultCDPPort is the Chrome DevTools Protocol port the reference names
// for its local Chrome.
//
// 【一手 — out/langserver/flags.txt: "-cdp_port=9222: Port for Chrome
// DevTools Protocol"; the same default in the CLI's own hidden flags
// (out/local-macos/FINDINGS.md §2, item 4)】
const DefaultCDPPort = 9222

// Config is the browser policy, in the reference's own flag shape.
//
// The reference CLI hides four flags for the browser 【一手 + 反编译 —
// out/local-macos/FINDINGS.md §2, items 4-7: the four "local-Chrome CDP
// control" flags】, and their usage strings are in the binary as well
// ("Port for Chrome DevTools Protocol", "Chrome user data directory.",
// "Whether Chrome runs in headless mode.", "Whether to use local chrome"
// — the 1.3.4 strings dump 【strings】):
//
//	--cdp_port=9222                  Port for Chrome DevTools Protocol
//	--local_chrome_headless=true      Whether Chrome runs in headless mode.
//	--local_chrome_user_data_dir=""   Chrome user data directory.
//	--use_local_chrome=false          Whether to use local chrome
//
// The clone takes the same four values.
type Config struct {
	// UseLocalChrome drives the Chrome that is already running on the
	// machine instead of starting one.
	UseLocalChrome bool

	// Headless starts the Chrome without a window.
	//
	// The reference's *flag* default is headless — its hidden
	// --local_chrome_headless defaults to true 【一手 — the 1.4.7
	// flag table: "local_chrome_headless", default "true"】 — and its
	// own chrome-devtools-mcp skill documents the visible mode as the
	// opt-in
	// ("chrome-devtools start --headless=false # Start with visible
	// browser window" — out/ide-language-server/skills/chrome-devtools-cli/SKILL.md
	// 【一手】).
	//
	// The clone's *policy* default is the other way round: the browser
	// the clone drives is the user's own browser (see startOrConnectBrowser),
	// and a browser that works without a window on the screen cannot be
	// watched, cannot be logged into and cannot be taken over. The flag
	// still decides, as soon as it is given.
	Headless bool

	// UserDataDir is the profile the started Chrome uses. Empty means
	// the clone's own persistent profile.
	UserDataDir string

	// Port is the Chrome DevTools Protocol port of the local Chrome.
	Port int
}

// DefaultConfig is the policy the clone uses when no flag says otherwise.
//
// It is the reference's shape — the same four values, the same port —
// with one deliberate difference: the browser shows a window. The clone
// attaches to the user's own Chrome whenever there is one (the session
// is then the user's, and nothing has to be logged in again), and the
// Chrome it has to start itself is a visible one.
//
// The reference's flag table is untouched: --local_chrome_headless
// still defaults to "true" there, and the clone honours the flag when
// it is given (internal/cliapp/browser_flags.go).
func DefaultConfig() Config {
	return Config{
		UseLocalChrome: false,
		Headless:       false,
		UserDataDir:    "",
		Port:           DefaultCDPPort,
	}
}

var (
	configMu sync.RWMutex
	config   = DefaultConfig()
)

// Configure sets the browser policy. The command calls it once, with the
// values of the reference's own four flags.
func Configure(c Config) {
	if c.Port <= 0 {
		c.Port = DefaultCDPPort
	}
	configMu.Lock()
	defer configMu.Unlock()
	config = c
}

// CurrentConfig returns the browser policy in force.
func CurrentConfig() Config {
	configMu.RLock()
	defer configMu.RUnlock()
	return config
}

// envDebugPort is the port CHROME_REMOTE_DEBUGGING_PORT names.
//
// The variable is the clone's own override — the reference carries the
// port on its `--cdp_port` flag — and it means "attach to a running
// Chrome", which is what its name says.
func envDebugPort() string {
	return strings.TrimSpace(os.Getenv("CHROME_REMOTE_DEBUGGING_PORT"))
}

// attachRequested reports whether the clone should drive a Chrome that
// is already running instead of starting one.
func attachRequested(cfg Config) bool {
	return cfg.UseLocalChrome || envDebugPort() != ""
}

// localChromePorts lists the ports a local Chrome could be on.
//
// The reference discovers the browser the same way: its language server
// exposes a CheckDevToolsActivePort method, and the CLI logs
// "[CDP Discovery] Failed: %v" 【strings — the 1.3.4 strings dump】.
// Chrome writes the port it chose into the DevToolsActivePort file of
// its user-data dir, so the file is where the port is found.
//
// The configured port comes last: it is the fallback for a Chrome that
// was started with a debugging port but no profile we can find.
func localChromePorts(cfg Config) []string {
	var ports []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		ports = append(ports, p)
	}
	if p := discoveredLocalChromePort(); p != "" {
		add(p)
	}
	add(envDebugPort())
	add(strconv.Itoa(cfg.Port))
	return ports
}

// discoveredLocalChromePort reads the port from the DevToolsActivePort
// file of the standard Chrome profiles.
func discoveredLocalChromePort() string {
	for _, dir := range chromeProfileDirs() {
		b, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
		if err != nil {
			continue
		}
		fields := strings.Fields(string(b))
		if len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

// chromeProfileDirs lists the user-data dirs of the standard Chrome
// installations.
func chromeProfileDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{
			filepath.Join(home, "Library", "Application Support", "Google", "Chrome"),
			filepath.Join(home, "Library", "Application Support", "Chromium"),
		}
	case "linux":
		return []string{
			filepath.Join(home, ".config", "google-chrome"),
			filepath.Join(home, ".config", "chromium"),
		}
	}
	return nil
}
