package persistent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/rclink"
)

// DefaultOfficialDaemonLabel is the launchd service label used by the official agy binary.
const DefaultOfficialDaemonLabel = "com.google.antigravity-cli-daemon"

// ResolveActiveAccount returns the active Google account's email address.
func ResolveActiveAccount() string {
	_, email := accounts.Current()
	if email != "" {
		return email
	}
	tok, err := auth.LoadTokenUnified()
	if err == nil && tok != nil {
		if em := tok.UserEmail(); em != "" {
			return em
		}
	}
	return ""
}

// OfficialDaemonLabel returns the active launchd service label.
// Under pigasus branding, it adds a "pigasus-" prefix to keep services isolated.
func OfficialDaemonLabel() string {
	if branding.Current().ID == branding.BrandPigasus {
		return "pigasus-" + DefaultOfficialDaemonLabel
	}
	return DefaultOfficialDaemonLabel
}

// ResolveInstanceName resolves the instance name for remote control:
// 1. explicitName if non-empty
// 2. cliRemoteControlHostname from ~/.gemini/config/config.json
// 3. system hostname fallback
// Under pigasus branding, it runtime-prepends "pigasus-" while sharing the same base tail.
func ResolveInstanceName(explicitName string) string {
	var raw string
	if explicitName != "" {
		raw = explicitName
	} else {
		cfgName := readConfigRemoteControlHostname()
		if cfgName != "" {
			raw = cfgName
		} else {
			hn, err := os.Hostname()
			if err == nil && hn != "" {
				raw = hn
			}
		}
	}
	if raw == "" {
		return ""
	}
	if branding.Current().ID == branding.BrandPigasus {
		if !strings.HasPrefix(raw, "pigasus-") {
			return "pigasus-" + raw
		}
	}
	return raw
}

func stripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		return b[3:]
	}
	return b
}

func resolveConfigPath() string {
	home := strings.TrimSpace(os.Getenv("GEMINI_CONFIG_HOME"))
	if home == "" {
		userHome, _ := os.UserHomeDir()
		if userHome == "" {
			return ""
		}
		home = filepath.Join(userHome, ".gemini", "config")
	}
	return filepath.Join(home, "config.json")
}

