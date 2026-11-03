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
	"os"

	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// clipboardWrite is clipboard.Write, indirected so tests can exercise the
// /copy handler without touching the real system clipboard.
var clipboardWrite = slashactions.ClipboardWrite

// plannerResponses returns the assistant ("planner") responses in order, which
// is what /copy indexes. The reference's command description calls them
// "planner response[s]".
func (m *runMode) plannerResponses() []string {
	return slashactions.PlannerResponses(m.session)
}

// resolveCopy implements /copy and /copy <n>. It returns the text to
// put on the clipboard ("" when there is nothing) and the line to
// show.
func resolveCopy(args string, responses []string) (string, string) {
	return slashactions.ResolveCopy(args, responses)
}

// conversationTitle is the text /title names. The reference's value is
// unrecovered; a /rename name wins, then the first user message, then the
// program name — 【推测】.
func (m *runMode) conversationTitle() string {
	fallback := ""
	if len(os.Args) > 0 {
		fallback = os.Args[0]
	}
	return slashactions.ConversationTitle(m.conversationName, m.session, fallback)
}

// mcpServersFeedback renders /mcp. Recovered: `No MCP servers configured.` for
// the empty case (the reference's default). The per-server line is 【推测】 —
// the reference opens an interactive panel (`MCP servers`, `MCP server %d of
// %d  (enter to search)`) whose layout cannot be captured.
func mcpServersFeedback(cfg *auth.MCPConfig) string {
	return slashactions.MCPServersFeedback(cfg)
}
