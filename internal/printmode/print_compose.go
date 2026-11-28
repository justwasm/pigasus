package printmode

// Composed slash commands in print mode.
//
// `agy -p '/goal /learn do the thing'` expands both commands into @-mention
// blocks inside <ADDITIONAL_METADATA> — the same shape the interactive host
// produces. Captured from live 1.5.18: the request body is 3,106 bytes, the
// turn text is `/goal /learn do the thing` verbatim, and two [Slash Command]
// blocks follow.
//
// The clone's print path used to send the raw text, so a stacked prompt
// reached the model with no instruction at all. This file builds the same
// envelope the TUI builds, reusing slashparse.ExpandComposed for the grammar
// so both surfaces read the rule from one place.

import (
	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/slashparse"
)

// printExpandableBody returns the @-mention body for a command the reference
// expands in print mode, and whether it expands at all.
//
// The set and the bodies are the interactive host's (see
// tui/slash_compose.go): /plan, /boost and /teamwork-preview do not expand,
// and /learn and /grill-me carry their wrapper tags while /goal and /schedule
// are bare. Each body is byte-pinned by devutils/wirealign.
func printExpandableBody(name string) (string, bool) {
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

// printComposedMentions applies the composed-command rule to a print-mode
// prompt. It returns the rewritten turn text, the mention blocks, and whether
// anything expanded.
//
// The print mode's separator rule is wider than the interactive one (Rule B:
// every Unicode whitespace rune), but every captured composed prompt uses
// plain spaces, and ExpandComposed's separator set is the intersection of the
// two rules. A prompt separating its commands with an exotic whitespace would
// take the interactive rule here; that is a recorded narrow gap, not a guess.
func printComposedMentions(prompt string) (string, []backend.Mention, bool) {
	wire, names := slashparse.ExpandComposed(prompt, func(name string) bool {
		_, ok := printExpandableBody(name)
		return ok
	})
	if len(names) == 0 {
		return prompt, nil, false
	}
	mentions := make([]backend.Mention, 0, len(names))
	for _, name := range names {
		body, ok := printExpandableBody(name)
		if !ok {
			continue
		}
		mentions = append(mentions, backend.Mention{
			Name: "/" + name,
			Kind: backend.MentionKindSlashCommand,
			Body: body,
		})
	}
	return wire, mentions, len(mentions) > 0
}
