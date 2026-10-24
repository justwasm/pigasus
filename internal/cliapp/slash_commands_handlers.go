package cliapp

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/codesearchaction"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/customize"
	"github.com/justwasm/pigasus/internal/isolation"
	"github.com/justwasm/pigasus/internal/loginui"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/subagenttools"
)

// slash_commands_handlers.go — individual slash command action handlers.

func (m *runMode) slashClear(args string) (bool, tea.Cmd) {
	m.abortTurnForLocalCommand()
	m.response = nil
	m.session = nil
	m.blocks = nil
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.streamQueue = nil
	m.queuedPrompt = ""
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveReplyBuf = ""
	m.streamedText = false
	m.wireSettingsPending = true
	m.resetSessionTokenCounters()
	// A cleared conversation starts without the previous one's
	// subagent definitions: the definitions are conversation-scoped.
	m.subagentDefs = subagenttools.NewStore()
	m.resetTrajectory()
	return true, nil
}

func (m *runMode) slashNew(args string) (bool, tea.Cmd) {
	return m.slashClear(args)
}

func (m *runMode) slashExit(args string) (bool, tea.Cmd) {
	m.quitting = true
	return true, tea.Quit
}

func (m *runMode) slashHelp(args string) (bool, tea.Cmd) {
	m.helpMode = true
	m.helpTab = 0
	m.input.Reset()
	m.historyIdx = 0
	m.historyEdit = ""
	return true, nil
}

