package cliapp

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/internal/voice"
)

func renderHelp(tab int, model, tier, cwd, email, conversationID string, m *runMode) string {
	tabs := helpTabs(m)
	if tab < 0 || tab >= len(tabs) {
		tab = 0
	}
	var b strings.Builder
	b.WriteString(renderTabBarTabs(tab, tabs))
	b.WriteString("\n")
	switch tab {
	case 0:
		renderHelpGeneral(&b, model, tier, shortenCwd(cwd), email, conversationID)
	case 1:
		renderHelpCommands(&b, m)
	case 2:
		renderHelpShortcuts(&b, m)
	case 3:
		renderHelpVim(&b, m)
	}
	return b.String()
}

const (
	helpHeaderOpen  = tuilib.HelpHeaderOpen
	helpHeaderClose = tuilib.HelpHeaderClose
	cHelpDescOpen   = tuilib.HelpDescOpen
	cHelpDescClose  = tuilib.HelpDescClose
)

var helpKeyboardHint = tuilib.HelpKeyboardHint

func renderHelpGeneralDescription(b *strings.Builder) {
	tuilib.RenderHelpGeneralDescription(b)
}

func renderHelpGeneralMetadata(b *strings.Builder, tier, cwd, email, conversationID string) {
	tuilib.RenderHelpGeneralMetadata(b, tier, cwd, email, conversationID)
}

func renderHelpGeneralQuickRef(b *strings.Builder) {
	tuilib.RenderHelpGeneralQuickRef(b)
}

func renderHelpGeneral(b *strings.Builder, model, tier, cwd, email, conversationID string) {
	tuilib.RenderHelpGeneral(b, model, tier, cwd, email, conversationID)
}

func renderHelpCommands(b *strings.Builder, m *runMode) {
	b.WriteString(helpHeaderOpen + "Available Commands" + helpHeaderClose + "\n\n")
	cmds := tuilib.SortedSlashCommands()
	total := len(cmds)
	m.helpListClampWindow(total)
	start := m.helpWindow
	visible := helpListVisible(total, start, m.helpListWindowSize())
	for i := 0; i < visible; i++ {
		idx := start + i
		row := buildCommandRow(cmds[idx], idx == m.helpCursor, m.width)
		b.WriteString(row + "\n")
	}
	from := start + 1
	to := start + visible
	fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n\n", helpDimGrayOpen, from, to, total, helpDimGrayClose)
	b.WriteString(helpKeyboardHint + "\n\n")
}

func renderHelpShortcuts(b *strings.Builder, m *runMode) {
	b.WriteString(helpHeaderOpen + "Keyboard Shortcuts" + helpHeaderClose + "\n")
	b.WriteString("  " + helpBlueOpen + "/keybindings" + helpBlueClose + " " + helpDimGrayOpen + "to customize" + helpDimGrayClose + "\n\n")
	total := len(helpShortcuts)
	m.helpListClampWindow(total)
	start := m.helpWindow
	visible := helpListVisible(total, start, m.helpListWindowSize())
	for i := 0; i < visible; i++ {
		idx := start + i
		b.WriteString(buildShortcutRow(helpShortcuts[idx], idx == m.helpCursor) + "\n")
	}
	from := start + 1
	to := start + visible
	fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n\n", helpDimGrayOpen, from, to, total, helpDimGrayClose)
	b.WriteString(helpKeyboardHint + "\n\n")
}

const (
	helpBlueOpen     = tuilib.HelpBlueOpen
	helpBlueClose    = tuilib.HelpBlueClose
	helpDimGrayOpen  = tuilib.HelpDimGrayOpen
	helpDimGrayClose = tuilib.HelpDimGrayClose
	helpDescColumn   = tuilib.HelpDescColumn
)

func formatShortcutDesc(b *strings.Builder, desc string, highlighted bool) {
	tuilib.FormatShortcutDesc(b, desc, highlighted)
}

func buildShortcutRow(row string, highlighted bool) string {
	return tuilib.BuildShortcutRow(row, highlighted)
}

var helpShortcuts = tuilib.HelpShortcuts

func buildCommandRow(c tuilib.SlashCommand, highlighted bool, width int) string {
	return tuilib.BuildCommandRow(c, highlighted, width)
}

