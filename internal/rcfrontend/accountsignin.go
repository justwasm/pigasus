// accountsignin.go — the web UI's "add account" flow.
//
// # What it is
//
// The TUI's /accounts panel adds an account with the login modal
// (internal/loginui), which runs the reference's own browser OAuth
// flow: the terminal opens Google's authorization page, the user
// approves, and the code that comes back is pasted into the modal.
//
// The web UI runs the same flow, with the two halves split between
// the browser and the server:
//
//	POST /__agy/api/accounts/add            → {"id": "…", "authUrl": "…"}
//	POST /__agy/api/accounts/add {"id": "…", "code": "…"}
//
// The server keeps the PKCE verifier — the half of the pair the
// browser never sees — and the widget shows the authorization URL and
// a field for the code.
//
// # Why not the device flow
//
// The device-authorization grant (internal/auth/device_flow.go) would
// be the better fit: no paste-back at all. Google's device endpoint
// refuses this client, though:
//
//	POST https://oauth2.googleapis.com/device/code
//	→ {"error":"invalid_client","error_description":"Invalid client type."}
//
// 【一手】 measured against the CLI's own public client id (the one the
// reference ships), 2026-10-26. The grant is not available for a
// desktop client, so the web UI uses the browser flow the reference
// uses.
//
// # What a finished sign-in does
//
// The exchange writes the new credential to the shared locations and
// registers the account, which makes it the active one — the same
// order the TUI's login modal uses (internal/cliapp/slash_panel_accounts.go:
// registerAccount after the sign-in).
package rcfrontend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
)

// The states a sign-in can be in.
const (
	// signinPending is the state between the start and the user's answer.
	signinPending = "pending"
	// signinDone is the state after a successful sign-in.
	signinDone = "done"
	// signinFailed is the state after a failed sign-in.
	signinFailed = "failed"
)

// signinExchangeTimeout bounds the code exchange.
//
// The exchange is one POST to Google's token endpoint, plus the
// userinfo probe the email needs: 30 seconds is the same budget the
// TUI's login modal gives it.
const signinExchangeTimeout = 30 * time.Second

// signinSessionTTL is how long an unfinished sign-in is kept.
//
// The verifier is only useful for the sign-in it belongs to, and the
// user may never come back to it.
const signinSessionTTL = time.Hour

// signinSession is one sign-in in flight.
type signinSession struct {
	mu        sync.Mutex
	id        string
	status    string
	authURL   string
	verifier  string
	email     string
	err       string
	createdAt time.Time
}

// setFailed ends the sign-in with an error.
func (s *signinSession) setFailed(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = signinFailed
	s.err = message
}

// setDone ends the sign-in with the account it produced.
func (s *signinSession) setDone(email string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = signinDone
	s.email = email
}

// snapshot renders the session's current state.
func (s *signinSession) snapshot() signinPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	return signinPayload{
		ID:      s.id,
		Status:  s.status,
		AuthURL: s.authURL,
		Email:   s.email,
		Error:   s.err,
	}
}

// verifierValue returns the PKCE verifier the session keeps.
func (s *signinSession) verifierValue() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verifier
}

// signinPayload is the answer the add endpoint returns.
type signinPayload struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// AuthURL is Google's authorization page: the user approves there
	// and comes back with a code.
	AuthURL string `json:"authUrl,omitempty"`
	Email   string `json:"email,omitempty"`
	Error   string `json:"error,omitempty"`
}