func readConfigRemoteControlHostname() string {
	configPath := resolveConfigPath()
	if configPath == "" {
		return ""
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	data = stripBOM(data)
	var doc struct {
		UserSettings struct {
			CliRemoteControlHostname string `json:"cliRemoteControlHostname"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return ""
	}
	return doc.UserSettings.CliRemoteControlHostname
}

// ConfiguredInstanceName returns the instance name the configuration
// file carries, without the fallbacks.
//
// ResolveInstanceName falls back to the machine's host name when the
// configuration carries none. A watcher that wants to know whether the
// *user* renamed the instance cannot use that: the host name is
// always there, so the watcher would see a name it did not write and
// push a rename nobody asked for.
//
// This is the raw value: the empty string when the configuration
// carries none.
func ConfiguredInstanceName() string {
	return strings.TrimSpace(readConfigRemoteControlHostname())
}

// SetInstanceName updates userSettings.cliRemoteControlHostname in ~/.gemini/config/config.json.
// Notice: the "pigasus-" prefix is never written to ~/.gemini/config/config.json; it is
// only prepended at runtime by ResolveInstanceName so agy and pigasus share the exact same base name.
func SetInstanceName(name string) error {
	name = strings.TrimPrefix(name, "pigasus-")
	configPath := resolveConfigPath()
	if configPath == "" {
		return fmt.Errorf("cannot resolve gemini config path")
	}
	data, err := os.ReadFile(configPath)
	var raw map[string]interface{}
	if err == nil {
		data = stripBOM(data)
		_ = json.Unmarshal(data, &raw)
	}
	if raw == nil {
		raw = make(map[string]interface{})
	}
	userSettings, ok := raw["userSettings"].(map[string]interface{})
	if !ok {
		userSettings = make(map[string]interface{})
		raw["userSettings"] = userSettings
	}
	userSettings["cliRemoteControlHostname"] = name
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(configPath), 0755)
	return os.WriteFile(configPath, out, 0644)
}

// PlaintextDaemonStatus queries launchd for daemon status lines and prints them
// in parity with official `agy remote-control status`.
func PlaintextDaemonStatus(ctx context.Context, name string) []string {
	label := OfficialDaemonLabel()
	uid := os.Getuid()
	cmd := exec.CommandContext(ctx, "launchctl", "print", fmt.Sprintf("gui/%d/%s", uid, label))
	out, err := cmd.CombinedOutput()
	if err != nil && name != "" {
		// Fallback to openagy-specific label if official daemon isn't running
		label = Label(name)
		cmd = exec.CommandContext(ctx, "launchctl", "print", fmt.Sprintf("gui/%d/%s", uid, label))
		out, err = cmd.CombinedOutput()
	}

	var lines []string
	if err != nil {
		lines = append(lines, "Daemon status: not running")
	} else {
		for _, line := range strings.Split(string(out), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "state = ") || strings.HasPrefix(trimmed, "pid = ") {
				lines = append(lines, "Daemon "+trimmed)
			}
		}
		logPath := OfficialLogPath()
		lines = append(lines, fmt.Sprintf("Logs: %s", logPath))
	}

	account := ResolveActiveAccount()
	if account != "" {
		lines = append(lines, fmt.Sprintf("Account: %s", account))
	} else {
		lines = append(lines, "Account: not signed in")
	}

	instName := ResolveInstanceName(name)
	if instName != "" {
		// The link reaches this instance: the account chooser wrapping
		// the deep link, so the browser picks the account the daemon
		// registered with. See internal/rclink.
		lines = append(lines, fmt.Sprintf("Instance name: %s (find it at %s)", instName, rclink.BrowserURL(InstanceID(), "")))
	} else {
		lines = append(lines, "Instance name: not assigned yet; the daemon generates one on its first start.")
	}
	if runtime.GOOS != "darwin" {
		// There is no launchd here, so there is no registered daemon to
		// ask: the daemon runs in the foreground, in the terminal that
		// started it.
		lines = append(lines, "Daemon mode: foreground (this platform has no launchd; `remote-control run` runs the daemon in the foreground)")
	}
	return lines
}

// OfficialPlistPath returns the launchd plist path for the official daemon.
func OfficialPlistPath() string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, "Library", "LaunchAgents", OfficialDaemonLabel()+".plist")
}

// OfficialBinaryPath resolves the binary to invoke for remote-control serve.
func OfficialBinaryPath() string {
	if branding.Current().ID == branding.BrandPigasus {
		if p, err := exec.LookPath("pigasus"); err == nil {
			return p
		}
		if _, err := os.Stat("/tmp/pigasus"); err == nil {
			return "/tmp/pigasus"
		}
		bin, err := os.Executable()
		if err == nil && bin != "" && !strings.HasSuffix(bin, ".test") {
			return bin
		}
		return os.Args[0]
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	localAgy := filepath.Join(home, ".local", "bin", "agy")
	if _, err := os.Stat(localAgy); err == nil {
		return localAgy
	}
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	bin, err := os.Executable()
	if err == nil && bin != "" {
		return bin
	}
	return os.Args[0]
}

// OfficialLogPath returns the canonical daemon log path.
func OfficialLogPath() string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	logFile := "antigravity-cli-daemon.log"
	if branding.Current().ID == branding.BrandPigasus {
		logFile = "pigasus-" + logFile
	}
	return filepath.Join(home, "Library", "Logs", logFile)
}

// BuildOfficialPlist renders the LaunchAgent plist matching official agy.
//
// desktop appends the `--desktop` switch to the daemon's own
// command line, so the persistent daemon's Mendel read carries the
// hub identity and the dashboard's page is the reference's desktop
// UI. Without it the plist is byte-identical to the reference's
// shape.
func BuildOfficialPlist(binary string, session bool, desktop bool) string {
	logPath := OfficialLogPath()
	label := OfficialDaemonLabel()
	desktopArg := ""
	if desktop {
		desktopArg = " '--desktop'"
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
`)
	if !session {
		b.WriteString(`    <key>KeepAlive</key>
    <dict>
      <key>SuccessfulExit</key>
      <false/>
    </dict>
`)
	}
	fmt.Fprintf(&b, `    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
      <string>/bin/sh</string>
      <string>-c</string>
      <string>exec '%s' 'remote-control' 'serve'%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>StandardOutPath</key>
    <string>%s</string>
  </dict>
</plist>`, label, binary, desktopArg, logPath, logPath)
	return b.String()
}

// StartOfficialDaemon writes and bootstraps the official LaunchAgent daemon.
func StartOfficialDaemon(ctx context.Context, name string, session bool, desktop bool) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("%w (running on %s/%s); run `pigasus remote-control run` to start the daemon in the foreground",
			ErrUnsupported, runtime.GOOS, runtime.GOARCH)
	}
	if name != "" {
		_ = SetInstanceName(name)
	}
	plistPath := OfficialPlistPath()
	bin := OfficialBinaryPath()
	content := BuildOfficialPlist(bin, session, desktop)
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, []byte(content), 0644); err != nil {
		return err
	}
	uid := os.Getuid()
	label := OfficialDaemonLabel()
	cmd := exec.CommandContext(ctx, "launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plistPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		// If already registered, bootout and re-bootstrap so modified arguments (like --desktop) take effect.
		_ = exec.CommandContext(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, label)).Run()
		reboot := exec.CommandContext(ctx, "launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plistPath)
		if rOut, rErr := reboot.CombinedOutput(); rErr != nil {
			kick := exec.CommandContext(ctx, "launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", uid, label))
			if kOut, kErr := kick.CombinedOutput(); kErr != nil {
				return fmt.Errorf("launchctl bootstrap failed: %s (%w); bootout+bootstrap failed: %s (%v); kickstart failed: %s (%v)", strings.TrimSpace(string(out)), err, strings.TrimSpace(string(rOut)), rErr, strings.TrimSpace(string(kOut)), kErr)
			}
		}
	}
	return nil
}

// StopOfficialDaemon unregisters and deletes the LaunchAgent daemon.
func StopOfficialDaemon(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	uid := os.Getuid()
	label := OfficialDaemonLabel()
	_ = exec.CommandContext(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, label)).Run()
	_ = os.Remove(OfficialPlistPath())
	dir := DefaultLaunchAgentDir()
	if dir != "" {
		matches, _ := filepath.Glob(filepath.Join(dir, "com.openagy.remote-control-*.plist"))
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}
	return nil
}
