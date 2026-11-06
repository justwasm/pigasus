package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/prompt/customize"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/rewind"
	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	"github.com/justwasm/pigasus/internal/app/subagent/builtin"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/compaction"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/compat/agy"
	"github.com/justwasm/pigasus/internal/features/models"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
	"github.com/justwasm/pigasus/internal/features/tools/isolation"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	persistentrc "github.com/justwasm/pigasus/internal/infra/remote/persistent"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/tui/commands"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"github.com/justwasm/pigasus/internal/ui/keybindings"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchaction"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/hostactions"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
	"github.com/justwasm/pigasus/internal/ui/views/slashquote"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/urlchunk"
)

// ---- slash_commands.go ----

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
// synthesized prompt to m.engine.session, and starts the model stream via
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
// Handlers mutate m.engine.response to show user echo + feedback, reset
// m.composer.input/m.composer.picker, and never call the model — except the five prompt-
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

// slashRespond appends the REF-style slash command echo block to m.engine.response.
//
//	> /<cmd>          (bold blue)
//	  ⎿  <msg>        (bold blue prefix + muted body)
//	<optional extra lines, rendered as-is>
//	<blank>
//
// The block's bytes belong to the ui tier (tuilib.SlashEchoLines); this
// method owns only what the host owns: the rows land in m.engine.response and
// the transcript's row cache is marked stale.
//
// Use this for every response-producing case. Multi-line responses pass
// the additional lines as extra; they appear after the `⎿` wrapper and
// before the trailing blank (matches /voice and /remote-control in REF).
func (m *runMode) slashRespond(cmd, msg string, extra ...string) {
	m.engine.response = append(m.engine.response, tuilib.SlashEchoLines(cmd, msg, extra...)...)
	m.viewport.rowsCacheDirty = true
}

// slashRespondPlain appends a single message line to m.engine.response WITHOUT
// the `> /cmd` + `⎿` wrapper. Use for /compact's "Nothing to compact yet."
// message — REF special-cases /compact: no `> /cmd` line, no `⎿` wrapper,
// the text appears in the prompt area as `> <msg>`. We approximate that
// by writing the styled line to the transcript instead.
func (m *runMode) slashRespondPlain(msg string) {
	m.engine.response = append(m.engine.response, panelGray238+msg+panelGray238C)
	m.viewport.rowsCacheDirty = true
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
		"projects":         (*runMode).slashProjects,
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

	m.composer.input.Reset()
	if m.composer.undoMgr != nil {
		m.composer.undoMgr.Clear()
	}
	m.composer.picker.Reset()
	m.composer.promptHistory.Reset()

	disp := m.commandDispatcher()
	handled, cmd := disp.Dispatch(buf)
	if handled {
		return true, cmd
	}

	cmdName, _ := slashparse.Split(buf)
	// Unknown command in standalone mode: visible feedback rather than silently
	// forwarding to the LLM. Cycle 99: the wording is the recovered
	// reference string (a live 1.3.6 TUI capture of "/zzznotacommand"
	// renders "Unknown command: /zzznotacommand" and sends nothing to
	// the model).
	m.slashRespond("/"+cmdName, agy.FormatUnknownCommand(cmdName))
	return true, nil
}

func (m *runMode) StartManualCompact(instructions string) {
	m.startManualCompact(instructions)
}

func (m *runMode) HostActionSeams() hostactions.Seams {
	return m.hostActionSeams()
}

func (m *runMode) commandDispatcher() *commands.Dispatcher {
	d := commands.NewDispatcher(m)
	for name, h := range slashHandlers {
		fn := h
		d.Register(name, func(args string) (bool, tea.Cmd) {
			return fn(m, args)
		})
	}
	commands.RegisterSessionCommands(d, m)
	d.Register("agent", func(args string) (bool, tea.Cmd) {
		return m.slashAgentCmd(args)
	})
	d.RegisterChained(func(cmds []string, task string) (bool, tea.Cmd) {
		return m.handleChainedSlashCommands(cmds, task)
	})
	return d
}

func (m *runMode) IsStreaming() bool {
	return m.engine.streaming
}

func (m *runMode) SetInputNotice(msg string) {
	m.viewport.inputNotice = msg
}

func (m *runMode) HandleChainedSlashCommands(commands []string, task string) (bool, tea.Cmd) {
	return m.handleChainedSlashCommands(commands, task)
}

func (m *runMode) SendAsPrompt(token, prompt string) tea.Cmd {
	return m.sendAsPrompt(token, prompt)
}

func (m *runMode) TurnEngine() any {
	return m.turnEngine()
}

