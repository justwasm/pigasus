package tui

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"

	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/palette"
)

func Test_LiveThemeSwitch_RepaintsUserPromptAndAssistantText(t *testing.T) {
	origScheme, _ := palette.CurrentScheme()
	defer func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origScheme)
	}()

	m := newRootModel(nil, "test@example.com", "gemini-test", "Pro", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	// Start under solarized light
	m.applyThemeLive("solarized light")

	// Add a user prompt
	prompt := "How does the color scheme look?"
	m.conversation.response = append(m.conversation.response, transcript.UserPromptRow(prompt))

	// Add an assistant response (simulating streamed turn completion)
	replyRaw := "The text should be crisp and visible on dark terminals."
	styled := mdrender.RenderMarkdown(replyRaw, m.viewport.width)
	replyLines := strings.Split(styled, "\n")
	start := len(m.conversation.response)
	m.conversation.response = append(m.conversation.response, replyLines...)
	m.conversation.assistants = append(m.conversation.assistants, transcript.AssistantBlock{
		Start: start,
		Lines: len(replyLines),
		Raw:   replyRaw,
	})

	// Under solarized light:
	// - user prompt uses Solarized UserFg #268bd2 bold (\x1b[38;2;38;139;210;1m)
	// - model reply uses Solarized Fg #073642 (\x1b[38;2;7;54;66m)
	solarizedPromptFg := "\x1b[38;2;38;139;210;1m"
	solarizedBodyFg := "\x1b[38;2;7;54;66m"
	if !strings.Contains(m.conversation.response[0], solarizedPromptFg) {
		t.Errorf("solarized light prompt row missing expected fg %q; got: %q", solarizedPromptFg, m.conversation.response[0])
	}
	joinedReplies := strings.Join(m.conversation.response[start:], "\n")
	if !strings.Contains(joinedReplies, solarizedBodyFg) {
		t.Errorf("solarized light body text missing expected fg %q; got: %q", solarizedBodyFg, joinedReplies)
	}

	// 1. Switch to "dark" live
	m.applyThemeLive("dark")

	// Under dark:
	// - user prompt should now wear Dark UserFg #8ab4f8 bold (\x1b[38;2;138;180;248;1m)
	// - model reply should now wear Dark Fg #e8eaed (\x1b[38;2;232;234;237m, Google crisp white)
	darkPromptFg := "\x1b[38;2;138;180;248;1m"
	darkBodyFg := "\x1b[38;2;232;234;237m"
	if !strings.Contains(m.conversation.response[0], darkPromptFg) {
		t.Errorf("dark scheme prompt row missing expected fg %q; got: %q", darkPromptFg, m.conversation.response[0])
	}
	joinedReplies = strings.Join(m.conversation.response[start:], "\n")
	if !strings.Contains(joinedReplies, darkBodyFg) {
		t.Errorf("dark scheme body text missing expected crisp white fg %q; got: %q", darkBodyFg, joinedReplies)
	}

	// 2. Switch to "tokyo night" live
	m.applyThemeLive("tokyo night")

	// Under tokyo night:
	// - user prompt should wear TokyoNight UserFg #7aa2f7 bold (\x1b[38;2;122;162;247;1m)
	// - model reply should wear TokyoNight Fg #c0caf5 (\x1b[38;2;192;202;245m)
	tokyoPromptFg := "\x1b[38;2;122;162;247;1m"
	tokyoBodyFg := "\x1b[38;2;192;202;245m"
	if !strings.Contains(m.conversation.response[0], tokyoPromptFg) {
		t.Errorf("tokyo night prompt row missing expected fg %q; got: %q", tokyoPromptFg, m.conversation.response[0])
	}
	joinedReplies = strings.Join(m.conversation.response[start:], "\n")
	if !strings.Contains(joinedReplies, tokyoBodyFg) {
		t.Errorf("tokyo night body text missing expected fg %q; got: %q", tokyoBodyFg, joinedReplies)
	}

	// 3. Switch back to "solarized light" live
	m.applyThemeLive("solarized light")
	if !strings.Contains(m.conversation.response[0], solarizedPromptFg) {
		t.Errorf("restored solarized light prompt row missing expected fg %q; got: %q", solarizedPromptFg, m.conversation.response[0])
	}
	joinedReplies = strings.Join(m.conversation.response[start:], "\n")
	if !strings.Contains(joinedReplies, solarizedBodyFg) {
		t.Errorf("restored solarized light body text missing expected fg %q; got: %q", solarizedBodyFg, joinedReplies)
	}

	// 4. Test all 8 schemes systematically
	schemes := []struct {
		name     string
		wantUser string
		wantBody string
	}{
		{"light", "\x1b[38;2;25;103;210;1m", "\x1b[38;2;32;33;36m"},
		{"solarized light", "\x1b[38;2;38;139;210;1m", "\x1b[38;2;7;54;66m"},
		{"colorblind-friendly light", "\x1b[38;2;25;103;210;1m", "\x1b[38;2;32;33;36m"},
		{"dark", "\x1b[38;2;138;180;248;1m", "\x1b[38;2;232;234;237m"},
		{"solarized dark", "\x1b[38;2;38;139;210;1m", "\x1b[38;2;238;232;213m"},
		{"colorblind-friendly dark", "\x1b[38;2;138;180;248;1m", "\x1b[38;2;232;234;237m"},
		{"tokyo night", "\x1b[38;2;122;162;247;1m", "\x1b[38;2;192;202;245m"},
		{"terminal", "\x1b[34;1m", ""},
	}

	for _, sc := range schemes {
		t.Run(sc.name, func(t *testing.T) {
			m.applyThemeLive(sc.name)
			if !strings.Contains(m.conversation.response[0], sc.wantUser) {
				t.Errorf("%s: prompt row missing expected fg %q; got: %q", sc.name, sc.wantUser, m.conversation.response[0])
			}
			if sc.wantBody != "" {
				joined := strings.Join(m.conversation.response[start:], "\n")
				if !strings.Contains(joined, sc.wantBody) {
					t.Errorf("%s: body text missing expected fg %q; got: %q", sc.name, sc.wantBody, joined)
				}
			}
		})
	}
}

