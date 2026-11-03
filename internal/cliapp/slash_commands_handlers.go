package cliapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/prompt/customize"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchaction"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/rewind"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/models"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/tools/isolation"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
	persistentrc "github.com/justwasm/pigasus/internal/infra/remote/persistent"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/urlchunk"
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
	m.store.ClearStreamQueue()
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveReplyBuf = ""
	m.streamedText = false
	m.wireSettingsPending = true
	m.store.ResetSessionTokens()
	// A cleared conversation starts without the previous one's
	// subagent definitions: the definitions are conversation-scoped.
	m.subagentDefs = subagenttools.NewStore()
	m.resetTrajectory()
	return true, nil
}

func (m *runMode) slashExit(args string) (bool, tea.Cmd) {
	m.quitting = true
	return true, tea.Quit
}

func (m *runMode) slashHelp(args string) (bool, tea.Cmd) {
	m.helpPanel.Open(0)
	m.input.Reset()
	m.promptHistory.Reset()
	return true, nil
}

func (m *runMode) slashModel(args string) (bool, tea.Cmd) {
	// "/model <name>" is a direct setter — the reference changes the
	// model and echoes "Model set to <label>". Only a bare "/model" opens
	// the Switch Model panel. 【一手】 agy 1.4.6, live.
	if trimmed := slashparse.Trim(args); trimmed != "" {
		return true, m.applyModelArgument(trimmed)
	}
	if m.flags != nil {
		m.slashContent(printmode.PrintSlashModels(m.flags))
	} else {
		m.slashContent(modelcatalog.ModelSlug(m.model) + "\t" + m.model)
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
	sel := convstore.LoadSelection(id, "continue", convstore.ConversationPath(id))
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
	if strings.EqualFold(slashparse.Trim(args), "btw") {
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
	m.slashRespond("/diff", diffFeedback(m.cwd, m.width))
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
	// Goal is cancelled on fork point
	m.activeGoal = nil

	projectID := strings.TrimSpace(args)
	agentDir := confhome.Antigravity()
	var oldArtifactDir string
	if m.conversationID != "" {
		oldArtifactDir = filepath.Join(agentDir, "brain", m.conversationID)
	}

	forkID, forkNotice, forkMsg, err := convstore.ForkConversationFull(
		convstore.BuildSteps(m.trajectoryFacts()),
		m.conversationID,
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
		m.conversationID = forkID
		if forkNotice != "" {
			m.session = append(m.session, backend.Message{
				Role:    "user",
				Content: forkNotice,
			})
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
	notice, editorCmd := editorkit.OpenFeedback(args, settings.String("editor"), m.cwd, diffaction.PathExists)
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

func (m *runMode) slashRemoteControl(args string) (bool, tea.Cmd) {
	convID := m.activeRemoteConversationID()
	return m.handleRemoteControl(args, convID)
}

func (m *runMode) activeRemoteConversationID() string {
	if m == nil || m.conversationID == "" {
		return ""
	}
	if m.conversationStarted || len(m.session) > 0 || len(m.trajLoaded) > 0 || m.conversationID != m.initialConversationID {
		return m.conversationID
	}
	return ""
}

func (m *runMode) handleRemoteControl(args, conversationID string) (bool, tea.Cmd) {
	if auth.IsBusinessAccount() {
		m.slashRespond("/remote-control", rchost.SlashADCErrorMessage())
		return true, nil
	}
	next, msg := tuilib.RemoteControlFeedback(args, m.remoteControl)
	m.remoteControl = next
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
		if m.remoteControlUUID == "" {
			m.remoteControlUUID = persistentrc.InstallationUUID()
		}
		persistentrc.EnsureInstanceID()
		desktop := m.remoteControlDesktop || (m.flags != nil && cliflags.BoolValue(m.flags, "desktop"))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = persistentrc.StartOfficialDaemon(ctx, hostname, false, desktop)
		}()
		_, extra := buildRemoteControlCard(conversationID, m.remoteControlUUID, hostname, func(u string) []string {
			return urlchunk.Rows(u, rcURLIndent, m.rcURLChunkWidth(), m.hyperlinkCaps)
		})
		if runtime.GOOS != "darwin" {
			extra = append(extra,
				"Note: this platform has no launchd to auto-start the background daemon.",
				"Run `pigasus remote-control run` in another terminal to connect.")
		}
		m.slashRespond("/remote-control", msg, extra...)
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
	next, msg := tuilib.RenameFeedback(args, m.conversationName)
	m.conversationName = next
	m.slashRespond("/rename", msg)
	return true, nil
}

func (m *runMode) slashResume(args string) (bool, tea.Cmd) {
	if err := m.resumePicker.Open(); err != nil {
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
	r := rewind.PopTail(m.session)
	m.session = r.Session
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
		if m.statusLineCfg.Enabled {
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
		m.statusLine = "on"
		m.statusLineExternal = false
		m.statusLineCommand = ""
		m.statusLineCfg = statusline.Config{}
		m.ensureExternalState().ResetDisabled()
		m.slashRespond("/statusline", tuilib.StatuslineClearedMessage)
	default:
		command := strings.TrimSpace(args)
		if err := statusline.PersistCommand(command); err != nil {
			m.slashRespond("/statusline", "Error: "+err.Error())
			return true, nil
		}
		m.statusLine = "on"
		m.statusLineExternal = true
		m.statusLineCommand = command
		// The layout members survive: only the command and the
		// visibility change.
		m.statusLineCfg.Type = "command"
		m.statusLineCfg.Command = command
		m.statusLineCfg.Enabled = true
		m.ensureExternalState().ResetDisabled()
		m.slashRespond("/statusline", tuilib.StatuslineSetMessage(command))
	}
	return true, m.maybeArmStatusLineTick()
}

// applyStatuslineOn switches the statusline on and answers with the
// reference's "Statusline on." message (" (<command>)" when a custom
// command is configured).
func (m *runMode) applyStatuslineOn() {
	cfg := m.statusLineCfg
	statusline.PersistVisibility(true)
	m.statusLine = "on"
	m.statusLineExternal = strings.TrimSpace(cfg.Command) != ""
	m.statusLineCfg.Enabled = true
	m.slashRespond("/statusline", tuilib.StatuslineEnabledMessage(cfg.Command))
}

// applyStatuslineOff switches the statusline off and answers with msg —
// the short "Statusline off." for the explicit subcommand, the long
// "Statusline off. Run /statusline to re-enable." for the bare toggle.
func (m *runMode) applyStatuslineOff(msg string) {
	statusline.PersistVisibility(false)
	m.statusLine = "on"
	m.statusLineExternal = false
	m.statusLineCfg.Enabled = false
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
	if m.taskReg != nil {
		for _, t := range m.taskReg.List() {
			states = append(states, string(t.State))
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
	cfg := m.titleCfg
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
	m.titleCfg.Enabled = enabled
	if enabled {
		m.title = "on"
		m.slashRespond("/title", tuilib.TitleEnabledMessage(command))
	} else {
		m.title = "off"
		m.slashRespond("/title", tuilib.TitleDisabledMessage)
	}
	return m.maybeArmStatusLineTick()
}

func (m *runMode) slashUsage(args string) (bool, tea.Cmd) {
	m.slashContent("error: /usage failed: retrieving quota summary: failed to retrieve user quota summary: no quota summary is available for this account")
	return true, nil
}

func (m *runMode) slashVoice(args string) (bool, tea.Cmd) {
	return true, m.voiceSurface.Start("/voice")
}

func (m *runMode) slashAddDir(args string) (bool, tea.Cmd) {
	m.slashRespond("/add-dir", tuilib.AddDirFeedback(args))
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
	m.slashContent(changelog.Markdown)
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
