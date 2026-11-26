package livevoice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/justwasm/pigasus/geminicode"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/voice"
)

func makeTestAccount(email, accessToken string, remaining float64) accounts.Account {
	return accounts.Account{
		Email: email,
		Token: &auth.AntigravityToken{
			Token: auth.AntigravityAccessToken{
				AccessToken: accessToken,
				TokenType:   "Bearer",
				Expiry:      time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
			IDTokenEmail: email,
		},
		Quota: &auth.QuotaSnapshot{
			FetchedAt: time.Now(),
			Groups: []auth.QuotaGroupSnapshot{
				{
					Name: "Gemini",
					Buckets: []auth.QuotaBucketSnapshot{
						{DisplayName: "Gemini 3.8 Flash", RemainingFraction: remaining},
					},
				},
			},
		},
	}
}

func setupTestPool(t *testing.T, accs ...accounts.Account) *accountrouting.Router {
	t.Helper()
	dir := t.TempDir()
	registry := filepath.Join(dir, "accounts.json")
	config := filepath.Join(dir, "config.json")

	active := ""
	if len(accs) > 0 {
		active = accs[0].Email
	}
	doc := map[string]any{
		"active":   active,
		"accounts": accs,
	}
	regBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal accounts: %v", err)
	}
	if err := os.WriteFile(registry, regBytes, 0o600); err != nil {
		t.Fatalf("write accounts.json: %v", err)
	}
	cfgDoc := map[string]any{
		"account_pool": map[string]any{
			"enabled":               true,
			"max_attempts":          3,
			"switch_on_quota_error": true,
			"aggregate":             map[string]any{"enabled": true},
		},
	}
	cfgBytes, err := json.MarshalIndent(cfgDoc, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(config, cfgBytes, 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", registry)
	t.Setenv("OPENAGY_CONFIG", config)
	t.Setenv("OPENAGY_CONFIG_HOME", dir)

	r := accountrouting.NewRouter()
	r.SetPolicy(EffectivePolicy)
	r.SetLoadShared(func() (*auth.AntigravityToken, string, error) {
		if len(accs) > 0 && accs[0].Token != nil {
			return accs[0].Token, accs[0].Email, nil
		}
		return nil, "", auth.ErrNoToken
	})
	r.SetFallback(func(context.Context) (string, error) {
		if len(accs) > 0 && accs[0].Token != nil {
			return accs[0].Token.Token.AccessToken, nil
		}
		return "", auth.ErrNoToken
	})
	ResetSharedRouterForTest(r)
	t.Cleanup(func() {
		ResetSharedRouterForTest(nil)
	})
	return r
}

type stubLiveBackend struct {
	createFn func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error)
}

func (b *stubLiveBackend) CreateSession(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
	return b.createFn(ctx, cfg)
}

type stubHandshakeSession struct {
	voice.LiveSession
	hsErr error
}

func (s *stubHandshakeSession) WaitHandshake(time.Duration) error {
	return s.hsErr
}

func TestPoolBackend_OAuthPoolFailoverAndHandshakeProbe(t *testing.T) {
	origBackoff := BackoffFunc
	BackoffFunc = func(int, string) time.Duration { return 0 }
	defer func() { BackoffFunc = origBackoff }()

	acc1 := makeTestAccount("acc1@example.com", "tok-1", 0.95)
	acc2 := makeTestAccount("acc2@example.com", "tok-2", 0.85)
	acc3 := makeTestAccount("acc3@example.com", "tok-3", 0.75)
	router := setupTestPool(t, acc1, acc2, acc3)

	var tried []string
	backend, err := ResolveBackend(Options{
		Router:         router,
		APIKeyFn:       func() string { return "" },
		HandshakeProbe: 50 * time.Millisecond,
		OAuthFactory: func(token string) voice.LiveBackend {
			tried = append(tried, token)
			return &stubLiveBackend{
				createFn: func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
					switch token {
					case "tok-1":
						return nil, errors.New("rpc error: code = Unavailable desc = MODEL_CAPACITY_EXHAUSTED")
					case "tok-2":
						// Succeeds CreateSession, but fails immediate handshake probe with PermissionDenied
						mb := voice.NewMockLiveBackend()
						s, _ := mb.CreateSession(ctx, cfg)
						return &stubHandshakeSession{
							LiveSession: s,
							hsErr:       errors.New("rpc error: code = PermissionDenied desc = insufficient authentication scopes"),
						}, nil
					default:
						mb := voice.NewMockLiveBackend()
						return mb.CreateSession(ctx, cfg)
					}
				},
			}
		},
	})
	if err != nil {
		t.Fatalf("ResolveBackend failed: %v", err)
	}

	sess, err := backend.CreateSession(context.Background(), voice.LiveSessionConfig{Model: "gemini-3.8-live"})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	defer sess.Close()

	if len(tried) != 3 || tried[0] != "tok-1" || tried[1] != "tok-2" || tried[2] != "tok-3" {
		t.Fatalf("unexpected failover sequence: %v", tried)
	}
	if !router.IsExcluded("acc1@example.com", "gemini") {
		t.Errorf("expected acc1 to be excluded for gemini family after Unavailable")
	}
	if !router.IsExcluded("acc2@example.com") {
		t.Errorf("expected acc2 to be excluded after PermissionDenied handshake failure")
	}
}

