// accounts_cmds.go — `agy accounts …`.
//
// # Why a separate command
//
// The reference (`agy`) keeps exactly one credential, so it has no
// account registry and no command for one. The clone's registry is a
// superset: the accounts the user signed in to, and the switch between
// them.
//
// The command is hidden, like the other openagy-only subcommands:
// `agy --help` lists the reference's twelve visible subcommands and
// nothing else (see internal/cli/parser_test.go).
//
// # What a switch writes
//
// `agy accounts switch <email>` writes the account's token to the shared
// credential locations — the macOS keychain entry the reference reads
// (service "gemini", account "antigravity") and the three on-disk
// paths — so the installed `agy` runs as that account from its next
// start-up. See internal/accounts.
package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/accounts"
)

// ---- usage ---------------------------------------------------------------

func usageAccounts(n string) string {
	return "Usage: " + n + " accounts <subcommand> [flags]\n" +
		"\n" +
		"Manage the signed-in Google accounts\n" +
		"\n" +
		"Subcommands:\n" +
		"  list     List the known accounts\n" +
		"  switch   Make one account the active one\n" +
		"  remove   Forget one account\n" +
		"\n" +
		"The registry is " + accounts.Path() + "\n"
}

func usageAccountsSwitch(n string) string {
	return "Usage: " + n + " accounts switch <email>\n" +
		"\n" +
		"Make one account the active one\n"
}

func usageAccountsRemove(n string) string {
	return "Usage: " + n + " accounts remove <email>\n" +
		"\n" +
		"Forget one account\n"
}

// ---- handlers ------------------------------------------------------------

// runAccountsList prints the accounts the clone knows about and marks
// the active one.
func runAccountsList(j *JetskiCLI, args []string) error {
	st, err := accounts.Load()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	active := ""
	if _, email := accounts.Current(); email != "" {
		active = email
	} else if st.Active != "" {
		active = st.Active
	}
	fmt.Fprintf(j.Out, "accounts: %s\n", accounts.Path())
	if len(st.Accounts) == 0 {
		fmt.Fprintln(j.Out, "no accounts signed in")
		return nil
	}
	for _, acc := range st.Accounts {
		marker := ""
		if strings.EqualFold(acc.Email, active) {
			marker = "  (active)"
		}
		tier := acc.PlanTier
		if strings.TrimSpace(tier) == "" {
			tier = "-"
		}
		weekly, fiveHour := accountsQuotaColumns(acc)
		fmt.Fprintf(j.Out, "%-40s %-18s %-16s %-16s%s\n", acc.Email, tier, weekly, fiveHour, marker)
	}
	return nil
}

// accountsQuotaColumns renders an account's cached quota: the weekly
// and the five-hour remaining, the lowest across the model groups.
//
// The numbers come from the registry's cache, so the command never
// queries the server: the /accounts panel is what refreshes them.
//
// The percentages are right-aligned in a fixed-width column, so they
// line up down the list.
func accountsQuotaColumns(acc accounts.Account) (string, string) {
	return acc.QuotaColumns()
}

// runAccountsSwitch makes one account the active one.
func runAccountsSwitch(j *JetskiCLI, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return usageFail(j, usageAccountsSwitch(j.Name), "Error: usage: accounts switch <email>")
	}
	email := strings.TrimSpace(args[0])
	acc, err := accounts.Activate(context.Background(), email)
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	fmt.Fprintf(j.Out, "active account: %s\n", acc.Email)
	return nil
}

// runAccountsRemove forgets one account.
func runAccountsRemove(j *JetskiCLI, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return usageFail(j, usageAccountsRemove(j.Name), "Error: usage: accounts remove <email>")
	}
	email := strings.TrimSpace(args[0])
	acc, active, err := accounts.Remove(email)
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	if acc.Email == "" {
		return errExitCodeMsg(1, fmt.Sprintf("Error: no account named %q", email))
	}
	if active {
		fmt.Fprintf(j.Out, "removed %s (signed out)\n", acc.Email)
		return nil
	}
	fmt.Fprintf(j.Out, "removed %s\n", acc.Email)
	return nil
}

// runAccountsParent is the no-subcommand form: it prints the list.
func runAccountsParent(j *JetskiCLI, args []string) error {
	if len(args) == 0 {
		return runAccountsList(j, args)
	}
	return usageFail(j, usageAccounts(j.Name), "Error: unknown accounts subcommand")
}
