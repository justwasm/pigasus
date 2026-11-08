// acp_commands.go — the slash commands one ACP session advertises.
//
// The reference (AGY 1.5.2) shows the commands and skills of the agent a
// session actually runs under, not the built-in agent's list. The ACP
// server's configuration carries that agent (Config.AgentName /
// Config.AgentPath), so the command list is built once per session:
//
//   - the built-in command catalogue (model/clear/plan/…),
//   - the active custom agent's name,
//   - every skill the agent lists in its `skills:` frontmatter.
//
// Sessions that keep the built-in agent get exactly the old catalogue.

package agentserver

import (
	"sort"
	"strings"

	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	"github.com/justwasm/pigasus/internal/app/prompt/customagent"
)

// acpSkill is one skill advertised in a session's command list.
type acpSkill struct {
	Name        string
	Description string
}

// agentSkills returns the skills of the server's active custom agent,
// sorted by name. A built-in agent, an unreadable agent file, or an
// agent without `skills:` returns nil.
func agentSkills(cfg Config) []acpSkill {
	path := strings.TrimSpace(cfg.AgentPath)
	if path == "" {
		return nil
	}
	agent, err := customagent.LoadAgent(path)
	if err != nil {
		return nil
	}
	entries := agent.SkillEntries()
	out := make([]acpSkill, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimPrefix(strings.TrimSpace(entry.Name), "/")
		if name == "" {
			continue
		}
		out = append(out, acpSkill{Name: name, Description: strings.TrimSpace(entry.Description)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// v1CommandsFor builds the v1 session's command list from the server
// configuration.
func v1CommandsFor(cfg Config) []acp1.AvailableCommand {
	cmds := defaultV1Commands()
	if name := strings.TrimSpace(cfg.AgentName); name != "" {
		cmds = append(cmds, acp1.AvailableCommand{
			Name:        "/agent",
			Description: "Active custom agent: " + name,
		})
	}
	for _, skill := range agentSkills(cfg) {
		cmds = append(cmds, acp1.AvailableCommand{
			Name:        "/" + skill.Name,
			Description: skillDescription(skill),
		})
	}
	return cmds
}

// v2CommandsFor builds the v2 session's command list from the server
// configuration.
func v2CommandsFor(cfg Config) []acp2.AvailableCommand {
	cmds := defaultV2Commands()
	if name := strings.TrimSpace(cfg.AgentName); name != "" {
		cmds = append(cmds, acp2.AvailableCommand{
			Name:        "/agent",
			Description: "Active custom agent: " + name,
		})
	}
	for _, skill := range agentSkills(cfg) {
		cmds = append(cmds, acp2.AvailableCommand{
			Name:        "/" + skill.Name,
			Description: skillDescription(skill),
		})
	}
	return cmds
}

func skillDescription(skill acpSkill) string {
	if skill.Description != "" {
		return skill.Description
	}
	return "Skill from the active agent"
}

// sessionCommandsV1 returns the commands a v1 session advertises. A
// session built before commands were attached (a test fixture, or a
// resumed session) falls back to the built-in catalogue.
func sessionCommandsV1(s *acpV1Session) []acp1.AvailableCommand {
	if s == nil || len(s.commands) == 0 {
		return defaultV1Commands()
	}
	return s.commands
}

// commandsV2Of returns the commands a v2 session advertises, falling
// back to the built-in catalogue when the session store reports none.
func commandsV2Of(cmds []acp2.AvailableCommand) []acp2.AvailableCommand {
	if len(cmds) == 0 {
		return defaultV2Commands()
	}
	return cmds
}
