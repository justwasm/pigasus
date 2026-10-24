package loginui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ClipboardReadText is the pluggable clipboard reader hook.
var ClipboardReadText = defaultReadClipboard

func defaultReadClipboard() string {
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("pbpaste")
		if out, err := cmd.Output(); err == nil {
			return string(out)
		}
	}
	return ""
}

// Update handles incoming BubbleTea messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil
	case spinnerTickMsg:
		m.SpinFrame = (m.SpinFrame + 1) % len(brailleFrames)
		return m, m.tickSpinner()
	case exchangeSuccessMsg:
		m.Authenticated = true
		m.State = StateSuccess
		return m, tea.Quit
	case exchangeErrMsg:
		m.Err = msg
		m.State = StateError
		return m, nil
	case tea.PasteMsg:
		if m.State == StateOAuth {
			if text := strings.TrimSpace(msg.Content); text != "" {
				m.CodeInput += text
			}
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)
	}
	return m, nil
}

func (m *Model) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.State {
	case StateSelectMethod:
		return m.handleSelectMethodKey(msg)
	case StateOAuth:
		return m.handleOAuthKey(msg)
	case StateCloudProject:
		return m.handleCloudProjectKey(msg)
	case StateError:
		return m.handleErrorKey(msg)
	case StateSuccess:
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) handleSelectMethodKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		} else {
			m.Cursor = 1
		}
	case "down", "j":
		if m.Cursor < 1 {
			m.Cursor++
		} else {
			m.Cursor = 0
		}
	case "enter":
		if m.Cursor == 0 {
			return m, m.startOAuth()
		}
		m.State = StateCloudProject
		m.CloudCursor = 0
	case "ctrl+c", "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) handleOAuthKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if (msg.Code == 'v' || msg.Code == 'V') && (msg.Mod.Contains(tea.ModCtrl) || msg.Mod.Contains(tea.ModSuper) || msg.Mod.Contains(tea.ModMeta)) {
		if text := strings.TrimSpace(ClipboardReadText()); text != "" {
			m.CodeInput += text
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.State = StateSelectMethod
	case "enter":
		if strings.TrimSpace(m.CodeInput) != "" {
			m.State = StateExchanging
			return m, m.exchangeCode(m.CodeInput)
		}
	case "backspace":
		if len(m.CodeInput) > 0 {
			m.CodeInput = m.CodeInput[:len(m.CodeInput)-1]
		}
	case "ctrl+c":
		return m, tea.Quit
	default:
		if msg.Text != "" {
			m.CodeInput += msg.Text
		} else if msg.Code >= ' ' && msg.Code != tea.KeyDelete {
			m.CodeInput += string(msg.Code)
		}
	}
	return m, nil
}

func (m *Model) handleCloudProjectKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.CloudCursor > 0 {
			m.CloudCursor--
		} else {
			m.CloudCursor = 1
		}
	case "down", "j":
		if m.CloudCursor < 1 {
			m.CloudCursor++
		} else {
			m.CloudCursor = 0
		}
	case "enter":
		if m.CloudCursor == 0 {
			os.Setenv("AGY_ADC_AUTH", "1")
			m.Authenticated = true
			m.State = StateSuccess
			return m, tea.Quit
		}
		m.State = StateSelectMethod
	case "esc":
		m.State = StateSelectMethod
	case "ctrl+c", "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) handleErrorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter", "backspace":
		m.Err = nil
		m.State = StateSelectMethod
	case "ctrl+c", "q":
		return m, tea.Quit
	}
	return m, nil
}