// Test_LiveThemeSwitch_ConfigSubmenuPreviewDoesNotRepaint pins the preview
// the way the reference draws it: walking the Color Scheme submenu leaves
// the transcript, the banner and every other row on the committed scheme,
// and only the preview card beside the options follows the cursor.
//
// 【一手】 agy 1.5.18 (tuialign 46_scheme_chrome_fixed), arrows light →
// dark with nothing committed: the card's border moves #DADCE0 →
// #3C4043 while the banner body, the composer rules and the "Keyboard:"
// footer all stay on #5F6368. The clone used to repaint the whole frame on
// every arrow, which is what the user saw as the chrome following the
// scheme.
func Test_LiveThemeSwitch_ConfigSubmenuPreviewAndCancel(t *testing.T) {
	origScheme, _ := palette.CurrentScheme()
	defer func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origScheme)
	}()

	// The settings file is isolated: the panel reads the Color Scheme
	// row's current value from it.
	cycle196TestHome(t, map[string]any{"colorScheme": "solarized light"})

	m := newRootModel(nil, "test@example.com", "gemini-test", "Pro", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	// Start under solarized light
	m.applyThemeLive("solarized light")

	// Add a user prompt and assistant response
	m.conversation.response = append(m.conversation.response, transcript.UserPromptRow("hello"))
	styled := mdrender.RenderMarkdown("response body", m.viewport.width)
	lines := strings.Split(styled, "\n")
	start := len(m.conversation.response)
	m.conversation.response = append(m.conversation.response, lines...)
	m.conversation.assistants = append(m.conversation.assistants, transcript.AssistantBlock{
		Start: start,
		Lines: len(lines),
		Raw:   "response body",
	})

	m.overlays.panel = slashPanel{kind: panelregistry.Config, cmd: "/config"}
	m.overlays.panel.cfg = m.newConfigPanel()
	m.overlays.panel.cfg.SetCursor(configRowIndexForTest(t, m, "Color Scheme"))

	// Enter opens the inline option submenu on the Color Scheme row.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.overlays.panel.cfg.SubmenuOpen() {
		t.Fatal("expected the option submenu to be open")
	}

	// Walk the values down to "dark": the submenu opens on the row's
	// current value ("solarized light"), so two steps down reach "dark".
	for i := 0; i < 2; i++ {
		m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	}

	// The transcript is untouched: a move in the submenu is not a commit.
	solarizedPromptFg := "\x1b[38;2;38;139;210;1m"
	solarizedBodyFg := "\x1b[38;2;7;54;66m"
	if !strings.Contains(m.conversation.response[0], solarizedPromptFg) {
		t.Errorf("a preview repainted the prompt row; got: %q", m.conversation.response[0])
	}
	joined := strings.Join(m.conversation.response[start:], "\n")
	if !strings.Contains(joined, solarizedBodyFg) {
		t.Errorf("a preview repainted the body; got: %q", joined)
	}
	if mdrender.CurrentColorScheme() != "solarized light" {
		t.Errorf("a preview moved the live theme to %q", mdrender.CurrentColorScheme())
	}

	// Esc closes the submenu; nothing was applied, so nothing is undone.
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if mdrender.CurrentColorScheme() != "solarized light" {
		t.Errorf("Esc moved the live theme to %q", mdrender.CurrentColorScheme())
	}
}