func (m *runMode) LookupSkillPrompt(name, args string) (string, bool) {
	if skill, ok := slashSkillLookup(name); ok {
		return printmode.SlashSkillPrompt(skill, args), true
	}
	return "", false
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
// land in m.engine.response.
func (m *runMode) slashContent(text string) {
	m.engine.response = append(m.engine.response, rows.TabAlignedRows(text)...)
}

func (m *runMode) applyQuoteInsert(content string) tea.Cmd {
	m.composer.input.SetValue(content)
	m.composer.promptHistory.Reset()
	m.composer.picker.Update([]rune(tuilib.FirstLine(content)))
	return nil
}

func (m *runMode) handleQuote(args string) (bool, tea.Cmd) {
	content, errMsg := slashquote.ResolveQuote(args, m.engine.session)
	if errMsg != "" {
		m.slashRespond("/quote", errMsg)
		return true, nil
	}
	cmd := m.applyQuoteInsert(content)
	return true, cmd
}

func (m *runMode) sendAsPrompt(token, prompt string) tea.Cmd {
	m.pushHistory(token)
	m.composer.input.Reset()
	m.engine.streaming = true
	m.composer.promptHistory.Reset()
	m.engine.response = append(m.engine.response, "> "+token)
	m.engine.liveStart = len(m.engine.response)
	m.engine.liveSteps = nil
	m.engine.activeTool = ""
	m.engine.thinking = false
	m.composer.picker.Reset()
	m.appendUserTurn(prompt)
	tickCmd := func() tea.Msg { return m.engine.spinner.Tick() }
	return tea.Batch(m.streamPrompt(prompt), tickCmd)
}

func boostPrompt() string {
	return fmt.Sprintf("<ORCHESTRATOR>\n%s\n</ORCHESTRATOR>", builtin.OrchestratorPrompt())
}

func (m *runMode) handleChainedSlashCommands(cmds []string, task string) (bool, tea.Cmd) {
	if len(cmds) == 0 {
		return false, nil
	}

	var hasBoost, hasPlan, hasGoal, hasLearn, hasGrillMe, hasTeamwork bool
	var skillPrompts []string

	for _, c := range cmds {
		name := c
		if canonical, ok := slashcommands.Canonical(c); ok {
			name = canonical
		}
		switch name {
		case "boost":
			hasBoost = true
		case "plan":
			hasPlan = true
		case "goal":
			hasGoal = true
		case "learn":
			hasLearn = true
		case "grill-me":
			hasGrillMe = true
		case "teamwork-preview", "teamwork":
			hasTeamwork = true
		default:
			if prompt, ok := m.LookupSkillPrompt(c, task); ok {
				skillPrompts = append(skillPrompts, prompt)
			} else {
				return false, nil
			}
		}
	}

	// 1. Compose state modifiers
	if hasPlan {
		m.session.mode = permissions.ModePlan
		if m.session.policy != nil {
			m.session.policy.Mode = permissions.ModePlan
		}
		m.composer.input.Placeholder = permissions.ModeBanner(permissions.ModePlan)
		m.composer.modeHintDismissed = false
	}

	if hasGoal {
		if m.session.activeGoal != nil && m.session.activeGoal.IsActive() {
			m.session.activeGoal.State = convstore.GoalStateCancelled
		}
		goalText := task
		if goalText == "" {
			goalText = "Help me set a clear goal for this session"
		}
		m.session.activeGoal = convstore.NewGoalState(goalText, len(m.engine.session))
	}

	// 2. Compose prompts
	var parts []string
	if task != "" {
		parts = append(parts, task)
	}
	if hasPlan {
		parts = append(parts, slashactions.PlanPrompt())
	}
	if hasGoal {
		parts = append(parts, slashactions.GoalPrompt())
	}
	if hasLearn {
		parts = append(parts, slashactions.LearnPrompt())
	}
	if hasGrillMe {
		parts = append(parts, slashactions.GrillMePrompt())
	}
	if hasTeamwork {
		parts = append(parts, slashactions.TeamworkPrompt())
	}
	for _, sp := range skillPrompts {
		parts = append(parts, sp)
	}

	inner := strings.Join(parts, "\n\n")

	var finalPrompt string
	if hasBoost {
		if inner != "" {
			finalPrompt = fmt.Sprintf("%s\n\n#### Current Task to Execute\n\n**Task**: %s", boostPrompt(), inner)
		} else {
			finalPrompt = boostPrompt()
		}
	} else {
		finalPrompt = inner
	}

	// 3. Display echo: > /boost /plan /goal <task>
	var tokenParts []string
	for _, c := range cmds {
		tokenParts = append(tokenParts, "/"+c)
	}
	token := strings.Join(tokenParts, " ")
	if task != "" {
		token += " " + task
	}

	return true, m.sendAsPrompt(token, finalPrompt)
}

func (m *runMode) slashPlan(args string) (bool, tea.Cmd) {
	trimmed := strings.TrimSpace(args)
	if strings.HasPrefix(trimmed, "/") {
		cmds, task := slashparse.ParseLeadingCommands("/plan " + trimmed)
		if len(cmds) > 1 {
			return m.handleChainedSlashCommands(cmds, task)
		}
	}

	m.session.mode = permissions.ModePlan
	if m.session.policy != nil {
		m.session.policy.Mode = permissions.ModePlan
	}
	m.composer.input.Placeholder = permissions.ModeBanner(permissions.ModePlan)
	m.composer.modeHintDismissed = false

	prompt := slashactions.PlanPrompt()
	if trimmed != "" {
		prompt = trimmed + "\n\n" + prompt
	}
	return true, m.sendAsPrompt("/plan", prompt)
}

func (m *runMode) slashGoal(args string) (bool, tea.Cmd) {
	trimmed := strings.TrimSpace(args)
	if strings.HasPrefix(trimmed, "/") {
		cmds, task := slashparse.ParseLeadingCommands("/goal " + trimmed)
		if len(cmds) > 1 {
			return m.handleChainedSlashCommands(cmds, task)
		}
	}

	if m.session.activeGoal != nil && m.session.activeGoal.IsActive() {
		m.session.activeGoal.State = convstore.GoalStateCancelled
	}
	goalText := trimmed
	prompt := slashactions.GoalPrompt()
	if trimmed != "" {
		prompt = trimmed + "\n\n" + prompt
	} else {
		goalText = "Help me set a clear goal for this session"
	}
	m.session.activeGoal = convstore.NewGoalState(goalText, len(m.engine.session))
	return true, m.sendAsPrompt("/goal", prompt)
}

func (m *runMode) slashLearn(args string) (bool, tea.Cmd) {
	trimmed := strings.TrimSpace(args)
	if strings.HasPrefix(trimmed, "/") {
		cmds, task := slashparse.ParseLeadingCommands("/learn " + trimmed)
		if len(cmds) > 1 {
			return m.handleChainedSlashCommands(cmds, task)
		}
	}
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
	trimmed := strings.TrimSpace(args)
	if strings.HasPrefix(trimmed, "/") {
		cmds, task := slashparse.ParseLeadingCommands("/teamwork-preview " + trimmed)
		if len(cmds) > 1 {
			return m.handleChainedSlashCommands(cmds, task)
		}
	}
	prompt := slashactions.TeamworkPrompt()
	if trimmed != "" {
		prompt = trimmed + "\n\n" + prompt
	}
	return true, m.sendAsPrompt("/teamwork-preview", prompt)
}

func (m *runMode) slashBoost(args string) (bool, tea.Cmd) {
	clean := strings.TrimSpace(args)
	if strings.HasPrefix(clean, "/") {
		cmds, task := slashparse.ParseLeadingCommands("/boost " + clean)
		if len(cmds) > 1 {
			return m.handleChainedSlashCommands(cmds, task)
		}
	}
	prompt := boostPrompt()
	if clean != "" {
		prompt = fmt.Sprintf("%s\n\n#### Current Task to Execute\n\n**Task**: %s", prompt, clean)
	}
	return true, m.sendAsPrompt("/boost", prompt)
}

func (m *runMode) slashGrillMe(args string) (bool, tea.Cmd) {
	clean := strings.TrimSpace(args)
	if strings.HasPrefix(clean, "/") {
		cmds, task := slashparse.ParseLeadingCommands("/grill-me " + clean)
		if len(cmds) > 1 {
			return m.handleChainedSlashCommands(cmds, task)
		}
	}
	prompt := slashactions.GrillMePrompt()
	if clean != "" {
		prompt = clean + "\n\n" + prompt
	}
	return true, m.sendAsPrompt("/grill-me", prompt)
}

// ---- slash_commands_handlers.go ----

// slash_commands_handlers.go — individual slash command action handlers.

func (m *runMode) hostActionSeams() hostactions.Seams {
	return hostactions.Seams{
		AbortTurn: m.abortTurnForLocalCommand,
		ClearState: func() {
			if m.session.conversationStarted || len(m.engine.session) > 0 {
				m.saveSession()
			}
			if m.session.supervisor != nil {
				m.session.supervisor.Clear()
			}
			m.engine.response = nil
			m.engine.session = nil
			m.engine.blocks = nil
			m.engine.liveSteps = nil
			m.engine.liveStepRuns = nil
			m.engine.activeTool = ""
			m.session.store.ClearStreamQueue()
			m.engine.replyStart = -1
			m.engine.liveReplyIdx = -1
			m.engine.liveReplyBuf = ""
			m.engine.streamedText = false
			m.session.wireSettingsPending = true
			m.session.store.ResetSessionTokens()
			m.session.autoTitle = ""
			m.session.conversationID = newConversationID()
			m.session.conversationStarted = false
			m.session.hookSessionStarted = false
			m.engine.subagentDefs = subagenttools.NewStore()
			m.engine.subagentRegistry = subagenttools.NewRegistry(m.session.conversationID)
			m.resetTrajectory()
		},
		Quit: func() {
			m.engine.quitting = true
		},
		OpenHelp: func() {
			m.viewport.helpPanel.Open(0)
		},
		OpenPanel: func(cmd string) bool {
			return m.openSlashPanel(cmd)
		},
		GetPlannerResponses: m.plannerResponses,
		GetBtwCard:          m.copyBtwCard,
		WriteClipboard: func(text string) error {
			return clipboardWrite(text)
		},
		Respond: func(cmd string, msg string, extra ...string) {
			m.slashRespond(cmd, msg, extra...)
		},
		ResetInputs: func() {
			m.composer.input.Reset()
			m.composer.promptHistory.Reset()
		},
	}
}

func (m *runMode) slashClear(args string) (bool, tea.Cmd) {
	hostactions.HandleClear(m.hostActionSeams(), args)
	return true, nil
}

func (m *runMode) slashExit(args string) (bool, tea.Cmd) {
	hostactions.HandleExit(m.hostActionSeams(), args)
	return true, tea.Quit
}

func (m *runMode) slashHelp(args string) (bool, tea.Cmd) {
	hostactions.HandleHelp(m.hostActionSeams(), args)
	return true, nil
}

func (m *runMode) slashModel(args string) (bool, tea.Cmd) {
	// "/model <name>" is a direct setter — the reference changes the
	// model and echoes "Model set to <label>". Only a bare "/model" opens
	// the Switch Model panel. 【一手】 agy 1.4.6, live.
	if trimmed := slashparse.Trim(args); trimmed != "" {
		return true, m.applyModelArgument(trimmed)
	}
	if m.session.flags != nil {
		m.slashContent(printmode.PrintSlashModels(m.session.flags))
	} else {
		m.slashContent(modelcatalog.ModelSlug(m.session.model) + "\t" + m.session.model)
	}
	return true, nil
}

func (m *runMode) slashConfig(args string) (bool, tea.Cmd) {
	body := printmode.PrintSlashConfig()
	m.slashContent(body)
	customize.RecordToken("config", body)
	return true, nil
}

func (m *runMode) slashContext(args string) (bool, tea.Cmd) {
	m.slashRespond("/context", "Context usage: session has "+strconv.Itoa(m.sessionCount())+" message(s)")
	return true, nil
}

func (m *runMode) slashContinue(args string) (bool, tea.Cmd) {
	id, _, err := convstore.ResolveContinue(m.session.cwd)
	if err != nil {
		m.slashRespond("/continue", "error: "+err.Error())
		return true, nil
	}
	if id == "" {
		m.slashRespond("/continue", "No previous conversation to continue.")
		return true, nil
	}
	sel := convstore.LoadSelection(id, "continue", convstore.ConversationPath(id))
	if sel.Store == nil {
		m.slashRespond("/continue", "Failed to load conversation "+id)
		return true, nil
	}
	m.session.conversationID = sel.ID
	m.engine.response = nil
	m.engine.session = nil
	if m.session.supervisor != nil {
		m.session.supervisor.Clear()
	}
	m.engine.blocks = nil
	m.engine.liveSteps = nil
	m.engine.activeTool = ""
	m.seedConversation(sel.Store)
	m.session.autoTitle = ""
	m.engine.response = append(m.engine.response, "  ⎿  Resumed conversation "+sel.ID)
	return true, nil
}

func (m *runMode) slashCopy(args string) (bool, tea.Cmd) {
	hostactions.HandleCopy(m.hostActionSeams(), args)
	return true, nil
}

// slashCredits opens the credits panel.
//
// 【一手】 agy 1.4.8, live: `/credits` opens the panel — on the picker path
// and on the direct path alike (the composer holding "/credits" with the
// picker dismissed). The panel's own text is
//
//	Remaining AI Credits: AI Credits not enabled (enable in /settings)
//
// while the account carries no enabled G1 credits.
//
// The clone used to answer the command with an error line ("no credits info
// found"), which is both the wrong surface and a wrong claim: the account's
// G1 credits are readable (see internal/printmode/print_slash_credits.go).
func (m *runMode) slashCredits(args string) (bool, tea.Cmd) {
	m.openSlashPanel("credits")
	return true, nil
}

func (m *runMode) slashDiff(args string) (bool, tea.Cmd) {
	m.slashRespond("/diff", diffFeedback(m.session.cwd, m.viewport.width))
	return true, nil
}

func (m *runMode) slashEffort(args string) (bool, tea.Cmd) {
	if trimmed := slashparse.Trim(args); trimmed != "" {
		level, ok := printmode.ParseEffortWord(trimmed)
		if !ok {
			m.slashRespond("/effort", models.InvalidEffortMessage(trimmed))
			return true, nil
		}
		return true, m.applyEffortLevel(level, effortpanel.EffortIndexForName(level))
	}
	if m.session.flags != nil {
		m.slashContent(printmode.PrintSlashEffort(m.session.flags))
	} else {
		m.slashContent("high")
	}
	return true, nil
}

func (m *runMode) slashFeedback(args string) (bool, tea.Cmd) {
	hostactions.HandleFeedback(m.hostActionSeams(), args)
	return true, nil
}

func (m *runMode) slashFeatureDiff(args string) (bool, tea.Cmd) {
	return true, nil
}

func (m *runMode) slashFork(args string) (bool, tea.Cmd) {
	m.abortTurnForLocalCommand()
	// Goal is cancelled on fork point
	m.session.activeGoal = nil

	projectID := strings.TrimSpace(args)
	agentDir := confhome.Antigravity()
	var oldArtifactDir string
	if m.session.conversationID != "" {
		oldArtifactDir = filepath.Join(agentDir, "brain", m.session.conversationID)
	}

	forkID, forkNotice, forkMsg, err := convstore.ForkConversationFull(
		convstore.BuildSteps(m.trajectoryFacts()),
		m.session.conversationID,
		oldArtifactDir,
		"", // automatically derived as <agentDir>/brain/<forkID>
		projectID,
		true, // droppedSubagents
		true, // canceledTasks
	)
	if err != nil {
		m.slashRespond("/fork", "  Failed to fork: "+err.Error())
		return true, nil
	}

	lines := strings.Split(forkMsg, "\n")
	var extra []string
	for _, l := range lines[1:] {
		extra = append(extra, panelGray238+l+panelGray238C)
	}
	m.slashRespond("/fork", lines[0], extra...)
	if forkID != "" {
		m.session.conversationID = forkID
		if forkNotice != "" {
			noticeMsg := backend.Message{
				Role:    "user",
				Content: forkNotice,
			}
			m.engine.session = append(m.engine.session, noticeMsg)
			if m.session.supervisor != nil {
				m.session.supervisor.Append(noticeMsg)
			}
			m.saveSession()
		}
	}
	return true, nil
}

func (m *runMode) slashHooks(args string) (bool, tea.Cmd) {
	return true, nil
}

func (m *runMode) slashLogout(args string) (bool, tea.Cmd) {
	// Cycle multi-account-switch: a sign-out is a real sign-out. The
	// account leaves the registry as well, so /accounts cannot switch
	// back to a credential the user threw away. The reply line is the
	// reference's, byte for byte.
	_, email := accounts.Current()
	m.slashRespond("/logout", tuilib.LogoutFeedback())
	if email != "" {
		_, _, _ = accounts.Remove(email)
	}
	return true, nil
}

func (m *runMode) slashLogin(args string) (bool, tea.Cmd) {
	if backendselect.HasOAuthToken() {
		m.slashRespond("/login", "Already signed in. Use /logout to sign out.")
		return true, nil
	}
	lm := loginui.New()
	lm.Width = m.viewport.width
	lm.Height = m.viewport.height
	m.overlays.login = lm
	return true, lm.Init()
}

func (m *runMode) slashMCP(args string) (bool, tea.Cmd) {
	cfg, err := auth.LoadMCPConfig()
	if err != nil {
		m.slashRespond("/mcp", "error: "+err.Error())
		return true, nil
	}
	body := mcpServersFeedback(cfg)
	m.slashRespond("/mcp", body)
	customize.RecordToken("mcp", body)
	return true, nil
}

func (m *runMode) slashOpen(args string) (bool, tea.Cmd) {
	notice, editorCmd := editorkit.OpenFeedback(args, settings.String("editor"), m.session.cwd, diffaction.PathExists)
	if notice != "" {
		m.slashRespond("/open", notice)
	}
	if editorCmd == nil {
		return true, nil
	}
	return true, tea.ExecProcess(editorCmd, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

func (m *runMode) slashPermissions(args string) (bool, tea.Cmd) {
	m.slashContent(printmode.PrintSlashPermissions())
	return true, nil
}

// slashPlugin implements /plugin <verb>.
//
// Cycle plugin-case-insensitive: the verb is compared with
// strings.EqualFold, so `List`, `LIST` and `list` all dispatch the
// same handler (AGY 1.3.6 changelog line 21).
//
// The verb and the rest of the line are parsed by the slash grammar's
// owner, internal/core/slashparse.
func (m *runMode) slashPlugin(args string) (bool, tea.Cmd) {
	trimmed := slashparse.Trim(args)
	if trimmed == "" {
		m.slashRespond("/plugin", "Plugins: 0 installed")
		return true, nil
	}
	verb, _ := slashparse.SplitVerb(trimmed)
	switch {
	case strings.EqualFold(verb, "list"):
		m.slashRespond("/plugin list", "No plugins installed.")
	case strings.EqualFold(verb, "install"):
		m.slashRespond("/plugin install", "Usage: /plugin install <target>")
	case strings.EqualFold(verb, "validate"):
		m.slashRespond("/plugin validate", "Usage: /plugin validate [path]")
	case strings.EqualFold(verb, "enable"), strings.EqualFold(verb, "disable"):
		m.slashRespond("/plugin "+verb, "Usage: /plugin "+verb+" <name>")
	case strings.EqualFold(verb, "uninstall"):
		m.slashRespond("/plugin uninstall", "Usage: /plugin uninstall <name>")
	default:
		// Cycle 383 (A-28): the reference's own format string is
		// "Unknown subcommand %q." — the verb is quoted and the
		// sentence ends with a period. Evidence 【strings】: the agy
		// 1.4.11 string table carries the literal.
		m.slashRespond("/plugin", fmt.Sprintf("Unknown subcommand %q.", verb))
	}
	return true, nil
}

func (m *runMode) slashProjects(args string) (bool, tea.Cmd) {
	if m.openSlashPanel("projects") {
		return true, nil
	}
	m.slashRespond("/projects", "No projects found.")
	return true, nil
}

func (m *runMode) slashRemoteControl(args string) (bool, tea.Cmd) {
	convID := m.activeRemoteConversationID()
	return m.handleRemoteControl(args, convID)
}

func (m *runMode) activeRemoteConversationID() string {
	if m == nil || m.session.conversationID == "" {
		return ""
	}
	if m.session.conversationStarted || (m.session.supervisor != nil && m.session.supervisor.Len() > 0) || len(m.engine.session) > 0 || len(m.engine.trajLoaded) > 0 || m.session.conversationID != m.session.initialConversationID {
		return m.session.conversationID
	}
	return ""
}

func (m *runMode) handleRemoteControl(args, conversationID string) (bool, tea.Cmd) {
	if auth.IsBusinessAccount() {
		m.slashRespond("/remote-control", rchost.SlashADCErrorMessage())
		return true, nil
	}
	next, msg := tuilib.RemoteControlFeedback(args, m.session.remoteControl)
	m.session.remoteControl = next
	if msg == "Remote control on for this session." {
		if curTok, curEmail := accounts.Current(); curTok != nil && curEmail != "" {
			_ = auth.SaveTokenUnified(curTok)
		}
		hostname := persistentrc.ResolveInstanceName("")
		if hostname == "" {
			hn, err := os.Hostname()
			if err == nil && hn != "" {
				hostname = hn
			}
		}
		if hostname == "" {
			hostname = "localhost"
		}
		// The browser URL must name the id the daemon *registers*
		// with the remote service. A random uuid was never
		// registered, so the cloud had no row for it at all and the
		// companion app answered "Instance unavailable — No instance
		// found."
		//
		// 【一手】 the reference's id is "<uuid>-v2" (the
		// "remote-control-" in its log lines is the session's tag)
		// and the companion app lives on antigravity.google.com
		// ("https://antigravity.google.com/r/<id>" loads the remote
		// view; "https://antigravity.google/r/<id>" answers 404).
		//
		// The instance cannot know the browser's account index: the
		// cloud's registry rows are per Google account and the bare
		// URL resolves against the browser's *default* one, so with
		// several accounts signed in the cloud answers "No instance
		// found. Try switching to a different account." The link the
		// clone prints therefore goes through Google's account
		// chooser, which names the account the daemon registered with
		// and then continues to the deep link. See
		// docs/cycles/rc-cloud-remote-view/deliverable.md §7.4.
		if m.session.remoteControlUUID == "" {
			m.session.remoteControlUUID = persistentrc.InstallationUUID()
		}
		persistentrc.EnsureInstanceID()
		desktop := m.session.remoteControlDesktop || (m.session.flags != nil && cliflags.BoolValue(m.session.flags, "desktop"))

		// Check if we are running in attached / external engine mode (e.g. ACP / custom agent)
		// If m.engine.attachedEngine is true, run in-process without spawning external daemon!
		if m.engine.attachedEngine && m.engine.engine != nil {
			backend := rchost.NewEngineCascadeBackend(m.engine.engine, m.session.model)
			if tl, ok := m.engine.turnLock.(*rchost.SimpleTurnLock); ok && tl != nil {
				backend.SetTurnLock(tl)
			} else {
				tl := &rchost.SimpleTurnLock{}
				m.engine.turnLock = tl
				backend.SetTurnLock(tl)
			}
			ctx := context.Background()
			disp, stop, err := rchost.StartInProcessServe(ctx, backend, desktop, nil)
			if err == nil {
				m.engine.remoteControlStop = stop
				m.engine.inprocessDisp = disp
			}
		} else {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_ = persistentrc.StartOfficialDaemon(ctx, hostname, false, desktop)
			}()
		}
		_, extra := buildRemoteControlCard(conversationID, m.session.remoteControlUUID, hostname, func(u string) []string {
			return urlchunk.Rows(u, rcURLIndent, m.rcURLChunkWidth(), m.viewport.hyperlinkCaps)
		})
		if runtime.GOOS != "darwin" {
			extra = append(extra,
				"Note: this platform has no launchd to auto-start the background daemon.",
				"Run `pigasus remote-control run` in another terminal to connect.")
		}
		m.slashRespond("/remote-control", msg, extra...)
	} else {
		if m.engine.remoteControlStop != nil {
			m.engine.remoteControlStop()
			m.engine.remoteControlStop = nil
			m.engine.inprocessDisp = nil
		}
		m.slashRespond("/remote-control", msg)
	}
	return true, nil
}

func (m *runMode) slashRename(args string) (bool, tea.Cmd) {
	if strings.TrimSpace(args) == "" {
		m.openRenamePanel()
		return true, nil
	}
	next, msg := tuilib.RenameFeedback(args, m.session.conversationName)
	m.session.conversationName = next
	m.slashRespond("/rename", msg)
	return true, nil
}

func (m *runMode) slashResume(args string) (bool, tea.Cmd) {
	if err := m.overlays.resumePicker.Open(); err != nil {
		m.slashRespond("/resume", "Failed to list conversations: "+err.Error())
		return true, nil
	}
	return true, nil
}

func (m *runMode) slashRewind(args string) (bool, tea.Cmd) {
	// Cycle 381, tranche 75: the rewind policy is the session layer's
	// (internal/app/session/rewind). The tail pop used to run on a
	// copy of the slice, so the session it reported a rewind for was
	// never actually truncated; the policy now hands the truncated
	// session back and the caller keeps it.
	var r rewind.Result
	if m.session.supervisor != nil {
		r = m.session.supervisor.RewindPopTail()
		m.engine.session = r.Session
	} else {
		r = rewind.PopTail(m.engine.session)
		m.engine.session = r.Session
	}
	m.slashRespond("/rewind", rewind.TailEcho(r))
	return true, nil
}

// slashSkills handles /skills and its `reload` verb.
//
// The bare command is answered by the /skills panel (the panel owns the
// listing); this handler answers the command's argument forms.
//
// `/skills reload` re-scans the skill roots from disk and answers the
// reference's own line — "Reload skills from disk" — above the reloaded
// listing. The reload is best-effort: a root that cannot be read keeps
// the skills that were loaded from it.
//
// 【strings】 agy 1.4.x: the binary's string table carries the literal
// "Reload skills from disk" next to "/skills reload"; the wording after
// that line is unrecovered (【推测】 the listing is kept).
func (m *runMode) slashSkills(args string) (bool, tea.Cmd) {
	if skills.IsReload(args) {
		listing := skills.RenderList(skills.Reload())
		m.slashContent(skills.ReloadMessage)
		m.slashContent(listing)
		customize.RecordToken("skills", listing)
		return true, nil
	}
	body := printmode.PrintSlashSkills()
	m.slashContent(body)
	customize.RecordToken("skills", body)
	return true, nil
}

// sessionCount returns the active message count from supervisor or fallback session slice.
func (m *runMode) sessionCount() int {
	if m.session.supervisor != nil {
		return m.session.supervisor.Len()
	}
	return len(m.engine.session)
}

// slashStatusline handles /statusline and its subcommands.
//
// The reference's subcommand set (agy 1.4.8):
//
//	/statusline                 → *toggles* the statusline
//	/statusline <command>       → "Statusline set to: <command>"
//	/statusline on | enable     → "Statusline on." (" (<command>)" when one
//	                              is configured)
//	/statusline off | disable   → "Statusline off."
//	/statusline delete | reset  → "Custom statusline command cleared. …"
//	/statusline help            → the usage block
//
// The bare command is a toggle, not a usage block: 1.4.8 flips the
// `statusLine.enabled` member in settings.json and answers "Statusline
// on." when it switches the statusline on, "Statusline off. Run
// /statusline to re-enable." when it switches it off. The explicit
// `off` / `disable` subcommands answer with the short "Statusline off.".
//
// The subcommand names are matched case-insensitively — the upstream
// changelog for 1.0.1 records the fix that made the parsing
// case-insensitive.
//
// Evidence 【一手】: agy 1.4.7, driven through every subcommand; agy 1.4.8,
// driven through the bare toggle (twice, reading settings.json's
// `statusLine.enabled` after each press) and through `on`, `off`,
// `enable`, `disable` and `help`.
func (m *runMode) slashStatusline(args string) (bool, tea.Cmd) {
	// The command may have been changed outside the TUI, so the block is
	// re-read before it is answered.
	m.reloadStatusLineConfig()
	verb := strings.ToLower(strings.TrimSpace(args))
	if verb == "" {
		// Cycle 351: the bare command toggles (agy 1.4.8).
		if m.viewport.statusLineCfg.Enabled {
			m.applyStatuslineOff(tuilib.StatuslineToggledOffMessage)
		} else {
			m.applyStatuslineOn()
		}
		return true, m.maybeArmStatusLineTick()
	}
	switch verb {
	case "help":
		head, rows := tuilib.StatuslineUsageRows()
		m.slashRespond("/statusline", head, indentStatuslineUsage(rows)...)
		return true, nil
	case "on", "enable":
		m.applyStatuslineOn()
	case "off", "disable":
		m.applyStatuslineOff(tuilib.StatuslineDisabledMessage)
	case "delete", "reset":
		statusline.ClearCommand()
		m.viewport.statusLine = "on"
		m.viewport.statusLineExternal = false
		m.viewport.statusLineCommand = ""
		m.viewport.statusLineCfg = statusline.Config{}
		m.ensureExternalState().ResetDisabled()
		m.slashRespond("/statusline", tuilib.StatuslineClearedMessage)
	default:
		command := strings.TrimSpace(args)
		if err := statusline.PersistCommand(command); err != nil {
			m.slashRespond("/statusline", "Error: "+err.Error())
			return true, nil
		}
		m.viewport.statusLine = "on"
		m.viewport.statusLineExternal = true
		m.viewport.statusLineCommand = command
		// The layout members survive: only the command and the
		// visibility change.
		m.viewport.statusLineCfg.Type = "command"
		m.viewport.statusLineCfg.Command = command
		m.viewport.statusLineCfg.Enabled = true
		m.ensureExternalState().ResetDisabled()
		m.slashRespond("/statusline", tuilib.StatuslineSetMessage(command))
	}
	return true, m.maybeArmStatusLineTick()
}

// applyStatuslineOn switches the statusline on and answers with the
// reference's "Statusline on." message (" (<command>)" when a custom
// command is configured).
func (m *runMode) applyStatuslineOn() {
	cfg := m.viewport.statusLineCfg
	statusline.PersistVisibility(true)
	m.viewport.statusLine = "on"
	m.viewport.statusLineExternal = strings.TrimSpace(cfg.Command) != ""
	m.viewport.statusLineCfg.Enabled = true
	m.slashRespond("/statusline", tuilib.StatuslineEnabledMessage(cfg.Command))
}

// applyStatuslineOff switches the statusline off and answers with msg —
// the short "Statusline off." for the explicit subcommand, the long
// "Statusline off. Run /statusline to re-enable." for the bare toggle.
func (m *runMode) applyStatuslineOff(msg string) {
	statusline.PersistVisibility(false)
	m.viewport.statusLine = "on"
	m.viewport.statusLineExternal = false
	m.viewport.statusLineCfg.Enabled = false
	m.slashRespond("/statusline", msg)
}

// indentStatuslineUsage indents the usage block's rows so they line up
// under the `⎿` line's message.
func indentStatuslineUsage(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, "     "+row)
	}
	return out
}

