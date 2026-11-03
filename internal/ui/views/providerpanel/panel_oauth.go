// panel_oauth.go — the /provider panel's browser / device login.
//
// # The flow
//
// A preset provider that asks for a browser login opens a device
// authorisation: the provider shows a URL and a one-time code, the user
// signs in, and the code comes back to the panel. Google's own providers
// ask for the authorisation code to be pasted in instead.
//
// # The seam
//
// The panel opens the URL through the host's `OpenBrowser` seam — the
// browser is the machine's, not the panel's, and this package does not
// depend on the CLI host.

package providerpanel

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/oauthflow"
	"github.com/justwasm/pigasus/palette"
)

// OAuthState holds the live state of an in-progress browser / device
// authentication session.
type OAuthState struct {
	// Provider is the provider being authenticated.
	Provider string
	// Name is the provider's display name.
	Name string
	// DeviceAuth is the device authorisation the provider issued.
	DeviceAuth *oauthflow.DeviceAuth
	// Spinning is true while the dialog's spinner runs.
	Spinning bool
	// SpinFrame is the spinner's animation frame.
	SpinFrame int
	// Notice is the dialog's last message.
	Notice string
	// CodeInput is the authorisation code the user pasted (Google).
	CodeInput string
	// LocalEmail is the account a local session belongs to.
	LocalEmail string
	// LocalToken is the access token of the local session.
	LocalToken string
	// PollCancel stops the poll when the dialog closes.
	PollCancel context.CancelFunc
}

// OAuthTickMsg advances the dialog's spinner.
type OAuthTickMsg time.Time

// OAuthResultMsg is the outcome of a login: the token, or the reason it
// failed.
type OAuthResultMsg struct {
	// Provider is the provider the answer belongs to.
	Provider string
	// Token is the access token, when the login worked.
	Token string
	// Err is the reason the login failed.
	Err error
}

// tickOAuth schedules the next spinner frame.
func (p *Panel) tickOAuth() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return OAuthTickMsg(t)
	})
}

// ApplyOAuthTick advances the spinner and schedules the next frame.
//
// It returns nil when the dialog is gone: a spinner that outlives its
// dialog keeps the TUI ticking for nothing.
func (p *Panel) ApplyOAuthTick() tea.Cmd {
	if p == nil || p.oauth == nil {
		return nil
	}
	p.oauth.SpinFrame++
	return p.tickOAuth()
}

// pollOAuthCmd waits for the device authorisation to complete.
func (p *Panel) pollOAuthCmd(ctx context.Context, da *oauthflow.DeviceAuth) tea.Cmd {
	return func() tea.Msg {
		token, err := oauthflow.Poll(ctx, da)
		return OAuthResultMsg{
			Provider: da.Provider,
			Token:    token,
			Err:      err,
		}
	}
}

// startOAuth begins a browser/device OAuth flow for a provider.
func (p *Panel) startOAuth(providerID string) tea.Cmd {
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	da, err := oauthflow.Initiate(ctx, providerID)
	if err != nil {
		cancel()
		p.Notice("Failed to initiate authentication: " + err.Error())
		return nil
	}

	// If direct token was found, save and activate immediately without prompting.
	if da.DirectToken != "" {
		cancel()
		return p.saveOAuthToken(providerID, da.DirectToken, true)
	}

	name := providerID
	if cfg, ok := models.FindCatwalkProvider(providerID); ok {
		name = cfg.Name
	}

	var localEmail, localToken string
	if providerID == "google-antigravity" || providerID == "antigravity" {
		if tok, email := accounts.Current(); tok != nil && tok.Token.AccessToken != "" {
			localEmail = email
			localToken = tok.Token.AccessToken
		} else if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil && tok.Token.AccessToken != "" {
			localEmail = tok.UserEmail()
			localToken = tok.Token.AccessToken
		}
	}

	p.oauth = &OAuthState{
		Provider:   providerID,
		Name:       name,
		DeviceAuth: da,
		Spinning:   true,
		LocalEmail: localEmail,
		LocalToken: localToken,
		PollCancel: cancel,
	}

	// Automatically attempt to open the verification URL in user's browser
	if da.VerificationURL != "" {
		_ = p.openBrowser(da.VerificationURL)
	}

	return tea.Batch(
		p.pollOAuthCmd(ctx, da),
		p.tickOAuth(),
	)
}

// submitOAuthCode exchanges a pasted authorisation code for a token.
func (p *Panel) submitOAuthCode(code string) (bool, tea.Cmd) {
	if p == nil {
		return true, nil
	}
	oauth := p.oauth
	if oauth == nil || oauth.DeviceAuth == nil {
		return true, nil
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return true, nil
	}

	oauth.Notice = palette.DimGrayOpen + " Exchanging code..." + palette.DimGrayClose

	return true, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		token, err := oauthflow.ExchangeCode(ctx, oauth.DeviceAuth, code)
		return OAuthResultMsg{
			Provider: oauth.Provider,
			Token:    token,
			Err:      err,
		}
	}
}

