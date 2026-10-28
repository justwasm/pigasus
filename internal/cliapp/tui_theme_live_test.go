package cliapp

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/mdrender"
	"github.com/justwasm/pigasus/palette"
)

func Test_LiveThemeSwitch_RepaintsUserPromptAndAssistantText(t *testing.T) {
	origScheme, _ := palette.CurrentScheme()
	defer func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origScheme)
	}()

	m := NewRunMode(nil, "test@example.com", "gemini-test", "Pro", "/tmp", true)
	m.width = 80
	m.height = 24

	// Start under solarized light
	m.applyThemeLive("solarized light")

	// Add a user prompt
	prompt := "How does the color scheme look?"
	m.response = append(m.response, userPromptRow(prompt))

	// Add an assistant response (simulating streamed turn completion)
	replyRaw := "The text should be crisp and visible on dark terminals."
	styled := renderMarkdown(replyRaw, m.width)
	replyLines := strings.Split(styled, "\n")
	start := len(m.response)
	m.response = append(m.response, replyLines...)
	m.assistants = append(m.assistants, assistantBlock{
		start: start,
		lines: len(replyLines),
		raw:   replyRaw,
	})

	// Under solarized light:
	// - user prompt uses Solarized UserFg #268bd2 bold (\x1b[38;2;38;139;210;1m)
	// - model reply uses Solarized Fg #073642 (\x1b[38;2;7;54;66m)
	solarizedPromptFg := "\x1b[38;2;38;139;210;1m"
	solarizedBodyFg := "\x1b[38;2;7;54;66m"
	if !strings.Contains(m.response[0], solarizedPromptFg) {
		t.Errorf("solarized light prompt row missing expected fg %q; got: %q", solarizedPromptFg, m.response[0])
	}
	joinedReplies := strings.Join(m.response[start:], "\n")
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
	if !strings.Contains(m.response[0], darkPromptFg) {
		t.Errorf("dark scheme prompt row missing expected fg %q; got: %q", darkPromptFg, m.response[0])
	}
	joinedReplies = strings.Join(m.response[start:], "\n")
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
	if !strings.Contains(m.response[0], tokyoPromptFg) {
		t.Errorf("tokyo night prompt row missing expected fg %q; got: %q", tokyoPromptFg, m.response[0])
	}
	joinedReplies = strings.Join(m.response[start:], "\n")
	if !strings.Contains(joinedReplies, tokyoBodyFg) {
		t.Errorf("tokyo night body text missing expected fg %q; got: %q", tokyoBodyFg, joinedReplies)
	}

	// 3. Switch back to "solarized light" live
	m.applyThemeLive("solarized light")
	if !strings.Contains(m.response[0], solarizedPromptFg) {
		t.Errorf("restored solarized light prompt row missing expected fg %q; got: %q", solarizedPromptFg, m.response[0])
	}
	joinedReplies = strings.Join(m.response[start:], "\n")
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
			if !strings.Contains(m.response[0], sc.wantUser) {
				t.Errorf("%s: prompt row missing expected fg %q; got: %q", sc.name, sc.wantUser, m.response[0])
			}
			if sc.wantBody != "" {
				joined := strings.Join(m.response[start:], "\n")
				if !strings.Contains(joined, sc.wantBody) {
					t.Errorf("%s: body text missing expected fg %q; got: %q", sc.name, sc.wantBody, joined)
				}
			}
		})
	}
}

func Test_LiveThemeSwitch_ConfigSubmenuPreviewAndCancel(t *testing.T) {
	origScheme, _ := palette.CurrentScheme()
	defer func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origScheme)
	}()

	m := NewRunMode(nil, "test@example.com", "gemini-test", "Pro", "/tmp", true)
	m.width = 80
	m.height = 24

	// Start under solarized light
	m.applyThemeLive("solarized light")

	// Add a user prompt and assistant response
	m.response = append(m.response, userPromptRow("hello"))
	styled := renderMarkdown("response body", m.width)
	lines := strings.Split(styled, "\n")
	start := len(m.response)
	m.response = append(m.response, lines...)
	m.assistants = append(m.assistants, assistantBlock{
		start: start,
		lines: len(lines),
		raw:   "response body",
	})

	rows := configRows()
	colorSchemeIdx := -1
	for i, r := range rows {
		if r.Name == "Color Scheme" {
			colorSchemeIdx = i
			break
		}
	}
	if colorSchemeIdx < 0 {
		t.Fatal("Color Scheme row not found in configRows")
	}

	m.panel = slashPanel{
		cursor:        colorSchemeIdx,
		configSubmenu: false,
	}
	m.openConfigSubmenu(rows)
	m.panel.configSubmenuOrigVal = "solarized light"

	if !m.panel.configSubmenu {
		t.Fatal("expected configSubmenu to be open")
	}

	// Find the index of "dark" in the Color Scheme values
	csRow := rows[colorSchemeIdx]
	darkIdx := -1
	for i, v := range csRow.Values {
		if v.Display == "dark" {
			darkIdx = i
			break
		}
	}
	if darkIdx < 0 {
		t.Fatal("'dark' option not found in Color Scheme values")
	}

	// Move cursor to "dark" and preview
	m.panel.configSubmenuCursor = darkIdx
	m.previewThemeForRow(csRow, len(csRow.Values))

	// Verify transcript was updated to "dark"
	darkPromptFg := "\x1b[38;2;138;180;248;1m"
	darkBodyFg := "\x1b[38;2;232;234;237m"
	if !strings.Contains(m.response[0], darkPromptFg) {
		t.Errorf("expected previewed dark prompt to wear %q, got: %q", darkPromptFg, m.response[0])
	}
	joined := strings.Join(m.response[start:], "\n")
	if !strings.Contains(joined, darkBodyFg) {
		t.Errorf("expected previewed dark body to wear %q, got: %q", darkBodyFg, joined)
	}

	// Now simulate Esc key -> should cancel and restore original value
	m.handleConfigSubmenuKey(tea.Key{Code: tea.KeyEsc})

	// Verify transcript restored to solarized light
	solarizedPromptFg := "\x1b[38;2;38;139;210;1m"
	solarizedBodyFg := "\x1b[38;2;7;54;66m"
	if !strings.Contains(m.response[0], solarizedPromptFg) {
		t.Errorf("expected restored prompt to wear %q, got: %q", solarizedPromptFg, m.response[0])
	}
	joined = strings.Join(m.response[start:], "\n")
	if !strings.Contains(joined, solarizedBodyFg) {
		t.Errorf("expected restored body to wear %q, got: %q", solarizedBodyFg, joined)
	}
}

