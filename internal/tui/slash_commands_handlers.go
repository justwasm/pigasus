package tui

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
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/prompt/customize"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/rewind"
	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/personas"
	"github.com/justwasm/pigasus/internal/features/tools/isolation"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/infra/backend/backendselect"
	"github.com/justwasm/pigasus/internal/infra/remote/persistentrc"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchaction"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/hostactions"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/internal/ui/views/wirehud"
	"github.com/justwasm/pigasus/internal/voice"
	"github.com/justwasm/pigasus/urlchunk"
)

// ---- slash_commands_handlers.go ----

// slash_commands_handlers.go — individual slash command action handlers.

func (m *RootModel) hostActionSeams() hostactions.Seams {
	return hostactions.Seams{
		AbortTurn: m.abortTurnForLocalCommand,
		ClearState: func() {
			if m.session.conversationStarted || m.session.messageCount() > 0 {
				m.saveSession()
			}
			if sup := m.session.sup(); sup != nil {
				sup.Clear()
			}
			m.conversation.response = nil
			m.session.bumpEpoch()
			m.conversation.blocks = nil
			m.conversation.liveSteps = nil
			m.conversation.liveStepRuns = nil
			m.forgetConversationState()
			m.conversation.activeTool = ""
			m.session.store.ClearStreamQueue()
			m.conversation.replyStart = -1
			m.conversation.liveReplyIdx = -1
			m.conversation.liveReplyBuf = ""
			m.conversation.streamedText = false
			m.session.wireSettingsPending = true
			m.session.store.ResetSessionTokens()
			m.session.autoTitle = ""
			m.session.conversationID = newConversationID()
			m.session.conversationStarted = false
			m.session.hookSessionStarted = false
			if resetter, ok := m.turn.engine.(agentturn.SessionResetter); ok {
				resetter.ResetSession()
			}
			m.agents.defs = subagenttools.NewStore()
			m.agents.registry = subagenttools.NewRegistry(m.session.conversationID)
			m.resetTrajectory()
		},
		Quit: func() {
			m.display.quitting = true
		},
		OpenHelp: func() {
			m.openHelp(0)
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
			m.prompt.input.Reset()
			m.prompt.promptHistory.Reset()
		},
	}
}

func (m *RootModel) slashConfig(args string) (bool, tea.Cmd) {
	body := printmode.PrintSlashConfig()
	m.slashContent(body)
	customize.RecordToken("config", body)
	return true, nil
}

func (m *RootModel) slashContext(args string) (bool, tea.Cmd) {
	m.slashRespond("/context", "Context usage: session has "+strconv.Itoa(m.sessionCount())+" message(s)")
	return true, nil
}