// saveOAuthToken writes the token into the provider's configuration.
func (p *Panel) saveOAuthToken(providerID, token string, fromLocal bool) tea.Cmd {
	if p == nil {
		return nil
	}
	cfg, ok := models.PresetProviderConfig(providerID)
	if !ok {
		cfg = models.ProviderConfig{
			ID:   providerID,
			Type: models.ProviderTypeOpenAI,
		}
	}
	cfg.ID = providerID
	cfg.APIKey = token

	ed := models.ProviderEditor()
	if err := ed.PutProvider(providerID, cfg); err != nil {
		p.Notice("Failed to save provider: " + err.Error())
		return nil
	}
	modelsList := cfg.EffectiveModels()
	if len(modelsList) == 0 {
		if preset, ok := models.PresetProviderConfig(providerID); ok {
			modelsList = preset.EffectiveModels()
		}
	}
	if len(modelsList) > 0 {
		for _, model := range modelsList {
			_ = ed.PutModel(providerID, model)
		}
	}

	p.Reload()
	if fromLocal {
		p.Notice(fmt.Sprintf("✓ Authenticated %s from local session!", providerID))
	} else {
		p.Notice(fmt.Sprintf("✓ Successfully authenticated %s!", providerID))
	}
	return nil
}

// ApplyOAuthResultMsg records the outcome of a login.
func (p *Panel) ApplyOAuthResultMsg(msg OAuthResultMsg) {
	if p == nil {
		return
	}
	oauth := p.oauth
	if oauth == nil || oauth.Provider != msg.Provider {
		return
	}
	if msg.Err != nil {
		oauth.Notice = "\x1b[31m (" + msg.Err.Error() + ")\x1b[m"
		p.Notice("Authentication failed: " + msg.Err.Error())
		return
	}
	p.oauth = nil
	if msg.Token == "" {
		p.Notice("Authentication returned empty token")
		return
	}
	_ = p.saveOAuthToken(msg.Provider, msg.Token, false)
}

// handleOAuthKey handles keys while the OAuth authentication dialog is
// visible.
func (p *Panel) handleOAuthKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	oauth := p.oauth
	if oauth == nil {
		return false, nil
	}

	isGoogle := oauth.Provider == "google-antigravity" || oauth.Provider == "antigravity"

	switch key.Code {
	case tea.KeyEsc:
		if oauth.PollCancel != nil {
			oauth.PollCancel()
		}
		p.oauth = nil
		p.cacheValid = false
		return true, nil

	case tea.KeyBackspace:
		if isGoogle && len(oauth.CodeInput) > 0 {
			oauth.CodeInput = oauth.CodeInput[:len(oauth.CodeInput)-1]
			return true, nil
		}

	case tea.KeyEnter:
		if isGoogle && strings.TrimSpace(oauth.CodeInput) != "" {
			return p.submitOAuthCode(strings.TrimSpace(oauth.CodeInput))
		}
		// Open verification URL in browser
		if oauth.DeviceAuth != nil && oauth.DeviceAuth.VerificationURL != "" {
			_ = p.openBrowser(oauth.DeviceAuth.VerificationURL)
			oauth.Notice = verifyOKOpen + " (Opened link in browser!)" + verifyOKClose
			return true, nil
		}

	case 'a', 'A':
		// If local active account exists, user can press 'a' to activate it directly
		if isGoogle && oauth.CodeInput == "" && oauth.LocalToken != "" {
			if oauth.PollCancel != nil {
				oauth.PollCancel()
			}
			p.oauth = nil
			return true, p.saveOAuthToken(oauth.Provider, oauth.LocalToken, true)
		}

	case 'c', 'C':
		// Copy one-time device code to clipboard
		if !isGoogle || oauth.CodeInput == "" {
			if oauth.DeviceAuth != nil && oauth.DeviceAuth.UserCode != "" {
				_ = p.writeClipboard(oauth.DeviceAuth.UserCode)
				oauth.Notice = verifyOKOpen + " (Code copied to clipboard!)" + verifyOKClose
				return true, nil
			}
		}

	case 'y', 'Y', 'u', 'U':
		// Copy verification URL to clipboard
		if !isGoogle || oauth.CodeInput == "" {
			if oauth.DeviceAuth != nil && oauth.DeviceAuth.VerificationURL != "" {
				_ = p.writeClipboard(oauth.DeviceAuth.VerificationURL)
				oauth.Notice = verifyOKOpen + " (URL copied to clipboard!)" + verifyOKClose
				return true, nil
			}
		}
	}

	// Paste handling: Cmd+V / Ctrl+V / ModSuper+V / ModMeta+V
	if (key.Code == 'v' || key.Code == 'V') && (key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModMeta)) {
		if text := strings.TrimSpace(p.readClipboard()); text != "" {
			if isGoogle {
				oauth.CodeInput = text
				return p.submitOAuthCode(text)
			}
		}
		return true, nil
	}

	// Typing characters into Google authorization code input
	if isGoogle && key.Mod == 0 && key.Text != "" && key.Text != "\t" && key.Text != "\n" && key.Text != "\r" {
		oauth.CodeInput += key.Text
		return true, nil
	}

	return true, nil
}