func Test_LiveThemeSwitch_SlashEchoBlockRepainted(t *testing.T) {
	origScheme, _ := palette.CurrentScheme()
	defer func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origScheme)
	}()

	m := NewRunMode(nil, "test@example.com", "gemini-test", "Pro", "/tmp", true)
	m.width = 80
	m.height = 24

	// Start under solarized light
	m.applyThemeLive("solarized light")

	// Append a slash command exit block:
	// > /config
	//   ⎿  Exited /config command
	m.response = append(m.response, slashEchoLines("/config", "Exited /config command")...)

	// 1. Under Solarized Light:
	// - header: WarningBlueOpen \x1b[38;2;38;139;210;1m
	// - hook continuation line: hook in \x1b[38;2;38;139;210;1m, text in SubtleOpen \x1b[38;2;88;110;117m
	solBlue := "\x1b[38;2;38;139;210;1m"
	solMuted := "\x1b[38;2;88;110;117m"
	if !strings.Contains(m.response[0], solBlue) {
		t.Errorf("solarized light slash header missing %q; got: %q", solBlue, m.response[0])
	}
	if !strings.Contains(m.response[1], solBlue) || !strings.Contains(m.response[1], solMuted) {
		t.Errorf("solarized light slash continuation missing %q or %q; got: %q", solBlue, solMuted, m.response[1])
	}

	// 2. Switch live to Dark
	m.applyThemeLive("dark")

	// Under Dark:
	// - header: Dark WarningBlueOpen (#8ab4f8;1) \x1b[38;2;138;180;248;1m
	// - hook continuation line: hook in \x1b[38;2;138;180;248;1m, text in Dark SubtleOpen (#bdc1c6) \x1b[38;2;189;193;198m
	darkBlue := "\x1b[38;2;138;180;248;1m"
	darkMuted := "\x1b[38;2;189;193;198m"
	if !strings.Contains(m.response[0], darkBlue) {
		t.Errorf("dark slash header missing %q; got: %q", darkBlue, m.response[0])
	}
	if !strings.Contains(m.response[1], darkBlue) || !strings.Contains(m.response[1], darkMuted) {
		t.Errorf("dark slash continuation missing %q or %q; got: %q", darkBlue, darkMuted, m.response[1])
	}

	// 3. Switch live to Tokyo Night
	m.applyThemeLive("tokyo night")
	tokyoBlue := "\x1b[38;2;122;162;247;1m"
	tokyoMuted := "\x1b[38;2;169;177;214m"
	if !strings.Contains(m.response[0], tokyoBlue) {
		t.Errorf("tokyo night slash header missing %q; got: %q", tokyoBlue, m.response[0])
	}
	if !strings.Contains(m.response[1], tokyoBlue) || !strings.Contains(m.response[1], tokyoMuted) {
		t.Errorf("tokyo night slash continuation missing %q or %q; got: %q", tokyoBlue, tokyoMuted, m.response[1])
	}

	// 4. Switch live to Terminal
	m.applyThemeLive("terminal")
	termBlue := "\x1b[94;1m"
	termMuted := "\x1b[37m"
	if !strings.Contains(m.response[0], termBlue) {
		t.Errorf("terminal slash header missing %q; got: %q", termBlue, m.response[0])
	}
	if !strings.Contains(m.response[1], termBlue) || !strings.Contains(m.response[1], termMuted) {
		t.Errorf("terminal slash continuation missing %q or %q; got: %q", termBlue, termMuted, m.response[1])
	}

	// 5. Test closePanel / slashRespond in active Dark theme emits dark colors directly
	m.applyThemeLive("dark")
	m.panel = slashPanel{kind: panelConfig, cmd: "/config"}
	m.closePanel()

	lastHeader := m.response[len(m.response)-3]
	lastHook := m.response[len(m.response)-2]
	if !strings.Contains(lastHeader, darkBlue) {
		t.Errorf("freshly closed /config header missing darkBlue %q; got: %q", darkBlue, lastHeader)
	}
	if !strings.Contains(lastHook, darkBlue) || !strings.Contains(lastHook, darkMuted) {
		t.Errorf("freshly closed /config hook missing darkBlue %q or darkMuted %q; got: %q", darkBlue, darkMuted, lastHook)
	}
}