// slashTasks handles /tasks.
func (m *runMode) slashTasks(args string) (bool, tea.Cmd) {
	var states []string
	if m.engine.taskReg != nil {
		for _, t := range m.engine.taskReg.List() {
			states = append(states, string(t.State))
		}
	}
	if len(states) == 0 {
		eng := m.turnEngine()
		if eng != nil {
			name, caps := agentturn.CapabilitiesOf(eng)
			if caps&agentturn.CapTasks != 0 && name != "pigasus" {
				token := "/tasks"
				if args != "" {
					token += " " + args
				}
				return true, m.sendAsPrompt(token, token)
			}
		}
	}
	running, done := slashactions.TasksCount(states)
	m.slashRespond("/tasks", slashactions.TasksFeedback(running, done))
	return true, nil
}

// slashTitle handles /title, /title on and /title off.
//
// The reference accepts `on`/`enable` and `off`/`disable`
// (case-insensitively); a bare `/title` toggles the feature, and anything
// else is answered with the usage line.
//
// Turning the title on needs a `title` block in settings.json; without one
// the reference reports that the command is missing — and it reports that
// for every verb, the toggle included.
//
// Evidence 【一手】: agy 1.4.7, driven through every verb:
//   - `/title` with the title on → "Custom title disabled."
//   - `/title` with the title off → "Custom title enabled. (<command>)"
//   - `/title bogus` → "Usage: /title [on|off]"
//   - `/title` without a command → the "No title command configured" error
func (m *runMode) slashTitle(args string) (bool, tea.Cmd) {
	// The block may have been changed outside the TUI, so it is re-read
	// before it is answered.
	m.reloadStatusLineConfig()
	verb := slashactions.ParseTitleVerb(args)
	if verb == slashactions.TitleVerbInvalid {
		m.slashRespond("/title", tuilib.TitleUsage)
		return true, nil
	}
	cfg := m.viewport.titleCfg
	if strings.TrimSpace(cfg.Command) == "" {
		m.slashRespond("/title", tuilib.TitleNoCommandMessage)
		return true, nil
	}
	targetState := slashactions.ResolveTitleToggle(verb, cfg.Enabled)
	return true, m.setTitleEnabled(targetState, cfg.Command)
}

