package tui

// Composed slash commands.
//
// The reference expands a slash command wherever it appears in a prompt, not
// only in a leading run, and it expands each one into its own @-mention block
// instead of concatenating the bodies into one leading instruction. Both
// halves were verified against live agy 1.5.18 captures: `-p '/goal /learn do
// the thing'` and the same text typed into the TUI produce the same wire —
// the turn verbatim plus two [Slash Command] blocks — and `x /learn y` typed
// into the TUI produces `x /learn y/learn` with one block, the same shape the
// print mode produces.
//
// The grammar (which tokens expand, the leading-run re-serialisation, the
// trailing append quirk) lives in internal/core/slashparse/compose.go, whose
// test table is the ~57 captures. This file supplies the app-tier half: which
// commands expand, and what each one's body is. The split matters because the
// two halves fail differently — the scanner's table is the evidence for the
// grammar, and each body here is already byte-pinned by devutils/wirealign.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// expandableCommandBody reports the @-mention body for a command the
// reference expands, and whether the command expands at all.
//
// /plan, /boost and /teamwork-preview do NOT expand: the reference sends
// those verbatim, so they carry no body and are absent here. Everything the
// reference does expand is listed. A name that is neither expands nothing.
func (m *RootModel) expandableCommandBody(name string) (string, bool) {
	switch name {
	case "learn":
		return "<LEARN>\n" + slashactions.LearnPrompt() + "</LEARN>", true
	case "goal":
		return slashactions.GoalPrompt(), true
	case "grill-me":
		return "<GRILL_ME>\n" + slashactions.GrillMePrompt() + "</GRILL_ME>", true
	case "schedule":
		return slashactions.SchedulePrompt(), true
	}
	return "", false
}

// isExpandableCommand is expandableCommandBody's predicate.
func (m *RootModel) isExpandableCommand(name string) bool {
	_, ok := m.expandableCommandBody(name)
	return ok
}

// handleChainedSlashCommands answers a prompt that names more than one slash
// command, the way the reference does: the turn carries the text the
// reference would send and each expanded command rides in
// <ADDITIONAL_METADATA> as its own mention block, in the reference's order.
//
// cmds and task come from slashparse.ParseLeadingCommands, which normalises
// whitespace inside the leading run — and the reference re-serialises that
// same run with single spaces, so rebuilding "cmd1 cmd2 task" here is
// faithful. The two callers are the dispatcher (a buffer that starts with
// "/") and the individual handlers (which delegate when they see a stack).
// A command in the MIDDLE of ordinary text never reaches either — it has no
// leading slash — and is caught by composeOrdinaryPrompt on the submit path.
func (m *RootModel) handleChainedSlashCommands(cmds []string, task string) (bool, tea.Cmd) {
	if len(cmds) == 0 {
		return false, nil
	}
	var b strings.Builder
	for i, c := range cmds {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('/')
		b.WriteString(c)
	}
	if task != "" {
		b.WriteByte(' ')
		b.WriteString(task)
	}
	return m.composeSlashTurn(b.String())
}

// composeOrdinaryPrompt is the submit path's hook: an ordinary prompt (one
// that does not start with "/") can still name an expandable command, and the
// reference expands it. It returns false when the prompt names none, so the
// caller proceeds with a plain turn.
func (m *RootModel) composeOrdinaryPrompt(prompt string) (bool, tea.Cmd) {
	if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
		// Leading commands are the dispatcher's, not this hook's.
		return false, nil
	}
	return m.composeSlashTurn(prompt)
}

// verbatimCommands are the prompt commands the reference sends as the user
// typed them, with no expansion: /plan, /boost and /teamwork-preview (and its
// /teamwork alias). They still change session state (/plan sets the mode), so
// they cannot be left to the generic unknown-command path, but they add
// nothing to the wire.
//
// They are listed separately from the expandable set because the reference
// treats them differently in one observable way: it preserves the spelling
// the user typed. `/teamwork build a parser` reaches the wire as
// `/teamwork build a parser`, not as the canonical `/teamwork-preview
// build a parser` — captured from agy 1.5.18, both spellings.
var verbatimCommands = map[string]bool{
	"plan":             true,
	"boost":            true,
	"teamwork":         true,
	"teamwork-preview": true,
}

// verbatimCommandName reports the canonical state-modifier name for a
// verbatim command token, and whether the token is one.
func verbatimCommandName(raw string) (string, bool) {
	if !strings.HasPrefix(raw, "/") || len(raw) < 2 {
		return "", false
	}
	name := raw[1:]
	if !verbatimCommands[name] {
		return "", false
	}
	switch name {
	case "teamwork":
		return "teamwork", true
	}
	return name, true
}

