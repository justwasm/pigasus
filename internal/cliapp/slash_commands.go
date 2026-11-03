package cliapp

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	"github.com/justwasm/pigasus/internal/app/subagent/builtin"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/slashquote"
)

// slash_commands.go — cycle 77 slash command router; cycle 102 content pass.
//
// When the user presses Enter on a buffer starting with "/", we route
// to a per-command handler instead of the LLM. This matches the v1.3.5
// binary's behavior where every slash command opens a dedicated menu
// or performs a local action.
//
// Cycle 102 replaced the cycle-77 stub bodies for the commands whose
// *content* is recoverable; cycle 167 closed the remaining stubs that
// needed real implementation (resume, fork, rewind, compact, plan, etc).
// The reference opens an interactive panel for each command; the panel
// layout cannot be captured (the reference account's quota is exhausted,
// so no TUI transcript is obtainable). What we render instead is the
// exact content the print-mode handler emits for the same command —
// content 【一手】, panel layout 【推测】.
//
// Cycle 194: the seven Class-D behavioral commands in TODO.md §2.4
// (`/goal`, `/plan`, `/learn`, `/grill-me`, `/keybindings`, `/schedule`,
// `/browser`) now match the v1.4.0 reference. /goal, /plan, /learn,
// /grill-me are prompt-sending commands — the slash form is a shortcut
// for a normal model call, so the handler echoes the token, appends a
// synthesized prompt to m.session, and starts the model stream via
// sendAsPrompt. /keybindings opens ~/.gemini/antigravity-cli/keybindings.json
// in the configured editor (creates the file with "{}" if missing).
//
// Cycle 342: /schedule joined the prompt-sending commands — the
// reference's capture of it shows model output under the command, and
// the model registers the schedule through the `schedule` tool. /browser
// runs the browser agent (the reference's own description: "Invoke a
// browser agent for web tasks."). Both used to print a hard-coded
// "Error ID: <uuid>-1" quota block: the reference's capture of them was
// taken while the *reference account's* quota was exhausted, so the
// capture held the failure and the clone reproduced it — a false
// statement about the reader's own quota (see slash_browser.go).
//
// Handlers mutate m.response to show user echo + feedback, reset
// m.input/m.picker, and never call the model — except the five prompt-
// sending commands above, which delegate to sendAsPrompt.
//
// Cycle 381, tranche 81: the slash *grammar* — the "/cmd args" parse,
// the "/plugin verb rest" parse and the trims — belongs to
// internal/core/slashparse. This file used to carry its own copies of
// all three.
//
// Cycle 195: every slash command that produces a transcript message now
// emits the REF v1.4.0 echo wrapper:
//
//	\x1b[1m\x1b[38;5;26m> /<cmd>\x1b[0m
//	\x1b[1m\x1b[38;5;26m  ⎿  \x1b[0m\x1b[38;5;238m<msg>\x1b[39m
//	<blank>
//
// Panel-opening commands (/mcp, /help, /agents, /hooks, /artifact, …) and
// transcript-wiping commands (/clear, /new) do not call slashRespond and
// therefore do not emit the echo. The /compact empty-state message has
// no `> /cmd` line and no `⎿` wrapper — it writes directly into the
// prompt area (or, in our case, the transcript with prompt-area styling),
// matching REF's class-B special case (TODO.md §2.2).

// slashRespond appends the REF-style slash command echo block to m.response.
//
//	> /<cmd>          (bold blue)
//	  ⎿  <msg>        (bold blue prefix + muted body)
//	<optional extra lines, rendered as-is>
//	<blank>
//
// The block's bytes belong to the ui tier (tuilib.SlashEchoLines); this
// method owns only what the host owns: the rows land in m.response and
// the transcript's row cache is marked stale.
//
// Use this for every response-producing case. Multi-line responses pass
// the additional lines as extra; they appear after the `⎿` wrapper and
// before the trailing blank (matches /voice and /remote-control in REF).
func (m *runMode) slashRespond(cmd, msg string, extra ...string) {
	m.response = append(m.response, tuilib.SlashEchoLines(cmd, msg, extra...)...)
	m.rowsCacheDirty = true
}

