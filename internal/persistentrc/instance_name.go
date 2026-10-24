package persistentrc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultOfficialDaemonLabel is the launchd service label used by the official agy binary.
const DefaultOfficialDaemonLabel = "com.google.antigravity-cli-daemon"

// ResolveInstanceName resolves the instance name for remote control:
// 1. explicitName if non-empty
// 2. cliRemoteControlHostname from ~/.gemini/config/config.json
// 3. system hostname fallback
func ResolveInstanceName(explicitName string) string {
	if explicitName != "" {
		return explicitName
	}
	cfgName := readConfigRemoteControlHostname()
	if cfgName != "" {
		return cfgName
	}
	hn, err := os.Hostname()
	if err == nil && hn != "" {
		return hn
	}
	return ""
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

// SetInstanceName updates userSettings.cliRemoteControlHostname in ~/.gemini/config/config.json.
func SetInstanceName(name string) error {
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
	label := DefaultOfficialDaemonLabel
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

	instName := ResolveInstanceName(name)
	if instName != "" {
		lines = append(lines, fmt.Sprintf("Instance name: %s (find it at https://antigravity.google.com)", instName))
	} else {
		lines = append(lines, "Instance name: not assigned yet; the daemon generates one on its first start.")
	}
	return lines
}

// OfficialPlistPath returns the launchd plist path for the official daemon.
func OfficialPlistPath() string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, "Library", "LaunchAgents", DefaultOfficialDaemonLabel+".plist")
}

// OfficialBinaryPath resolves the binary to invoke for remote-control serve.
func OfficialBinaryPath() string {
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
	return filepath.Join(home, "Library", "Logs", "antigravity-cli-daemon.log")
}

// BuildOfficialPlist renders the LaunchAgent plist matching official agy.
func BuildOfficialPlist(binary string, session bool) string {
	logPath := OfficialLogPath()
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
      <string>exec '%s' 'remote-control' 'serve'</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>StandardOutPath</key>
    <string>%s</string>
  </dict>
</plist>`, DefaultOfficialDaemonLabel, binary, logPath, logPath)
	return b.String()
}

// StartOfficialDaemon writes and bootstraps the official LaunchAgent daemon.
func StartOfficialDaemon(ctx context.Context, name string, session bool) error {
	if name != "" {
		_ = SetInstanceName(name)
	}
	plistPath := OfficialPlistPath()
	bin := OfficialBinaryPath()
	content := BuildOfficialPlist(bin, session)
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, []byte(content), 0644); err != nil {
		return err
	}
	uid := os.Getuid()
	cmd := exec.CommandContext(ctx, "launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plistPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		// If already registered, kickstart -k restarts it cleanly.
		kick := exec.CommandContext(ctx, "launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", uid, DefaultOfficialDaemonLabel))
		if kOut, kErr := kick.CombinedOutput(); kErr != nil {
			return fmt.Errorf("launchctl bootstrap failed: %s (%w); kickstart failed: %s (%v)", strings.TrimSpace(string(out)), err, strings.TrimSpace(string(kOut)), kErr)
		}
	}
	return nil
}

// StopOfficialDaemon unregisters and deletes the LaunchAgent daemon.
func StopOfficialDaemon(ctx context.Context) error {
	uid := os.Getuid()
	_ = exec.CommandContext(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, DefaultOfficialDaemonLabel)).Run()
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
