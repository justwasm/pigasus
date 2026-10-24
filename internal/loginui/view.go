package loginui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/tuilib"
)

// View renders the login screen.
func (m *Model) View() tea.View {
	var b strings.Builder

	lines := tuilib.RenderLogoLines()
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}

	b.WriteString("\n Welcome to the \x1b[38;2;66;133;244mAntigravity CLI\x1b[m. You are currently not signed in.\n\n")

	switch m.State {
	case StateSelectMethod:
		m.renderSelectMethod(&b)
	case StateOAuth:
		m.renderOAuth(&b)
	case StateExchanging:
		m.renderExchanging(&b)
	case StateCloudProject:
		m.renderCloudProject(&b)
	case StateSuccess:
		b.WriteString("\x1b[32mAuthentication successful!\x1b[m\n")
	case StateError:
		m.renderError(&b)
	}

	return tea.NewView(b.String())
}

func (m *Model) renderSelectMethod(b *strings.Builder) {
	glyph := brailleFrames[m.SpinFrame%len(brailleFrames)]
	fmt.Fprintf(b, " %s  Signing in... Select login method:\n", glyph)
	if m.Cursor == 0 {
		b.WriteString(" > 1. Google OAuth\n")
		b.WriteString("   2. Use a Google Cloud project\n\n")
	} else {
		b.WriteString("   1. Google OAuth\n")
		b.WriteString(" > 2. Use a Google Cloud project\n\n")
	}
	b.WriteString("   \x1b[94;1m↑/↓\x1b[90;22m Navigate · \x1b[94;1menter\x1b[90;22m Select\x1b[m\n")
}

func (m *Model) renderOAuth(b *strings.Builder) {
	b.WriteString("Your browser should open automatically. If not:\n\n ")
	b.WriteString(m.AuthURL)
	b.WriteString("\n\n")
	b.WriteString("Copy and paste the URL or click on the link below:\n")
	b.WriteString("──────────────────────────────────────────────────────────────────────────────\n")
	fmt.Fprintf(b, " \x1b]8;;%s\x07\x1b[34;4m→ Click here to authenticate\x1b[m\x1b]8;;\x07\n", m.AuthURL)
	b.WriteString("──────────────────────────────────────────────────────────────────────────────\n\n")
	b.WriteString(" If you aren't automatically redirected, paste the authorization code below:\n")
	placeholder := "authorization code..."
	if m.CodeInput != "" {
		fmt.Fprintf(b, "> %s\x1b[7m \x1b[m\n\n", m.CodeInput)
	} else {
		fmt.Fprintf(b, "> \x1b[90m%s\x1b[m\n\n", placeholder)
	}
	b.WriteString("  \x1b[94;1mesc\x1b[90;22m Back · \x1b[94;1menter\x1b[90;22m Submit\x1b[m\n")
}

func (m *Model) renderExchanging(b *strings.Builder) {
	glyph := brailleFrames[m.SpinFrame%len(brailleFrames)]
	fmt.Fprintf(b, " %s  Exchanging authorization code...\n", glyph)
}

func (m *Model) renderCloudProject(b *strings.Builder) {
	b.WriteString("Google Cloud sign-in method:\n")
	if m.CloudCursor == 0 {
		b.WriteString(" > 1. Continue with Google Cloud\n")
		b.WriteString("   2. Advanced SSO config\n\n")
	} else {
		b.WriteString("   1. Continue with Google Cloud\n")
		b.WriteString(" > 2. Advanced SSO config\n\n")
	}
	b.WriteString("   · \x1b[94;1mesc\x1b[90;22m Back · \x1b[94;1menter\x1b[90;22m Select\x1b[m\n")
}

func (m *Model) renderError(b *strings.Builder) {
	if m.Err != nil {
		fmt.Fprintf(b, "\x1b[31mAuthentication failed: %v\x1b[m\n\n", m.Err)
	} else {
		b.WriteString("\x1b[31mAuthentication failed.\x1b[m\n\n")
	}
	b.WriteString("  \x1b[94;1menter/esc\x1b[90;22m Try again\x1b[m\n")
}