// setTitleEnabled persists the title block's visibility and answers the
// command.
func (m *runMode) setTitleEnabled(enabled bool, command string) tea.Cmd {
	if err := statusline.PersistTitleEnabled(enabled); err != nil {
		m.slashRespond("/title", "Error: "+err.Error())
		return nil
	}
	m.viewport.titleCfg.Enabled = enabled
	if enabled {
		m.viewport.title = "on"
		m.slashRespond("/title", tuilib.TitleEnabledMessage(command))
	} else {
		m.viewport.title = "off"
		m.slashRespond("/title", tuilib.TitleDisabledMessage)
	}
	return m.maybeArmStatusLineTick()
}

func (m *runMode) slashUsage(args string) (bool, tea.Cmd) {
	m.slashContent("error: /usage failed: retrieving quota summary: failed to retrieve user quota summary: no quota summary is available for this account")
	return true, nil
}

func (m *runMode) slashVoice(args string) (bool, tea.Cmd) {
	return true, m.viewport.voiceSurface.Start("/voice")
}

func (m *runMode) slashAddDir(args string) (bool, tea.Cmd) {
	m.slashRespond("/add-dir", tuilib.AddDirFeedback(args))
	return true, nil
}

func (m *runMode) slashAgents(args string) (bool, tea.Cmd) {
	eng := m.turnEngine()
	if eng != nil {
		name, caps := agentturn.CapabilitiesOf(eng)
		if caps&agentturn.CapAgents != 0 && name != "pigasus" {
			token := "/agents"
			if args != "" {
				token += " " + args
			}
			return true, m.sendAsPrompt(token, token)
		}
	}
	return true, nil
}

