package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
)

type loginState int

const (
	loginStateSelectMethod loginState = iota
	loginStateOAuth
	loginStateExchanging
	loginStateCloudProject
	loginStateSuccess
	loginStateError
)

type spinnerTickMsg time.Time

type exchangeSuccessMsg *auth.AntigravityToken

type exchangeErrMsg error

// brailleFrames matches the reference's Braille spinner sequence.
var brailleFrames = []string{"⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽", "⣾"}

type loginModel struct {
	state       loginState
	cursor      int
	cloudCursor int

	verifier  string
	challenge string
	authURL   string
	stateCode string

	codeInput string
	err       error
	spinFrame int

	authenticated bool
	width         int
	height        int
}

func newLoginModel() *loginModel {
	return &loginModel{
		state:  loginStateSelectMethod,
		cursor: 0,
		width:  80,
		height: 24,
	}
}

func (m *loginModel) Init() tea.Cmd {
	return m.tickSpinner()
}

func (m *loginModel) tickSpinner() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

func (m *loginModel) startOAuth() tea.Cmd {
	verifier, challenge, stateCode, err := auth.GeneratePKCE()
	if err != nil {
		m.err = err
		m.state = loginStateError
		return nil
	}
	m.verifier = verifier
	m.challenge = challenge
	m.stateCode = stateCode
	m.authURL = auth.BuildAuthorizationURL(challenge, stateCode)
	m.state = loginStateOAuth
	m.codeInput = ""

	_ = auth.OpenBrowser(m.authURL)
	return nil
}

func (m *loginModel) exchangeCode(code string) tea.Cmd {
	verifier := m.verifier
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tok, err := auth.ExchangeAuthorizationCode(ctx, code, verifier)
		if err != nil {
			return exchangeErrMsg(err)
		}
		return exchangeSuccessMsg(tok)
	}
}