// slashRespondPlain appends a single message line to m.response WITHOUT
// the `> /cmd` + `⎿` wrapper. Use for /compact's "Nothing to compact yet."
// message — REF special-cases /compact: no `> /cmd` line, no `⎿` wrapper,
// the text appears in the prompt area as `> <msg>`. We approximate that
// by writing the styled line to the transcript instead.
func (m *runMode) slashRespondPlain(msg string) {
	m.response = append(m.response, panelGray238+msg+panelGray238C)
	m.rowsCacheDirty = true
}

type slashHandler func(m *runMode, args string) (bool, tea.Cmd)

// slashHandlers is the host's command table: the canonical name of
// every command the host answers with a handler of its own.
//
// Cycle 381, tranche 88: the table is keyed by the *canonical* name
// only. The aliases ("/new" for "/clear", "/quit" for "/exit",
// "/featdiff" and "/diff-agy" for "/feature-diff", "/record" for
// "/voice", "/plugins" for "/plugin", "/teamwork" for
// "/teamwork-preview") are resolved through internal/ui/slashcommands,
// which owns the vocabulary, before the lookup happens.
//
// The one exception is "/branch": the catalogue lists the word as an
// alias of "/fork", but the host answers "/branch" with a command of
// its own, so the word is a command name here and the vocabulary
// resolves it to itself.
var slashHandlers map[string]slashHandler

func init() {
	slashHandlers = map[string]slashHandler{
		"clear":            (*runMode).slashClear,
		"exit":             (*runMode).slashExit,
		"help":             (*runMode).slashHelp,
		"model":            (*runMode).slashModel,
		"config":           (*runMode).slashConfig,
		"context":          (*runMode).slashContext,
		"continue":         (*runMode).slashContinue,
		"copy":             (*runMode).slashCopy,
		"credits":          (*runMode).slashCredits,
		"diff":             (*runMode).slashDiff,
		"effort":           (*runMode).slashEffort,
		"feedback":         (*runMode).slashFeedback,
		"feature-diff":     (*runMode).slashFeatureDiff,
		"fork":             (*runMode).slashFork,
		"hooks":            (*runMode).slashHooks,
		"keybindings":      (*runMode).slashKeybindings,
		"logout":           (*runMode).slashLogout,
		"login":            (*runMode).slashLogin,
		"mcp":              (*runMode).slashMCP,
		"open":             (*runMode).slashOpen,
		"permissions":      (*runMode).slashPermissions,
		"plugin":           (*runMode).slashPlugin,
		"remote-control":   (*runMode).slashRemoteControl,
		"rename":           (*runMode).slashRename,
		"resume":           (*runMode).slashResume,
		"rewind":           (*runMode).slashRewind,
		"skills":           (*runMode).slashSkills,
		"statusline":       (*runMode).slashStatusline,
		"tasks":            (*runMode).slashTasks,
		"title":            (*runMode).slashTitle,
		"usage":            (*runMode).slashUsage,
		"voice":            (*runMode).slashVoice,
		"add-dir":          (*runMode).slashAddDir,
		"agents":           (*runMode).slashAgents,
		"artifact":         (*runMode).slashArtifact,
		"btw":              (*runMode).slashBtw,
		"changelog":        (*runMode).slashChangelog,
		"codesearch":       (*runMode).slashCodesearch,
		"quote":            (*runMode).slashQuote,
		"compact":          (*runMode).slashCompact,
		"browser":          (*runMode).slashBrowser,
		"plan":             (*runMode).slashPlan,
		"goal":             (*runMode).slashGoal,
		"learn":            (*runMode).slashLearn,
		"teamwork-preview": (*runMode).slashTeamwork,
		"boost":            (*runMode).slashBoost,
		"grill-me":         (*runMode).slashGrillMe,
		"schedule":         (*runMode).slashSchedule,
		"branch":           (*runMode).slashBranch,
		"narration":        (*runMode).slashNarration,
		"share":            (*runMode).slashShare,
	}
}

