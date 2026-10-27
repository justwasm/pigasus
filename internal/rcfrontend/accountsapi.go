// accountsapi.go — the web UI's account switcher API.
//
// # What it is
//
// The TUI's /accounts panel (internal/cliapp/slash_panel_accounts.go)
// switches, adds and removes accounts. The web UI gets the same three
// operations over HTTP, so the browser can do what the terminal does:
//
//	GET  /__agy/api/accounts            the accounts and the active one
//	GET  /__agy/api/accounts/quota      one account's remaining quota
//	POST /__agy/api/accounts/switch     {"email": "..."}
//	POST /__agy/api/accounts/remove     {"email": "..."}
//	POST /__agy/api/accounts/add        start a sign-in
//	POST /__agy/api/accounts/add        {"id": "..."} — the sign-in's state
//
// The reference (`agy`) has one credential and no switcher, so this is a
// clone-only superset, like /provider and the /accounts panel.
//
// # The credential is shared
//
// A switch writes the account's token into the shared credential
// locations (internal/accounts.Activate → auth.SaveTokenUnified), so
// the running session talks as the new account from its next turn: the
// backend is built per turn (internal/rchost/remote_control_backends.go)
// and reads the credential then. The installed `agy` reads the same
// keychain entry, so it follows along.
//
// # Who may call it
//
// The endpoint mutates the credential, so it is guarded by a token that
// is generated per process and written into the page the server serves
// (see accountsWidgetHTML). A caller that did not load the page cannot
// guess it, and the endpoint sends no CORS header, so a page on
// another origin cannot read the answer either.
//
// The remote-control tunnel turns every proxied request into a POST
// (internal/rctunnel/httpproxy.go), so every endpoint answers a POST
// as well as a GET: a caller that reaches the server through the
// cloud tunnel must not get a 405 for using the wrong verb.
package rcfrontend

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/rctoken"
)

const (
	// accountsAPIPath is the endpoint the widget reads the account list from.
	accountsAPIPath = "/__agy/api/accounts"
	// accountsQuotaPath answers one account's remaining quota.
	accountsQuotaPath = "/__agy/api/accounts/quota"
	// accountsSwitchPath activates another account.
	accountsSwitchPath = "/__agy/api/accounts/switch"
	// accountsRemovePath forgets an account.
	accountsRemovePath = "/__agy/api/accounts/remove"
	// accountsAddPath starts a sign-in, and answers the state of one in flight.
	accountsAddPath = "/__agy/api/accounts/add"
	// accountsTokenHeader carries the per-process token.
	accountsTokenHeader = "X-Agy-Accounts-Token"

	// accountsQuotaFreshness is how long a cached quota counts as
	// current. The endpoint refreshes it when it is older than this, so
	// opening the quota section twice in a row costs one query.
	accountsQuotaFreshness = 2 * time.Minute

	// accountsBodyLimit is the largest request body the API reads.
	accountsBodyLimit = 1 << 20
)

// accountsState is the server's account-switcher state: the token the
// page carries, and the sign-ins in flight.
//
// It has its own lock: the Server's own mutex guards the patched HTML
// and the patched bundle, and holding it across a registry write would
// block every page load.
type accountsState struct {
	mu      sync.Mutex
	token   string
	signins map[string]*signinSession
}

// newAccountsState returns the state with a fresh token.
//
// The token is random per process: it is the only thing that separates
// "the page this server served" from "a page somewhere else", and a
// constant would separate nothing.
func newAccountsState() accountsState {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		// A failing CSPRNG is not a reason to serve an unguarded
		// endpoint: an empty token disables the API instead.
		return accountsState{signins: map[string]*signinSession{}}
	}
	return accountsState{
		token:   hex.EncodeToString(raw),
		signins: map[string]*signinSession{},
	}
}

// serveAccountsAPI routes the account switcher's endpoints.
func (s *Server) serveAccountsAPI(w http.ResponseWriter, r *http.Request, path string) {
	switch path {
	case accountsAPIPath:
		s.serveAccountsList(w, r)
	case accountsQuotaPath:
		s.serveAccountsQuota(w, r)
	case accountsSwitchPath:
		s.serveAccountsSwitch(w, r)
	case accountsRemovePath:
		s.serveAccountsRemove(w, r)
	case accountsAddPath:
		s.serveAccountsAdd(w, r)
	default:
		writeAccountsError(w, http.StatusNotFound, "unknown accounts endpoint")
	}
}

