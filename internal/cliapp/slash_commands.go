package cliapp

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/slashquote"
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
//	> /<cmd>          (bold blue, cWarningBlueOpen)
//	  ⎿  <msg>        (bold blue prefix + panelGray238 body)
//	<optional extra lines, rendered as-is>
//	<blank>
//
// Use this for every response-producing case. Multi-line responses pass
// the additional lines as extra; they appear after the `⎿` wrapper and
// before the trailing blank (matches /voice and /remote-control in REF).
func (m *runMode) slashRespond(cmd, msg string, extra ...string) {
	m.response = append(m.response,
		cWarningBlueOpen+"> "+cmd+cWarningBlueClose,
		cWarningBlueOpen+"  \u23bf  "+cWarningBlueClose+panelGray238+msg+panelGray238C,
	)
	m.response = append(m.response, extra...)
	m.response = append(m.response, "")
}

// slashRespondPlain appends a single message line to m.response WITHOUT
// the `> /cmd` + `⎿` wrapper. Use for /compact's "Nothing to compact yet."
// message — REF special-cases /compact: no `> /cmd` line, no `⎿` wrapper,
// the text appears in the prompt area as `> <msg>`. We approximate that
// by writing the styled line to the transcript instead.
func (m *runMode) slashRespondPlain(msg string) {
	m.response = append(m.response, panelGray238+msg+panelGray238C)
}

type slashHandler func(m *runMode, args string) (bool, tea.Cmd)

var slashHandlers = map[string]slashHandler{
	"clear":          (*runMode).slashClear,
	"new":            (*runMode).slashNew,
	"exit":           (*runMode).slashExit,
	"quit":           (*runMode).slashExit,
	"help":           (*runMode).slashHelp,
	"model":          (*runMode).slashModel,
	"config":         (*runMode).slashConfig,
	"context":        (*runMode).slashContext,
	"continue":       (*runMode).slashContinue,
	"copy":           (*runMode).slashCopy,
	"credits":        (*runMode).slashCredits,
	"diff":           (*runMode).slashDiff,
	"effort":         (*runMode).slashEffort,
	"feedback":       (*runMode).slashFeedback,
	"feature-diff":   (*runMode).slashFeatureDiff,
	"featdiff":       (*runMode).slashFeatureDiff,
	"diff-agy":       (*runMode).slashFeatureDiff,
	"fork":           (*runMode).slashFork,
	"hooks":          (*runMode).slashHooks,
	"keybindings":    (*runMode).slashKeybindings,
	"logout":         (*runMode).slashLogout,
	"login":          (*runMode).slashLogin,
	"mcp":            (*runMode).slashMCP,
	"open":           (*runMode).slashOpen,
	"permissions":    (*runMode).slashPermissions,
	"plugin":         (*runMode).slashPlugin,
	"remote-control": (*runMode).slashRemoteControl,
	"rename":         (*runMode).slashRename,
	"resume":         (*runMode).slashResume,
	"rewind":         (*runMode).slashRewind,
	"skills":         (*runMode).slashSkills,
	"statusline":     (*runMode).slashStatusline,
	"tasks":          (*runMode).slashTasks,
	"title":          (*runMode).slashTitle,
	"usage":          (*runMode).slashUsage,
	"voice":          (*runMode).slashVoice,
	"record":         (*runMode).slashRecord,
	"add-dir":        (*runMode).slashAddDir,
	"agents":         (*runMode).slashAgents,
	"artifact":       (*runMode).slashArtifact,
	"btw":            (*runMode).slashBtw,
	"changelog":      (*runMode).slashChangelog,
	"codesearch":     (*runMode).slashCodesearch,
	"quote":          (*runMode).slashQuote,
	"compact":        (*runMode).slashCompact,
	"browser":        (*runMode).slashBrowser,
	"plan":           (*runMode).slashPlan,
	"goal":           (*runMode).slashGoal,
	"learn":          (*runMode).slashLearn,
	"teamwork":       (*runMode).slashTeamwork,
	"boost":          (*runMode).slashBoost,
	"grill-me":       (*runMode).slashGrillMe,
	"schedule":       (*runMode).slashSchedule,
	"branch":         (*runMode).slashBranch,
	"narration":      (*runMode).slashNarration,
	"share":          (*runMode).slashShare,
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
	cmd, args := splitSlash(buf)
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
	m.historyIdx = 0
	m.historyEdit = ""

	if h, ok := slashHandlers[cmd]; ok {
		return h(m, args)
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
		token := "/" + cmd
		if args != "" {
			token += " " + args
		}
		return true, m.sendAsPrompt(token, printmode.SlashSkillPrompt(skill, args))
	}

	// Unknown command: visible feedback rather than silently
	// forwarding to the LLM. Cycle 99: the wording is the recovered
	// reference string (a live 1.3.6 TUI capture of "/zzznotacommand"
	// renders "Unknown command: /zzznotacommand" and sends nothing to
	// the model).
	m.slashRespond("/"+cmd, "Unknown command: /"+cmd)
	return true, nil
}