func TestPoolBackend_APIKeyTakesPrecedenceOverOAuthPool(t *testing.T) {
	origBackoff := BackoffFunc
	BackoffFunc = func(int, string) time.Duration { return 0 }
	defer func() { BackoffFunc = origBackoff }()

	acc1 := makeTestAccount("pool@example.com", "pool-tok", 0.90)
	router := setupTestPool(t, acc1)

	var apiKeyHit bool
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") == "primary-api-key" || strings.Contains(r.URL.RawQuery, "key=primary-api-key") {
			apiKeyHit = true
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		_, _, _ = c.Read(r.Context())
		_ = c.Write(r.Context(), websocket.MessageText, []byte(`{"setupComplete":{}}`))
		<-r.Context().Done()
	}))
	defer wsSrv.Close()
	t.Setenv("OPENAGY_VOICE_URL", "ws"+strings.TrimPrefix(wsSrv.URL, "http"))

	var oauthTried bool
	backend, err := ResolveBackend(Options{
		Router: router,
		APIKey: "primary-api-key",
		OAuthFactory: func(token string) voice.LiveBackend {
			oauthTried = true
			return &stubLiveBackend{
				createFn: func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
					return nil, errors.New("should not be called when API key succeeds")
				},
			}
		},
	})
	if err != nil {
		t.Fatalf("ResolveBackend failed: %v", err)
	}

	sess, err := backend.CreateSession(context.Background(), voice.LiveSessionConfig{Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	_ = sess.Close()

	if oauthTried {
		t.Fatalf("expected Gemini API key to take precedence over OAuth pool by default")
	}
	if !apiKeyHit {
		t.Fatalf("expected Gemini API key endpoint to be hit")
	}
}

func TestPoolBackend_PreferOAuthFallsBackToAPIKeyOnDelayedPermissionDenied(t *testing.T) {
	origBackoff := BackoffFunc
	BackoffFunc = func(int, string) time.Duration { return 0 }
	defer func() { BackoffFunc = origBackoff }()

	acc1 := makeTestAccount("pool@example.com", "pool-tok", 0.90)
	router := setupTestPool(t, acc1)

	// Mock Gemini Live WebSocket server for API key fallback verification
	var apiKeyHit bool
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") == "fallback-api-key" || strings.Contains(r.URL.RawQuery, "key=fallback-api-key") {
			apiKeyHit = true
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		_, _, _ = c.Read(r.Context())
		_ = c.Write(r.Context(), websocket.MessageText, []byte(`{"setupComplete":{}}`))
		<-r.Context().Done()
	}))
	defer wsSrv.Close()
	t.Setenv("OPENAGY_VOICE_URL", "ws"+strings.TrimPrefix(wsSrv.URL, "http"))

	// When PreferOAuth is set, OAuth pool is tried FIRST and falls back to API key
	// even when PermissionDenied arrives asynchronously during WaitHandshake.
	var oauthTried bool
	backend, err := ResolveBackend(Options{
		Router:         router,
		APIKey:         "fallback-api-key",
		PreferOAuth:    true,
		HandshakeProbe: 500 * time.Millisecond,
		OAuthFactory: func(token string) voice.LiveBackend {
			oauthTried = true
			return &stubLiveBackend{
				createFn: func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
					mb := voice.NewMockLiveBackend()
					s, _ := mb.CreateSession(ctx, cfg)
					return &delayedHandshakeSession{
						LiveSession: s,
						delay:       40 * time.Millisecond,
						hsErr:       errors.New("rpc error: code = PermissionDenied desc = permission_denied"),
					}, nil
				},
			}
		},
	})
	if err != nil {
		t.Fatalf("ResolveBackend failed: %v", err)
	}

	sess, err := backend.CreateSession(context.Background(), voice.LiveSessionConfig{Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatalf("CreateSession fallback failed: %v", err)
	}
	_ = sess.Close()

	if !oauthTried {
		t.Fatalf("expected OAuth pool to be tried before API key fallback when PreferOAuth is true")
	}
	if !apiKeyHit {
		t.Fatalf("expected fallback to Gemini API key after OAuth pool failed")
	}
}

type delayedHandshakeSession struct {
	voice.LiveSession
	delay time.Duration
	hsErr error
}

func (s *delayedHandshakeSession) WaitHandshake(timeout time.Duration) error {
	if s.delay > timeout {
		time.Sleep(timeout)
		return nil
	}
	time.Sleep(s.delay)
	return s.hsErr
}

