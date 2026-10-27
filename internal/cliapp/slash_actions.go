// slash_actions.go — cycle 103: the TUI slash commands that perform a real
// local action instead of printing a cycle-77 placeholder.
//
// Three commands moved from placeholder text to an actual effect:
//
//	/add-dir <path>  validate the path and record it in settings.json
//	/copy            put the last assistant response on the system clipboard
//	/logout          delete the stored OAuth token
//
// Every user-visible string below is recovered from the 1.3.6 string table
// unless marked 【推测】.
package cliapp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/clipboard"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// clipboardWrite is copyToClipboard, indirected so tests can exercise the
// /copy handler without touching the real system clipboard.
var clipboardWrite = copyToClipboard

// addDirFeedback validates the /add-dir argument and records the directory.
func addDirFeedback(arg string) string {
	return tuilib.AddDirFeedback(arg)
}

// unquotePath strips one layer of matching single or double quotes, which is
// how a path with spaces is typed into the input box.
func unquotePath(s string) string {
	return tuilib.UnquotePath(s)
}

// lastResponseText returns the most recent non-empty assistant message — what
// /copy puts on the clipboard. Recovered: the literal `last response` sits in
// the same string blob as `Copied %s to clipboard.`.
func (m *runMode) lastResponseText() string {
	rs := m.plannerResponses()
	if len(rs) == 0 {
		return ""
	}
	return rs[len(rs)-1]
}

// plannerResponses returns the assistant ("planner") responses in order, which
// is what /copy indexes. The reference's command description calls them
// "planner response[s]".
func (m *runMode) plannerResponses() []string {
	var out []string
	for _, msg := range m.session {
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
			out = append(out, msg.Content)
		}
	}
	return out
}

// resolveCopy implements /copy and /copy <n>. It returns the text to
// put on the clipboard ("" when there is nothing) and the line to
// show.
//
// Recovered: the command description ("Copy the last planner response
// to the clipboard, the n-th with '/copy n', or the /btw answer with
// '/copy btw'"), the success format `Copied %s to clipboard.` with
// `last response` as the filler for the default form and
// `No response to copy.`
//
// Cycle 230: /copy btw is no longer this function's job. The /copy
// switch in slash_commands.go dispatches the btw form to
// runMode.copyBtwCard first, so resolveCopy only sees /copy and
// /copy <n>. The reference also has a "The /btw answer is still being
// generated." branch for an in-flight /btw; cycle 230's btw flow is
// synchronous, so the only states copyBtwCard reports are done or
// failed.
//
// Indexing is 1-based from the most recent response. The filler for
// the n-th form is 【推测】 — only the default form's `last response`
// is recovered — and so is the fallback for a non-numeric argument
// (the default form).
func resolveCopy(args string, responses []string) (string, string) {
	args = trimSpace(args)
	if args == "" {
		if len(responses) == 0 {
			return "", "No response to copy."
		}
		return responses[len(responses)-1], "Copied last response to clipboard."
	}
	n, err := strconv.Atoi(args)
	if err != nil || n < 1 || n > len(responses) {
		if err != nil || n < 1 {
			// Not an index at all: behave like the bare form.
			if len(responses) == 0 {
				return "", "No response to copy."
			}
			return responses[len(responses)-1], "Copied last response to clipboard."
		}
		return "", "No response to copy."
	}
	return responses[len(responses)-n], fmt.Sprintf("Copied response %d to clipboard.", n)
}

// normalizeTitle cleans a session/conversation title so it can be safely shown
// in lists, breadcrumbs and the terminal window title.
func normalizeTitle(s string) string {
	return tuilib.NormalizeTitle(s)
}

// conversationTitle is the text /title names. The reference's value is
// unrecovered; a /rename name wins, then the first user message, then the
// program name — 【推测】.
//
// Cycle "session-title-strip": the stored conversationName is run through
// normalizeTitle here as defence-in-depth, in case it is set by a future code
// path (e.g. /resume hydrating from disk) that skips renameFeedback. The
// renameFeedback path already normalises at write time; that is what keeps
// the breadcrumb and the window title clean in practice.
func (m *runMode) conversationTitle() string {
	if s := normalizeTitle(m.conversationName); s != "" {
		return s
	}
	for _, msg := range m.session {
		if msg.Role == "user" {
			if s := normalizeTitle(msg.Content); s != "" {
				return s
			}
		}
	}
	return filepath.Base(os.Args[0])
}

// renameFeedback implements /rename.
func renameFeedback(arg, current string) (string, string) {
	return tuilib.RenameFeedback(arg, current)
}

// tasksFeedback reports the background-task counters.
func tasksFeedback(running, done int) string {
	return tuilib.TasksFeedback(running, done)
}

// remoteControlFeedback implements /remote-control [on|off].
func remoteControlFeedback(arg string, current bool) (bool, string) {
	return tuilib.RemoteControlFeedback(arg, current)
}

// mcpServersFeedback renders /mcp. Recovered: `No MCP servers configured.` for
// the empty case (the reference's default). The per-server line is 【推测】 —
// the reference opens an interactive panel (`MCP servers`, `MCP server %d of
// %d  (enter to search)`) whose layout cannot be captured.
func mcpServersFeedback(cfg *auth.MCPConfig) string {
	if cfg == nil || len(cfg.Servers) == 0 {
		return tuilib.MCPServersFeedback(false)
	}
	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	mcpsBlocked := auth.MCPsBlocked()
	var b strings.Builder
	b.WriteString(tuilib.MCPServersFeedback(true))
	for _, name := range names {
		s := cfg.Servers[name]
		state := "disabled"
		if s.Enabled {
			state = "enabled"
		}
		if mcpsBlocked {
			state = "blocked"
		}
		target := s.URL
		if target == "" {
			target = strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
		}
		fmt.Fprintf(&b, "\n  %s\t%s\t%s\t%s", name, s.Type, state, target)
	}
	return b.String()
}

// copyToClipboard writes text to the system clipboard. The reference vendors
// github.com/atotto/clipboard — recovered symbols
// `google3/third_party/golang/github_com/atotto/clipboard/v/v0/clipboard.readAll`
// and `…clipboard.getPasteCommand` — which on darwin shells out to
// pbcopy/pbpaste and on Linux to xclip/xsel/wl-copy.
//
// copyToClipboard writes text to the system clipboard via internal/clipboard.
func copyToClipboard(text string) error {
	return clipboard.Write(text)
}

// authServerName is the value interpolated into the recovered
// `Signed out from server: %s`.
func authServerName() string {
	return tuilib.AuthServerName()
}

// logoutFeedback deletes the stored OAuth token and returns the recovered
// confirmation line.
func logoutFeedback() string {
	return tuilib.LogoutFeedback()
}