// serveAccountsAdd starts a sign-in, or carries it out.
//
// A request without an id starts a sign-in; a request with an id and a
// code carries it out; a request with an id alone answers the state of
// one in flight.
func (s *Server) serveAccountsAdd(w http.ResponseWriter, r *http.Request) {
	body, err := readAccountsBody(r)
	if err != nil {
		writeAccountsError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.accountsAuthorized(r, body) {
		writeAccountsError(w, http.StatusForbidden, "the accounts token is missing or wrong")
		return
	}
	id, code := signinRequestFrom(body, r)
	switch {
	case id == "":
		sess, err := s.startSignin()
		if err != nil {
			writeAccountsError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeAccountsJSON(w, http.StatusOK, sess.snapshot())
	case code == "":
		s.answerSigninState(w, id)
	default:
		s.completeSignin(w, r, id, code)
	}
}

// signinRequestFrom reads the sign-in session id and the code out of
// the request.
func signinRequestFrom(body []byte, r *http.Request) (id, code string) {
	var req struct {
		ID   string `json:"id"`
		Code string `json:"code"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	id = strings.TrimSpace(req.ID)
	if id == "" {
		id = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	code = strings.TrimSpace(req.Code)
	if code == "" {
		code = strings.TrimSpace(r.URL.Query().Get("code"))
	}
	return id, code
}

// answerSigninState answers the state of one sign-in.
func (s *Server) answerSigninState(w http.ResponseWriter, id string) {
	sess := s.signinSession(id)
	if sess == nil {
		writeAccountsError(w, http.StatusNotFound, "no such sign-in")
		return
	}
	writeAccountsJSON(w, http.StatusOK, sess.snapshot())
}

// signinSession returns the sign-in with the given id.
func (s *Server) signinSession(id string) *signinSession {
	s.acct.mu.Lock()
	defer s.acct.mu.Unlock()
	return s.acct.signins[id]
}

// startSignin starts a browser OAuth sign-in and returns the session.
//
// The verifier stays on the server: it is the half of the PKCE pair
// the browser never sees, and the code that comes back is useless
// without it.
func (s *Server) startSignin() (*signinSession, error) {
	verifier, _, state, err := auth.GeneratePKCE()
	if err != nil {
		return nil, err
	}
	sess := &signinSession{
		id:        newSigninID(),
		status:    signinPending,
		authURL:   auth.BuildAuthorizationURL(verifier, state),
		verifier:  verifier,
		createdAt: time.Now(),
	}
	s.acct.mu.Lock()
	s.pruneSigninsLocked()
	s.acct.signins[sess.id] = sess
	s.acct.mu.Unlock()
	return sess, nil
}

// pruneSigninsLocked drops the sign-ins nobody finished.
//
// A sign-in that was never carried out keeps its PKCE verifier alive for
// as long as the process runs, so the map is pruned when a new one starts.
// The caller holds the lock.
func (s *Server) pruneSigninsLocked() {
	for id, sess := range s.acct.signins {
		if time.Since(sess.createdAt) > signinSessionTTL {
			delete(s.acct.signins, id)
		}
	}
}

// newSigninID mints an id for a sign-in session.
func newSigninID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(raw)
}

// completeSignin exchanges the code and registers the account.
//
// The exchange writes the new credential to the shared locations: the
// sign-in *is* the activation, the way the TUI's login modal does it.
// The email comes from the token's id_token, which the exchange fills
// from the userinfo probe when the token itself carries no claim.
func (s *Server) completeSignin(w http.ResponseWriter, r *http.Request, id, code string) {
	sess := s.signinSession(id)
	if sess == nil {
		writeAccountsError(w, http.StatusNotFound, "no such sign-in")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), signinExchangeTimeout)
	defer cancel()

	tok, err := auth.ExchangeAuthorizationCode(ctx, code, sess.verifierValue())
	if err != nil {
		sess.setFailed(err.Error())
		writeAccountsJSON(w, http.StatusBadGateway, sess.snapshot())
		return
	}
	email := strings.TrimSpace(tok.UserEmail())
	if email == "" {
		email = strings.TrimSpace(auth.EffectiveUserEmail())
	}
	if email == "" {
		sess.setFailed("the sign-in succeeded but the account's email could not be resolved")
		writeAccountsJSON(w, http.StatusBadGateway, sess.snapshot())
		return
	}
	if _, _, err := accounts.RegisterWithEmail(email, tok); err != nil {
		sess.setFailed(err.Error())
		writeAccountsJSON(w, http.StatusInternalServerError, sess.snapshot())
		return
	}
	sess.setDone(email)
	writeAccountsJSON(w, http.StatusOK, sess.snapshot())
}
