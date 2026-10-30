// render.go — account row formatting and quota rendering for account lists and panels.
package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// RowLine renders one account row for the /accounts panel.
//
// The row carries the three things the panel knows about the account:
// the email, the cached plan tier and the cached quota. The quota is
// the weekly and the five-hour remaining — the lowest across the
// model groups, because that is the window that decides whether the
// account can keep working.
func RowLine(acc Account, cursor bool, activeEmail string, emailPad int, blueOpen, blueClose string) string {
	email := acc.Email
	if strings.TrimSpace(email) == "" {
		email = "(unknown)"
	}
	active := acc.Email != "" && strings.EqualFold(acc.Email, activeEmail)
	marker := ""
	if active {
		marker = "(active)"
	}
	tier := acc.PlanTier
	if strings.TrimSpace(tier) == "" {
		tier = "-"
	}
	name := email
	pad := emailPad - len(name)
	if pad < 1 {
		pad = 1
	}
	value := fmt.Sprintf("%-20s %-22s %-4s %s", tier,
		QuotaCell(acc), QuotaSnapshotAge(acc.Quota), marker)
	if cursor {
		return blueOpen + "> " + name + blueClose + strings.Repeat(" ", pad) + value
	}
	return "  " + name + strings.Repeat(" ", pad) + value
}

// QuotaCell renders the account's cached quota.
//
// The percentages are right-aligned, so the numbers line up down the
// column. A dash means the account has no cached quota: it was never
// queried, or the query failed.
func QuotaCell(acc Account) string {
	return acc.QuotaCell()
}

// QuotaSnapshotAge renders how old a cached quota is, so a stale
// number is not read as a live one.
func QuotaSnapshotAge(snap *auth.QuotaSnapshot) string {
	if snap == nil || snap.FetchedAt.IsZero() {
		return "-"
	}
	d := time.Since(snap.FetchedAt)
	switch {
	case d < 2*time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
