// Package rclink builds the browser links that reach this
// installation's remote-control instance.
//
// # What the link has to solve
//
// The companion dashboard's instance rows are **per Google account**:
// the cloud's registry keys them by the account that registered them
// (X-Goog-AuthUser), and the dashboard's own "/u/<N>/" path segment
// selects one — the index of the account in the browser's account
// chooser.
//
// The bare link ("{companionOrigin}/r/{instanceID}") resolves against
// the browser's *default* account instead. With several accounts signed
// in, the cloud then answers "No instance found. Try switching to a
// different account." — the instance is registered, and the dashboard
// simply looks in the wrong account's row.
//
// Google's account chooser resolves that. It asks which account to use
// — naming the one the daemon registered with — and then continues to
// the target. So the link the clone prints goes through
// accounts.google.com/AccountChooser:
//
//	https://accounts.google.com/AccountChooser?Email=<address>&continue=<deep link>
//
// The "continue" parameter carries the whole deep link, percent-encoded.
// The parameters come out in the order Google's own links carry them
// ("Email" first) because url.Values.Encode sorts the keys.
//
// # What is clone-side
//
// This shape is the **clone's own**. The reference prints the bare
// companion origin and asks the user to look for the instance by name
// ("Open https://antigravity.google.com and look for %q."); it never
// builds a chooser link. The deep link it points at is the reference's
// own — "{companionOrigin}/r/{instanceID}" — and the chooser around it
// is not.
//
// See docs/cycles/rc-cloud-remote-view/deliverable.md §7.4 for the
// account-index measurement this answers.
package link

import (
	"strings"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
)

// DeepLink returns the bare link to an instance's remote view:
// "{companionOrigin}/r/{instanceID}".
//
// rawQuery is appended when it is not empty — the conversation-scoped
// form carries "p=c/<conversation id>".
func DeepLink(instanceID, rawQuery string) string {
	origin := strings.TrimRight(strings.TrimSpace(endpoints.Current().CompanionOrigin), "/")
	link := origin + "/r/" + instanceID
	if q := strings.TrimSpace(rawQuery); q != "" {
		link += "?" + q
	}
	return link
}

// BrowserURL returns the URL a user opens to reach an instance's remote
// view.
//
// It is the account chooser wrapping the deep link when the signed-in
// account's address is known, and the deep link itself when it is not:
// a chooser that names no account chooses nothing, and the link would
// only cost the user a click.
func BrowserURL(instanceID, rawQuery string) string {
	return BrowserURLForAccount(ActiveAccount(), instanceID, rawQuery)
}

// BrowserURLForAccount is BrowserURL with an explicit account.
//
// An empty email yields the deep link, exactly as BrowserURL does when
// nothing is signed in.
func BrowserURLForAccount(email, instanceID, rawQuery string) string {
	return endpoints.Current().AccountChooserURL(email, DeepLink(instanceID, rawQuery))
}

// ActiveAccount returns the signed-in account's email address, or ""
// when nothing is signed in.
func ActiveAccount() string {
	_, email := accounts.Current()
	if strings.TrimSpace(email) == "" {
		email = auth.EffectiveUserEmail()
	}
	return strings.TrimSpace(email)
}
