package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/features/auth"
)

// pkceSession holds an in-flight OAuth sign-in request.
type pkceSession struct {
	ID        string    `json:"id"`
	AuthURL   string    `json:"auth_url"`
	Verifier  string    `json:"-"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

type oauthSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*pkceSession
}

var globalAuthStore = &oauthSessionStore{
	sessions: make(map[string]*pkceSession),
}

func (s *oauthSessionStore) createSession() (*pkceSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prune sessions older than 30 minutes
	now := time.Now()
	for id, sess := range s.sessions {
		if now.Sub(sess.CreatedAt) > 30*time.Minute {
			delete(s.sessions, id)
		}
	}

	verifier, _, state, err := auth.GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE: %w", err)
	}

	authURL := auth.BuildAuthorizationURL(verifier, state)

	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("failed to generate session id: %w", err)
	}
	id := hex.EncodeToString(idBytes)

	sess := &pkceSession{
		ID:        id,
		AuthURL:   authURL,
		Verifier:  verifier,
		State:     state,
		CreatedAt: now,
	}

	s.sessions[id] = sess
	return sess, nil
}

func (s *oauthSessionStore) getSession(id string) (*pkceSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *oauthSessionStore) removeSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// sanitizeCode extracts the authorization code even if the user pasted
// a full redirect URL (e.g. https://antigravity.google/oauth-callback?code=...&state=...)
func sanitizeCode(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "code=") {
		// Attempt parsing as URL or query string
		if u, err := url.Parse(raw); err == nil && u.Query().Get("code") != "" {
			return u.Query().Get("code")
		}
		// Fallback query split
		idx := strings.Index(raw, "code=")
		sub := raw[idx+5:]
		if amp := strings.Index(sub, "&"); amp != -1 {
			sub = sub[:amp]
		}
		if unesc, err := url.QueryUnescape(sub); err == nil {
			return unesc
		}
		return sub
	}
	return raw
}

// ExchangeToken exchanges the authorization code for an AntigravityToken.
func ExchangeToken(ctx context.Context, sessionID, rawCode string) (*auth.AntigravityToken, error) {
	code := sanitizeCode(rawCode)
	if code == "" {
		return nil, errors.New("empty authorization code")
	}

	sess, ok := globalAuthStore.getSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("invalid or expired oauth session id: %s", sessionID)
	}

	// Code exchange with Google OAuth2
	tok, err := auth.ExchangeAuthorizationCode(ctx, code, sess.Verifier)
	if err != nil {
		return nil, fmt.Errorf("oauth token exchange failed: %w", err)
	}

	// Clean up session once successfully exchanged
	globalAuthStore.removeSession(sessionID)
	return tok, nil
}

// GetExistingActiveToken loads the currently cached CLI token if available, refreshing if expired.
func GetExistingActiveToken(ctx context.Context) (*auth.AntigravityToken, error) {
	tok, err := auth.LoadTokenUnified()
	if err != nil {
		return nil, err
	}
	if tok != nil && tok.Token.RefreshToken != "" {
		if updated, _, err := tok.RefreshIfNeeded(ctx); err == nil && updated != nil {
			return updated, nil
		}
	}
	return tok, nil
}

// RefreshExistingToken refreshes an access token using a refresh token.
func RefreshExistingToken(ctx context.Context, refreshToken string) (*auth.AntigravityToken, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, errors.New("empty refresh token")
	}
	tok := &auth.AntigravityToken{
		Token: auth.AntigravityAccessToken{
			RefreshToken: refreshToken,
			TokenType:    "Bearer",
		},
		AuthMethod: "consumer",
	}
	updated, _, err := tok.RefreshQuiet(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	return updated, nil
}