func (m *runMode) slashArtifact(args string) (bool, tea.Cmd) {
	m.slashRespond("/artifact", "No artifacts")
	return true, nil
}

func (m *runMode) slashBtw(args string) (bool, tea.Cmd) {
	return true, m.runBtwSideQuestion(args)
}

func (m *runMode) slashChangelog(args string) (bool, tea.Cmd) {
	m.slashContent(changelog.Markdown)
	return true, nil
}

func (m *runMode) slashCodesearch(args string) (bool, tea.Cmd) {
	if strings.TrimSpace(args) == "" {
		m.slashRespond("/codesearch", codesearchEmptyMessage)
		return true, nil
	}
	m.slashRespond("/codesearch", codesearchaction.Feedback(args, m.session.cwd))
	return true, nil
}

func (m *runMode) slashQuote(args string) (bool, tea.Cmd) {
	return m.handleQuote(args)
}

func (m *runMode) slashCompact(args string) (bool, tea.Cmd) {
	if m.engine.streaming {
		m.viewport.inputNotice = "Nothing to compact yet."
		return true, nil
	}
	eng := m.turnEngine()
	if handler, ok := eng.(agentturn.ActionHandler); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		res, err := handler.ExecuteAction(ctx, "compact", args)
		cancel()
		if err == nil && res.Success && res.Message != "" && res.Message != "Action processed by local engine" {
			m.slashRespond("/compact", res.Message)
			return true, nil
		}
	}
	instructions := slashparse.Trim(args)
	m.startManualCompact(instructions)
	return true, nil
}

// slashBrowser runs the browser agent.
//
// Cycle 342: the command used to print a hard-coded quota block, because
// the reference's capture of it was taken while the reference account's
// quota was exhausted — the capture held the failure, and the clone
// reproduced it for every user. The command now does the work the
// reference's own description names: "Invoke a browser agent for web
// tasks." (see slash_browser.go).
func (m *runMode) slashBrowser(args string) (bool, tea.Cmd) {
	return m.runBrowserCommand(args)
}

// slashSchedule sends the instruction to the model, which registers the
// schedule through the `schedule` tool.
//
// Cycle 342: the command used to print the same hard-coded quota block
// /browser printed. The reference's capture of /schedule shows model
// output under the command, so the command is a prompt-sending one —
// like /plan and /goal — and the clone now sends one.
func (m *runMode) slashSchedule(args string) (bool, tea.Cmd) {
	instruction := strings.TrimSpace(args)
	if instruction == "" {
		m.slashRespond("/schedule", "Usage: /schedule <instruction>")
		return true, nil
	}
	return true, m.sendAsPrompt("/schedule", instruction+"\n\n"+slashactions.SchedulePrompt())
}

func (m *runMode) slashBranch(args string) (bool, tea.Cmd) {
	m.abortTurnForLocalCommand()
	runIsolatedSlash(m, "/branch", args, isolation.BranchFeedback)
	return true, nil
}

func (m *runMode) slashNarration(args string) (bool, tea.Cmd) {
	return m.handleNarrationSlash(args)
}

func (m *runMode) slashShare(args string) (bool, tea.Cmd) {
	runIsolatedSlash(m, "/share", args, isolation.ShareFeedback)
	return true, nil
}

// ---- slash_helpers.go ----

// slash_helpers.go — cycle 167 helpers that close the cycle-77 stub
// bodies. Each function does the smallest useful thing for the
// command that invokes it; the references for the recovered messages
// are inline so future cycles know where to tighten them.

const keybindingsFileName = "keybindings.json"

func resolveKeybindingsEditor(configuredEditor string) (name string, args []string, source string, ok bool) {
	configuredEditor = slashparse.Trim(configuredEditor)
	if configuredEditor != "" && !strings.EqualFold(configuredEditor, "auto") {
		name, args = editorkit.SplitCommand(configuredEditor)
		return name, args, "settings.editor", true
	}
	if e := slashparse.Trim(os.Getenv("EDITOR")); e != "" {
		name, args = editorkit.SplitCommand(e)
		return name, args, "$EDITOR", true
	}
	if v := slashparse.Trim(os.Getenv("VISUAL")); v != "" {
		name, args = editorkit.SplitCommand(v)
		return name, args, "$VISUAL", true
	}
	return "", nil, "", false
}

const noEditorConfiguredMessage = "No editor configured: set $EDITOR or $VISUAL"

func openKeybindingsFile(configuredEditor string) (notice string, cmd *exec.Cmd, errMsg string) {
	home := confhome.Antigravity()
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", nil, "Failed to create " + home + ": " + err.Error()
	}
	path := filepath.Join(home, keybindingsFileName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			return "", nil, "Failed to create " + path + ": " + err.Error()
		}
		notice = "Created " + path
	} else if err != nil {
		return "", nil, "Failed to stat " + path + ": " + err.Error()
	}
	name, args, _, ok := resolveKeybindingsEditor(configuredEditor)
	if !ok {
		return notice, nil, noEditorConfiguredMessage
	}
	if _, err := exec.LookPath(name); err != nil {
		return notice, nil, "Failed to resolve editor: " + err.Error()
	}
	return notice, exec.Command(name, append(args, path)...), ""
}

// slashKeybindings reports the configured keybindings.
//
// The report is read-only: the file is read and never written, so the
// command can never re-bind the exit action.
//
// The editor path is a separate, explicit verb ("/keybindings edit"):
// the reference opens the file in $EDITOR (official-docs/settings.md:
// "/keybindings opens the file in your editor"), but the command's own
// answer is the report — the two are different surfaces, and a command
// that silently opens an editor cannot also answer a question.
func (m *runMode) slashKeybindings(args string) (bool, tea.Cmd) {
	if strings.EqualFold(slashparse.Trim(args), "edit") {
		return m.slashKeybindingsEdit()
	}
	path := filepath.Join(confhome.Antigravity(), keybindingsFileName)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		m.slashRespond("/keybindings", "Failed to read "+path+": "+err.Error())
		return true, nil
	}
	report, err := keybindings.Report(path, data)
	if err != nil {
		m.slashRespond("/keybindings", err.Error())
		return true, nil
	}
	m.slashContent(report)
	return true, nil
}

