package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/convstore"
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
// /schedule and /browser return a "Error ID: <uuid>-1" message to match
// the reference's behaviour for commands that have no backend.
//
// Handlers mutate m.response to show user echo + feedback, reset
// m.input/m.picker, and never call the model — except the four prompt-
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
	// handler output. Captured /changelog and /help runs against the
	// ref binary both show the command's *result* (changelog dump, /help
	// panel) but never a "> /changelog" or "> /help" line in the
	// transcript. Cycles 92/120 added the echo to match what the
	// picker overlay rendered, but the picker overlay and the
	// transcript are two different surfaces — the transcript stays
	// clean of slash command echoes.
	m.input.Reset()
	m.picker.Reset()
	m.historyIdx = 0
	m.historyEdit = ""
	switch cmd {

	// Local system commands. These match v1.3.5 behavior of performing
	// a visible action locally with no model call.
	case "clear":
		// Cycle 73: /clear wipes conversation transcript (no-op for
		// feedback — the cleared view speaks for itself).
		//
		// Cycle 292: the reference runs /clear *during* a stream too,
		// aborting the active turn and discarding its partial output
		// (measured on 1.4.3, re-measured on 1.4.4). Everything the turn
		// owns goes with it: the queue, the usage counters, and the
		// settings-change state the next first turn must resend.
		m.abortTurnForLocalCommand()
		m.response = nil
		m.session = nil
		// Cycle 99: drop the recorded tool-step blocks too, or ctrl+o
		// would try to re-render into the now-empty transcript.
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
		return true, nil

	case "new":
		// Cycle 102: /new starts a fresh conversation. Cycle 77 never routed
		// it, so it fell through to "Unknown command: /new" even though the
		// reference lists it. Same effect as /clear in this build.
		// Cycle 292: same mid-stream semantics as /clear.
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
		return true, nil

	case "exit", "quit":
		// Cycle 77: /exit and /quit both close the program (matches
		// the parenthetical alias "(quit)" the v1.3.5 picker shows
		// next to /exit).
		m.quitting = true
		return true, tea.Quit

	case "help":
		// Cycle 83: /help opens the 3-tab TUI menu overlay
		// (general/commands/shortcuts). View() renders the panel,
		// Update() intercepts ←/→/Tab/Esc. Replacement for the
		// cycle 77 inline list dump.
		m.helpMode = true
		m.helpTab = 0
		m.input.Reset()
		m.historyIdx = 0
		m.historyEdit = ""
		return true, nil

	case "model":
		// Cycle 193: the production path is `openSlashPanel` in
		// tui_run.go (intercepted before handleSlashCommand
		// reaches this switch). The stub below remains for tests
		// that drive handleSlashCommand directly (slash_content_test.go)
		// because removing it would also require updating every
		// such test. A future cycle that switches the TUI to
		// lazy-routing can delete it.
		if m.flags != nil {
			m.slashContent(printSlashModels(m.flags))
		} else {
			m.slashContent(modelSlug(m.model) + "\t" + m.model)
		}
		return true, nil

	case "config":
		// Cycle 193: stub kept for direct-test callers (see /model).
		body := printSlashConfig()
		m.slashContent(body)
		// Cycle customization-token-budget: report the rendered
		// payload so the context-assembly step can trim it when
		// the user's context window is tight. See
		// cmd/wagyu/customize_recorder.go and
		// internal/customize for the mechanism.
		recordCustomizationToken("config", body)
		return true, nil

	case "context":
		m.slashRespond("/context", "Context usage: session has "+itoa(len(m.session))+" message(s)")
		return true, nil

	case "continue":
		// Cycle resume-continuation: the v1.4.0 changelog adds a
		// /continue slash command that adopts the most recent
		// conversation in the current workspace or its parent/child
		// directories — the same lookup `--continue` runs at startup.
		// It differs from /resume in two ways: (1) it never opens the
		// picker (the picker is the cycle-167 fallback when there is
		// no "most recent" candidate); (2) it accepts whatever the
		// v1.4.0 walk finds, so an invocation from a deeply-nested
		// subdirectory lands on the parent's most recent session.
		//
		// The "Resumed conversation <id>" echo reuses the cycle-167
		// `⎿` wrapper via slashRespond so the visible transcript stays
		// consistent with /resume's "Resumed conversation <id>" line.
		// The picker-style "wipe + seed" pattern is lifted verbatim
		// from resumeConversation (cycle 167) so the resumed session
		// inherits the picked conversation's turns and any subsequent
		// prompt appends to the resumed db rather than the freshly
		// minted one.
		id, _, err := resolveContinue(m.cwd)
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
		// Adopt the picked conversation. m.conversationID takes the
		// adopted id so the next submitPrompt appends to that db.
		m.conversationID = sel.ID
		m.response = nil
		m.session = nil
		m.blocks = nil
		m.liveSteps = nil
		m.activeTool = ""
		m.seedConversation(sel.Store)
		// Cycle resume-continuation: clear the cached auto-title so the
		// next submitPrompt re-derives it from the resumed session's
		// first user message — without this, a /continue after a long
		// conversation would persist the previous session's title.
		m.autoTitle = ""
		m.response = append(m.response,
			"  ⎿  Resumed conversation "+sel.ID)
		return true, nil

	case "copy":
		// Cycle 103/105: real clipboard write with the recovered argument
		// forms. Recovered: "Copied %s to clipboard." (with `last response`
		// as the filler), "No response to copy." and
		// "No /btw response to copy."; see resolveCopy.
		// Cycle 230: /copy btw is a separate branch — the resolved
		// payload comes from the active side-question card, not from
		// the plannerResponses() slice the assistant turns feed.
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

	case "credits":
		// Cycle 102: recovered failure text (see print_slash.go).
		m.slashContent("error: /credits failed: retrieving credits: no credits info found")
		return true, nil

	case "diff":
		// Cycle 107: the real working-copy diff. Recovered:
		// "No uncommitted changes", "no workspace directory", the per-VCS
		// error wrapper and the viewer footer; see diffFeedback.
		//
		// Cycle diff-cjk-cutoff: m.width (or 0 to disable) is handed to
		// diffFeedback as the per-line cap so the transcript can hold a
		// long diff body without overflowing; the truncation is
		// grapheme-cluster-aware so CJK and ZWJ family-emoji sequences
		// are never split at the cutoff edge.
		m.slashRespond("/diff", diffFeedback(m.cwd, m.width))
		return true, nil

	case "effort":
		// Cycle effort-picker: /effort with a level argument is a
		// direct setter — same echo as the picker Enter. /effort with
		// no argument opens the timeline-gauge picker (handled by
		// openSlashPanelArgs in tui_run.go's prompt dispatch).
		if trimmed := trimSpace(args); trimmed != "" {
			level, ok := parseEffortWord(trimmed)
			if !ok {
				m.slashRespond("/effort", "Invalid level: "+trimmed+". Use low|medium|high.")
				return true, nil
			}
			return true, m.applyEffortLevel(level, effortIndexForName(level))
		}
		// Bare /effort in the slash router — keep the cycle-102
		// print-mode content so a -p /effort call still receives the
		// "high" answer. The TUI's interactive path lands in
		// openSlashPanelArgs which short-circuits this case.
		if m.flags != nil {
			m.slashContent(printSlashEffort(m.flags))
		} else {
			m.slashContent("high")
		}
		return true, nil

	case "feedback":
		// Cycle 167: openagy runs no feedback service. Print the
		// acknowledgement the reference shows after the user types
		// free-form text in the panel; we accept the typed text
		// silently so the buffer clears. The argument is the
		// feedback body (the reference's panel is multi-line; we
		// pass through whatever was after /feedback).
		m.slashRespond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
		return true, nil

	case "feature-diff", "featdiff", "diff-agy":
		// Cycle 236: open the /feature-diff overlay panel. The
		// production path is `openSlashPanel` in tui_run.go
		// (intercepted before handleSlashCommand reaches this
		// switch); the stub below remains for tests that drive
		// handleSlashCommand directly (slash_content_test.go,
		// slash_cycle236_test.go).
		//
		// Fallback behaviour: when the JSON data file isn't reachable,
		// the panel renders a single "data file not found" notice —
		// same graceful-degradation pattern as the cycle-235
		// /changelog viewer.
		return true, nil

	case "fork":
		// Cycle 167: fork the live conversation by creating a new
		// convstore database with the same step set. Falls back
		// to an in-memory clone when the store is read-only or
		// the active conversation is the unnamed startup one.
		//
		// Cycle 292: the reference forks mid-stream — it aborts the
		// active turn, keeps the text already generated, and forks that.
		// backendChunkMsg has been extending an assistant message in
		// m.session as it streamed, so the fork already sees the partial
		// reply; abortTurnForLocalCommand keeps it from being finalised
		// twice.
		m.abortTurnForLocalCommand()
		forkID, forkMsg := forkConversation(m.session, m.conversationID)
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

	case "hooks":
		// Cycle 102: nothing configured → the reference prints nothing.
		return true, nil

	case "keybindings":
		// Cycle 194: REF's /keybindings opens the JSON keybindings
		// file in $EDITOR (capture shows vim status line
		// "39L, 1360B" against ~/.gemini/antigravity-cli/keybindings.json).
		// The print-mode handler still rejects /keybindings ("it
		// opens the interactive keybinding editor"), confirming
		// this is an interactive-only command. We create the file
		// with "{}" if it doesn't exist so the editor always has
		// something to save back, then defer to the standard
		// editor-spawn path.
		//
		// Cycle 235 (AGY 1.3.3 line 55): a missing $EDITOR/$VISUAL
		// previously left /keybindings silently no-op'ing — the editor
		// fallback (vim) launched, found no TTY, and exited with
		// "Vim: Warning: Output is not to a terminal" + a confusing
		// non-zero exit. /keybindings now checks $EDITOR then $VISUAL
		// via resolveKeybindingsEditor and surfaces the canonical
		// "No editor configured" error, followed by the bindings table
		// so the user has something useful to read while they install
		// or configure one.
		notice, editorCmd, errMsg := openKeybindingsFile(auth.SettingsString("editor"))
		if errMsg != "" {
			m.slashRespond("/keybindings", errMsg)
			m.slashContent(printSlashKeybindings())
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

	case "logout":
		// Cycle 103: real credential removal. Recovered:
		// "Signed out from server: %s".
		m.slashRespond("/logout", logoutFeedback())
		return true, nil

	case "login":
		if hasOAuthToken() {
			m.slashRespond("/login", "Already signed in. Use /logout to sign out.")
			return true, nil
		}
		lm := newLoginModel()
		lm.width = m.width
		lm.height = m.height
		m.login = lm
		return true, lm.Init()

	case "mcp":
		// Cycle 106: list the configured MCP servers. Recovered:
		// "No MCP servers configured." for the empty case.
		cfg, err := auth.LoadMCPConfig()
		if err != nil {
			m.slashRespond("/mcp", "error: "+err.Error())
			return true, nil
		}
		body := mcpServersFeedback(cfg)
		m.slashRespond("/mcp", body)
		// Cycle customization-token-budget: report the rendered
		// server list so the context-assembly step can trim it
		// when the user's context window is tight.
		recordCustomizationToken("mcp", body)
		return true, nil

	case "open":
		// Cycle 108: open a file in the configured editor. Recovered:
		// "Usage: /open <file_path>", "File not found: %s",
		// "Opening file: %s", "No editor configured; using %s",
		// "Failed to resolve editor: %v" and
		// "Editor exited with error: %v".
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

	case "permissions":
		// Cycle 102: the recovered permission rule table.
		m.slashContent(printSlashPermissions())
		return true, nil

	case "plugin":
		// Cycle plugin-case-insensitive: `/plugin <subcmd>` (any
		// case) is dispatched as a subcommand handler — the
		// cycle's case-insensitive jetski lookup would already
		// route `agy plugin List`, and the slash form must match.
		// openSlashPanelArgs short-circuits the panel path for
		// any non-empty argument tail, so we always have args
		// here when reached. Bare `/plugin` (no args) is caught
		// by openSlashPanelArgs before this case fires, so this
		// branch never sees a zero-args invocation.
		trimmed := trimSpace(args)
		if trimmed == "" {
			// Bare `/plugin` should have been routed to the
			// panel; the fallback message is the cycle-167
			// "Plugins: 0 installed" line the reference emits
			// when its plugin directory is empty.
			m.slashRespond("/plugin", "Plugins: 0 installed")
			return true, nil
		}
		// splitPluginArgs is identical to splitSlash's contract
		// but applied to the post-plugin tail: split on the first
		// whitespace into (verb, rest). Verb is matched
		// case-insensitively (AGY 1.3.6 changelog line 21). The
		// rest is currently ignored — this cycle routes the
		// dispatch decision; the actual argument forwarding to
		// `cli.runPluginInstall` etc. is a follow-up.
		verb, _ := splitPluginArgs(trimmed)
		switch {
		case strings.EqualFold(verb, "list"):
			// `/plugin list` (any case) emits the same
			// recovered header the `agy plugin list` CLI
			// prints. The TUI never persisted plugins in
			// the openagy test rig, so the body is the
			// cycle-167 "no plugins installed" placeholder;
			// a future cycle can wire `cli.legacyLoadPlugins`
			// through here so on-disk installs land.
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

	case "remote-control":
		// AGY 1.4.1 changelog line 79: under Application Default
		// Credentials (AGY_ADC_AUTH) sign-in Remote Control does not
		// work yet, so the `/remote-control` slash command emits a
		// clear error in the transcript instead of toggling RC. The
		// picker hides the entry on the same condition (see
		// `cmd/wagyu/tui_run.go` NewRunMode + tuilib PickerState
		// .SetHiddenCommands), so an operator who finds the command
		// by typing its full name (the picker is gone) or via muscle
		// memory still sees the same self-explanatory error the
		// flag path prints. Cycle 264.
		if IsBusinessAccount() {
			m.slashRespond("/remote-control", remoteControlSlashADCErrorMessage())
			return true, nil
		}
		// Cycle 106: track the session flag. Recovered:
		// "Usage: /remote-control [on|off]", "Remote control on for this
		// session." and "Remote control off. This machine is no longer
		// reachable from other devices.". Cycle 192: REF's on-state
		// echoes three lines — the toggle confirmation + a Hostname
		// line + a takeover URL line, the latter two indented with
		// five spaces (continuation under the `⎿` wrapper; TODO.md
		// §2.3). Real values are wired in a later cycle — placeholders
		// `<host>` and `<uuid>-v2` stand in. The helper is still
		// called for state-tracking; we only append the host/URL
		// lines when the helper's message is the "on" confirmation,
		// so the off/usage branches don't get the extra rows.
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

	case "rename":
		// Cycle 106: rename the conversation. Recovered:
		// "Error: Please provide a new name for the conversation." and
		// "Conversation renamed to: %s".
		//
		// Cycle charmland-component-adoption/02: when `/rename` is
		// invoked with no arguments, open the rename panel instead
		// of echoing the error message. The panel is powered by
		// bubbles/v2/textinput (see slash_rename_panel.go). The
		// arg-based path through renameFeedback is unchanged.
		if strings.TrimSpace(args) == "" {
			m.openRenamePanel()
			return true, nil
		}
		next, msg := renameFeedback(args, m.conversationName)
		m.conversationName = next
		m.slashRespond("/rename", msg)
		return true, nil

	case "resume":
		// Cycle 167: real conversation picker. Lists every
		// <id>.db in StoreDir()/conversations via
		// convstore.ListConversations, opens the picker overlay,
		// and on Enter calls m.resumeConversation which loads +
		// seeds the chosen database the same way `--conversation`
		// does at startup. Closes the cycle-77 stub. /resume with
		// no conversation still loaded prints the picker; once a
		// conversation is live the user can keep /resume-ing to
		// switch.
		list, err := convstore.ListConversations()
		if err != nil {
			m.slashRespond("/resume", "Failed to list conversations: "+err.Error())
			return true, nil
		}
		// Cycle 184: REF's /resume hides the *current* session —
		// resuming the conversation the user is already in would
		// be a no-op, and the reference marks it as "(no title)
		// / just now / N steps" at the top of the list, which
		// shifts every other row by one and breaks the harness.
		if m.conversationID != "" {
			filtered := list[:0]
			for _, c := range list {
				if c.ID != m.conversationID {
					filtered = append(filtered, c)
				}
			}
			list = filtered
		}
		// Cycle 278: an empty store still opens the picker. Verified
		// against agy 1.4.2 with an empty conversations/ directory:
		//
		//	  Conversations
		//	  No conversations available.
		//	  Press Enter to return.
		//	  <blank>
		//	  Keyboard: enter Select  tab Switch Tab  esc Go back / Clear search
		//
		// The clone used to print one transcript line and never open the
		// overlay, so the reference's empty state was unreachable.
		m.resumeList = list
		m.resumeCursor = 0
		m.resumeWindow = 0
		m.resumeQuery = ""
		m.resumeTab = 0
		m.resumeCursorOn = false
		m.resumeMode = true
		return true, nil

	case "rewind":
		// Cycle 167: pop the last user/assistant turn pair from
		// the live session so the next prompt replays without
		// them. Reference behaviour: a tail clip rather than a
		// full rollback (verified from /tmp/agy_rewind.txt — the
		// session shrinks by one user + one assistant message).
		if n := rewindSession(m.session); n > 0 {
			m.slashRespond("/rewind", fmt.Sprintf("Rewound %d message(s) from the session tail", n))
		} else {
			m.slashRespond("/rewind", "Nothing to rewind.")
		}
		return true, nil

	case "skills":
		// Cycle 102: the recovered skill list.
		body := printSlashSkills()
		m.slashContent(body)
		// Cycle customization-token-budget: report the rendered
		// payload so the context-assembly step can trim it when
		// the user's context window is tight.
		recordCustomizationToken("skills", body)
		return true, nil

	case "statusline":
		// Cycle 105: toggle the status row and persist it. Recovered:
		// "Statusline on." / "Statusline off.".
		// Cycle status-line-script: when a statusLine.command is
		// configured, the toggle becomes a three-state cycle instead
		// of the two-state fallback. The three states are:
		//     A: built-in row visible   (statusLine = "", external = false)
		//     B: external row visible   (statusLine = "", external = true)
		//     C: row hidden             (statusLine = "off")
		// `/statusline` advances A → B → C → A → ... so the
		// configured script gets its turn in the rotation. Without
		// a configured command the cycle-105 two-state toggle (A ↔ C)
		// stays in place.
		//
		// The `m.statusLineExternal` bool tracks whether the visible
		// row is *currently* produced by the external script (state B).
		// It is seeded from settings.json at startup (true if a command
		// is configured + enabled) and advanced by /statusline; statusRow()
		// and the external state helpers use it to pick the rendering
		// branch.
		//
		// Persistence: when a statusLine.command block is present the
		// toggle writes to statusLine.enabled (visible=true|false)
		// rather than overwriting the statusLine key with a bool —
		// the latter would clobber the configured command. The
		// persistStatusLineVisibility helper handles the writeback,
		// falling back to a no-op if the block has not been created
		// yet (the cycle-105 path then owns the cycle-184 bool schema).
		if _, ok := loadStatusLineConfig(); ok {
			// Three-state cycle.
			switch {
			case m.statusLine == "off":
				// C → A. statusLine clears, render mode stays
				// at "external off" (which is the same value
				// state A wants) — the bool is left alone if
				// it was already false. C → A is the only
				// transition where we don't touch the bool.
				m.statusLine = ""
				persistStatusLineVisibility(true)
				m.slashRespond("/statusline", "Statusline on.")
			case m.statusLineExternal:
				// B → C. statusLine becomes "off" and the bool
				// clears — the cycle's wrap-around lands on A
				// (built-in), not B, so the bool flips off
				// here and the C → A transition does not
				// touch it.
				m.statusLine = "off"
				m.statusLineExternal = false
				persistStatusLineVisibility(false)
				m.slashRespond("/statusline", "Statusline off. Run /statusline to re-enable.")
			default:
				// A → B. statusLine stays "" (state A and
				// state B both render "visible"); the bool
				// flips on. enabled stays true so the next
				// start still arms the script.
				m.statusLineExternal = true
				persistStatusLineVisibility(true)
				m.slashRespond("/statusline", "Statusline on.")
			}
			return true, nil
		}
		// Cycle-105 path: no configured command, so /statusline
		// only flips between built-in and hidden. The clone-side
		// `statusLine = ""` means "use the built-in renderer" so
		// the toggle goes "" ↔ "off". statuslineFeedback
		// (slash_actions.go) returns the next value + the
		// on-screen line.
		next, msg := statuslineFeedback(m.statusLine)
		if err := auth.SetSettingsBool("statusLine", next == "on"); err == nil {
			m.statusLine = next
		}
		m.slashRespond("/statusline", msg)
		return true, nil

	case "tasks":
		// Cycle 106: report the background-task counters. Recovered:
		// "No background tasks." and "%d running, %d done".
		// Cycle tasks-panel: when the runMode has a registry,
		// read the live counters; otherwise (print mode, fresh
		// session) fall back to the cycle-106 hardcoded "no
		// tasks" line.
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

	case "title":
		// Cycle 105: toggle the custom terminal window title and persist
		// it. Recovered: "Usage: /title [on|off]",
		// "Custom title enabled. (%s)" and "Custom title disabled.".
		// Cycle 192: REF's behaviour on this build is to *always* error
		// — its settings.json has no "title" block, so the prompt-
		// driven runner reports "No title command configured in
		// settings.json." We surface the exact REF message verbatim,
		// independent of the helper's state-driven text (TODO.md §2.3).
		// The helper is still called for the state side-effect so the
		// settings.json write + m.title toggle still flow — REF's
		// failure doesn't preclude persistence, it just suppresses
		// the success echo.
		next, _ := titleFeedback(args, m.title, m.conversationTitle())
		if err := auth.SetSettingsBool("title", next == "on"); err == nil {
			m.title = next
		}
		m.slashRespond("/title", "Error: No title command configured in settings.json. Please add a \"title\" block.")
		return true, nil

	case "usage":
		// Cycle 102: recovered failure text (see print_slash.go).
		m.slashContent("error: /usage failed: retrieving quota summary: failed to retrieve user quota summary: no quota summary is available for this account")
		return true, nil

	case "voice", "record":
		// Cycle 167 + voice dictation alignment: start voice dictation
		// overlay (matching AGY 1.4.3 AudioModel).
		return true, m.startVoiceDictation("/" + cmd)

	case "add-dir":
		// Cycle 103: validate the path and record it in settings.json's
		// trustedWorkspaces (see slash_actions.go).
		m.slashRespond("/add-dir", addDirFeedback(args))
		return true, nil

	case "agents":
		// Cycle 102: nothing configured → the reference prints nothing.
		return true, nil

	case "artifact":
		// Cycle 167: openagy has no artifact viewer. The reference
		// opens a list/grid (cycle 185); this branch is the
		// print-mode / non-panel fallback. Cycle 192: REF's text is
		// just "No artifacts" with no leading indent — the panel
		// tints it grey, but the underlying string is unadorned
		// (TODO.md §2.3).
		m.slashRespond("/artifact", "No artifacts")
		return true, nil

	case "btw":
		// Cycle 230: /btw opens an independent side-question Session
		// (no parent transcript, no hooks) and renders the streamed
		// answer as a collapsed card. The card body lives in
		// internal/sidequestion; this branch only fans the slash
		// command out to runBtwSideQuestion and lets the existing
		// transcript renderer pick up the rows it emits.
		return true, m.runBtwSideQuestion(args)

	case "changelog":
		// Cycle 102: the recovered release-notes blob.
		m.slashContent(printSlashChangelog)
		return true, nil

	case "codesearch":
		// Cycle 228: bare `/codesearch` (no args) is short-circuited
		// by openSlashPanelArgs before reaching this case. If we are
		// here it means either the panel route was bypassed (older
		// callers, --print mode) or the args carry a query that the
		// panel machinery rejected (empty after parsing). Both
		// resolve to the same echo.
		if strings.TrimSpace(args) == "" {
			m.slashRespond("/codesearch", codesearchEmptyMessage)
			return true, nil
		}
		// The with-args case still uses the cycle-213 synchronous
		// echo path because the print-mode handler has no panel to
		// stream into. The panel's progressive search lives in
		// openSlashPanelArgs → codesearchStart.
		m.slashRespond("/codesearch", codeSearchFeedback(args, m.cwd))
		return true, nil

	case "quote":
		// feature/trim-quote-trailing-newline-pretty: AGY 1.2.13
		// recorded "/quote (quote a previous turn) inserting a
		// trailing newline before the user submits" — quoting
		// 'line1' actually inserted 'line1\n' and looked like an
		// extra blank line at the top of the prompt. handleQuote
		// in slash_quote.go resolves the n-th prior user turn and
		// inserts it verbatim; quoteInsertText strips trailing
		// newlines so the prompt renders exactly as the user
		// originally typed it.
		return m.handleQuote(args)

	case "compact":
		// Cycle 229: real /compact slash command, wired to PiG's
		// coding.Session.Compact() through runCycle.Compactor. Cycle
		// 167's stub (compactSession helper) used to drop the older
		// half of m.session so the next model call saw a shorter
		// history; cycle 229 keeps that helper as the fallback for
		// any caller that has not yet wired a pigrt.Runtime —
		// print mode, offline tests, and (today) every existing
		// direct-handleSlashCommand test. Cycle 227 will populate
		// m.compactor when the auto-compact runtime integration
		// lands, and from that point on the stub stops running.
		//
		// The customInstructions argument is the trailing text the
		// user typed after /compact ("/compact focus on auth").
		// PiG forwards it to the summarisation prompt; AGY 1.4.0
		// exposes the same knob ("the `/compact <message>` form
		// summarizes the conversation and then answers your message",
		// recovered in cmd/wagyu/testdata/print_changelog.md). The
		// transcript echo and the tokens row are emitted by
		// startManualCompact; this branch only arms the work.
		// Cycle 292: /compact during a stream does not interrupt the
		// turn. The reference answers with the same "Nothing to compact
		// yet." row it uses for an empty session (measured on 1.4.3 and
		// 1.4.4 while a slow mock stream was in flight), then keeps
		// streaming.
		if m.streaming {
			m.inputNotice = "Nothing to compact yet."
			return true, nil
		}
		instructions := trimSpace(args)
		m.startManualCompact(instructions)
		return true, nil

	case "browser":
		// Cycle 194: REF's /browser returns an error of the form
		// "Error ID: <uuid>-1" (TODO.md §2.4 class D; the
		// reference invokes a sub-agent that doesn't exist, the
		// model returns an error). Cycle 217: the cycle-194
		// capture
		// (`/tmp/agycmp/panels/ppref.browser.raw.txt`) showed
		// the error is preceded by a quota-warning line in
		// fg 166 — "⚠ Individual quota reached. Please upgrade
		// your subscription to increase your limits. Resets in
		// <hh>h<mm>m<ss>s." — followed by the fg-241 "Error ID:
		// <uuid>-1" row. Surface both verbatim. The clone never
		// actually checks the quota, so the reset duration is
		// the placeholder "0h0m0s" — only the verbatim phrasing
		// is recoverable here. Generate a fresh UUID per call.
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

	case "plan":
		// Cycle 194: REF's /plan sends a planning prompt to the
		// model ("⣾  Generating..." appears in the capture).
		// The plan-mode toggle is bound to Shift+Tab, not /plan —
		// cycle 167 confused the two. sendAsPrompt echoes the
		// "/plan" token, appends a planning-style prompt to the
		// session, and starts the stream.
		return true, m.sendAsPrompt("/plan", planPrompt())

	case "goal":
		// Cycle 194: REF's /goal is a prompt-sending shortcut,
		// not a goal-string setter (cycle 167's "Goal mode: …"
		// echo was a stub). The slash form simply asks the
		// model to help the user articulate a clear goal for
		// the session; sendAsPrompt echoes the token and starts
		// the stream. Argument handling is delegated to the
		// model — "/goal ship the v2 feature" and bare "/goal"
		// both produce a model call.
		return true, m.sendAsPrompt("/goal", goalPrompt())

	case "learn":
		// Cycle 194: REF's /learn is a prompt-sending shortcut,
		// not an in-memory learnings appender (cycle 167's
		// "Captured learning: …" echo was a stub). The slash
		// form asks the model to record or surface a learning;
		// sendAsPrompt echoes the token and starts the stream.
		return true, m.sendAsPrompt("/learn", learnPrompt())

	case "teamwork":
		// REF's /teamwork is a prompt-sending shortcut that coordinates
		// a team of autonomous agents to tackle complex projects.
		prompt := teamworkPrompt()
		if args != "" {
			prompt = args + "\n\n" + prompt
		}
		return true, m.sendAsPrompt("/teamwork", prompt)

	case "boost":
		// REF's /boost is a prompt-sending shortcut that invokes
		// the Boost multi-agent orchestrator for complex tasks.
		prompt := boostPrompt()
		if args != "" {
			prompt = args + "\n\n" + prompt
		}
		return true, m.sendAsPrompt("/boost", prompt)

	case "grill-me":
		// Cycle 194: REF's /grill-me is a prompt-sending shortcut.
		// The slash form asks the model to interview the user
		// with probing questions (Socratic mode); sendAsPrompt
		// echoes the token and starts the stream. Cycle 167's
		// "Interview mode enabled" message was a stub.
		return true, m.sendAsPrompt("/grill-me", grillMePrompt())

	case "schedule":
		// Cycle 194: REF's /schedule returns an error of the
		// form "Error ID: <uuid>-1" (TODO.md §2.4 class D; the
		// reference invokes a scheduler that doesn't exist, the
		// model returns an error). Cycle 217: the cycle-194
		// capture
		// (`/tmp/agycmp/panels/ppref.schedule.raw.txt`) showed
		// the error is preceded by a quota-warning line in
		// fg 166 — same wording as /browser's. Emit both
		// verbatim and let `newUUIDv4` mint a fresh id. The
		// reset duration is the placeholder "0h0m0s" — only
		// the verbatim phrasing is recoverable from REF's
		// degraded-state capture.
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

	case "branch":
		// Cycle branch-share-isolation: AGY 1.3.3 changelog line 51
		// second half — /branch forks the workspace into a sandbox
		// that does not leak CWD, env vars, or registration state
		// back to the parent session. Output is captured and
		// drained into the parent transcript on exit. The
		// isolation mechanics live in slash_isolation.go so the
		// surface stays small here.
		//
		// The slash command echoes the `> /branch` token once,
		// runs the work inside the sandbox, then appends the
		// captured lines to m.response. The parent runMode's
		// session, blocks, history, and other mutable state are
		// untouched — the worker only ever sees iw.cwd, iw.env,
		// and iw.registeredSkills/Agents, all of which are
		// restored to their pre-Enter values on Exit.
		//
		// Cycle 292: /branch runs locally while a turn streams, so it
		// first aborts that turn the way /fork does. (The reference
		// treats /branch as a plain /fork alias; the isolated-sandbox
		// behaviour here is a pre-existing deviation, left in place.)
		m.abortTurnForLocalCommand()
		runIsolatedSlash(m, "/branch", args, isolateBranchFeedback)
		return true, nil

	case "narration":
		// Cycle 250: AGY 1.4.2 "StartNarration" gRPC mirror.
		// /narration writes a short intro paragraph to m.response
		// (transcript only, not m.session) so the agent's first
		// turn starts with consistent context. The transcript-
		// only contract is the cycle's headline guarantee; see
		// TestSlashNarration_DoesNotAffectSessionHistory.
		//
		// Sub-commands:
		//   /narration              → catalogue default
		//   /narration <text>       → verbatim user line
		//   /narration off          → mute for the session
		//   /narration auto         → catalogue, time-driven only
		//   /narration auto <type>  → catalogue, time+taskType
		//
		// Print mode (-p) is unaffected — handleSlashCommand
		// never fires on the print path; print_slash.go handles
		// /narration as a no-op so the prompt-submit path stays
		// clean.
		return m.handleNarrationSlash(args)

	case "share":
		// Cycle branch-share-isolation: /share is the share-side
		// counterpart of /branch. Same isolation machinery; the
		// only difference is the user-visible feedback (see
		// isolateShareFeedback), which advertises the read-only
		// view the sandbox exposes.
		runIsolatedSlash(m, "/share", args, isolateShareFeedback)
		return true, nil

	default:
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
			return true, m.sendAsPrompt(token, slashSkillPrompt(skill, args))
		}
		// Unknown command: visible feedback rather than silently
		// forwarding to the LLM. Cycle 99: the wording is the recovered
		// reference string (a live 1.3.6 TUI capture of "/zzznotacommand"
		// renders "Unknown command: /zzznotacommand" and sends nothing to
		// the model).
		m.slashRespond("/"+cmd, "Unknown command: /"+cmd)
		return true, nil
	}
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

// itoa is a tiny local helper to avoid importing strconv just for one
// call. Returns the base-10 string form of a non-negative int.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// sendAsPrompt is the cycle-194 helper for slash commands that REF
// treats as a regular model call. The user's typed token (e.g. "/goal")
// is echoed into the transcript exactly the way a normal user message
// would be; a synthesized prompt is appended to m.session so the next
// wire call sees it; and the agent loop starts streaming.
//
// The token and prompt are tracked separately because the user typed
// the slash form ("/goal ship the v2 feature") but REF's prompt
// template substitutes a goal-setting instruction. Sending the raw
// "/goal …" token as a model message would confuse the assistant;
// sending only the template would lose the user's argument. We pick
// the template + the model's normal tool surface — the same
// compromise cycle 167 made for /btw (see slash_commands.go "btw"
// branch) — and document the approximation in TODO.md §2.4.
//
// This mirrors the prompt-send dance the Enter-key handler does for
// a normal user message (see tui_run.go lines ~1866-1888): push
// history, echo, reset input, set streaming + liveStart, append to
// m.session, batch the stream with a spinner tick.
func (m *runMode) sendAsPrompt(token, prompt string) tea.Cmd {
	m.pushHistory(token)
	m.input.Reset()
	m.streaming = true
	m.historyIdx = 0
	m.historyEdit = ""
	// Echo the slash token verbatim, mirroring the Enter path's
	// "  > <text>" framing. m.liveStart anchors the streaming
	// tool-step block immediately after the echo so ctrl+o can
	// re-render it.
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

// planPrompt is the placeholder prompt /plan sends to the model.
// The reference's exact prompt text is internal; this captures the
// intent ("help me plan the next steps for what I just said"). Marked
// 【推测】 in TODO.md §2.4.
func planPrompt() string {
	return "Help me create a plan for the work I just described. Outline the next concrete steps, call out anything that needs clarification before we start, and end with the single question I should answer first."
}

// goalPrompt is the placeholder prompt /goal sends to the model.
// Mirrors REF's behaviour of asking the assistant to help articulate
// a session goal from whatever the user has just been discussing.
func goalPrompt() string {
	return "Help me set a clear goal for this session. Restate the goal in one sentence, list the success criteria, and flag anything that could pull us off-track."
}

// learnPrompt is the placeholder prompt /learn sends to the model.
// The reference's exact text is internal; this captures the intent
// ("record a learning about something I just saw").
func learnPrompt() string {
	return "Capture a learning from what I just discussed. Write it as a short note I can refer back to, with the takeaway up front and any open questions below."
}

// teamworkPrompt is the placeholder prompt /teamwork sends to the model.
// Mirrors REF's behaviour of invoking a team of agents to autonomously
// tackle large projects.
func teamworkPrompt() string {
	return "Coordinate a team of autonomous agents to tackle this project. Decompose the goal into clear milestones, assign specialized roles, track progress systematically, and verify outcomes before synthesizing the final result."
}

// boostPrompt is the placeholder prompt /boost sends to the model.
// Mirrors REF's behaviour of activating the Boost multi-agent
// orchestrator for complex reasoning and verification.
func boostPrompt() string {
	return "Apply deep thinking, strategic planning, multiple perspectives, and rigorous verification to solve the task at hand. Explore alternative approaches, evaluate tradeoffs, and verify each step thoroughly."
}

// grillMePrompt is the placeholder prompt /grill-me sends to the
// model. REF uses it for Socratic follow-ups; the prompt asks the
// assistant to interrogate the user's reasoning.
func grillMePrompt() string {
	return "Interrogate my reasoning on the topic I just raised. Ask probing questions that expose weak assumptions, missing evidence, or unstated tradeoffs. Keep going until I've either tightened the argument or admitted it's not yet defensible."
}

// keybindingsFileName is the file name REF's /keybindings edits.
// Confirmed by probe: REF opens ~/.gemini/antigravity-cli/keybindings.json
// and the capture shows vim's status line "39L, 1360B" against that
// path (TODO.md §2.4 + /tmp/agycmp/panels/ppref.keybindings.raw.txt).
const keybindingsFileName = "keybindings.json"

// resolveKeybindingsEditor picks the editor to launch for /keybindings.
//
// Unlike resolveEditor (used by /open and ctrl+g), this helper does NOT fall
// back to editorFallback() when both $EDITOR and $VISUAL are unset. The AGY
// 1.3.3 changelog records:
//
//	"Fixed /keybindings silently no-op'ing when EDITOR/visual env var is
//	 unset; it now shows a clear error: 'No editor configured: set $EDITOR
//	 or $VISUAL'."
//
// The "silent no-op" was the prior flow: the cycle-185 editorFallback()
// returned `vim` when $EDITOR was empty, the spawn succeeded, but the editor
// had no TTY and exited with a confusing non-zero status. /keybindings now
// surfaces the changelog-prescribed message instead and gives up.
//
// Precedence (matches standard Unix convention, recovered from the same
// changelog line):
//
//  1. settings.json `editor`, when set to a non-empty and non-"auto" value
//     wins over both env vars. The user configured a specific editor
//     explicitly, so we honour it.
//  2. $EDITOR (overrides $VISUAL when both are set).
//  3. $VISUAL (used only when $EDITOR is unset/empty).
//
// Returns (name, args, source, true) when an editor was resolved, or
// ("", nil, "", false) when none is configured. The 4th boolean is the
// "did we find one?" flag — the caller renders the error in that case.
// `source` is exported for the test surfaces; the production caller does
// not surface it.
func resolveKeybindingsEditor(configuredEditor string) (name string, args []string, source string, ok bool) {
	configuredEditor = trimSpace(configuredEditor)
	if configuredEditor != "" && !strings.EqualFold(configuredEditor, "auto") {
		name, args = splitCommand(configuredEditor)
		return name, args, "settings.editor", true
	}
	if e := trimSpace(os.Getenv("EDITOR")); e != "" {
		name, args = splitCommand(e)
		return name, args, "$EDITOR", true
	}
	if v := trimSpace(os.Getenv("VISUAL")); v != "" {
		name, args = splitCommand(v)
		return name, args, "$VISUAL", true
	}
	return "", nil, "", false
}

// noEditorConfiguredMessage is the recovered slash-side message produced
// when /keybindings cannot find an editor (cycle 235, AGY 1.3.3 line 55).
// Reused by tests so the wording is pinned once.
const noEditorConfiguredMessage = "No editor configured: set $EDITOR or $VISUAL"

// openKeybindingsFile resolves the keybindings.json path under
// AntigravityConfigHome(), creates the file with a "{}" skeleton if
// it does not exist (so the editor always has something to write
// back), and returns the editor command. The returned strings are
// the lines to print before launching the editor (a single notice,
// possibly empty) and the command itself (nil when no editor can be
// resolved). errMsg is non-empty on hard failures (missing binary,
// write conflict, no $EDITOR/$VISUAL) so the caller can surface them
// rather than spawn a useless editor session.
//
// Cycle 235: editor resolution goes through resolveKeybindingsEditor
// instead of resolveEditor — $EDITOR and $VISUAL are tried in standard
// Unix order, and a missing-both state surfaces noEditorConfiguredMessage
// instead of falling back to `vim`.
//
// Mirrors the structure of openFeedback in slash_actions.go so the
// editor-spawn / finish-handshake path stays in one place.
func openKeybindingsFile(configuredEditor string) (notice string, cmd *exec.Cmd, errMsg string) {
	home := auth.AntigravityConfigHome()
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", nil, "Failed to create " + home + ": " + err.Error()
	}
	path := filepath.Join(home, keybindingsFileName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// First-run skeleton. An empty object is valid JSON and
		// gives the editor something to save back without forcing
		// the user to handcraft a starting document. We do NOT
		// pre-seed any defaults — REF's first-run file is also
		// empty until the user edits it.
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