func (m *runMode) slashModel(args string) (bool, tea.Cmd) {
	// "/model <name>" is a direct setter — the reference changes the
	// model and echoes "Model set to <label>". Only a bare "/model" opens
	// the Switch Model panel. 【一手】 agy 1.4.6, live.
	if trimmed := trimSpace(args); trimmed != "" {
		return true, m.applyModelArgument(trimmed)
	}
	if m.flags != nil {
		m.slashContent(printmode.PrintSlashModels(m.flags))
	} else {
		m.slashContent(modelSlug(m.model) + "\t" + m.model)
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
	m.slashRespond("/context", "Context usage: session has "+strconv.Itoa(len(m.session))+" message(s)")
	return true, nil
}

func (m *runMode) slashContinue(args string) (bool, tea.Cmd) {
	id, _, err := convstore.ResolveContinue(m.cwd)
	if err != nil {
		m.slashRespond("/continue", "error: "+err.Error())
		return true, nil
	}
	if id == "" {
		m.slashRespond("/continue", "No previous conversation to continue.")
		return true, nil
	}
	sel := loadConversationSelection(id, "continue", convstore.ConversationPath(id))
	if sel.Store == nil {
		m.slashRespond("/continue", "Failed to load conversation "+id)
		return true, nil
	}
	m.conversationID = sel.ID
	m.response = nil
	m.session = nil
	m.blocks = nil
	m.liveSteps = nil
	m.activeTool = ""
	m.seedConversation(sel.Store)
	m.autoTitle = ""
	m.response = append(m.response, "  ⎿  Resumed conversation "+sel.ID)
	return true, nil
}

func (m *runMode) slashCopy(args string) (bool, tea.Cmd) {
	if strings.EqualFold(trimSpace(args), "btw") {
		payload, msg := m.copyBtwCard()
		if payload != "" {
			if err := clipboardWrite(payload); err != nil {
				m.slashRespond("/copy btw", "Failed to copy to clipboard: "+err.Error())
				return true, nil
			}
		}
		m.slashRespond("/copy btw", msg)
		return true, nil
	}
	payload, msg := resolveCopy(args, m.plannerResponses())
	if payload != "" {
		if err := clipboardWrite(payload); err != nil {
			m.slashRespond("/copy", "Failed to copy to clipboard: "+err.Error())
			return true, nil
		}
	}
	m.slashRespond("/copy", msg)
	return true, nil
}

func (m *runMode) slashCredits(args string) (bool, tea.Cmd) {
	m.slashContent("error: /credits failed: retrieving credits: no credits info found")
	return true, nil
}

func (m *runMode) slashDiff(args string) (bool, tea.Cmd) {
	m.slashRespond("/diff", diffFeedback(m.cwd, m.width))
	return true, nil
}

func (m *runMode) slashEffort(args string) (bool, tea.Cmd) {
	if trimmed := trimSpace(args); trimmed != "" {
		level, ok := printmode.ParseEffortWord(trimmed)
		if !ok {
			m.slashRespond("/effort", "Invalid level: "+trimmed+". Use low|medium|high.")
			return true, nil
		}
		return true, m.applyEffortLevel(level, effortIndexForName(level))
	}
	if m.flags != nil {
		m.slashContent(printmode.PrintSlashEffort(m.flags))
	} else {
		m.slashContent("high")
	}
	return true, nil
}

func (m *runMode) slashFeedback(args string) (bool, tea.Cmd) {
	m.slashRespond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
	return true, nil
}

func (m *runMode) slashFeatureDiff(args string) (bool, tea.Cmd) {
	return true, nil
}

func (m *runMode) slashFork(args string) (bool, tea.Cmd) {
	m.abortTurnForLocalCommand()
	forkID, forkMsg := forkTrajectory(m.trajectorySteps(), m.conversationID)
	lines := strings.Split(forkMsg, "\n")
	var extra []string
	for _, l := range lines[1:] {
		extra = append(extra, panelGray238+l+panelGray238C)
	}
	m.slashRespond("/fork", lines[0], extra...)
	if forkID != "" {
		m.conversationID = forkID
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
	m.slashRespond("/logout", logoutFeedback())
	if email != "" {
		_, _, _ = accounts.Remove(email)
	}
	return true, nil
}

func (m *runMode) slashLogin(args string) (bool, tea.Cmd) {
	if hasOAuthToken() {
		m.slashRespond("/login", "Already signed in. Use /logout to sign out.")
		return true, nil
	}
	lm := loginui.New()
	lm.Width = m.width
	lm.Height = m.height
	m.login = lm
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
	notice, editorCmd := openFeedback(args, auth.SettingsString("editor"), m.cwd)
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

func (m *runMode) slashPlugin(args string) (bool, tea.Cmd) {
	trimmed := trimSpace(args)
	if trimmed == "" {
		m.slashRespond("/plugin", "Plugins: 0 installed")
		return true, nil
	}
	verb, _ := splitPluginArgs(trimmed)
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
		m.slashRespond("/plugin", "Unknown subcommand: "+verb)
	}
	return true, nil
}

func (m *runMode) slashRemoteControl(args string) (bool, tea.Cmd) {
	if IsBusinessAccount() {
		m.slashRespond("/remote-control", rchost.SlashADCErrorMessage())
		return true, nil
	}
	next, msg := remoteControlFeedback(args, m.remoteControl)
	m.remoteControl = next
	if msg == "Remote control on for this session." {
		m.slashRespond("/remote-control", msg,
			"     Hostname: <host>",
			"     Open https://antigravity.google.com/r/<uuid>-v2 on another device to take over.")
	} else {
		m.slashRespond("/remote-control", msg)
	}
	return true, nil
}

func (m *runMode) slashRename(args string) (bool, tea.Cmd) {
	if strings.TrimSpace(args) == "" {
		m.openRenamePanel()
		return true, nil
	}
	next, msg := renameFeedback(args, m.conversationName)
	m.conversationName = next
	m.slashRespond("/rename", msg)
	return true, nil
}

func (m *runMode) slashResume(args string) (bool, tea.Cmd) {
	list, err := convstore.ListConversations()
	if err != nil {
		m.slashRespond("/resume", "Failed to list conversations: "+err.Error())
		return true, nil
	}
	if m.conversationID != "" {
		filtered := list[:0]
		for _, c := range list {
			if c.ID != m.conversationID {
				filtered = append(filtered, c)
			}
		}
		list = filtered
	}
	m.resumeList = list
	m.resumeCursor = 0
	m.resumeWindow = 0
	m.resumeQuery = ""
	m.resumeTab = 0
	m.resumeCursorOn = false
	m.resumeMode = true
	return true, nil
}

func (m *runMode) slashRewind(args string) (bool, tea.Cmd) {
	if n := rewindSession(m.session); n > 0 {
		m.slashRespond("/rewind", fmt.Sprintf("Rewound %d message(s) from the session tail", n))
	} else {
		m.slashRespond("/rewind", "Nothing to rewind.")
	}
	return true, nil
}

func (m *runMode) slashSkills(args string) (bool, tea.Cmd) {
	body := printmode.PrintSlashSkills()
	m.slashContent(body)
	customize.RecordToken("skills", body)
	return true, nil
}

func (m *runMode) slashStatusline(args string) (bool, tea.Cmd) {
	if _, ok := loadStatusLineConfig(); ok {
		switch {
		case m.statusLine == "off":
			m.statusLine = ""
			persistStatusLineVisibility(true)
			m.slashRespond("/statusline", "Statusline on.")
		case m.statusLineExternal:
			m.statusLine = "off"
			m.statusLineExternal = false
			persistStatusLineVisibility(false)
			m.slashRespond("/statusline", "Statusline off. Run /statusline to re-enable.")
		default:
			m.statusLineExternal = true
			persistStatusLineVisibility(true)
			m.slashRespond("/statusline", "Statusline on.")
		}
		return true, nil
	}
	next, msg := statuslineFeedback(m.statusLine)
	if err := auth.SetSettingsBool("statusLine", next == "on"); err == nil {
		m.statusLine = next
	}
	m.slashRespond("/statusline", msg)
	return true, nil
}

func (m *runMode) slashTasks(args string) (bool, tea.Cmd) {
	running, done := 0, 0
	if m.taskReg != nil {
		for _, t := range m.taskReg.List() {
			switch t.State {
			case "running", "pending":
				running++
			case "done", "failed", "killed":
				done++
			}
		}
	}
	m.slashRespond("/tasks", tasksFeedback(running, done))
	return true, nil
}

func (m *runMode) slashTitle(args string) (bool, tea.Cmd) {
	next, _ := titleFeedback(args, m.title, m.conversationTitle())
	if err := auth.SetSettingsBool("title", next == "on"); err == nil {
		m.title = next
	}
	m.slashRespond("/title", "Error: No title command configured in settings.json. Please add a \"title\" block.")
	return true, nil
}

func (m *runMode) slashUsage(args string) (bool, tea.Cmd) {
	m.slashContent("error: /usage failed: retrieving quota summary: failed to retrieve user quota summary: no quota summary is available for this account")
	return true, nil
}

func (m *runMode) slashVoice(args string) (bool, tea.Cmd) {
	return true, m.startVoiceDictation("/voice")
}

func (m *runMode) slashRecord(args string) (bool, tea.Cmd) {
	return true, m.startVoiceDictation("/record")
}

func (m *runMode) slashAddDir(args string) (bool, tea.Cmd) {
	m.slashRespond("/add-dir", addDirFeedback(args))
	return true, nil
}

func (m *runMode) slashAgents(args string) (bool, tea.Cmd) {
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
	m.slashContent(printmode.PrintSlashChangelog)
	return true, nil
}

func (m *runMode) slashCodesearch(args string) (bool, tea.Cmd) {
	if strings.TrimSpace(args) == "" {
		m.slashRespond("/codesearch", codesearchEmptyMessage)
		return true, nil
	}
	m.slashRespond("/codesearch", codesearchaction.Feedback(args, m.cwd))
	return true, nil
}

func (m *runMode) slashQuote(args string) (bool, tea.Cmd) {
	return m.handleQuote(args)
}

func (m *runMode) slashCompact(args string) (bool, tea.Cmd) {
	if m.streaming {
		m.inputNotice = "Nothing to compact yet."
		return true, nil
	}
	instructions := trimSpace(args)
	m.startManualCompact(instructions)
	return true, nil
}

func (m *runMode) slashBrowser(args string) (bool, tea.Cmd) {
	errID, err := newUUIDv4()
	if err != nil {
		m.slashRespond("/browser", "Failed to generate error ID: "+err.Error())
		return true, nil
	}
	quota := panelGray166 +
		"\u26a0 Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 0h0m0s." +
		panelGray166C
	errLine := panelGray241 + "Error ID: " + errID + "-1" + panelGray241C
	m.slashRespond("/browser", quota, errLine)
	return true, nil
}

func (m *runMode) slashSchedule(args string) (bool, tea.Cmd) {
	errID, err := newUUIDv4()
	if err != nil {
		m.slashRespond("/schedule", "Failed to generate error ID: "+err.Error())
		return true, nil
	}
	quota := panelGray166 +
		"\u26a0 Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 0h0m0s." +
		panelGray166C
	errLine := panelGray241 + "Error ID: " + errID + "-1" + panelGray241C
	m.slashRespond("/schedule", quota, errLine)
	return true, nil
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