func renderBanner(w io.Writer, email, model, tier, cwd string) {
	artRows := tuilib.RenderLogoLines()
	fmt.Fprintf(w, "%s  "+cBlueBoldOpen+"%s "+cliVersion+"\x1b[0m\n", artRows[0], branding.Current().CLIName)
	emailLine := email
	if tier != "" {
		emailLine += " (" + tier + ")"
	}
	fmt.Fprintf(w, logoLines[1]+"\n", artRows[1], emailLine)
	fmt.Fprintf(w, logoLines[2]+"\n", artRows[2], model)
	fmt.Fprintf(w, logoLines[3]+"\n", artRows[3], shortenCwd(cwd))
	if !tuilib.EnvHideLogoOn() {
		fmt.Fprintln(w, artRows[4])
	} else {
		fmt.Fprintln(w)
	}
}

// shortenCwd replaces the leading $HOME with "~".
func shortenCwd(cwd string) string {
	return tuilib.ShortenCwd(cwd)
}

// stripTier removes a parenthesised tier suffix from a model display name.
func stripTier(model string) string {
	return tuilib.StripTier(model)
}

// renderAgentWarning is the reference's ⚠ Agent Not Found card.
func renderAgentWarning(body string) string {
	return tuilib.RenderAgentWarning(body)
}

// renderWarning emits the cycle-81 fallback warning block.
func renderWarning(from, to string) string {
	return tuilib.RenderWarning(from, to)
}

const (
	tabBarBrandOpen     = tuilib.HelpBlueOpen
	tabBarBrandClose    = tuilib.HelpBlueClose
	tabBarActiveOpen    = tuilib.TabBarActiveOpen
	tabBarActiveClose   = tuilib.TabBarActiveClose
	tabBarInactiveOpen  = tuilib.TabBarInactiveOpen
	tabBarInactiveClose = tuilib.TabBarInactiveClose
	tabBarHintOpen      = tuilib.TabBarHintOpen
	tabBarHintClose     = tuilib.TabBarHintClose
	tabBarHintSuffix    = tuilib.TabBarHintSuffix
)

// renderTabBar emits the cycle-85 styled tab bar.
func renderTabBar(active int) string {
	return tuilib.RenderTabBar(active)
}

// renderTabBarTabs emits the tab bar for an arbitrary tab list.
func renderTabBarTabs(active int, tabs []string) string {
	return tuilib.RenderTabBarTabs(active, tabs)
}

const (
	voicePhaseNone       = int(voice.PhaseNone)
	voicePhaseConnecting = int(voice.PhaseConnecting)
	voicePhaseRecording  = int(voice.PhaseRecording)
	voicePhaseFinishing  = int(voice.PhaseFinishing)
)

// voiceConnectDoneMsg reports whether the voice transcription session
// established a stream or failed.
type voiceConnectDoneMsg struct {
	err error
}

// voiceTickMsg is sent periodically during active recording to update
// the running duration timer.
type voiceTickMsg struct{}

// startVoiceDictation initiates voice dictation mode (matching AGY AudioModel).
func (m *runMode) startVoiceDictation(trigger string) tea.Cmd {
	if m.voiceMode {
		// If already dictating, pressing F5 / invoking again confirms and inserts the draft.
		return m.confirmVoiceDictation()
	}
	m.voiceMode = true
	m.voicePhase = voicePhaseConnecting
	m.voiceTrigger = trigger
	m.voiceCancel = false

	rec, _ := voice.NewRecorder("voice-"+fmt.Sprintf("%d", time.Now().UnixNano()), voice.MaxRecordingDuration, nil)
	m.voiceRecorder = rec

	return m.connectVoiceCmd()
}

func (m *runMode) connectVoiceCmd() tea.Cmd {
	if m.voiceConnectFn != nil {
		return func() tea.Msg {
			return voiceConnectDoneMsg{err: m.voiceConnectFn()}
		}
	}
	return func() tea.Msg {
		// When ANTIGRAVITY_MIC is specified (e.g. running over SSH with
		// reverse forwarded port 4713 from a local machine running
		// `wagyu mic-serve` or `agy mic-serve`), attempt to connect to the
		// remote microphone daemon over TCP.
		if micAddr := os.Getenv("ANTIGRAVITY_MIC"); micAddr != "" {
			conn, err := net.DialTimeout("tcp", micAddr, 2*time.Second)
			if err != nil {
				return voiceConnectDoneMsg{
					err: fmt.Errorf("cannot reach the mic server at %s (tcp): %w", micAddr, err),
				}
			}
			_ = conn.Close()
		}
		// In interactive mode, voice dictation connects successfully and
		// enters the active recording phase (matching AGY 1.4.3).
		return voiceConnectDoneMsg{err: nil}
	}
}

