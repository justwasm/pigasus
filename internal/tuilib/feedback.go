// feedback.go — feedback formatters and validators for TUI slash commands.
package tuilib

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/justwasm/pigasus/internal/auth"
)

// trimSpace trims leading and trailing ASCII space and tab characters.
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// UnquotePath strips one layer of matching single or double quotes, which is
// how a path with spaces is typed into the input box.
func UnquotePath(s string) string {
	s = trimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// AddDirFeedback validates the /add-dir argument and records the directory.
//
// Recovered strings:
//
//	error: no directory specified         (empty argument)
//	error: directory not found: <path>    (stat fails)
//	error: not a directory: <path>        (regular file)
//	Added %s to workspace                 (success)
func AddDirFeedback(arg string) string {
	dir := UnquotePath(arg)
	if dir == "" {
		return "error: no directory specified"
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "error: directory not found: " + dir
	}
	if !info.IsDir() {
		return "error: not a directory: " + dir
	}
	if err := auth.AddTrustedWorkspace(dir); err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("Added %s to workspace", dir)
}

// StatuslineFeedback toggles the status line and returns (persisted value,
// message). Recovered: the off-state message includes a re-enable instruction
// ("Statusline off. Run /statusline to re-enable."); the on-state message is
// just "Statusline on." (/tmp/agycmp/statusline_probe.py).
func StatuslineFeedback(current string) (string, string) {
	if current == "off" {
		return "on", "Statusline on."
	}
	return "off", "Statusline off. Run /statusline to re-enable."
}

// TitleFeedback handles /title, /title on and /title off and returns
// (persisted value, message). Recovered: `Usage: /title [on|off]`,
// `Custom title enabled. (%s)` and `Custom title disabled.`
func TitleFeedback(arg, current, titleText string) (string, string) {
	switch strings.ToLower(trimSpace(arg)) {
	case "":
		if current == "on" {
			return "off", "Custom title disabled."
		}
		return "on", fmt.Sprintf("Custom title enabled. (%s)", titleText)
	case "on":
		return "on", fmt.Sprintf("Custom title enabled. (%s)", titleText)
	case "off":
		return "off", "Custom title disabled."
	}
	return current, "Usage: /title [on|off]"
}

// NormalizeTitle cleans a session/conversation title so it can be safely shown
// in lists, breadcrumbs and the terminal window title.
func NormalizeTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	inWS := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !inWS {
				b.WriteByte(' ')
				inWS = true
			}
			continue
		}
		b.WriteRune(r)
		inWS = false
	}
	return b.String()
}

// RenameFeedback implements /rename. Recovered: `Error: Please provide a new
// name for the conversation.` and `Conversation renamed to: %s`. It returns the
// new name (unchanged on error) and the line to show.
func RenameFeedback(arg, current string) (string, string) {
	name := NormalizeTitle(arg)
	if name == "" {
		return current, "Error: Please provide a new name for the conversation."
	}
	return name, fmt.Sprintf("Conversation renamed to: %s", name)
}

// TasksFeedback reports the background-task counters. Recovered: `No background
// tasks.` for the empty case and `%d running, %d done` for a non-empty one.
func TasksFeedback(running, done int) string {
	if running == 0 && done == 0 {
		return "No background tasks."
	}
	return fmt.Sprintf("%d running, %d done", running, done)
}

// RemoteControlFeedback implements /remote-control [on|off]. Recovered:
// `Usage: /remote-control [on|off]`, `Remote control on for this session.` and
// `Remote control off. This machine is no longer reachable from other devices.`
func RemoteControlFeedback(arg string, current bool) (bool, string) {
	switch strings.ToLower(trimSpace(arg)) {
	case "", "on":
		return true, "Remote control on for this session."
	case "off":
		return false, "Remote control off. This machine is no longer reachable from other devices."
	}
	return current, "Usage: /remote-control [on|off]"
}

// MCPServersFeedback renders /mcp feedback header or empty state.
func MCPServersFeedback(hasServers bool) string {
	if !hasServers {
		return "No MCP servers configured."
	}
	return "MCP servers"
}

// AuthServerName is the value interpolated into the recovered
// `Signed out from server: %s`.
func AuthServerName() string {
	u, err := url.Parse(auth.CloudCodeEndpoint)
	if err != nil || u.Host == "" {
		return auth.CloudCodeEndpoint
	}
	return u.Host
}

// LogoutFeedback deletes the stored OAuth token and returns the recovered
// confirmation line. A missing token is not an error — the reference's logout
// is idempotent.
func LogoutFeedback() string {
	if os.Getenv("GEMINI_API_KEY") != "" {
		return "The active session uses a Gemini API key from the environment (GEMINI_API_KEY). To change it, update or unset the environment variable."
	}
	if os.Getenv("AGY_ADC_AUTH") != "" {
		return "The active session uses Application Default Credentials (AGY_ADC_AUTH). To change accounts, run 'gcloud auth application-default login' or unset the variable."
	}
	_, _ = auth.ClearTokenUnified()
	if err := os.Remove(auth.TokenFilePath()); err != nil && !os.IsNotExist(err) {
		return "error: logout failed: " + err.Error()
	}
	return fmt.Sprintf("Signed out from server: %s", AuthServerName())
}