func (m *loginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinnerTickMsg:
		m.spinFrame = (m.spinFrame + 1) % len(brailleFrames)
		return m, m.tickSpinner()

	case exchangeSuccessMsg:
		m.authenticated = true
		m.state = loginStateSuccess
		return m, tea.Quit

	case exchangeErrMsg:
		m.err = msg
		m.state = loginStateError
		return m, nil

	case tea.PasteMsg:
		if m.state == loginStateOAuth {
			text := strings.TrimSpace(msg.Content)
			if text != "" {
				m.codeInput += text
			}
			return m, nil
		}

	case tea.KeyPressMsg:
		switch m.state {
		case loginStateSelectMethod:
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				} else {
					m.cursor = 1
				}
				return m, nil
			case "down", "j":
				if m.cursor < 1 {
					m.cursor++
				} else {
					m.cursor = 0
				}
				return m, nil
			case "enter":
				if m.cursor == 0 {
					return m, m.startOAuth()
				}
				m.state = loginStateCloudProject
				m.cloudCursor = 0
				return m, nil
			case "ctrl+c", "q":
				return m, tea.Quit
			}

		case loginStateOAuth:
			if (msg.Code == 'v' || msg.Code == 'V') && msg.Mod.Contains(tea.ModCtrl) && !msg.Mod.Contains(tea.ModAlt) {
				text := strings.TrimSpace(clipboardReadText())
				if text != "" {
					m.codeInput += text
				}
				return m, nil
			}
			if (msg.Code == 'v' || msg.Code == 'V') && (msg.Mod.Contains(tea.ModSuper) || msg.Mod.Contains(tea.ModMeta)) {
				text := strings.TrimSpace(clipboardReadText())
				if text != "" {
					m.codeInput += text
				}
				return m, nil
			}
			switch msg.String() {
			case "esc":
				m.state = loginStateSelectMethod
				return m, nil
			case "enter":
				if strings.TrimSpace(m.codeInput) != "" {
					m.state = loginStateExchanging
					return m, m.exchangeCode(m.codeInput)
				}
				return m, nil
			case "backspace":
				if len(m.codeInput) > 0 {
					m.codeInput = m.codeInput[:len(m.codeInput)-1]
				}
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			default:
				if msg.Text != "" {
					m.codeInput += msg.Text
				} else if msg.Code >= ' ' && msg.Code != tea.KeyDelete {
					m.codeInput += string(msg.Code)
				}
				return m, nil
			}

		case loginStateCloudProject:
			switch msg.String() {
			case "up", "k":
				if m.cloudCursor > 0 {
					m.cloudCursor--
				} else {
					m.cloudCursor = 1
				}
				return m, nil
			case "down", "j":
				if m.cloudCursor < 1 {
					m.cloudCursor++
				} else {
					m.cloudCursor = 0
				}
				return m, nil
			case "enter":
				if m.cloudCursor == 0 {
					os.Setenv("AGY_ADC_AUTH", "1")
					m.authenticated = true
					m.state = loginStateSuccess
					return m, tea.Quit
				}
				m.state = loginStateSelectMethod
				return m, nil
			case "esc":
				m.state = loginStateSelectMethod
				return m, nil
			case "ctrl+c", "q":
				return m, tea.Quit
			}

		case loginStateError:
			switch msg.String() {
			case "esc", "enter", "backspace":
				m.err = nil
				m.state = loginStateSelectMethod
				return m, nil
			case "ctrl+c", "q":
				return m, tea.Quit
			}

		case loginStateSuccess:
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *loginModel) View() tea.View {
	var b strings.Builder

	// Top logo
	lines := renderLogoLines()
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}

	b.WriteString("\n Welcome to the \x1b[38;2;66;133;244mAntigravity CLI\x1b[m. You are currently not signed in.\n\n")

	switch m.state {
	case loginStateSelectMethod:
		glyph := brailleFrames[m.spinFrame%len(brailleFrames)]
		fmt.Fprintf(&b, " %s  Signing in... Select login method:\n", glyph)
		if m.cursor == 0 {
			b.WriteString(" > 1. Google OAuth\n")
			b.WriteString("   2. Use a Google Cloud project\n\n")
		} else {
			b.WriteString("   1. Google OAuth\n")
			b.WriteString(" > 2. Use a Google Cloud project\n\n")
		}
		b.WriteString("   \x1b[94;1m↑/↓\x1b[90;22m Navigate · \x1b[94;1menter\x1b[90;22m Select\x1b[m\n")

	case loginStateOAuth:
		b.WriteString("Your browser should open automatically. If not:\n\n ")
		b.WriteString(m.authURL)
		b.WriteString("\n\n")
		b.WriteString("Copy and paste the URL or click on the link below:\n")
		b.WriteString("──────────────────────────────────────────────────────────────────────────────\n")
		fmt.Fprintf(&b, " \x1b]8;;%s\x07\x1b[34;4m→ Click here to authenticate\x1b[m\x1b]8;;\x07\n", m.authURL)
		b.WriteString("──────────────────────────────────────────────────────────────────────────────\n\n")
		b.WriteString(" If you aren't automatically redirected, paste the authorization code below:\n")
		placeholder := "authorization code..."
		if m.codeInput != "" {
			fmt.Fprintf(&b, "> %s\x1b[7m \x1b[m\n\n", m.codeInput)
		} else {
			fmt.Fprintf(&b, "> \x1b[90m%s\x1b[m\n\n", placeholder)
		}
		b.WriteString("  \x1b[94;1mesc\x1b[90;22m Back · \x1b[94;1menter\x1b[90;22m Submit\x1b[m\n")

	case loginStateExchanging:
		glyph := brailleFrames[m.spinFrame%len(brailleFrames)]
		fmt.Fprintf(&b, " %s  Exchanging authorization code...\n", glyph)

	case loginStateCloudProject:
		b.WriteString("Google Cloud sign-in method:\n")
		if m.cloudCursor == 0 {
			b.WriteString(" > 1. Continue with Google Cloud\n")
			b.WriteString("   2. Advanced SSO config\n\n")
		} else {
			b.WriteString("   1. Continue with Google Cloud\n")
			b.WriteString(" > 2. Advanced SSO config\n\n")
		}
		b.WriteString("   · \x1b[94;1mesc\x1b[90;22m Back · \x1b[94;1menter\x1b[90;22m Select\x1b[m\n")

	case loginStateSuccess:
		b.WriteString("\x1b[32mAuthentication successful!\x1b[m\n")

	case loginStateError:
		if m.err != nil {
			fmt.Fprintf(&b, "\x1b[31mAuthentication failed: %v\x1b[m\n\n", m.err)
		} else {
			b.WriteString("\x1b[31mAuthentication failed.\x1b[m\n\n")
		}
		b.WriteString("  \x1b[94;1menter/esc\x1b[90;22m Try again\x1b[m\n")
	}

	return tea.NewView(b.String())
}

// runPrintModeAuthIfNeeded checks if OAuth credentials exist before running
// a headless prompt, prompting for the authorization URL and reading the code
// from the controlling terminal if not signed in (parity with agy 1.4.3).
func runPrintModeAuthIfNeeded(w io.Writer, r io.Reader) error {
	if hasOAuthToken() || os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("AGY_ADC_AUTH") != "" {
		return nil
	}
	verifier, challenge, state, err := auth.GeneratePKCE()
	if err != nil {
		return err
	}
	authURL := auth.BuildAuthorizationURL(challenge, state)
	fmt.Fprintf(w, "Authentication required. Please visit the URL to log in:\n  %s\n\nWaiting for authentication (timeout 60s)...\nOr, paste the authorization code here and press Enter:\n", authURL)
	_ = auth.OpenBrowser(authURL)

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)
	go func() {
		code, err := auth.ReadAuthorizationCode(r)
		if err != nil {
			errChan <- err
			return
		}
		codeChan <- code
	}()

	select {
	case code := <-codeChan:
		if strings.TrimSpace(code) == "" {
			return errors.New("authentication interrupted")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tok, err := auth.ExchangeAuthorizationCode(ctx, code, verifier)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		_ = auth.SaveTokenUnified(tok)
		fmt.Fprintln(w, "Authentication successful!")
		return nil
	case err := <-errChan:
		return fmt.Errorf("authentication failed: %w", err)
	case <-time.After(60 * time.Second):
		return errors.New("authentication timed out")
	}
}