// ---- the paste seam ----------------------------------------------------

// HandlePaste routes pasted text into the panel.
//
// The panel has three faces and each takes the paste differently: the
// login dialog takes the authorisation code, the form takes the field's
// value, and the list takes the search query.
func (p *Panel) HandlePaste(s string) tea.Cmd {
	if p == nil {
		return nil
	}
	if p.oauth != nil {
		oauth := p.oauth
		if oauth.Provider == "google-antigravity" || oauth.Provider == "antigravity" {
			clean := strings.TrimSpace(s)
			if clean != "" {
				oauth.CodeInput = clean
				_, cmd := p.submitOAuthCode(clean)
				return cmd
			}
		}
		return nil
	}
	if p.form != nil {
		if p.form.Field().Kind != FieldChoice {
			updated, cmd := p.input.Update(tea.PasteMsg{Content: strings.TrimSpace(s)})
			p.input = updated
			p.formStore()
			return cmd
		}
		return nil
	}
	p.query += s
	p.searchActive = true
	p.cursor = 0
	p.window = 0
	p.confirm = -1
	p.cacheValid = false
	return nil
}

// ---- the dialog --------------------------------------------------------

// oauthLines renders the interactive OAuth dialog with optional width constraint.
func (p *Panel) oauthLines(width ...int) []string {
	if p == nil {
		return nil
	}
	oauth := p.oauth
	if oauth == nil {
		return nil
	}
	w := 0
	if len(width) > 0 {
		w = width[0]
	}

	isGoogle := oauth.Provider == "google-antigravity" || oauth.Provider == "antigravity"

	lines := []string{
		panelTitleOpen + "Authenticate with " + oauth.Name + panelTitleClose,
		"  " + searchUnderline,
		"",
		"  " + palette.DimGrayOpen + "Follow the instructions below to complete sign-in in your browser:" + palette.DimGrayClose,
		"",
	}

	if oauth.DeviceAuth != nil && oauth.DeviceAuth.VerificationURL != "" {
		url := oauth.DeviceAuth.VerificationURL
		if w > 10 {
			url = ansi.Truncate(url, w-6, "…")
		}
		lines = append(lines,
			"  1. Visit link in browser (opens automatically):",
			"     "+palette.BlueOpen+url+palette.BlueClose,
			"",
		)
	}

	if isGoogle {
		lines = append(lines, "  2. Paste authorization code below:")
		if oauth.CodeInput != "" {
			lines = append(lines, "     \x1b[1;32m"+oauth.CodeInput+"\x1b[7m \x1b[m", "")
		} else {
			lines = append(lines, "     \x1b[90m(paste authorization code with Cmd+V or Ctrl+V)\x1b[m", "")
		}
	} else if oauth.DeviceAuth != nil && oauth.DeviceAuth.UserCode != "" {
		lines = append(lines,
			"  2. Enter one-time authorization code:",
			"     \x1b[1;32m"+oauth.DeviceAuth.UserCode+"\x1b[m",
			"",
		)
	}

	braille := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	frame := braille[oauth.SpinFrame%len(braille)]
	var status string
	if isGoogle {
		status = "  " + palette.DimGrayOpen + frame + " Waiting for authorization code..." + palette.DimGrayClose
	} else {
		status = "  " + palette.DimGrayOpen + frame + " Waiting for authorization in browser..." + palette.DimGrayClose
	}
	if oauth.Notice != "" {
		status += oauth.Notice
	}
	lines = append(lines, status, "")

	if isGoogle && oauth.LocalToken != "" {
		emailDisplay := oauth.LocalEmail
		if emailDisplay == "" {
			emailDisplay = "active account"
		}
		localLine := "  " + palette.DimGrayOpen + "Local session detected: " + palette.DimGrayClose + "\x1b[1;36m" + emailDisplay + "\x1b[m (press 'a' to bind)"
		if w > 2 {
			localLine = ansi.Truncate(localLine, w-2, "…")
		}
		lines = append(lines, localLine, "")
	}

	if isGoogle {
		if oauth.CodeInput != "" {
			lines = append(lines, panelKeyboardHint("enter", "Submit Code", "backspace", "Delete", "cmd+v", "Paste", "y", "Copy URL", "esc", "Cancel"))
		} else if oauth.LocalToken != "" {
			if w > 0 && w < 100 {
				lines = append(lines, panelKeyboardHint("a", "Local Session", "cmd+v", "Paste", "enter", "Open", "esc", "Cancel"))
			} else {
				lines = append(lines, panelKeyboardHint("a", "Use Local Session", "cmd+v", "Paste Code", "enter", "Open Browser", "y/u", "Copy URL", "esc", "Cancel"))
			}
		} else {
			lines = append(lines, panelKeyboardHint("cmd+v", "Paste Code", "enter", "Open Browser", "y/u", "Copy URL", "esc", "Cancel"))
		}
	} else {
		lines = append(lines, panelKeyboardHint("c", "Copy Code", "y/u", "Copy URL", "enter", "Open Browser", "esc", "Cancel"))
	}
	return lines
}