// slashKeybindingsEdit opens the keybindings file in the configured
// editor.
//
// The file is created with an empty object when it does not exist — the
// reference creates it only when the user asks for the customization
// command 【strings】.
func (m *runMode) slashKeybindingsEdit() (bool, tea.Cmd) {
	notice, editorCmd, errMsg := openKeybindingsFile(settings.String("editor"))
	if errMsg != "" {
		m.slashRespond("/keybindings", errMsg)
		return true, nil
	}
	if notice != "" {
		m.slashRespond("/keybindings", notice)
	}
	if editorCmd == nil {
		return true, nil
	}
	return true, tea.ExecProcess(editorCmd, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

// runIsolatedSlash wires the /branch and /share slash-command
// handlers to the isolation sandbox.
func runIsolatedSlash(m *runMode, cmd, args string, feedback isolation.FeedbackFn) {
	if m == nil || feedback == nil {
		return
	}
	parentCwd := m.session.cwd
	if parentCwd == "" {
		parentCwd = defaultWorkspaceForRunMode()
	}
	iw := isolation.New()
	iw.Capture(parentCwd, isolation.EnvSnapshot(os.Environ()))
	out := iw.Run(func(sandbox *isolation.Workspace) {
		sandbox.CaptureOutput(feedback(args, parentCwd)...)
		sandbox.RegisterSkill("branch-isolate:helper")
		sandbox.RegisterAgent("branch-isolate:scout")
	})
	if len(out) > 0 {
		m.slashRespond(cmd, out[0], out[1:]...)
	}
}

type jsonSkillInfo = skillsui.SkillInfo

var printSlashSkillInfos = skills.Discover

// slashSkillCatalog is the source of the invocable-skill list. It is a var
// rather than a direct call so tests can substitute a synthetic catalog
// without depending on the host's ~/.gemini tree. Production always uses
// printSlashSkillInfos (print_slash.go) — the same enumerator /skills uses.
var slashSkillCatalog = printSlashSkillInfos

// slashSkillLookup resolves name against the skill catalog. It returns the
// winning skill and true, or a zero value and false. Names are compared
// verbatim (case-sensitive); when two skills share an alias the
// alphabetically-first (catalog order) wins.
func slashSkillLookup(name string) (jsonSkillInfo, bool) {
	if name == "" {
		return jsonSkillInfo{}, false
	}
	for _, s := range slashSkillCatalog() {
		for _, n := range printmode.SkillInvocationNames(s) {
			if n == name {
				return s, true
			}
		}
	}
	return jsonSkillInfo{}, false
}

// ════════════════════════════════════════════════════════════════════════
// cycle 203 — the /skills picker
// ════════════════════════════════════════════════════════════════════════
//
// The /skills panel (slash_panel_routing.go) used to hardcode "63 skills", one
// group header, eleven invented rows and a fabricated description for
// every entry ("Skill installed under ~/.gemini/skills/<name>"). Cycle 203
// rebuilds the panel on the real catalogue — slashSkillCatalog, i.e.
// printSlashSkillInfos (print_slash.go), the same enumerator that already
// byte-matches `agy -p /skills` — plus the reference layout recovered from
// the caps/ref.h*.raw.txt probes (agy 1.4.0, 220 wide):
//
//   - bold "Skills" title; a "<N> skills" summary with no indent (N is the
//     catalogue size); a blank row; then the fixed "Create new skills"
//     block (Workspace = <logical cwd>, Global / Shared literal);
//   - one group per catalogue root — "Shared skills" (~/.gemini/skills),
//     the builtin bundle (absolute path · "Built-in"), then every plugin
//     group in plugin-name order (absolute path · "From <plugin dir>");
//   - a blank row before every group header except the first rendered;
//   - per-skill rows "  <name>: <desc>", the cursor row using "› " inside
//     the blue name span, the description truncated to width-1 columns
//     with an ASCII "..." (a builtin row reserves a trailing
//     " · built-in");
//   - a "  [a-b of N items]" counter (dim gray 241) only while the window
//     is shorter than the item list; N counts the group headers as items;
//   - the standard "Keyboard: ↑/↓ Navigate  enter View  esc Cancel" legend.
//
// Everything below is measured, not guessed:
//
//	visible items  n = height*3/4 - 11 (integer division), floor 1
//	                 (h=50→26, 24→7, 20→4, 30→11, 98→62, h≥99→63=all)
//	cursor         indexes skills only (headers are unselectable) and
//	                 ranges over -1 (no selection — the initial state) ..
//	                 len(skills)-1; ↓ from -1 → 0, and both ends clamp
//	                 (↑ on the first skill stays put, it does not clear)
//	window start   max(0, cursorItem-(n-1)); the counter follows it
//	truncation     whole rendered line ≤ width-1 columns, mid-word cut

// The picker's model, the rows, the cursor, the detail view and the keys
// all live in internal/ui/views/skillsui (cycle 381). The host keeps only
// the catalogue the panel reads — slashSkillCatalog — and the seam file
// skills_host.go.

// ---- slash_compact.go ----

// slash_compact.go — cycle 229 seam between the manual /compact
// slash command and PiG's coding.Session.Compact(), extended by
// cycle 237 with a generic summarizer path.
//
// Cycle 229 closes the PiG loop: the manual /compact slash command
// fires PiG's Compact(ctx, customInstructions), listens for the
// matching CompactionEndEvent, and surfaces the upstream token counts
// in the transcript. AGY 1.4.0 builds on the same API — strings like
// `summaryCache` and `cascade_config.checkpoint_config.max_token_limit`
// (recovered in docs/research/agy-auto-compact-vs-pig.md) are the
// upstream knobs PiG already exposes through Session.Compact.
//
// Cycle 237 closes the AGY 1.4.2 gap: the reference binary shipped
// `/compact <message>` (summarize, then answer the user's appended
// question) and a "Compacting conversation..." progress label with
// Esc-cancel. The cycle-229 PiG seam covers the production runtime;
// cycle 237 adds a parallel path that does not require a pigrt
// runtime, so offline tests / print mode / direct handleSlashCommand
// callers see the cycle-237 "Compacted N messages (-Y%)" echo
// instead of the cycle-167 half-trim stub.
//
// The seam is `manualCompactor` — a three-method interface so the
// production wiring and the test wiring (a fakeCompactor that
// records calls and replays synthetic events) compose in the same
// handleSlashCommand path. runMode holds one compactor; cycle 227
// populates it when the pigrt.Runtime TUI integration lands, and
// cycle 229 leaves it nil for callers that have not wired PiG yet
// (print mode, offline tests). Cycle 237 falls back to
// internal/compaction.ComputeSummary when the compactor is nil — the
// cycle-167 half-trim helper is retired.
//
// Cycle 381 (tranche 27) removed the *pigrt.Runtime adapter this
// file used to carry. It had no caller and no test: the TUI's turn
// engine is PiG's agent.Agent, not pigrt.Runtime, so the host had no
// runtime for it to wrap, and the manual /compact path takes the
// generic summarizer branch in production (m.engine.compactor is nil; only
// the tests set it). The interface and the branch stay — they are
// the seam — but the unreachable implementation is gone.
//
// Cycle 237 deliberately does NOT touch internal/pigrt/runtime.go,
// internal/agent/pig.go, or cmd/wagyu/tui_run.go (cycle 232's
// territory). The Esc-cancel hook uses a package-level channel
// (compactCancel) so the TUI's existing Esc handler can fire it
// from a future cycle without this file changing again — see the
// 硬約束 section of the cycle-237 deliverable for the trade-off.

// manualCompactor is aliased to compaction.ManualCompactor.
type manualCompactor = compaction.ManualCompactor

// startManualCompact is the /compact slash-command body. It is on
// runMode (not a free function) because the transcript rows live on
// m.engine.response, and the cycle-229 spec calls for two consecutive rows
// ("Compacting context…" + "Compacted: …") styled as a single
// bracketed event.
//
// Cycle 237 split the old "Branch 1" (cycle-167 stub) into a real
// summarizer that calls internal/compaction.ComputeSummary
// directly. The cycle-167 half-trim of m.engine.session is gone; the new
// path replaces m.engine.session with one user-role message carrying the
// model's reply (matching AGY 1.4.2's "the transcript becomes the
// summary" contract).
//
// The method has four branches:
//
//   - m.engine.compactor == nil: cycle-237 generic summarizer. Fires
//     compaction.ComputeSummary in a goroutine with a cancellable
//     context (cycle-237's local channel). The transcript gets
//     "Compacting conversation…" (matching the AGY 1.4.2 spec)
//     and a follow-up "Compacted N messages (-Y%)" / "[compact
//     cancelled]" row when the goroutine resolves. A trailing
//     user-supplied customInstructions (the "/compact <message>"
//     form) is dispatched as a fresh prompt after the summary
//     lands.
//
//   - m.engine.compactor != nil && m.engine.compactor.IsCompacting() == true:
//     short-circuit with "⎿  Already compacting." — the cycle-229
//     spec calls out the race where a user types /compact while
//     PiG is mid-summarisation (auto-compact from cycle 227 OR a
//     second /compact press before the first finishes). PiG
//     itself would abort the in-flight compact and start a new
//     one; the gate keeps the transcript honest.
//
//   - happy path (cycle-229): emit "⎿  Compacting context…" +
//     custom-instructions echo, subscribe a one-shot listener on
//     the compactor, and fire Compact(ctx, customInstructions) in
//     a goroutine. The goroutine posts a compactDoneMsg back to
//     the bubbletea runtime so the transcript gets the success /
//     failure row.
//
//   - happy path (cycle-237 fallback): emit "⎿  Compacting
//     conversation…" + custom-instructions echo, run the generic
//     summarizer with a 60 s timeout, replace m.engine.session with the
//     summary, and echo the stats. The same cancel channel the
//     Esc handler will reach from a future cycle (cycle 237 left
//     it as a package-level singleton — see compactCancel below)
//     aborts the summarization without touching m.session.
//
// The Compact() / ComputeSummary calls use a 60 s timeout — the
// same window the TUI prompt stream uses (streamPrompt in
// tui_run.go). Long compactions that exceed it are aborted at
// the goroutine level; PiG's own compactCancel path
// (coding/session.go:compactCancel) takes over from there.
func (m *runMode) startManualCompact(customInstructions string) {
	// Branch 1 (cycle-237): no PiG compactor wired. Use the generic
	// summarizer. The cycle-167 half-trim is retired — the new path
	// replaces m.engine.session with one user-role message carrying the
	// model's reply, matching AGY 1.4.2's "transcript becomes the
	// summary" contract.
	if m.engine.compactor == nil {
		m.startGenericCompact(customInstructions)
		return
	}

	// Branch 2: an auto-compact (cycle 227) or a manual press from
	// a moment ago is still running. PiG's coding.Session would
	// queue / abort-and-restart, but the cycle-229 spec is explicit:
	// echo "Already compacting." and skip the call so the user
	// sees a clear "I heard you, but I am already busy" line.
	if m.engine.compactor.IsCompacting() {
		m.engine.response = append(m.engine.response,
			rows.NoticeCompact("Already compacting."),
			"",
		)
		return
	}

	// Branch 3: cycle-229 happy path. Echo the running row first so
	// the transcript shows the command was accepted, then arm the
	// one-shot listener and kick off Compact() in the background.
	//
	// `m.engine.prog` is the running *tea.Program; nil in tests, in which
	// case the result is delivered straight into m.engine.response (the
	// goroutine skips the prog.Send call). The split is the same
	// shape cycle 227's compactStartMsg / compactEndMsg handlers
	// use — cycle 229's manual path just collapses the start/end
	// pair into a single two-row transcript block.
	m.engine.response = append(m.engine.response,
		rows.NoticeCompact("Compacting context\u2026"),
		"",
	)
	compactor := m.engine.compactor
	prog := m.engine.prog
	go runManualCompactionTask(compactor, customInstructions, prog)
}

func runManualCompactionTask(compactor manualCompactor, customInstructions string, prog *tea.Program) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	compaction.RunManualTask(ctx, compactor, customInstructions, func(msg compactDoneMsg) {
		if prog != nil {
			prog.Send(msg)
		}
	})
}

// startGenericCompact is the cycle-237 path: no PiG compactor, so
// the slash command drives internal/compaction.ComputeSummary
// directly. The transcript gets:
//
//	⎿  Compacting conversation…                       (running row, cycle-237)
//	⎿  Compacted 12 messages (-87%). [follow-up row]  (success)
//	or
//	⎿  Compaction cancelled. Conversation was not compacted. (Esc / timeout)
//
// The session is replaced with one user-role message carrying the
// summary, so the next model turn sees only the summary in its
// history. The original session is preserved verbatim until the
// goroutine resolves, so a cancellation does not clobber the
// user's history (TestSlashCompact_CancelRestoresSession).
type genericCompactDoneMsg = compaction.GenericDoneMsg

func runGenericCompactionTask(backendClient backend.Client, model, systemPrompt string, overhead int, sessSnap []backend.Message, customInstructions string, started time.Time, prog *tea.Program, onDone func(genericCompactDoneMsg)) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cleanup := compaction.InstallCancel(cancel)
	defer cleanup()

	compaction.RunGenericTask(ctx, backendClient, model, systemPrompt, overhead, sessSnap, customInstructions, started, func(msg genericCompactDoneMsg) {
		if prog != nil {
			prog.Send(msg)
		} else if onDone != nil {
			onDone(msg)
		}
	})
}

