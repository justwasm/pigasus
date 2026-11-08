// remote_triggers.go — /triggers in a `--remote-agent` session.
//
// The trigger RPCs are the IDE/web automation surface. The CLI has no
// reference slash command for them, so the host exposes a hidden
// /triggers command: list, add, pause/resume and delete.
package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

// slashTriggers manages the remote environment's triggers.
func (m *RootModel) slashTriggers(args string) (bool, tea.Cmd) {
	engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine)
	if !ok || !m.session.remoteAgent {
		m.slashRespond("/triggers", "Remote triggers are only available in a --remote-agent session.")
		return true, nil
	}
	trimmed := slashparse.Trim(args)
	verb, rest := slashparse.SplitVerb(trimmed)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch {
	case trimmed == "" || strings.EqualFold(verb, "list"):
		triggers, err := engine.Triggers(ctx)
		if err != nil {
			m.slashRespond("/triggers", "error: "+err.Error())
			return true, nil
		}
		if len(triggers) == 0 {
			m.slashRespond("/triggers", "No remote triggers.")
			return true, nil
		}
		lines := []string{"Remote triggers:"}
		for _, trigger := range triggers {
			line := "  " + trigger.TriggerID + "  " + trigger.State.String()
			if trigger.DisplayName != "" {
				line += "  " + trigger.DisplayName
			}
			if trigger.Summary != "" {
				line += " — " + trigger.Summary
			} else if trigger.Prompt != "" {
				line += " — " + trigger.Prompt
			}
			lines = append(lines, line)
		}
		m.slashContent(strings.Join(lines, "\n"))
		return true, nil
	case strings.EqualFold(verb, "add"):
		prompt := slashparse.Trim(rest)
		if prompt == "" {
			m.slashRespond("/triggers", "Usage: /triggers add <prompt>")
			return true, nil
		}
		optionID := "time"
		if options, err := engine.TriggerOptions(ctx); err == nil && len(options) > 0 && options[0].ID != "" {
			optionID = options[0].ID
		}
		name := firstLine(prompt)
		if len(name) > 60 {
			name = name[:60]
		}
		created, err := engine.CreateTrigger(ctx, remoteagentclient.RemoteTrigger{
			DisplayName: name,
			Prompt:      prompt,
			OptionID:    optionID,
			Summary:     prompt,
		})
		if err != nil {
			m.slashRespond("/triggers", "error: "+err.Error())
			return true, nil
		}
		m.slashRespond("/triggers", "Created trigger "+created.TriggerID+".")
		return true, nil
	case strings.EqualFold(verb, "pause"), strings.EqualFold(verb, "resume"):
		id := slashparse.Trim(rest)
		if id == "" {
			m.slashRespond("/triggers", "Usage: /triggers "+verb+" <trigger-id>")
			return true, nil
		}
		state := remoteagentclient.TriggerStatePaused
		if strings.EqualFold(verb, "resume") {
			state = remoteagentclient.TriggerStateActive
		}
		trigger, err := findRemoteTrigger(ctx, engine, id)
		if err != nil {
			m.slashRespond("/triggers", "error: "+err.Error())
			return true, nil
		}
		trigger.State = state
		updated, err := engine.UpdateTrigger(ctx, trigger, []string{"state"})
		if err != nil {
			m.slashRespond("/triggers", "error: "+err.Error())
			return true, nil
		}
		m.slashRespond("/triggers", "Trigger "+updated.TriggerID+" is now "+updated.State.String()+".")
		return true, nil
	case strings.EqualFold(verb, "delete"):
		id := slashparse.Trim(rest)
		if id == "" {
			m.slashRespond("/triggers", "Usage: /triggers delete <trigger-id>")
			return true, nil
		}
		if err := engine.DeleteTrigger(ctx, id); err != nil {
			m.slashRespond("/triggers", "error: "+err.Error())
			return true, nil
		}
		m.slashRespond("/triggers", "Trigger "+id+" deleted.")
		return true, nil
	default:
		m.slashRespond("/triggers", "Usage: /triggers [list|add <prompt>|pause <id>|resume <id>|delete <id>]")
		return true, nil
	}
}

func findRemoteTrigger(ctx context.Context, engine *agentturn.RemoteAgentEngine, id string) (remoteagentclient.RemoteTrigger, error) {
	triggers, err := engine.Triggers(ctx)
	if err != nil {
		return remoteagentclient.RemoteTrigger{}, err
	}
	for _, trigger := range triggers {
		if trigger.TriggerID == id {
			return trigger, nil
		}
	}
	return remoteagentclient.RemoteTrigger{}, errTriggerNotFound{id: id}
}

type errTriggerNotFound struct{ id string }

func (e errTriggerNotFound) Error() string { return "trigger " + e.id + " not found" }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
