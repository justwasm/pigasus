package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/accountspanel"
)

// AccountsHost defines the host operations needed by the /accounts panel.
type AccountsHost interface {
	ApplyActiveAccount(email string)
	SignOutActiveAccount()
	StartAccountLogin() tea.Cmd
	FetchPlanTierCmd() tea.Cmd
	FetchQuotaCmd(email string) tea.Cmd
	SessionTierFor(email string) (string, bool)
	ClosePanel() tea.Cmd
}

// NewAccountsPanel builds the /accounts panel with the given host seams.
func NewAccountsPanel(h AccountsHost) *accountspanel.Panel {
	return accountspanel.New(accountspanel.Seams{
		ApplyAccount: h.ApplyActiveAccount,
		SignOut:      h.SignOutActiveAccount,
		StartLogin:   h.StartAccountLogin,
		RefreshTier:  h.FetchPlanTierCmd,
		QuotaCmd:     h.FetchQuotaCmd,
		SessionTier:  h.SessionTierFor,
		Close:        h.ClosePanel,
	})
}