func (m *runMode) startGenericCompact(customInstructions string) {
	if len(m.engine.session) == 0 && (m.session.supervisor == nil || m.session.supervisor.Len() == 0) {
		m.slashRespondPlain("Nothing to compact yet.")
		return
	}

	var sessSnap []backend.Message
	if m.session.supervisor != nil && m.session.supervisor.Len() > 0 {
		sessSnap = m.session.supervisor.Messages()
	} else {
		sessSnap = make([]backend.Message, len(m.engine.session))
		copy(sessSnap, m.engine.session)
	}

	m.engine.response = append(m.engine.response,
		rows.NoticeCompact("Compacting conversation\u2026"),
		"",
	)

	backendClient := m.engine.backend
	model := m.session.model
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	prog := m.engine.prog
	started := time.Now()

	systemPrompt := m.systemPrompt()
	overhead := compaction.ContextOverheadTokens(systemPrompt, modelDeclarationsJSON())

	if prog != nil {
		// Production: the result travels as a tea.Msg and is applied by
		// the Update loop, on the UI goroutine.
		go runGenericCompactionTask(backendClient, model, systemPrompt, overhead, sessSnap, customInstructions, started, prog, nil)
		return
	}
	// No program to post to (the test path). Hand the result over a
	// channel instead: the worker never touches m.engine.response or m.engine.session,
	// which belong to the goroutine that owns the runMode.
	ch := make(chan genericCompactDoneMsg, 1)
	m.engine.compactDone = ch
	go runGenericCompactionTask(backendClient, model, systemPrompt, overhead, sessSnap, customInstructions, started, nil, func(msg genericCompactDoneMsg) {
		ch <- msg
	})
}

func modelDeclarationsJSON() []byte {
	decls := surface.ModelDeclarations()
	b, err := json.Marshal(decls)
	if err != nil {
		return nil
	}
	return b
}

func (m *runMode) applyGenericCompactSuccess(msg genericCompactDoneMsg, customInstructions string) tea.Cmd {
	// What the summary carries into the next prompt is counted beside
	// the summary in the "after" figure — the earlier requests and the
	// snapshots the reference's checkpoint carries. The clone's resume
	// block carries the requests; it has no artifact, subagent or
	// running-task snapshots.
	carriedTokens := 0
	if msg.Summary != "" {
		requests := compaction.UserRequests(m.engine.session, compaction.MaxUserRequests)
		carriedTokens = compaction.EstimateTokens(compaction.CarriedBlock(requests))
		m.engine.session = []backend.Message{{
			Role:    "user",
			Content: compaction.ResumingBlock(requests, msg.Summary),
		}}
		if m.session.supervisor != nil {
			m.session.supervisor.Replace(m.engine.session)
		}
	}
	beforeTokens, afterTokens, summaryTokens := compaction.ComputeCompactTokens(msg.StatsBefore.Tokens, msg.InputTokens, msg.StatsAfter.Tokens, msg.OutputTokens, msg.Overhead, msg.Summary, carriedTokens)
	m.engine.response = append(m.engine.response,
		compaction.DividerRow(m.viewport.width),
		compaction.StatsRow(m.viewport.width,
			msg.StatsBefore.Messages,
			beforeTokens,
			afterTokens,
			summaryTokens,
			msg.Took),
		"",
	)
	if customInstructions = strings.TrimSpace(customInstructions); customInstructions != "" {
		m.engine.session = append(m.engine.session, backend.Message{Role: "user", Content: customInstructions})
		if m.session.supervisor != nil {
			m.session.supervisor.Append(backend.Message{Role: "user", Content: customInstructions})
		}
		return m.streamPrompt(customInstructions)
	}
	return nil
}

func (m *runMode) applyGenericCompactResult(msg genericCompactDoneMsg, customInstructions string) tea.Cmd {
	switch {
	case msg.Cancelled:
		m.engine.response = append(m.engine.response,
			rows.NoticeCompact(compaction.CompactionCancelledLiteral),
			"",
		)
		return nil
	case msg.Err != nil:
		m.engine.response = append(m.engine.response,
			rows.NoticeCompact("Compact failed: "+msg.Err.Error()),
			"",
		)
		return nil
	default:
		return m.applyGenericCompactSuccess(msg, customInstructions)
	}
}

// ---- slash_btw.go ----

// slash_btw.go — cycle 230: /btw side-question card lifecycle.
//
// The clone's /btw command launches an independent PiG coding.Session
// (no parent transcript, no hooks) and renders the streamed answer as
// a floating card above the input area (aboveInputCard). /copy btw copies the
// active card's text to the system clipboard.

// newBtwManager is the hook the slash command uses to build a side-
// question Manager. Production callers go through btwManagerFor which
// binds to the runMode's backend; tests replace the variable directly.
var newBtwManager = func(opts sidequestion.Options) *sidequestion.Manager {
	return sidequestion.NewManager(opts)
}

// btwManagerFor returns (or constructs on first call) the Manager the
// runMode uses for /btw. Nil is a deliberate possibility: a runMode
// without a backend cannot stream, so we surface /btw unavailable instead
// of panicking on nil.
func (m *runMode) btwManagerFor() *sidequestion.Manager {
	if m.overlays.btwManager != nil {
		return m.overlays.btwManager
	}
	if m.engine.backend == nil {
		return nil
	}
	m.overlays.btwManager = newBtwManager(sidequestion.Options{
		StreamWithToolsFn: func(ctx context.Context, prompt string, hooks sidequestion.ToolHooks) (int, error) {
			catalog := surface.Catalog(m.session.cwd)
			allDecls := toolrunner.ToolDeclsFromRegistry(catalog)
			var safeDecls []backend.ToolDecl
			for _, d := range allDecls {
				switch d.Name {
				case "read_file", "view_content_chunk", "grep_search", "find_by_name", "file_search", "list_dir", "list_directory", "codebase_search", "search_web":
					safeDecls = append(safeDecls, d)
				}
			}
			var messages []backend.Message
			if m.session.supervisor != nil && m.session.supervisor.Len() > 0 {
				messages = m.session.supervisor.Messages()
			} else if len(m.engine.session) > 0 {
				messages = make([]backend.Message, len(m.engine.session))
				copy(messages, m.engine.session)
			}
			messages = append(messages, backend.Message{Role: "user", Content: prompt})
			tokens := 0
			for turn := 0; turn < 5; turn++ {
				streamCh, err := m.engine.backend.Stream(ctx, backend.StreamRequest{
					Model:        m.session.model,
					SystemPrompt: sidequestion.DefaultSystemPrompt,
					Messages:     messages,
					Tools:        safeDecls,
				})
				if err != nil {
					return tokens, err
				}
				var textBuf strings.Builder
				var toolCalls []backend.ToolCall
				for chunk := range streamCh {
					if chunk.Error != nil {
						return tokens, chunk.Error
					}
					if chunk.Text != "" {
						textBuf.WriteString(chunk.Text)
						hooks.OnDelta(chunk.Text)
					}
					if len(chunk.ToolCalls) > 0 {
						toolCalls = append(toolCalls, chunk.ToolCalls...)
					}
					if chunk.Meta != nil {
						tokens = chunk.Meta.TotalTokens
					}
				}
				if len(toolCalls) == 0 {
					break
				}
				messages = append(messages, backend.Message{
					Role:      "assistant",
					Content:   textBuf.String(),
					ToolCalls: toolCalls,
				})
				for _, tc := range toolCalls {
					hooks.OnToolStart(tc.Name)
					res := runBtwTool(ctx, catalog, tc)
					hooks.OnToolEnd(tc.Name)
					messages = append(messages, backend.Message{
						Role:       "tool",
						Content:    res,
						ToolCallID: tc.ID,
						Name:       tc.Name,
					})
				}
			}
			return tokens, nil
		},
		CWD:          m.session.cwd,
		AgentDir:     confhome.Antigravity(),
		SystemPrompt: sidequestion.DefaultSystemPrompt,
	})
	return m.overlays.btwManager
}

// runBtwTool executes safe read-only tools for /btw and auto-denies any tool that would ask for permission.
func runBtwTool(ctx context.Context, catalog *tools.Registry, tc backend.ToolCall) string {
	switch tc.Name {
	case "read_file", "view_content_chunk", "grep_search", "find_by_name", "file_search", "list_dir", "list_directory", "codebase_search", "search_web":
		if t, ok := catalog.Get(tc.Name); ok {
			res, err := t.Run(ctx, json.RawMessage(tc.Arguments))
			if err != nil {
				return fmt.Sprintf("Error: %v", err)
			}
			if s, ok := res.(string); ok {
				return s
			}
			b, _ := json.Marshal(res)
			return string(b)
		}
		return "Error: tool not found"
	default:
		return "Permission denied: side questions deny permissions automatically and cannot run commands or mutate files."
	}
}

// btwDoneMsg delivers the settled side-question Card or failure.
type btwDoneMsg struct {
	id   string
	card *sidequestion.Card
	err  error
}

// btwTickMsg drives the spinner frame and answer delta re-renders while
// an asynchronous /btw query is in flight.
type btwTickMsg struct{}

func btwTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return btwTickMsg{} })
}