// serveAccountsList answers the account list.
func (s *Server) serveAccountsList(w http.ResponseWriter, r *http.Request) {
	body, err := readAccountsBody(r)
	if err != nil {
		writeAccountsError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.accountsAuthorized(r, body) {
		writeAccountsError(w, http.StatusForbidden, "the accounts token is missing or wrong")
		return
	}
	payload, err := accountsPayloadFor()
	if err != nil {
		writeAccountsError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAccountsJSON(w, http.StatusOK, payload)
}

// serveAccountsQuota answers one account's remaining quota.
//
// The app's own quota panel is fed by the RetrieveUserQuotaSummary RPC,
// which the clone's dispatcher answers (internal/remotecontrol/
// dispatch_quota.go) from the *active* account. This endpoint is the
// widget's own read: it names an account, so the widget can show every
// registered account's usage and not only the active one.
//
// The quota belongs to the account behind the token, so it is queried
// with that account's own credential (accounts.FetchQuota). A cached
// snapshot younger than accountsQuotaFreshness is served as it is —
// the switch warms the cache in the background, so the panel opens
// with the new account's numbers instead of a spinner.
func (s *Server) serveAccountsQuota(w http.ResponseWriter, r *http.Request) {
	body, err := readAccountsBody(r)
	if err != nil {
		writeAccountsError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.accountsAuthorized(r, body) {
		writeAccountsError(w, http.StatusForbidden, "the accounts token is missing or wrong")
		return
	}
	email := accountsQuotaEmail(body, r)
	if email == "" {
		writeAccountsError(w, http.StatusNotFound, "no account is signed in")
		return
	}
	answer := accountsQuotaPayload{Email: email}
	if !accountsQuotaIsFresh(email) {
		if _, err := accounts.FetchQuota(r.Context(), email); err != nil {
			answer.Error = err.Error()
		}
	}
	if acc, ok, err := accounts.Get(email); err == nil && ok {
		answer.Usage = usagePayloadFor(acc)
	}
	writeAccountsJSON(w, http.StatusOK, answer)
}

// accountsQuotaEmail picks the account the quota is asked for: the
// request names one, or the active account is meant.
func accountsQuotaEmail(body []byte, r *http.Request) string {
	var req struct {
		Email string `json:"email"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = strings.TrimSpace(r.URL.Query().Get("email"))
	}
	if email == "" {
		_, email = accounts.Current()
	}
	if email == "" {
		// The shared credential is the truth, but the registry knows
		// which account it was last used by.
		if st, err := accounts.Load(); err == nil {
			email = st.Active
		}
	}
	return email
}

// accountsQuotaIsFresh reports whether the registry holds a quota for
// the account that is recent enough to serve without a query.
func accountsQuotaIsFresh(email string) bool {
	acc, ok, err := accounts.Get(email)
	if err != nil || !ok || acc.Quota == nil {
		return false
	}
	return time.Since(acc.Quota.FetchedAt) < accountsQuotaFreshness
}

// serveAccountsSwitch activates another account.
//
// The switch is the TUI's switch: the token is written to the shared
// credential locations, so the clone, the web UI and the installed
// `agy` all use the new account from their next request.
func (s *Server) serveAccountsSwitch(w http.ResponseWriter, r *http.Request) {
	body, err := readAccountsBody(r)
	if err != nil {
		writeAccountsError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.accountsAuthorized(r, body) {
		writeAccountsError(w, http.StatusForbidden, "the accounts token is missing or wrong")
		return
	}
	email, err := accountsEmailFrom(body, r)
	if err != nil {
		writeAccountsError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := accounts.Activate(r.Context(), email); err != nil {
		writeAccountsError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The new account's quota is fetched in the background: the switch
	// itself must not wait for Google's Cloud Code endpoint, and the
	// quota section opens with the numbers that are already cached.
	warmAccountsQuota(email)
	s.answerAccountsList(w)
}

// warmAccountsQuota fetches an account's quota without blocking the
// caller. The registry keeps the snapshot, so the next read is cheap.
func warmAccountsQuota(email string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = accounts.FetchQuota(ctx, email)
	}()
}

// serveAccountsRemove forgets an account.
//
// Removing the active account signs out for real: the shared credential
// is cleared, so neither the clone nor the reference keeps using it.
// When another account is left the session continues on it — the point
// of the whole feature is that a sign-out does not end the session.
func (s *Server) serveAccountsRemove(w http.ResponseWriter, r *http.Request) {
	body, err := readAccountsBody(r)
	if err != nil {
		writeAccountsError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.accountsAuthorized(r, body) {
		writeAccountsError(w, http.StatusForbidden, "the accounts token is missing or wrong")
		return
	}
	email, err := accountsEmailFrom(body, r)
	if err != nil {
		writeAccountsError(w, http.StatusBadRequest, err.Error())
		return
	}
	acc, active, err := accounts.Remove(email)
	if err != nil {
		writeAccountsError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if acc.Email == "" {
		writeAccountsError(w, http.StatusNotFound, fmt.Sprintf("no account %s", email))
		return
	}
	if active {
		s.continueOnAnotherAccount(r.Context())
	}
	s.answerAccountsList(w)
}

// continueOnAnotherAccount activates another account after a sign-out.
//
// It is the TUI's rule: the sign-out is real, but the session carries
// on when a registered account is left.
func (s *Server) continueOnAnotherAccount(ctx context.Context) {
	st, err := accounts.Load()
	if err != nil || len(st.Accounts) == 0 {
		return
	}
	_, _ = accounts.Activate(ctx, st.Accounts[0].Email)
}

// answerAccountsList writes the current account list.
func (s *Server) answerAccountsList(w http.ResponseWriter) {
	payload, err := accountsPayloadFor()
	if err != nil {
		writeAccountsError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAccountsJSON(w, http.StatusOK, payload)
}

// accountsPayload is the answer the list endpoint returns.
type accountsPayload struct {
	Accounts []accountPayload `json:"accounts"`
	Active   string           `json:"active"`
	Registry string           `json:"registryPath"`
}

// accountPayload is one row of the widget's list.
//
// The tier and the quota are the *cached* answers: the registry keeps
// them next to the account's token, so the widget renders a row
// without a round trip per account.
type accountPayload struct {
	Email    string               `json:"email"`
	Active   bool                 `json:"active"`
	PlanTier string               `json:"planTier,omitempty"`
	Quota    string               `json:"quota,omitempty"`
	Usage    *accountUsagePayload `json:"usage,omitempty"`
	AddedAt  string               `json:"addedAt,omitempty"`
	LastUsed string               `json:"lastUsed,omitempty"`
}

// accountUsagePayload is one account's cached quota, in the shape the
// web UI's quota section renders: one group per model family, one
// bucket per window.
type accountUsagePayload struct {
	FetchedAt string              `json:"fetchedAt,omitempty"`
	Groups    []usageGroupPayload `json:"groups,omitempty"`
}

// usageGroupPayload is one model family's quota.
type usageGroupPayload struct {
	Name        string               `json:"name,omitempty"`
	Description string               `json:"description,omitempty"`
	Buckets     []usageBucketPayload `json:"buckets,omitempty"`
}

// usageBucketPayload is one quota window's state.
//
// RemainingFraction is the fraction that is left (1 is a full window),
// the same number the Cloud Code endpoint answers.
type usageBucketPayload struct {
	Label             string  `json:"label,omitempty"`
	Window            string  `json:"window,omitempty"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetAt           string  `json:"resetAt,omitempty"`
}

// accountsQuotaPayload is the answer the quota endpoint returns.
type accountsQuotaPayload struct {
	Email string               `json:"email"`
	Usage *accountUsagePayload `json:"usage,omitempty"`
	Error string               `json:"error,omitempty"`
}

// usagePayloadFor renders an account's cached quota.
//
// The registry holds the snapshot the /accounts panel caches, so the
// same numbers are shown without a second query.
func usagePayloadFor(acc accounts.Account) *accountUsagePayload {
	if acc.Quota == nil {
		return nil
	}
	out := &accountUsagePayload{FetchedAt: formatAccountsTime(acc.Quota.FetchedAt)}
	for _, g := range acc.Quota.Groups {
		group := usageGroupPayload{Name: g.Name, Description: g.Description}
		for _, b := range g.Buckets {
			group.Buckets = append(group.Buckets, usageBucketPayload{
				Label:             b.DisplayName,
				Window:            auth.QuotaWindow(b.DisplayName, b.Window),
				RemainingFraction: b.RemainingFraction,
				ResetAt:           formatAccountsTime(b.ResetTime),
			})
		}
		out.Groups = append(out.Groups, group)
	}
	return out
}

// accountsPayloadFor reads the registry and the shared credential.
//
// The credential is the truth about which account is active: the
// reference can change it too, so the registry's own Active field is
// only a hint.
func accountsPayloadFor() (accountsPayload, error) {
	st, err := accounts.Load()
	if err != nil {
		return accountsPayload{}, err
	}
	_, active := accounts.Current()
	if strings.TrimSpace(active) == "" {
		active = st.Active
	}
	out := accountsPayload{
		Active:   active,
		Registry: accounts.Path(),
		Accounts: make([]accountPayload, 0, len(st.Accounts)),
	}
	for _, acc := range st.Accounts {
		out.Accounts = append(out.Accounts, accountPayload{
			Email:    acc.Email,
			Active:   strings.EqualFold(acc.Email, active),
			PlanTier: acc.PlanTier,
			Quota:    strings.TrimSpace(acc.QuotaCell()),
			Usage:    usagePayloadFor(acc),
			AddedAt:  formatAccountsTime(acc.AddedAt),
			LastUsed: formatAccountsTime(acc.LastUsed),
		})
	}
	return out, nil
}

// formatAccountsTime renders a registry timestamp, or "" when it is unset.
func formatAccountsTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// accountsEmailFrom reads the target account's email from the request.
//
// The body is the normal way; the query parameter is the fallback for
// a GET, which has no body.
func accountsEmailFrom(body []byte, r *http.Request) (string, error) {
	var req struct {
		Email string `json:"email"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = strings.TrimSpace(r.URL.Query().Get("email"))
	}
	if email == "" {
		return "", errors.New("no account email given")
	}
	return email, nil
}

// accountsAuthorized reports whether the caller carries the server's token.
//
// The token is generated per process and written into the page the
// server serves, so only a client that loaded the page can act.
//
// Cycle rc-auth-bearer-required: the page's own secret is accepted as
// well. A peer that reached the page from outside the loopback
// interface had to present that secret to get the page at all, so
// the account switcher has to accept it — the page carries the
// widget's token only for the peers that may have the page, and the
// master secret is what a remote peer holds.
func (s *Server) accountsAuthorized(r *http.Request, body []byte) bool {
	want := s.acct.token
	if want == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get(accountsTokenHeader))
	if got == "" {
		got = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if got == "" {
		got = accountsTokenFromBody(body)
	}
	if got == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
		return true
	}
	return s.opts.CSRFToken != "" && rctoken.Verify(s.opts.CSRFToken, got)
}

// accountsTokenFromBody reads the token out of a JSON body.
func accountsTokenFromBody(body []byte) string {
	var req struct {
		Token string `json:"token"`
	}
	if len(body) == 0 {
		return ""
	}
	_ = json.Unmarshal(body, &req)
	return strings.TrimSpace(req.Token)
}

// readAccountsBody reads the request body once and puts it back, so the
// token check and the parameter decoding can both read it.
func readAccountsBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, accountsBodyLimit))
	_ = r.Body.Close()
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return raw, nil
}

// writeAccountsJSON writes a JSON answer.
//
// It sends no CORS header on purpose: the endpoint answers a page the
// server itself served, and a header that let another origin read the
// answer would undo the token.
func writeAccountsJSON(w http.ResponseWriter, code int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "accounts: encode", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write(append(raw, '\n'))
}

// writeAccountsError writes a JSON error.
func writeAccountsError(w http.ResponseWriter, code int, message string) {
	writeAccountsJSON(w, code, map[string]string{"error": message})
}
