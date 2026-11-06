package agentserver

import (
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"strings"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/features/agents"
)

// rawJSONOrQuoted converts a string into a valid jsontext.Value.
// If s is empty, it returns nil.
// If s is already valid JSON, it wraps s directly.
// Otherwise, it marshals s as a JSON string so that encoding never emits
// invalid JSON tokens or unescaped newlines onto the JSON-RPC wire.
func rawJSONOrQuoted(s string) jsontext.Value {
	if s == "" {
		return nil
	}
	b := []byte(s)
	if json.Valid(b) {
		return jsontext.Value(b)
	}
	m, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return jsontext.Value(m)
}

// formatAgentsSummary formats the available custom agents and running subagents into a readable Markdown report.
func formatAgentsSummary(workspace string) string {
	var sb strings.Builder

	customAgents := agents.DiscoverWith(agents.DiscoverOptions{Workspace: workspace})
	if len(customAgents) == 0 {
		sb.WriteString("### Custom Agents\n\nNo custom agents currently configured.\n")
	} else {
		sb.WriteString(fmt.Sprintf("### Custom Agents (%d available)\n\n", len(customAgents)))
		sb.WriteString("| Name | Description | Path |\n")
		sb.WriteString("| :--- | :--- | :--- |\n")
		for _, a := range customAgents {
			desc := strings.ReplaceAll(a.Description, "\n", " ")
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %s | `%s` |\n", a.Name, desc, a.Path))
		}
	}

	sb.WriteString("\n")

	var subagents []*subagent.Subagent
	if mgr := tools.SharedSubagentManager(); mgr != nil {
		subagents = mgr.List()
	}

	if len(subagents) == 0 {
		sb.WriteString("### Subagents\n\nNo subagents currently active.\n")
	} else {
		sb.WriteString(fmt.Sprintf("### Subagents (%d active)\n\n", len(subagents)))
		sb.WriteString("| ID | Name | Status | Title |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- |\n")
		for _, sa := range subagents {
			title := strings.ReplaceAll(sa.Title, "\n", " ")
			if len(title) > 50 {
				title = title[:47] + "..."
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n", sa.ID, sa.Name, sa.StatusLine(), title))
		}
	}

	return sb.String()
}