// composeSlashTurn is the one place the composed shape is built.
func (m *RootModel) composeSlashTurn(rawPrompt string) (bool, tea.Cmd) {
	wire, names := slashparse.ExpandComposed(rawPrompt, m.isExpandableCommand)

	// A prompt can name a verbatim command without naming an expandable one —
	// `/teamwork build a parser`, `/boost analyze the retry layer`. The
	// reference sends those verbatim, so the wire text is the prompt and the
	// mention list stays empty; the state modifiers still apply.
	verbatim := false
	for _, tok := range strings.Fields(wire) {
		if _, ok := verbatimCommandName(tok); ok {
			verbatim = true
			break
		}
	}

	if len(names) == 0 && !verbatim {
		return false, nil
	}

	// State modifiers apply for every command present in the prompt, not only
	// the ones that expand: /plan sets the mode and /goal arms the loop, and
	// those are what make the commands mean anything. They are not part of
	// the wire shape.
	// Scan what the user typed, not the wire text: the reference's trailing
	// append turns `/goal` into `/goal/goal`, which is not a command token.
	present := composedCommandsPresent(rawPrompt)
	if present["plan"] {
		m.session.mode = permissions.ModePlan
		if m.session.policy != nil {
			m.session.policy.Mode = permissions.ModePlan
		}
		m.prompt.input.Placeholder = permissions.ModeBanner(permissions.ModePlan)
		m.prompt.modeHintDismissed = false
	}
	if present["goal"] {
		if m.session.activeGoal != nil && m.session.activeGoal.IsActive() {
			m.session.activeGoal.State = convstore.GoalStateCancelled
		}
		// The goal text comes from what the user typed, not from the wire
		// text: the wire carries the reference's trailing append
		// (`… refactor engine/goal`), and that token is noise in a title.
		goalText := composedTask(rawPrompt)
		if goalText == "" {
			goalText = "Help me set a clear goal for this session"
		}
		m.session.activeGoal = convstore.NewGoalState(goalText, m.session.messageCount())
	}

	mentions := make([]backend.Mention, 0, len(names))
	for _, name := range names {
		body, ok := m.expandableCommandBody(name)
		if !ok {
			continue
		}
		mentions = append(mentions, backend.Mention{
			Name: "/" + name,
			Kind: backend.MentionKindSlashCommand,
			Body: body,
		})
	}

	return true, m.sendTurnWithMentions(rawPrompt, wire, mentions)
}

// composedCommandsPresent returns the canonical names of the state-modifier
// commands found anywhere in text. Only plan, goal and boost are asked about;
// everything else is simply absent.
func composedCommandsPresent(text string) map[string]bool {
	out := make(map[string]bool, 3)
	for _, tok := range strings.Fields(text) {
		if !strings.HasPrefix(tok, "/") || len(tok) < 2 {
			continue
		}
		name := tok[1:]
		if canonical, ok := slashcommands.Canonical(name); ok {
			name = canonical
		}
		switch name {
		case "plan", "goal", "boost":
			out[name] = true
		}
	}
	return out
}

// composedTask is the text the user typed around the commands: the prompt
// with every slash-command token removed. The goal loop records it as the
// goal's title, so `/goal ship the release` arms the loop with "ship the
// release" rather than the whole command line.
func composedTask(text string) string {
	var kept []string
	for _, tok := range strings.Fields(text) {
		if strings.HasPrefix(tok, "/") && len(tok) > 1 {
			continue
		}
		kept = append(kept, tok)
	}
	return strings.Join(kept, " ")
}

// sendTurnWithMentions starts a model turn whose transcript row shows what
// the user typed and whose request carries the reference's rewritten text
// plus one @-mention block per expanded command.
//
// The two strings differ on purpose, and both are the reference's:
//   - typed is what the user typed (`x /learn y`); the transcript shows it.
//   - wire is what the reference puts inside <USER_REQUEST>
//     (`x /learn y/learn`); the request carries it.
func (m *RootModel) sendTurnWithMentions(typed, wire string, mentions []backend.Mention) tea.Cmd {
	m.pushHistory(typed)
	m.prompt.input.Reset()
	m.conversation.streaming = true
	m.prompt.promptHistory.Reset()
	m.conversation.response = append(m.conversation.response, "> "+typed)
	m.conversation.liveStart = len(m.conversation.response)
	m.conversation.liveSteps = nil
	m.conversation.activeTool = ""
	m.conversation.thinking = false
	m.prompt.picker.Reset()
	m.appendUserTurnWithMentions(wire, mentions)
	tickCmd := func() tea.Msg { return m.conversation.spinner.Tick() }
	return tea.Batch(m.streamPrompt(wire), tickCmd)
}