// confirmVoiceDictation inserts any drafted speech into the prompt input
// and leaves voice mode.
func (m *runMode) confirmVoiceDictation() tea.Cmd {
	if !m.voiceMode {
		return nil
	}
	m.voiceMode = false
	if m.voiceRecorder != nil {
		_, draft, _, _ := m.voiceRecorder.Snapshot()
		if draft != "" {
			if m.input.Value() == "" {
				m.input.SetValue(draft)
			} else {
				m.input.InsertString(draft)
			}
		}
		_ = m.voiceRecorder.Stop()
	}
	m.voicePhase = voicePhaseNone
	return nil
}

// cancelVoiceDictation aborts voice mode without inserting text or printing an error.
func (m *runMode) cancelVoiceDictation() tea.Cmd {
	m.voiceCancel = true
	m.voiceMode = false
	m.voicePhase = voicePhaseNone
	if m.voiceRecorder != nil {
		m.voiceRecorder.Reset()
	}
	return nil
}

// handleVoiceKey handles keyboard input when voice mode is active.
// Returns handled = true if the key belonged to voice mode.
func (m *runMode) handleVoiceKey(key tea.Key) (bool, tea.Cmd) {
	if !m.voiceMode {
		return false, nil
	}
	switch key.Code {
	case tea.KeyEsc:
		return true, m.cancelVoiceDictation()
	case tea.KeyEnter, tea.KeyF5:
		if m.voicePhase == voicePhaseConnecting {
			return true, m.cancelVoiceDictation()
		}
		return true, m.confirmVoiceDictation()
	}
	// While voice mode is active, consume other keys to match AGY AudioModel.HandleKey.
	return true, nil
}

// handleVoiceConnectDone processes the result of the connection attempt.
func (m *runMode) handleVoiceConnectDone(msg voiceConnectDoneMsg) (tea.Model, tea.Cmd) {
	if m.voiceCancel {
		return m, nil
	}
	if msg.err != nil {
		m.voiceMode = false
		m.voicePhase = voicePhaseNone
		if strings.Contains(msg.err.Error(), "cannot reach the mic server") {
			m.slashRespond("/voice", "Could not start transcription: "+msg.err.Error())
		} else {
			m.slashRespond("/voice",
				"Could not start transcription: "+msg.err.Error(),
				"     Voice dictation needs a permission your current sign-in does not carry. Use /logout, sign in again, and retry. (Dictation is not available for business or enterprise accounts.)")
		}
		return m, nil
	}
	m.voicePhase = voicePhaseRecording
	if m.voiceRecorder != nil {
		_ = m.voiceRecorder.Start()
	}
	return m, voiceTickCmd()
}

// handleVoiceTick advances the voice duration timer and transitions to finishing
// when max recording duration is reached.
func (m *runMode) handleVoiceTick() (tea.Model, tea.Cmd) {
	if m.voiceMode && m.voicePhase == voicePhaseRecording && m.voiceRecorder != nil {
		m.voiceRecorder.Tick(time.Second)
		state, _, _, _ := m.voiceRecorder.Snapshot()
		if state == voice.StateAutoStopped {
			m.voicePhase = voicePhaseFinishing
		}
		return m, voiceTickCmd()
	}
	return m, nil
}

func voiceTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return voiceTickMsg{}
	})
}

// voiceView renders the voice banner overlay (matching AGY 1.4.3 AudioModel.Banner).
func (m *runMode) voiceView() string {
	if !m.voiceMode {
		return ""
	}
	elapsed := time.Duration(0)
	if m.voiceRecorder != nil {
		_, _, _, elapsed = m.voiceRecorder.Snapshot()
	}
	return voice.FormatBanner(voice.Phase(m.voicePhase), elapsed, m.width)
}