// runBtwSideQuestion is the /btw slash-command body.
//
// In reference agy:
//   - If args is empty:
//     If history exists and no question is active, shows history card (ShowBtwHistory).
//     Else emits "Usage: /btw <question>".
//   - If a question is already active (IsBtwActive), emits:
//     "A /btw question is already in progress. Please wait for it to finish."
//   - Otherwise, dispatches the question, sets active card index, and renders above the input.
func (m *runMode) runBtwSideQuestion(args string) tea.Cmd {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		if m.ShowBtwHistory() {
			return nil
		}
		m.slashRespond("/btw", "Usage: /btw <question>")
		return nil
	}
	if m.IsBtwActive() {
		m.slashRespond("/btw", "A /btw question is already in progress. Please wait for it to finish.")
		return nil
	}
	mgr := m.btwManagerFor()
	if mgr == nil {
		m.slashRespond("/btw", "/btw is unavailable in this environment.")
		return nil
	}

	preCtx := context.Background()
	question, blocked := m.firePreSideQuestionHook(preCtx, trimmed)
	if blocked {
		return nil
	}
	trimmed = question

	ctx, cancel := context.WithCancel(context.Background())
	m.overlays.activeBtwCancel = cancel

	card, run, err := mgr.StartAsk(ctx, trimmed)
	if err != nil {
		m.overlays.activeBtwCancel = nil
		cancel()
		m.engine.response = append(m.engine.response,
			panelGray238+"btw failed: "+err.Error()+panelGray238C,
			"",
		)
		m.firePostSideQuestionHook(preCtx, trimmed, nil, err)
		return nil
	}

	// In non-interactive test mode (m.engine.prog == nil and !m.overlays.asyncBtw), execute synchronously
	if m.engine.prog == nil && !m.overlays.asyncBtw {
		m.overlays.activeBtwCancel = nil
		cancel()
		if runErr := run(); runErr != nil {
			m.engine.response = append(m.engine.response,
				panelGray238+"btw failed: "+runErr.Error()+panelGray238C,
				"",
			)
			m.firePostSideQuestionHook(preCtx, trimmed, nil, runErr)
			return nil
		}
		m.overlays.btwCards = append(m.overlays.btwCards, card)
		m.overlays.activeBtwIndex = len(m.overlays.btwCards) - 1
		m.overlays.activeBtwID = card.ID
		m.overlays.btwScroll = 0
		m.firePostSideQuestionHook(preCtx, trimmed, card, nil)
		return nil
	}

	m.overlays.btwCards = append(m.overlays.btwCards, card)
	m.overlays.activeBtwIndex = len(m.overlays.btwCards) - 1
	m.overlays.activeBtwID = card.ID
	m.overlays.btwScroll = 0

	askCmd := func() tea.Msg {
		runErr := run()
		return btwDoneMsg{id: card.ID, card: card, err: runErr}
	}
	return tea.Batch(askCmd, btwTick())
}

func (m *runMode) updateBtwDoneMsg(msg btwDoneMsg) (tea.Model, tea.Cmd) {
	if m.overlays.activeBtwCancel != nil {
		m.overlays.activeBtwCancel = nil
	}
	if msg.err != nil {
		m.firePostSideQuestionHook(context.Background(), msg.card.Question, nil, msg.err)
	} else {
		m.firePostSideQuestionHook(context.Background(), msg.card.Question, msg.card, nil)
	}
	return m, nil
}

func (m *runMode) firePreSideQuestionHook(preCtx context.Context, trimmed string) (string, bool) {
	if m.session.hooks == nil {
		return trimmed, false
	}
	pre := hooks.SideQuestion(hooks.PreSideQuestion, trimmed)
	blocked, final, reason := m.session.hooks.Fire(preCtx, hooks.PreSideQuestion, pre)
	if !blocked {
		return final.Question, false
	}
	m.slashRespond("/btw", "blocked by hook: "+reason)
	post := hooks.SideQuestion(hooks.PostSideQuestion, final.Question)
	post.Blocked = true
	post.BlockReason = reason
	_, _, _ = m.session.hooks.Fire(preCtx, hooks.PostSideQuestion, post)
	return final.Question, true
}

func (m *runMode) firePostSideQuestionHook(preCtx context.Context, trimmed string, card *sidequestion.Card, err error) {
	if m.session.hooks == nil {
		return
	}
	post := hooks.SideQuestion(hooks.PostSideQuestion, trimmed)
	if err != nil {
		post.Blocked = true
		post.BlockReason = err.Error()
	} else if card != nil {
		post.Answer = card.Answer
		post.Tokens = card.Tokens
	}
	_, _, _ = m.session.hooks.Fire(preCtx, hooks.PostSideQuestion, post)
}

// copyBtwCard handles "/copy btw".
// Matches the reference binary's copyBtwAnswer (google3/third_party/jetski/cli/commands/commands.copyBtwAnswer):
//   - If no active card or len == 0: "No /btw response to copy."
//   - If active card has answer: returns answer, "Copied /btw answer to clipboard."
//   - If active card is still generating: "The /btw answer is still being generated."
//   - If active card failed: "The /btw question failed: <err>"
func (m *runMode) copyBtwCard() (string, string) {
	card := m.currentBtwResponse()
	if card == nil {
		return "", "No /btw response to copy."
	}
	_, answer, _, state, err := card.LockedSnapshot()
	if answer != "" {
		return answer, "Copied /btw answer to clipboard."
	}
	if state == sidequestion.StatePending || state == sidequestion.StateStreaming {
		return "", "The /btw answer is still being generated."
	}
	if state == sidequestion.StateFailed || err != nil {
		if err == nil {
			err = fmt.Errorf("unknown error")
		}
		return "", "The /btw question failed: " + err.Error()
	}
	return "", "No /btw response to copy."
}

// ShowBtwHistory reveals the most recent BTW card if history exists
// and no card is currently generating. Returns true if history was shown.
func (m *runMode) ShowBtwHistory() bool {
	if len(m.overlays.btwCards) == 0 {
		return false
	}
	if m.IsBtwActive() {
		return false
	}
	m.overlays.activeBtwIndex = len(m.overlays.btwCards) - 1
	m.overlays.activeBtwID = m.overlays.btwCards[m.overlays.activeBtwIndex].ID
	m.overlays.btwScroll = 0
	return true
}

// IsBtwActive reports whether any /btw question is currently in progress.
func (m *runMode) IsBtwActive() bool {
	for _, c := range m.overlays.btwCards {
		if c != nil && c.IsActive() {
			return true
		}
	}
	return false
}

// DismissBtwResponse dismisses the active BTW card and cancels any in-flight generation.
func (m *runMode) DismissBtwResponse() {
	if m.overlays.activeBtwCancel != nil {
		m.overlays.activeBtwCancel()
		m.overlays.activeBtwCancel = nil
	}
	if card := m.currentBtwResponse(); card != nil {
		card.MarkCancelled()
	}
	m.overlays.activeBtwIndex = -1
	m.overlays.activeBtwID = ""
	m.overlays.btwScroll = 0
}

// ToggleBtwExpanded flips the active BTW card's expansion state.
func (m *runMode) ToggleBtwExpanded() {
	card := m.currentBtwResponse()
	if card == nil {
		return
	}
	card.ToggleExpanded()
	m.overlays.btwScroll = 0
}

// NavigateBtw shifts the active BTW card index by delta (-1 or +1),
// clamping to [0, len-1].
func (m *runMode) NavigateBtw(delta int) {
	if len(m.overlays.btwCards) == 0 {
		return
	}
	idx := m.overlays.activeBtwIndex + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.overlays.btwCards) {
		idx = len(m.overlays.btwCards) - 1
	}
	m.overlays.activeBtwIndex = idx
	m.overlays.activeBtwID = m.overlays.btwCards[idx].ID
	m.overlays.btwScroll = 0
}

// btwScrollBy scrolls the active BTW card answer by delta (-1 or +1)
// when expanded. Returns true if the event was consumed.
func (m *runMode) btwScrollBy(delta int) bool {
	card := m.currentBtwResponse()
	if card == nil {
		return false
	}
	if !card.IsExpanded() {
		return false
	}
	_, answer, _, _, _ := card.LockedSnapshot()
	if answer == "" {
		return false
	}
	maxLines := sidequestion.MaxBtwAnswerLines(m.viewport.height, true)
	lines := strings.Split(answer, "\n")
	if len(lines) <= maxLines {
		return false
	}
	maxScroll := len(lines) - maxLines
	newScroll := m.overlays.btwScroll + delta
	if newScroll < 0 {
		newScroll = 0
	}
	if newScroll > maxScroll {
		newScroll = maxScroll
	}
	if newScroll == m.overlays.btwScroll {
		return true
	}
	m.overlays.btwScroll = newScroll
	return true
}

func (m *runMode) currentBtwResponse() *sidequestion.Card {
	if m.overlays.activeBtwIndex >= 0 && m.overlays.activeBtwIndex < len(m.overlays.btwCards) {
		return m.overlays.btwCards[m.overlays.activeBtwIndex]
	}
	return nil
}

func (m *runMode) isBtwCardVisible() bool {
	return m.overlays.activeBtwIndex >= 0 && m.overlays.activeBtwIndex < len(m.overlays.btwCards)
}

// renderBtwCard renders the active side-question card.
func (m *runMode) renderBtwCard() string {
	card := m.currentBtwResponse()
	if card == nil {
		return ""
	}
	return sidequestion.RenderBtwCard(card, sidequestion.CardRenderOptions{
		Width:     m.viewport.width,
		Height:    m.viewport.height,
		CardCount: len(m.overlays.btwCards),
		Scroll:    m.overlays.btwScroll,
		RenderMd:  mdrender.RenderMarkdown,
	})
}

// aboveInputCard renders the card directly above the input prompt.
func (m *runMode) aboveInputCard() string {
	if m.isBtwCardVisible() {
		return m.renderBtwCard()
	}
	return ""
}