func Test_LiveThemeSwitch_SlashEchoBlockRepainted(t *testing.T) {
	origScheme, _ := palette.CurrentScheme()
	defer func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origScheme)
	}()

	m := newRootModel(nil, "test@example.com", "gemini-test", "Pro", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	// Start under solarized light
	m.applyThemeLive("solarized light")

	// Append a slash command exit block:
	// > /config
	//   ⎿  Exited /config command
	m.conversation.response = append(m.conversation.response, tuilib.SlashEchoLines("/config", "Exited /config command")...)

	// 1. Under Solarized Light:
	// - header: WarningBlueOpen \x1b[38;2;38;139;210;1m
	// - hook continuation line: hook in \x1b[38;2;38;139;210;1m, text in SubtleOpen \x1b[38;2;88;110;117m
	solBlue := "\x1b[38;2;38;139;210;1m"
	solMuted := "\x1b[38;2;88;110;117m"
	if !strings.Contains(m.conversation.response[0], solBlue) {
		t.Errorf("solarized light slash header missing %q; got: %q", solBlue, m.conversation.response[0])
	}
	if !strings.Contains(m.conversation.response[1], solBlue) || !strings.Contains(m.conversation.response[1], solMuted) {
		t.Errorf("solarized light slash continuation missing %q or %q; got: %q", solBlue, solMuted, m.conversation.response[1])
	}

	// 2. Switch live to Dark
	m.applyThemeLive("dark")

	// Under Dark:
	// - header: Dark WarningBlueOpen (#8ab4f8;1) \x1b[38;2;138;180;248;1m
	// - hook continuation line: hook in \x1b[38;2;138;180;248;1m, text in Dark SubtleOpen (#bdc1c6) \x1b[38;2;189;193;198m
	darkBlue := "\x1b[38;2;138;180;248;1m"
	darkMuted := "\x1b[38;2;189;193;198m"
	if !strings.Contains(m.conversation.response[0], darkBlue) {
		t.Errorf("dark slash header missing %q; got: %q", darkBlue, m.conversation.response[0])
	}
	if !strings.Contains(m.conversation.response[1], darkBlue) || !strings.Contains(m.conversation.response[1], darkMuted) {
		t.Errorf("dark slash continuation missing %q or %q; got: %q", darkBlue, darkMuted, m.conversation.response[1])
	}

	// 3. Switch live to Tokyo Night
	m.applyThemeLive("tokyo night")
	tokyoBlue := "\x1b[38;2;122;162;247;1m"
	tokyoMuted := "\x1b[38;2;154;165;206m"
	if !strings.Contains(m.conversation.response[0], tokyoBlue) {
		t.Errorf("tokyo night slash header missing %q; got: %q", tokyoBlue, m.conversation.response[0])
	}
	if !strings.Contains(m.conversation.response[1], tokyoBlue) || !strings.Contains(m.conversation.response[1], tokyoMuted) {
		t.Errorf("tokyo night slash continuation missing %q or %q; got: %q", tokyoBlue, tokyoMuted, m.conversation.response[1])
	}

	// 4. Switch live to Terminal
	m.applyThemeLive("terminal")
	termBlue := "\x1b[94;1m"
	termMuted := "\x1b[37m"
	if !strings.Contains(m.conversation.response[0], termBlue) {
		t.Errorf("terminal slash header missing %q; got: %q", termBlue, m.conversation.response[0])
	}
	if !strings.Contains(m.conversation.response[1], termBlue) || !strings.Contains(m.conversation.response[1], termMuted) {
		t.Errorf("terminal slash continuation missing %q or %q; got: %q", termBlue, termMuted, m.conversation.response[1])
	}

	// 5. Test closePanel / slashRespond in active Dark theme emits dark colors directly
	m.applyThemeLive("dark")
	m.overlays.panel = slashPanel{kind: panelregistry.Config, cmd: "/config"}
	m.closePanel()

	lastHeader := m.conversation.response[len(m.conversation.response)-3]
	lastHook := m.conversation.response[len(m.conversation.response)-2]
	if !strings.Contains(lastHeader, darkBlue) {
		t.Errorf("freshly closed /config header missing darkBlue %q; got: %q", darkBlue, lastHeader)
	}
	if !strings.Contains(lastHook, darkBlue) || !strings.Contains(lastHook, darkMuted) {
		t.Errorf("freshly closed /config hook missing darkBlue %q or darkMuted %q; got: %q", darkBlue, darkMuted, lastHook)
	}
}