func (m *RootModel) slashContinue(args string) (bool, tea.Cmd) {
	if m.session.remoteAgent {
		return m.slashContinueRemote()
	}

	// 1. If the current conversation has an interrupted turn, resume it immediately.
	if m.hasInterruptedTurn() {
		return true, m.continueInterruptedTurn()
	}

	// 2. If the current conversation already completed its turns, report status.
	if m.session.messageCount() > 0 {
		m.slashRespond("/continue", "Last turn completed normally. Nothing to continue. Type a new message to start a turn.")
		return true, nil
	}

	// 3. Fresh session: resolve the most recent conversation in cwd and resume it.
	id := strings.TrimSpace(args)
	if id == "" {
		resolved, _, err := convstore.ResolveContinue(m.session.cwd)
		if err != nil {
			m.slashRespond("/continue", "error: "+err.Error())
			return true, nil
		}
		id = resolved
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
	m.conversation.response = nil
	m.session.bumpEpoch()
	if sup := m.session.sup(); sup != nil {
		sup.Clear()
	}
	m.conversation.blocks = nil
	m.conversation.liveSteps = nil
	m.conversation.activeTool = ""
	m.forgetConversationState()
	m.seedConversation(sel.Store)
	m.session.autoTitle = ""
	m.conversation.response = append(m.conversation.response, "  ⎿  Resumed conversation "+sel.ID)
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
func (m *RootModel) slashCredits(args string) (bool, tea.Cmd) {
	m.openSlashPanel("credits")
	return true, nil
}

func (m *RootModel) slashDiff(args string) (bool, tea.Cmd) {
	if m.session.remoteAgent {
		return m.slashDiffRemote(args)
	}
	m.slashRespond("/diff", diffFeedback(m.session.cwd, m.viewport.width))
	return true, nil
}

func (m *RootModel) slashEffort(args string) (bool, tea.Cmd) {
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

func (m *RootModel) slashFeatureDiff(args string) (bool, tea.Cmd) {
	return true, nil
}

func (m *RootModel) slashFork(args string) (bool, tea.Cmd) {
	m.abortTurnForLocalCommand()
	// Goal is cancelled on fork point
	m.session.activeGoal = nil

	projectID := strings.TrimSpace(args)
	agentDir := confhome.Antigravity()
	var oldArtifactDir string
	if m.session.conversationID != "" {
		oldArtifactDir = filepath.Join(agentDir, "brain", m.session.conversationID)
	}

	// The fork copies the conversation file, so a save still in flight would
	// be copied half-written. The session's steps are read here (they are
	// the live trajectory), but the file the fork duplicates is on disk.
	m.session.writer.drain()
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
			m.session.appendMessage(noticeMsg)
			m.saveSession()
		}
	}
	return true, nil
}

func (m *RootModel) slashHooks(args string) (bool, tea.Cmd) {
	return true, nil
}

func (m *RootModel) slashLogout(args string) (bool, tea.Cmd) {
	// Cycle multi-account-switch: a sign-out is a real sign-out. The
	// account leaves the registry as well, so /accounts cannot switch
	// back to a credential the user threw away. The reply line is the
	// reference's, byte for byte.
	_, email := accounts.Current()
	m.slashRespond("/logout", tuilib.LogoutFeedback())
	if email != "" {
		_, _, _ = accounts.Remove(email)
	}
	// The shared credential is gone: the composer must not answer "signed
	// in" from a read that predates the sign-out.
	m.forgetCredential()
	return true, nil
}

func (m *RootModel) slashLogin(args string) (bool, tea.Cmd) {
	if backendselect.HasOAuthToken() {
		m.slashRespond("/login", "Already signed in. Use /logout to sign out.")
		return true, nil
	}
	lm := loginui.New()
	lm.Width = m.viewport.width
	lm.Height = m.viewport.height
	m.auth.login = lm
	return true, lm.Init()
}

func (m *RootModel) slashMCP(args string) (bool, tea.Cmd) {
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

func (m *RootModel) slashOpen(args string) (bool, tea.Cmd) {
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

// slashPluginRemote lists, installs and uninstalls the remote environment's
// plugins through the proxy's plugin RPCs.
func (m *RootModel) slashPluginRemote(args string) (bool, tea.Cmd) {
	engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine)
	if !ok {
		m.slashRespond("/plugin", "No plugins installed.")
		return true, nil
	}
	trimmed := slashparse.Trim(args)
	verb, rest := slashparse.SplitVerb(trimmed)
	ctx, cancel := remoteagentclient.WithUnaryTimeout(context.Background())
	defer cancel()

	switch {
	case trimmed == "" || strings.EqualFold(verb, "list"):
		plugins, installations, err := engine.Plugins(ctx)
		if err != nil {
			m.slashRespond("/plugin list", "error: "+err.Error())
			return true, nil
		}
		lines := []string{"Remote plugins:"}
		if len(plugins) == 0 {
			lines = append(lines, "  No plugins available.")
		}
		for _, p := range plugins {
			name := p.DisplayName
			if name == "" {
				name = p.PluginID
			}
			line := "  " + name
			if p.PluginID != "" && p.PluginID != name {
				line += " (" + p.PluginID + ")"
			}
			if p.Version != "" {
				line += " v" + p.Version
			}
			if p.Description != "" {
				line += " — " + p.Description
			}
			lines = append(lines, line)
		}
		if len(installations) > 0 {
			lines = append(lines, "", "Installed remotely:")
			for _, inst := range installations {
				line := "  " + inst.PluginID
				if inst.Target != "" {
					line += " (" + inst.Target + ")"
				}
				lines = append(lines, line)
			}
		}
		m.slashContent(strings.Join(lines, "\n"))
		return true, nil
	case strings.EqualFold(verb, "install"):
		id := slashparse.Trim(rest)
		if id == "" {
			m.slashRespond("/plugin install", "Usage: /plugin install <target>")
			return true, nil
		}
		uri, err := engine.InstallPlugin(ctx, id)
		if err != nil {
			m.slashRespond("/plugin install", "error: "+err.Error())
			return true, nil
		}
		if uri != "" {
			m.slashRespond("/plugin install", "Authorize "+id+" at: "+uri)
		} else {
			m.slashRespond("/plugin install", "Plugin "+id+" installed.")
		}
		return true, nil
	case strings.EqualFold(verb, "uninstall"):
		id := slashparse.Trim(rest)
		if id == "" {
			m.slashRespond("/plugin uninstall", "Usage: /plugin uninstall <name>")
			return true, nil
		}
		if err := engine.UninstallPlugin(ctx, id); err != nil {
			m.slashRespond("/plugin uninstall", "error: "+err.Error())
			return true, nil
		}
		m.slashRespond("/plugin uninstall", "Plugin "+id+" uninstalled.")
		return true, nil
	default:
		m.slashRespond("/plugin", fmt.Sprintf("Unknown subcommand %q.", verb))
		return true, nil
	}
}

func (m *RootModel) slashPermissions(args string) (bool, tea.Cmd) {
	if m.session.remoteAgent {
		return m.slashPermissionsRemote(args)
	}
	m.slashContent(printmode.PrintSlashPermissions())
	return true, nil
}

// slashPermissionsRemote reads or writes the remote permission preset. The
// remote agent does not use the local rules, so the local list is not shown.
func (m *RootModel) slashPermissionsRemote(args string) (bool, tea.Cmd) {
	engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine)
	if !ok {
		m.slashContent(printmode.PrintSlashPermissions())
		return true, nil
	}
	ctx, cancel := remoteagentclient.WithUnaryTimeout(context.Background())
	defer cancel()

	if preset := slashparse.Trim(args); preset != "" {
		if err := engine.SetPermissionPreset(ctx, preset); err != nil {
			m.slashRespond("/permissions", "error: "+err.Error())
			return true, nil
		}
		m.slashRespond("/permissions", "Remote permission preset set to "+preset+".")
		return true, nil
	}

	settings, err := engine.PermissionSettings(ctx)
	if err != nil {
		m.slashRespond("/permissions", "error: "+err.Error())
		return true, nil
	}
	body := "Remote permission preset: " + settings.PermissionPreset
	if settings.ArtifactReviewMode != "" {
		body += "\nArtifact review mode: " + settings.ArtifactReviewMode
	}
	m.slashContent(body)
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
func (m *RootModel) slashPlugin(args string) (bool, tea.Cmd) {
	if m.session.remoteAgent {
		return m.slashPluginRemote(args)
	}
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

func (m *RootModel) slashProjects(args string) (bool, tea.Cmd) {
	if m.openSlashPanel("projects") {
		return true, nil
	}
	m.slashRespond("/projects", "No projects found.")
	return true, nil
}

func (m *RootModel) slashRemoteControl(args string) (bool, tea.Cmd) {
	convID := m.activeRemoteConversationID()
	return m.handleRemoteControl(args, convID)
}

func (m *RootModel) activeRemoteConversationID() string {
	if m == nil || m.session.conversationID == "" {
		return ""
	}
	loaded, _, _ := m.trajectoryCounts()
	if m.session.conversationStarted || m.session.messageCount() > 0 || loaded > 0 || m.session.conversationID != m.session.initialConversationID {
		return m.session.conversationID
	}
	return ""
}

func (m *RootModel) handleRemoteControl(args, conversationID string) (bool, tea.Cmd) {
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
		// If m.remote.attachedEngine is true, run in-process without spawning external daemon!
		if m.remote.attachedEngine && m.turn.engine != nil {
			backend := rchost.NewEngineCascadeBackend(m.turn.engine, m.session.model)
			backend.SetTurnLock(m.remote.EnsureTurnLock())
			ctx := context.Background()
			disp, stop, err := rchost.StartInProcessServe(ctx, backend, desktop, nil)
			if err == nil {
				m.remote.SetControl(stop, disp)
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
		m.remote.StopControl()
		m.slashRespond("/remote-control", msg)
	}
	return true, nil
}

func (m *RootModel) slashRename(args string) (bool, tea.Cmd) {
	if strings.TrimSpace(args) == "" {
		m.openRenamePanel()
		return true, nil
	}
	next, msg := tuilib.RenameFeedback(args, m.session.conversationName)
	m.session.conversationName = next
	m.slashRespond("/rename", msg)
	return true, nil
}

func (m *RootModel) slashRewind(args string) (bool, tea.Cmd) {
	// Cycle 381, tranche 75: the rewind policy is the session layer's
	// (internal/app/session/rewind). The tail pop used to run on a
	// copy of the slice, so the session it reported a rewind for was
	// never actually truncated; the policy now hands the truncated
	// session back and the caller keeps it.
	sup := m.session.sup()
	if sup == nil {
		return true, nil
	}
	r := sup.RewindPopTail()
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
func (m *RootModel) slashSkills(args string) (bool, tea.Cmd) {
	// A remote session's skills live on the proxy: refresh the cache
	// before the local reload path renders the listing.
	if m.session.remoteAgent && skills.IsReload(args) {
		if engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = engine.ReloadSkills(ctx)
			cancel()
		}
	}
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

// sessionCount returns the active message count.
func (m *RootModel) sessionCount() int { return m.session.messageCount() }

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
func (m *RootModel) slashStatusline(args string) (bool, tea.Cmd) {
	// The command may have been changed outside the TUI, so the block is
	// re-read before it is answered.
	m.reloadStatusLineConfig()
	verb := strings.ToLower(strings.TrimSpace(args))
	if verb == "" {
		// Cycle 351: the bare command toggles (agy 1.4.8).
		if m.statusLine.cfg.Enabled {
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
		m.statusLine.visible = "on"
		m.statusLine.external = false
		m.statusLine.command = ""
		m.statusLine.cfg = statusline.Config{}
		m.ensureExternalState().ResetDisabled()
		m.slashRespond("/statusline", tuilib.StatuslineClearedMessage)
	default:
		command := strings.TrimSpace(args)
		if err := statusline.PersistCommand(command); err != nil {
			m.slashRespond("/statusline", "Error: "+err.Error())
			return true, nil
		}
		m.statusLine.visible = "on"
		m.statusLine.external = true
		m.statusLine.command = command
		// The layout members survive: only the command and the
		// visibility change.
		m.statusLine.cfg.Type = "command"
		m.statusLine.cfg.Command = command
		m.statusLine.cfg.Enabled = true
		m.ensureExternalState().ResetDisabled()
		m.slashRespond("/statusline", tuilib.StatuslineSetMessage(command))
	}
	return true, m.maybeArmStatusLineTick()
}

// applyStatuslineOn switches the statusline on and answers with the
// reference's "Statusline on." message (" (<command>)" when a custom
// command is configured).
func (m *RootModel) applyStatuslineOn() {
	cfg := m.statusLine.cfg
	statusline.PersistVisibility(true)
	m.statusLine.visible = "on"
	m.statusLine.external = strings.TrimSpace(cfg.Command) != ""
	m.statusLine.cfg.Enabled = true
	m.slashRespond("/statusline", tuilib.StatuslineEnabledMessage(cfg.Command))
}

// applyStatuslineOff switches the statusline off and answers with msg —
// the short "Statusline off." for the explicit subcommand, the long
// "Statusline off. Run /statusline to re-enable." for the bare toggle.
func (m *RootModel) applyStatuslineOff(msg string) {
	statusline.PersistVisibility(false)
	m.statusLine.visible = "on"
	m.statusLine.external = false
	m.statusLine.cfg.Enabled = false
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
func (m *RootModel) slashTasks(args string) (bool, tea.Cmd) {
	var states []string
	if m.tasks.reg != nil {
		for _, t := range m.tasks.reg.List() {
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

// slashThinking handles /thinking, /thinking on and /thinking off.
//
// In AGY 1.5.14, /thinking toggles streaming the model's full thinking live in
// the transcript while it thinks and keeps finished thoughts expanded without
// pressing Ctrl+O. It is saved under "showThinking" in settings.json.
//
// Usage: /thinking [on|off]
func (m *RootModel) slashThinking(args string) (bool, tea.Cmd) {
	current, _ := settings.Bool("showThinking")
	v := strings.TrimSpace(strings.ToLower(args))
	var target bool
	switch v {
	case "":
		target = !current
	case "on", "enable", "true":
		target = true
	case "off", "disable", "false":
		target = false
	default:
		m.slashRespond("/thinking", "Usage: /thinking [on|off]")
		return true, nil
	}
	if err := settings.SetBool("showThinking", target); err != nil {
		m.slashRespond("/thinking", fmt.Sprintf("Error saving settings: %v", err))
		return true, nil
	}
	m.display.SetShowThinking(target)
	if target {
		m.slashRespond("/thinking", "Thinking shown in full: streamed in the transcript while the model thinks, kept expanded when", "     done.")
	} else {
		m.slashRespond("/thinking", "Thinking collapsed: the last line while the model thinks, ctrl+o to expand.")
	}
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
func (m *RootModel) slashTitle(args string) (bool, tea.Cmd) {
	// The block may have been changed outside the TUI, so it is re-read
	// before it is answered.
	m.reloadStatusLineConfig()
	verb := slashactions.ParseTitleVerb(args)
	if verb == slashactions.TitleVerbInvalid {
		m.slashRespond("/title", tuilib.TitleUsage)
		return true, nil
	}
	cfg := m.statusLine.titleCfg
	if strings.TrimSpace(cfg.Command) == "" {
		m.slashRespond("/title", tuilib.TitleNoCommandMessage)
		return true, nil
	}
	targetState := slashactions.ResolveTitleToggle(verb, cfg.Enabled)
	return true, m.setTitleEnabled(targetState, cfg.Command)
}

// setTitleEnabled persists the title block's visibility and answers the
// command.
func (m *RootModel) setTitleEnabled(enabled bool, command string) tea.Cmd {
	if err := statusline.PersistTitleEnabled(enabled); err != nil {
		m.slashRespond("/title", "Error: "+err.Error())
		return nil
	}
	m.statusLine.titleCfg.Enabled = enabled
	if enabled {
		m.statusLine.title = "on"
		m.slashRespond("/title", tuilib.TitleEnabledMessage(command))
	} else {
		m.statusLine.title = "off"
		m.slashRespond("/title", tuilib.TitleDisabledMessage)
	}
	return m.maybeArmStatusLineTick()
}

// slashWire toggles the real-time streaming wire diagnostics HUD overlay.
func (m *RootModel) slashWire(args string) (bool, tea.Cmd) {
	if m == nil {
		return true, nil
	}
	if m.overlays.wireHUD == nil {
		m.overlays.wireHUD = wirehud.New()
	}
	v := strings.TrimSpace(strings.ToLower(args))
	var visible bool
	switch v {
	case "":
		visible = m.overlays.wireHUD.Toggle()
	case "on", "enable", "true":
		m.overlays.wireHUD.SetVisible(true)
		visible = true
	case "off", "disable", "false":
		m.overlays.wireHUD.SetVisible(false)
		visible = false
	default:
		m.slashRespond("/wire", "Usage: /wire [on|off]")
		return true, nil
	}

	if visible {
		m.slashRespond("/wire", "Wire diagnostics HUD enabled (top-right overlay). Press ctrl+shift+w or type /wire to hide.")
	} else {
		m.slashRespond("/wire", "Wire diagnostics HUD disabled.")
	}
	return true, nil
}

// slashPersona shows or changes the active onboarding persona profile.
func (m *RootModel) slashPersona(args string) (bool, tea.Cmd) {
	if m == nil {
		return true, nil
	}
	target := strings.TrimSpace(args)
	if target == "" {
		active := m.Persona()
		skin := m.ProductSkin()
		skinName := "Developer (Software Engineering)"
		if skin == personas.ProductSkinNonTechnical {
			skinName = "Non-Technical (Knowledge Work)"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Active persona: %s [Skin: %s]\n\nAvailable personas:\n", active, skinName)
		for _, p := range personas.All() {
			marker := "  "
			if p.ID == active {
				marker = "▶ "
			}
			fmt.Fprintf(&b, "%s%-15s — %s: %s\n", marker, p.ID, p.DisplayName, p.Description)
		}
		b.WriteString("\nUsage: /persona <name> (e.g. /persona knowledge-work, /persona engineering)")
		m.slashRespond("/persona", b.String())
		return true, nil
	}

	pInfo, ok := m.SetPersona(target)
	if !ok {
		m.slashRespond("/persona", fmt.Sprintf("Unknown persona: %q. Available: engineering, knowledge-work, data, science, design", target))
		return true, nil
	}

	skinDesc := "Full developer tooling enabled"
	if pInfo.Skin == personas.ProductSkinNonTechnical {
		skinDesc = "Simplified Knowledge Work interface enabled"
	}
	m.slashRespond("/persona", fmt.Sprintf("Switched to persona %s (%s). %s.", pInfo.ID, pInfo.DisplayName, skinDesc))
	return true, nil
}

// slashSkin shows or changes the active product skin tier.
func (m *RootModel) slashSkin(args string) (bool, tea.Cmd) {
	if m == nil {
		return true, nil
	}
	target := strings.TrimSpace(args)
	if target == "" {
		skin := m.ProductSkin()
		skinDesc := "developer (Software Engineering)"
		if skin == personas.ProductSkinNonTechnical {
			skinDesc = "non-technical (Knowledge Work)"
		}
		m.slashRespond("/skin", fmt.Sprintf("Active product skin: %s\n\nOptions:\n  • developer     — Full interface for coding, developer tools, and managing agents.\n  • non-technical — Simplified interface for research, writing, and everyday tasks.\n\nUsage: /skin <developer|non-technical>", skinDesc))
		return true, nil
	}

	skinVal, ok := personas.ResolveSkin(target)
	if !ok || (skinVal != personas.ProductSkinDeveloper && skinVal != personas.ProductSkinNonTechnical) {
		m.slashRespond("/skin", fmt.Sprintf("Unknown skin: %q. Available: developer, non-technical", target))
		return true, nil
	}

	m.SetProductSkin(skinVal)
	skinName := "developer (Software Engineering)"
	if skinVal == personas.ProductSkinNonTechnical {
		skinName = "non-technical (Knowledge Work)"
	}
	m.slashRespond("/skin", fmt.Sprintf("Switched product skin to %s.", skinName))
	return true, nil
}

func (m *RootModel) slashUsage(args string) (bool, tea.Cmd) {
	if strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) != "" {
		m.slashContent("error: /usage failed: not available with a Gemini API key; see https://aistudio.google.com/usage for the key's usage")
		return true, nil
	}
	m.slashContent("error: /usage failed: retrieving quota summary: failed to retrieve user quota summary: no quota summary is available for this account")
	return true, nil
}

func (m *RootModel) slashVoice(args string) (bool, tea.Cmd) {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		if m.audio.Surface != nil && m.audio.Surface.Active() {
			return true, m.audio.Surface.Stop()
		}
		return true, m.audio.Surface.Start("/voice")
	}

	parts := strings.Fields(trimmed)
	subcmd := strings.ToLower(parts[0])

	switch subcmd {
	case "stop":
		if m.audio.Surface != nil && m.audio.Surface.Active() {
			return true, m.audio.Surface.Stop()
		}
		m.slashRespond("/voice", "Voice session stopped.")
		return true, nil

	case "dictate", "dictation":
		return true, m.audio.Surface.Start("/voice dictate")

	case "live":
		if m.audio.Surface != nil && m.audio.Surface.Active() {
			return true, m.audio.Surface.Stop()
		}
		return true, m.audio.Surface.Start("/voice live")

	case "mute":
		if m.audio.Surface == nil || !m.audio.Surface.Active() {
			m.slashRespond("/voice", "Live mode is not on. Start it with /voice live.")
			return true, nil
		}
		muted := m.ToggleLiveMute()
		if muted {
			m.slashRespond("/voice", "Microphone muted. The agent can't hear you until you unmute.")
		} else {
			m.slashRespond("/voice", "Microphone unmuted.")
		}
		return true, nil

	case "duplex":
		if len(parts) > 1 {
			val := strings.ToLower(parts[1])
			if val == "off" || val == "false" || val == "0" {
				m.SetLiveDuplex(false)
				m.slashRespond("/voice", "Half duplex: the microphone is muted while the agent speaks.")
				return true, nil
			} else if val == "on" || val == "true" || val == "1" {
				m.SetLiveDuplex(true)
				m.slashRespond("/voice", "Full duplex: you can interrupt the agent while it speaks. Use headphones, or it will hear itself.")
				return true, nil
			}
			m.slashRespond("/voice", "Usage: /voice duplex [on|off]")
			return true, nil
		}
		m.SetLiveDuplex(!m.audio.duplex)
		if m.audio.duplex {
			m.slashRespond("/voice", "Full duplex: you can interrupt the agent while it speaks. Use headphones, or it will hear itself.")
		} else {
			m.slashRespond("/voice", "Half duplex: the microphone is muted while the agent speaks.")
		}
		return true, nil

	case "speed":
		if len(parts) > 1 {
			val, err := strconv.ParseFloat(parts[1], 64)
			if err != nil || val < 0.5 || val > 3.0 {
				m.slashRespond("/voice", "Usage: /voice speed [0.5-3]")
				return true, nil
			}
			m.audio.speed = strconv.FormatFloat(val, 'f', -1, 64)
			m.slashRespond("/voice", fmt.Sprintf("Playback speed: %sx.", m.audio.speed))
		} else {
			spd := m.audio.speed
			if spd == "" {
				spd = "1.0"
			}
			m.slashRespond("/voice", fmt.Sprintf("Playback speed: %sx.", spd))
		}
		return true, nil

	case "volume":
		if len(parts) > 1 {
			val, err := strconv.Atoi(parts[1])
			if err != nil || val < 0 || val > 100 {
				m.slashRespond("/voice", "Usage: /voice volume [0-100]")
				return true, nil
			}
			m.audio.volume = strconv.Itoa(val)
			m.slashRespond("/voice", fmt.Sprintf("Volume: %d%%.", val))
		} else {
			vol := m.audio.volume
			if vol == "" {
				vol = "100"
			}
			v, _ := strconv.Atoi(vol)
			m.slashRespond("/voice", fmt.Sprintf("Volume: %d%%.", v))
		}
		return true, nil

	case "preview":
		return m.slashVoicePreview(parts[1:])

	case "speaker":
		availableVoices := []string{"fola", "arlo", "cleo", "finn", "gero", "lora", "neno", "olin", "tari"}
		voicesListStr := "fola, arlo, cleo, finn, gero, lora, neno, olin, tari"
		voicesDisplayList := "Fola, Arlo, Cleo, Finn, Gero, Lora, Neno, Olin, Tari"
		if len(parts) > 1 {
			voiceName := strings.ToLower(parts[1])
			valid := false
			for _, v := range availableVoices {
				if v == voiceName {
					valid = true
					break
				}
			}
			if !valid {
				m.slashRespond("/voice", fmt.Sprintf("Unknown voice %q.\nTry: %s.", parts[1], voicesListStr))
				return true, nil
			}
			name := strings.ToUpper(voiceName[:1]) + voiceName[1:]
			m.SetLiveVoice(name)
			if m.audio.Surface != nil && m.audio.Surface.Active() {
				m.slashRespond("/voice", fmt.Sprintf("Voice set to %s. It takes effect the next time live mode starts.", name))
			} else {
				m.slashRespond("/voice", fmt.Sprintf("Voice set to %s.", name))
			}
		} else {
			v := m.audio.voice
			if v == "" {
				v = "Fola"
			}
			m.slashRespond("/voice", fmt.Sprintf("Voice: %s.\nChange it with /voice speaker <name>: %s.", v, voicesDisplayList))
		}
		return true, nil

	case "fola", "arlo", "cleo", "finn", "gero", "lora", "neno", "olin", "tari":
		name := strings.ToUpper(subcmd[:1]) + subcmd[1:]
		m.SetLiveVoice(name)
		if m.audio.Surface != nil && m.audio.Surface.Active() {
			m.slashRespond("/voice", fmt.Sprintf("Voice set to %s. It takes effect the next time live mode starts.", name))
		} else {
			m.slashRespond("/voice", fmt.Sprintf("Voice set to %s.", name))
		}
		return true, nil

	default:
		m.slashRespond("/voice", fmt.Sprintf("Unknown /voice option %q.\nTry: /voice, /voice live, /voice dictate, /voice stop, /voice mute, /voice speaker [name], /voice preview [name] [1-3|all], /voice duplex [on|off], /voice volume [0-100], /voice speed [0.5-3].", parts[0]))
		return true, nil
	}
}

// slashVoicePreview auditions one of the recorded persona clips that ship in
// internal/voice/previews, so a speaker can be heard before it is selected.
func (m *RootModel) slashVoicePreview(args []string) (bool, tea.Cmd) {
	if len(args) == 0 {
		m.slashRespond("/voice", fmt.Sprintf("Recorded samples: %s.\nUsage: /voice preview <name> [1-%d|all].",
			strings.Join(voice.VoicePreviewPersonas(), ", "), voice.VoicePreviewVariants))
		return true, nil
	}
	switch strings.ToLower(args[0]) {
	case "on", "off":
		m.slashRespond("/voice", "The TUI plays a preview only when asked; switch-time clips are a livechat feature (/voice preview off there).")
		return true, nil
	}

	variant, playAll, err := voice.ParseVoicePreviewVariant(strings.Join(args[1:], " "))
	if err != nil {
		m.slashRespond("/voice", err.Error())
		return true, nil
	}
	target := args[0]
	first := variant
	if first == 0 {
		first = 1
	}
	sample, err := voice.ResolveVoicePreviewSample(target, first)
	if err != nil {
		m.slashRespond("/voice", err.Error())
		return true, nil
	}
	if playAll {
		m.slashRespond("/voice", fmt.Sprintf("Playing all %d samples of %s...", voice.VoicePreviewVariants, sample.Persona))
	} else {
		m.slashRespond("/voice", fmt.Sprintf("Playing %s...", sample))
	}

	return true, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), voice.VoicePreviewTimeout)
		defer cancel()
		last := first
		if playAll {
			last = voice.VoicePreviewVariants
		}
		for v := first; v <= last; v++ {
			if _, playErr := voice.PlayVoicePreview(ctx, target, v); playErr != nil {
				return nil
			}
		}
		return nil
	}
}

func (m *RootModel) slashAddDir(args string) (bool, tea.Cmd) {
	m.slashRespond("/add-dir", tuilib.AddDirFeedback(args))
	return true, nil
}

func (m *RootModel) slashAgents(args string) (bool, tea.Cmd) {
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

func (m *RootModel) slashArtifact(args string) (bool, tea.Cmd) {
	m.slashRespond("/artifact", "No artifacts")
	return true, nil
}

func (m *RootModel) slashBtw(args string) (bool, tea.Cmd) {
	return true, m.runBtwSideQuestion(args)
}

func (m *RootModel) slashChangelog(args string) (bool, tea.Cmd) {
	m.slashContent(changelog.Markdown)
	return true, nil
}

func (m *RootModel) slashCodesearch(args string) (bool, tea.Cmd) {
	if strings.TrimSpace(args) == "" {
		m.slashRespond("/codesearch", codesearchEmptyMessage)
		return true, nil
	}
	m.slashRespond("/codesearch", codesearchaction.Feedback(args, m.session.cwd))
	return true, nil
}

func (m *RootModel) slashQuote(args string) (bool, tea.Cmd) {
	return m.handleQuote(args)
}

// slashBrowser runs the browser agent.
//
// Cycle 342: the command used to print a hard-coded quota block, because
// the reference's capture of it was taken while the reference account's
// quota was exhausted — the capture held the failure, and the clone
// reproduced it for every user. The command now does the work the
// reference's own description names: "Invoke a browser agent for web
// tasks." (see slash_browser.go).
func (m *RootModel) slashBrowser(args string) (bool, tea.Cmd) {
	return m.runBrowserCommand(args)
}

// slashSchedule sends the instruction to the model, which registers the
// schedule through the `schedule` tool.
//
// Cycle 342: the command used to print the same hard-coded quota block
// /browser printed. The reference's capture of /schedule shows model
// output under the command, so the command is a prompt-sending one —
// like /plan and /goal — and the clone now sends one.
func (m *RootModel) slashSchedule(args string) (bool, tea.Cmd) {
	instruction := strings.TrimSpace(args)
	if instruction == "" {
		m.slashRespond("/schedule", "Usage: /schedule <instruction>")
		return true, nil
	}
	// /schedule expands as a mention, like /learn and /goal: the turn keeps
	// the command verbatim and the instruction rides in <ADDITIONAL_METADATA>.
	// Captured from agy 1.5.18 — `/schedule run the tests every morning`
	// produces a 437 B turn whose expansion body is the 74 B line
	// "Please use the schedule tool to create a schedule as specified by the
	// user", with no wrapper tag.
	return true, m.sendAsMention("/schedule", "/schedule "+instruction, slashactions.SchedulePrompt())
}

func (m *RootModel) slashBranch(args string) (bool, tea.Cmd) {
	m.abortTurnForLocalCommand()
	runIsolatedSlash(m, "/branch", args, isolation.BranchFeedback)
	return true, nil
}

func (m *RootModel) slashNarration(args string) (bool, tea.Cmd) {
	return m.handleNarrationSlash(args)
}

func (m *RootModel) slashShare(args string) (bool, tea.Cmd) {
	runIsolatedSlash(m, "/share", args, isolation.ShareFeedback)
	return true, nil
}
