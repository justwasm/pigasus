// Package loginui implements the interactive OAuth login model.
package loginui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
)

// State tracks the current phase of the login flow.
type State int

const (
	StateSelectMethod State = iota
	StateOAuth
	StateExchanging
	StateCloudProject
	StateSuccess
	StateError
)

type spinnerTickMsg time.Time
type ExchangeSuccessMsg = *auth.AntigravityToken
type ExchangeErrMsg error
type exchangeSuccessMsg = ExchangeSuccessMsg
type exchangeErrMsg = ExchangeErrMsg

var brailleFrames = []string{"⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽", "⣾"}

// Model is the BubbleTea model for OAuth authentication.
type Model struct {
	State       State
	Cursor      int
	CloudCursor int

	Verifier  string
	Challenge string
	AuthURL   string
	StateCode string

	CodeInput string
	Err       error
	SpinFrame int

	Authenticated bool
	Width         int
	Height        int
	// AltScreen renders the screen in the terminal's alternate
	// buffer. The reference's login screen runs there (it is a
	// whole-screen view), so the standalone gate asks for it and the
	// TUI keeps its own session setting.
	AltScreen bool
	// Scroll is the first visible line of the screen. The login
	// screen is taller than a phone-sized terminal, so the content
	// is scrollable — the reference scrolls it with shift+up /
	// shift+down.
	Scroll int
}

// New creates a new Model initialized to StateSelectMethod.
func New() *Model {
	return &Model{
		State:  StateSelectMethod,
		Cursor: 0,
		Width:  80,
		Height: 24,
	}
}

// Init starts the spinner ticking.
func (m *Model) Init() tea.Cmd {
	return m.tickSpinner()
}

func (m *Model) tickSpinner() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

func (m *Model) startOAuth() tea.Cmd {
	v, c, s, err := auth.GeneratePKCE()
	if err != nil {
		m.Err = err
		m.State = StateError
		return nil
	}
	m.Verifier = v
	m.Challenge = c
	m.StateCode = s
	m.AuthURL = auth.BuildAuthorizationURL(v, s)
	m.State = StateOAuth
	m.CodeInput = ""

	_ = auth.OpenBrowser(m.AuthURL)
	return nil
}

func (m *Model) exchangeCode(code string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		tok, err := auth.ExchangeAuthorizationCode(ctx, strings.TrimSpace(code), m.Verifier)
		if err != nil {
			return exchangeErrMsg(err)
		}
		return exchangeSuccessMsg(tok)
	}
}

// IsAuthenticated reports whether login completed successfully.
func (m *Model) IsAuthenticated() bool {
	return m.Authenticated
}