func TestResolveBackend_NoCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("OPENAGY_CONFIG_HOME", dir)
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(dir, "accounts.json"))
	t.Setenv("OPENAGY_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENAGY_API_KEY", "")
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_ACCESS_TOKEN", "")
	t.Setenv("PIGASUS_MOCK_VOICE", "")
	t.Setenv("OPENAGY_MOCK_VOICE", "")

	r := accountrouting.NewRouter()
	defer r.Close()

	_, err := ResolveBackend(Options{
		Router:      r,
		APIKeyFn:    func() string { return "" },
		LoadTokenFn: func() (*auth.AntigravityToken, error) { return nil, auth.ErrNoToken },
	})
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}
}

func TestClassifyFailures(t *testing.T) {
	if k, _ := ClassifyHTTPFailure(http.StatusServiceUnavailable, []byte(`MODEL_CAPACITY_EXHAUSTED`)); k != FailureCapacityOrQuota {
		t.Errorf("expected FailureCapacityOrQuota for 503, got %v", k)
	}
	if k, _ := ClassifyHTTPFailure(http.StatusForbidden, []byte(`TOS_VIOLATION`)); k != FailureAccountDisabled {
		t.Errorf("expected FailureAccountDisabled for TOS_VIOLATION, got %v", k)
	}
	if k, _ := ClassifyStreamError(&geminicode.StreamError{Code: 429, Status: "RESOURCE_EXHAUSTED"}); k != FailureCapacityOrQuota {
		t.Errorf("expected FailureCapacityOrQuota for 429 stream error, got %v", k)
	}
	if k, _ := ClassifyError(errors.New("rpc error: code = Unauthenticated")); k != FailureAuthExpired {
		t.Errorf("expected FailureAuthExpired for Unauthenticated, got %v", k)
	}
}

func TestCreateSessionWithProbe_ContextLifecycle(t *testing.T) {
	t.Run("close cancels session context", func(t *testing.T) {
		var capturedCtx context.Context
		backend := &stubLiveBackend{
			createFn: func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
				capturedCtx = ctx
				return voice.NewMockLiveBackend().CreateSession(ctx, cfg)
			},
		}

		sess, err := CreateSessionWithProbe(context.Background(), backend, voice.LiveSessionConfig{}, time.Second, 50*time.Millisecond)
		if err != nil {
			t.Fatalf("CreateSessionWithProbe failed: %v", err)
		}
		if capturedCtx == nil {
			t.Fatal("expected CreateSession to receive a non-nil context")
		}
		if err := capturedCtx.Err(); err != nil {
			t.Fatalf("expected session context to remain active before Close, got %v", err)
		}

		if err := sess.Close(); err != nil {
			t.Fatalf("sess.Close failed: %v", err)
		}
		select {
		case <-capturedCtx.Done():
		case <-time.After(time.Second):
			t.Fatal("expected session context to be canceled after Close")
		}
		if !errors.Is(capturedCtx.Err(), context.Canceled) {
			t.Fatalf("expected context.Canceled after Close, got %v", capturedCtx.Err())
		}
	})

	t.Run("handshake failure cancels session context", func(t *testing.T) {
		var capturedCtx context.Context
		hsErr := errors.New("rpc error: code = PermissionDenied desc = denied")
		backend := &stubLiveBackend{
			createFn: func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
				capturedCtx = ctx
				s, err := voice.NewMockLiveBackend().CreateSession(ctx, cfg)
				if err != nil {
					return nil, err
				}
				return &stubHandshakeSession{
					LiveSession: s,
					hsErr:       hsErr,
				}, nil
			},
		}

		_, err := CreateSessionWithProbe(context.Background(), backend, voice.LiveSessionConfig{}, time.Second, 50*time.Millisecond)
		if !errors.Is(err, hsErr) {
			t.Fatalf("expected handshake error %v, got %v", hsErr, err)
		}
		if capturedCtx == nil {
			t.Fatal("expected CreateSession to receive a non-nil context")
		}
		select {
		case <-capturedCtx.Done():
		case <-time.After(time.Second):
			t.Fatal("expected session context to be canceled after handshake failure")
		}
		if !errors.Is(capturedCtx.Err(), context.Canceled) {
			t.Fatalf("expected context.Canceled after handshake failure, got %v", capturedCtx.Err())
		}
	})

	t.Run("create timeout cancels session context", func(t *testing.T) {
		ctxCh := make(chan context.Context, 1)
		backend := &stubLiveBackend{
			createFn: func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
				ctxCh <- ctx
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}

		_, err := CreateSessionWithProbe(context.Background(), backend, voice.LiveSessionConfig{}, 20*time.Millisecond, 50*time.Millisecond)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
		capturedCtx := <-ctxCh
		select {
		case <-capturedCtx.Done():
		case <-time.After(time.Second):
			t.Fatal("expected session context to be canceled after create timeout")
		}
		if !errors.Is(capturedCtx.Err(), context.Canceled) {
			t.Fatalf("expected context.Canceled after create timeout, got %v", capturedCtx.Err())
		}
	})
}