// slashContent appends a recovered content block to the transcript. Cycle 102:
// the reference renders /config, /permissions, /skills, /model, /effort and
// /changelog as interactive panels whose layout is not recoverable; we render
// the exact content the print-mode handler emits for the same command (content
// 【一手】, layout 【推测】).
//
// The print handlers emit tab-separated rows; a raw tab collapses in the TUI
// renderer, so we lay the columns out ourselves.
func (m *runMode) slashContent(text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return
	}
	rows := strings.Split(text, "\n")
	cells := make([][]string, len(rows))
	var widths []int
	for i, r := range rows {
		cells[i] = strings.Split(r, "\t")
		for j, c := range cells[i] {
			if j >= len(widths) {
				widths = append(widths, 0)
			}
			if w := utf8.RuneCountInString(c); w > widths[j] {
				widths[j] = w
			}
		}
	}
	for _, row := range cells {
		var b strings.Builder
		for j, c := range row {
			b.WriteString(c)
			if j < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-utf8.RuneCountInString(c)+2))
			}
		}
		m.response = append(m.response, strings.TrimRight(b.String(), " "))
	}
}

// splitPluginArgs splits the post-`/plugin` argument tail into
// (verb, rest). The verb is the first whitespace-delimited token;
// `rest` is what follows with surrounding whitespace trimmed, or
// empty when the verb stands alone. Cycle plugin-case-insensitive:
// callers compare the verb with strings.EqualFold so `List`,
// `LIST`, and `list` all dispatch the same handler (AGY 1.3.6
// changelog line 21).
func splitPluginArgs(args string) (string, string) {
	for i := 0; i < len(args); i++ {
		if args[i] == ' ' || args[i] == '\t' || args[i] == '\n' {
			return args[:i], trimSpace(args[i+1:])
		}
	}
	return args, ""
}

// splitSlash parses "/cmd args" into ("cmd", "args"). Returns cmd
// without the leading "/" and args with surrounding whitespace
// trimmed. If buf is just "/cmd" args is empty.
func splitSlash(buf string) (string, string) {
	if buf == "" || buf[0] != '/' {
		return "", ""
	}
	rest := buf[1:]
	for i := 0; i < len(rest); i++ {
		if rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\n' {
			return rest[:i], trimSpace(rest[i+1:])
		}
	}
	return rest, ""
}

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

func quoteInsertText(content string) string {
	return slashquote.InsertText(content)
}

func resolveQuote(arg string, sess []backend.Message) (string, string) {
	return slashquote.ResolveQuote(arg, sess)
}

func (m *runMode) applyQuoteInsert(content string) tea.Cmd {
	m.input.SetValue(content)
	m.historyIdx = 0
	m.historyEdit = ""
	m.picker.Update([]rune(firstLine(content)))
	return nil
}

func (m *runMode) handleQuote(args string) (bool, tea.Cmd) {
	content, errMsg := resolveQuote(args, m.session)
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
	m.historyIdx = 0
	m.historyEdit = ""
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

func planPrompt() string {
	return "Help me create a plan for the work I just described. Outline the next concrete steps, call out anything that needs clarification before we start, and end with the single question I should answer first."
}

func goalPrompt() string {
	return "Help me set a clear goal for this session. Restate the goal in one sentence, list the success criteria, and flag anything that could pull us off-track."
}

func learnPrompt() string {
	return "Capture a learning from what I just discussed. Write it as a short note I can refer back to, with the takeaway up front and any open questions below."
}

func teamworkPrompt() string {
	return "Coordinate a team of autonomous agents to tackle this project. Decompose the goal into clear milestones, assign specialized roles, track progress systematically, and verify outcomes before synthesizing the final result."
}

func boostPrompt() string {
	return "Apply deep thinking, strategic planning, multiple perspectives, and rigorous verification to solve the task at hand. Explore alternative approaches, evaluate tradeoffs, and verify each step thoroughly."
}

func grillMePrompt() string {
	return "Interrogate my reasoning on the topic I just raised. Ask probing questions that expose weak assumptions, missing evidence, or unstated tradeoffs. Keep going until I've either tightened the argument or admitted it's not yet defensible."
}

// schedulePrompt is the prompt /schedule sends to the model.
//
// The reference's /schedule is a shortcut for a normal model call — the
// reference's capture shows model output under the command (cycle 194) —
// and the model then registers the schedule through the `schedule` tool,
// which the clone carries (internal/tools/schedule_state.go).
//
// The wording is the clone's own, like the other prompt-sending
// commands' (the reference's own prompt text is not recoverable).
func schedulePrompt() string {
	return "Register this instruction to run later, as a one-time timer or on a recurring schedule. Tell me when it is registered, when it will first run, and what it will do."
}

func (m *runMode) slashPlan(args string) (bool, tea.Cmd) {
	return true, m.sendAsPrompt("/plan", planPrompt())
}

func (m *runMode) slashGoal(args string) (bool, tea.Cmd) {
	return true, m.sendAsPrompt("/goal", goalPrompt())
}

func (m *runMode) slashLearn(args string) (bool, tea.Cmd) {
	return true, m.sendAsPrompt("/learn", learnPrompt())
}

func (m *runMode) slashTeamwork(args string) (bool, tea.Cmd) {
	prompt := teamworkPrompt()
	if args != "" {
		prompt = args + "\n\n" + prompt
	}
	return true, m.sendAsPrompt("/teamwork", prompt)
}

func (m *runMode) slashBoost(args string) (bool, tea.Cmd) {
	prompt := boostPrompt()
	if args != "" {
		prompt = args + "\n\n" + prompt
	}
	return true, m.sendAsPrompt("/boost", prompt)
}

func (m *runMode) slashGrillMe(args string) (bool, tea.Cmd) {
	return true, m.sendAsPrompt("/grill-me", grillMePrompt())
}