// handleSlashCommand inspects the buffer; if it's a slash command it
// runs the appropriate handler and returns (true, cmd). If the buffer
// is not a recognized slash command it returns (false, nil) so the
// caller can fall back to sending it to the LLM. cmd may be nil for
// commands that don't need to schedule anything (most) or non-nil for
// commands like /exit that need tea.Quit to terminate the program.
func (m *runMode) handleSlashCommand(buf string) (bool, tea.Cmd) {
	if len(buf) == 0 || buf[0] != '/' {
		return false, nil
	}
	// Strip the leading "/" and split off the command name.
	cmd, args := slashparse.Split(buf)
	// Cycle 99: a bare "/" (or "/ ") is still a slash command, not chat. The
	// reference answers it with "Unknown command: /" and never forwards it to
	// the model; previously we fell through and sent it as a chat message.
	//
	// Cycle 149: the v1.3.6 reference does NOT echo the slash command
	// itself into the transcript — the picker overlay's prompt area
	// already shows it, and /<cmd> invocations go straight to their
	// handler output.
	m.input.Reset()
	m.picker.Reset()
	m.promptHistory.Reset()

	// Cycle 381, tranche 88: the alias rule belongs to the
	// vocabulary, so the command name is resolved there before the
	// host's table is consulted. A name the vocabulary does not know
	// keeps its own spelling.
	name := cmd
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		name = canonical
	}

	// Check scope availability for any known command before dispatching or opening panels
	if cmdInfo, found := slashcommands.Find(name); found {
		sctx := m.slashScopeContext()
		if !slashcommands.IsAvailable(cmdInfo, sctx) {
			m.slashRespond("/"+name, fmt.Sprintf("%s is not supported by the %s backend", "/"+name, sctx.EngineName))
			return true, nil
		}
	}

	if h, ok := slashHandlers[name]; ok {
		return h(m, args)
	}

	if cmd == "agent" {
		return m.slashAgentCmd(args)
	}

	// Cycle 199: every skill the reference lists via /skills is also an
	// invocable slash command. Typing "/<skill>" sends the skill's
	// instruction text to the model as a prompt — verified with
	// `agy -p /terminal-browser` (a skill-framed completion comes back,
	// not the "not a recognized slash command or skill" fallback) and
	// several plugin/builtin skills. The catalog is derived by
	// printSlashSkillInfos, the same enumerator /skills renders, so the
	// two surfaces stay in lock-step. See slash_skills.go for the
	// captured alias rules and the 【推测】 prompt choice.
	if skill, ok := slashSkillLookup(cmd); ok {
		sctx := m.slashScopeContext()
		if sctx.EngineName != "pigasus" && sctx.EngineName != "agy" {
			m.slashRespond("/"+cmd, fmt.Sprintf("/%s is a Pigasus skill, not supported by the %s backend", cmd, sctx.EngineName))
			return true, nil
		}
		token := "/" + cmd
		if args != "" {
			token += " " + args
		}
		return true, m.sendAsPrompt(token, printmode.SlashSkillPrompt(skill, args))
	}

	// Dynamic agent slash commands: if the active engine offers this command,
	// send it through as a prompt/command to the agent backend.
	for _, dyn := range m.engineSlashCommands() {
		cleanDyn := strings.TrimPrefix(dyn.Name, "/")
		if strings.EqualFold(cleanDyn, cmd) {
			token := "/" + cmd
			if args != "" {
				token += " " + args
			}
			return true, m.sendAsPrompt(token, token)
		}
	}

	// Non-blocking pass-through for active agent backends (micro-acp pattern):
	// If an active agent engine is running, do not block unrecognized commands locally
	// with "Unknown command" — pass them through to the agent so backend-specific
	// slash commands or workflows are never shadowed.
	if m.engine != nil {
		token := "/" + cmd
		if args != "" {
			token += " " + args
		}
		return true, m.sendAsPrompt(token, token)
	}

	// Unknown command in standalone mode: visible feedback rather than silently
	// forwarding to the LLM. Cycle 99: the wording is the recovered
	// reference string (a live 1.3.6 TUI capture of "/zzznotacommand"
	// renders "Unknown command: /zzznotacommand" and sends nothing to
	// the model).
	m.slashRespond("/"+cmd, "Unknown command: /"+cmd)
	return true, nil
}

func (m *runMode) slashAgentCmd(args string) (bool, tea.Cmd) {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		m.slashRespond("/agent", "Usage: /agent <command> [args] - dispatch a command directly to the active agent")
		return true, nil
	}
	parts := strings.Fields(trimmed)
	cmdName := parts[0]
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, cmdName))
	token := "/" + cmdName
	if rest != "" {
		token += " " + rest
	}
	return true, m.sendAsPrompt(token, token)
}

// slashContent appends a recovered content block to the transcript. Cycle 102:
// the reference renders /config, /permissions, /skills, /model, /effort and
// /changelog as interactive panels whose layout is not recoverable; we render
// the exact content the print-mode handler emits for the same command (content
// 【一手】, layout 【推测】).
//
// The print handlers emit tab-separated rows; a raw tab collapses in the TUI
// renderer, so the columns are laid out by the ui tier's own row layout
// (rows.TabAlignedRows). This method owns only what the host owns: the rows
// land in m.response.
func (m *runMode) slashContent(text string) {
	m.response = append(m.response, rows.TabAlignedRows(text)...)
}

func (m *runMode) applyQuoteInsert(content string) tea.Cmd {
	m.input.SetValue(content)
	m.promptHistory.Reset()
	m.picker.Update([]rune(tuilib.FirstLine(content)))
	return nil
}

func (m *runMode) handleQuote(args string) (bool, tea.Cmd) {
	content, errMsg := slashquote.ResolveQuote(args, m.session)
	if errMsg != "" {
		m.slashRespond("/quote", errMsg)
		return true, nil
	}
	cmd := m.applyQuoteInsert(content)
	return true, cmd
}

func (m *runMode) sendAsPrompt(token, prompt string) tea.Cmd {
	m.pushHistory(token)
	m.input.Reset()
	m.streaming = true
	m.promptHistory.Reset()
	m.response = append(m.response, "> "+token)
	m.liveStart = len(m.response)
	m.liveSteps = nil
	m.activeTool = ""
	m.thinking = false
	m.picker.Reset()
	m.appendUserTurn(prompt)
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(prompt), tickCmd)
}

func boostPrompt() string {
	return fmt.Sprintf("<ORCHESTRATOR>\n%s\n</ORCHESTRATOR>", builtin.OrchestratorPrompt())
}

func (m *runMode) slashPlan(args string) (bool, tea.Cmd) {
	trimmed := strings.TrimSpace(args)
	m.mode = permissions.ModePlan
	if m.policy != nil {
		m.policy.Mode = permissions.ModePlan
	}
	m.input.Placeholder = permissions.ModeBanner(permissions.ModePlan)
	m.modeHintDismissed = false

	if strings.HasPrefix(trimmed, "/") {
		return m.dispatchSlashLine(trimmed)
	}

	prompt := slashactions.PlanPrompt()
	if trimmed != "" {
		prompt = trimmed + "\n\n" + prompt
	}
	return true, m.sendAsPrompt("/plan", prompt)
}

func (m *runMode) slashGoal(args string) (bool, tea.Cmd) {
	trimmed := strings.TrimSpace(args)
	if m.activeGoal != nil && m.activeGoal.IsActive() {
		m.activeGoal.State = convstore.GoalStateCancelled
	}
	goalText := trimmed
	prompt := slashactions.GoalPrompt()
	if trimmed != "" {
		prompt = trimmed + "\n\n" + prompt
	} else {
		goalText = "Help me set a clear goal for this session"
	}
	m.activeGoal = convstore.NewGoalState(goalText, len(m.session))
	return true, m.sendAsPrompt("/goal", prompt)
}

func (m *runMode) slashLearn(args string) (bool, tea.Cmd) {
	return true, m.sendAsPrompt("/learn", slashactions.LearnPrompt())
}

// slashTeamwork answers /teamwork-preview (and its /teamwork alias).
//
// The echo carries the command's canonical name: the reference's own
// command is "/teamwork-preview" (the reference CLI's changelog names
// it), and "/teamwork" is the alias the documentation advertises for
// it. Every other prompt-sending command echoes its canonical name
// too, so the alias and the command produce the same bytes.
func (m *runMode) slashTeamwork(args string) (bool, tea.Cmd) {
	prompt := slashactions.TeamworkPrompt()
	if args != "" {
		prompt = args + "\n\n" + prompt
	}
	return true, m.sendAsPrompt("/teamwork-preview", prompt)
}

func (m *runMode) slashBoost(args string) (bool, tea.Cmd) {
	prompt := boostPrompt()
	clean := strings.TrimSpace(args)
	if clean != "" {
		prompt = fmt.Sprintf("%s\n\n#### Current Task to Execute\n\n**Task**: %s", prompt, clean)
	}
	return true, m.sendAsPrompt("/boost", prompt)
}

func (m *runMode) slashGrillMe(args string) (bool, tea.Cmd) {
	prompt := slashactions.GrillMePrompt()
	clean := strings.TrimSpace(args)
	if clean != "" {
		prompt = clean + "\n\n" + prompt
	}
	return true, m.sendAsPrompt("/grill-me", prompt)
}
