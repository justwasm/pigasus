package livevoice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/voice"
)

const (
	// DefaultCreateTimeout bounds a single upstream CreateSession call.
	DefaultCreateTimeout = 20 * time.Second

	// DefaultHandshakeProbe bounds how long we wait for immediate upstream
	// handshake rejections (e.g. PermissionDenied / Unavailable) before
	// considering the session established. Real gRPC TLS + StreamVoice
	// PermissionDenied responses typically arrive in 2.3s-3.6s.
	DefaultHandshakeProbe = 5 * time.Second
)

// ErrNoCredentials is returned when neither OAuth pool accounts, active OAuth
// token, Gemini API key, nor an alternative voice endpoint is configured.
var ErrNoCredentials = errors.New("no active Google session or Gemini API Key found")

// Options configures live voice backend resolution and account-pool failover.
type Options struct {
	Mock           bool
	MockMode       string
	MockInterim    string
	AltBackend     string
	ForceAlt       bool
	APIKey         string
	ForceAPIKey    bool
	PreferOAuth    bool
	BearerToken    string
	QuotaProject   string
	Router         *accountrouting.Router
	CreateTimeout  time.Duration
	HandshakeProbe time.Duration
	OAuthFactory   func(token string) voice.LiveBackend
	APIKeyFn       func() string
	LoadTokenFn    func() (*auth.AntigravityToken, error)
	Logf           func(format string, args ...any)
}

func (o Options) apiKey() string {
	if k := strings.TrimSpace(o.APIKey); k != "" {
		return k
	}
	fn := o.APIKeyFn
	if fn == nil {
		fn = endpoints.CurrentGeminiAPIKey
	}
	return strings.TrimSpace(fn())
}

func (o Options) loadToken() (*auth.AntigravityToken, error) {
	fn := o.LoadTokenFn
	if fn == nil {
		return auth.LoadTokenUnified()
	}
	return fn()
}

func (o Options) router() *accountrouting.Router {
	if o.Router != nil {
		return o.Router
	}
	return SharedRouter()
}

func (o Options) createTimeout() time.Duration {
	if o.CreateTimeout > 0 {
		return o.CreateTimeout
	}
	return DefaultCreateTimeout
}

func (o Options) handshakeProbe() time.Duration {
	if o.HandshakeProbe > 0 {
		return o.HandshakeProbe
	}
	return DefaultHandshakeProbe
}

func (o Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

func (o Options) buildOAuthBackend(token string) voice.LiveBackend {
	if o.OAuthFactory != nil {
		return o.OAuthFactory(token)
	}
	var ac *voice.AICodeLiveBackend
	if alt := strings.TrimSpace(o.AltBackend); alt != "" {
		gl := voice.NewGeminiLiveBackendWithBearer(token)
		gl.SetEndpoint(alt)
		ac = voice.NewAICodeLiveBackendWithFallback(token, gl)
	} else {
		// Do not attach GeminiLiveBackendWithBearer(token) against the default
		// generativelanguage.googleapis.com endpoint: Antigravity OAuth tokens lack
		// the generative-language scope and always fail with WebSocket 1008,
		// delaying and masking the clean PermissionDenied signal needed for failover.
		ac = voice.NewAICodeLiveBackend(token)
	}
	if qp := strings.TrimSpace(o.QuotaProject); qp != "" {
		ac.SetQuotaProject(qp)
	}
	return ac
}

func isMockEnv(opts Options) (bool, string) {
	if opts.Mock {
		return true, opts.MockMode
	}
	if v := strings.TrimSpace(os.Getenv("PIGASUS_MOCK_VOICE")); v != "" {
		return true, v
	}
	if v := strings.TrimSpace(os.Getenv("OPENAGY_MOCK_VOICE")); v != "" {
		return true, v
	}
	return false, ""
}

func newMockBackend(mode, interim string) *voice.MockLiveBackend {
	mb := voice.NewMockLiveBackend()
	pcm24k := make([]byte, 4800)
	switch mode {
	case "interim":
		if interim == "" {
			interim = strings.TrimSpace(os.Getenv("PIGASUS_MOCK_INTERIM"))
		}
		if interim == "" {
			interim = "what is this project?"
		}
		mb.SetCustomEvents([]*voice.LiveServerEvent{
			{UserSpeech: interim, Interim: true},
		})
	case "speech", "simulate":
		mb.SetCustomEvents([]*voice.LiveServerEvent{
			{UserSpeech: "what is this project?", Interim: false},
			{VoiceTranscript: "This is Pigasus, an extensible clean-room AI agent engine.", Audio: pcm24k, TurnComplete: true},
		})
	default:
		mb.SetAutoEcho(true)
	}
	return mb
}

// CreateSessionWithProbe creates a LiveSession with a bounded timeout and probes
// HandshakeAwareSession implementations for immediate upstream rejections.
func CreateSessionWithProbe(
	ctx context.Context,
	backend voice.LiveBackend,
	cfg voice.LiveSessionConfig,
	createTimeout time.Duration,
	probeTimeout time.Duration,
) (voice.LiveSession, error) {
	if createTimeout <= 0 {
		createTimeout = DefaultCreateTimeout
	}
	if probeTimeout <= 0 {
		probeTimeout = DefaultHandshakeProbe
	}

	type createResult struct {
		sess voice.LiveSession
		err  error
	}
	createCh := make(chan createResult, 1)
	sessCtx, sessCancel := context.WithCancel(ctx)
	go func() {
		s, e := backend.CreateSession(sessCtx, cfg)
		createCh <- createResult{sess: s, err: e}
	}()

	timer := time.NewTimer(createTimeout)
	defer timer.Stop()

	select {
	case res := <-createCh:
		if res.err != nil {
			sessCancel()
			return nil, res.err
		}
		if hs, ok := res.sess.(voice.HandshakeAwareSession); ok {
			if hsErr := hs.WaitHandshake(probeTimeout); hsErr != nil {
				_ = res.sess.Close()
				sessCancel()
				return nil, hsErr
			}
		}
		if res.sess == nil {
			sessCancel()
			return nil, nil
		}
		return &cancelOnCloseSession{
			LiveSession: res.sess,
			cancel:      sessCancel,
		}, nil
	case <-timer.C:
		sessCancel()
		go func() {
			if res := <-createCh; res.sess != nil {
				_ = res.sess.Close()
			}
		}()
		return nil, context.DeadlineExceeded
	case <-ctx.Done():
		sessCancel()
		return nil, ctx.Err()
	}
}

type cancelOnCloseSession struct {
	voice.LiveSession
	cancel context.CancelFunc
}

func (s *cancelOnCloseSession) Close() error {
	var err error
	if s.LiveSession != nil {
		err = s.LiveSession.Close()
	}
	if s.cancel != nil {
		s.cancel()
	}
	return err
}

// OpenPoolOAuthSession attempts to establish a live voice session using the
// quota-aware OAuth account pool (with automatic failover across accounts and
// fallback to the active unified token / explicit BearerToken).
func OpenPoolOAuthSession(ctx context.Context, cfg voice.LiveSessionConfig, opts Options) (voice.LiveSession, error) {
	router := opts.router()
	policy := EffectivePolicy()
	maxAttempts := policy.MaxAttempts
	if maxAttempts < 3 {
		maxAttempts = 3
	}
	if cands, err := router.CandidateAccounts(ctx); err == nil && len(cands) > maxAttempts {
		maxAttempts = len(cands)
	}

	family := accountrouting.ModelFamily(cfg.Model)
	triedTokens := make(map[string]bool)
	var lastErr error
	permDeniedCount := 0
	maxPermDenied := policy.MaxAttempts
	if maxPermDenied < 3 {
		maxPermDenied = 3
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		var token, email string
		res, routeErr := router.RouteTurn(ctx, family)
		if routeErr == nil && strings.TrimSpace(res.AccessToken) != "" {
			token = strings.TrimSpace(res.AccessToken)
			email = strings.TrimSpace(res.Email)
		} else if tok, tokErr := opts.loadToken(); tokErr == nil && tok != nil && strings.TrimSpace(tok.Token.AccessToken) != "" {
			token = strings.TrimSpace(tok.Token.AccessToken)
			email = strings.TrimSpace(tok.UserEmail())
		} else if routeErr != nil && lastErr == nil {
			lastErr = routeErr
		}

		if token == "" {
			break
		}
		if triedTokens[token] {
			// Avoid spinning on the exact same fallback token when no pool rotation occurred.
			break
		}
		triedTokens[token] = true

		backend := opts.buildOAuthBackend(token)
		sess, err := CreateSessionWithProbe(ctx, backend, cfg, opts.createTimeout(), opts.handshakeProbe())
		if err != nil {
			lastErr = err
			kind, reason := ClassifyError(err)
			if kind != FailureNone {
				ApplyFailure(router, email, family, kind, reason)
			}
			if kind == FailurePermissionDenied {
				permDeniedCount++
				if permDeniedCount >= maxPermDenied {
					opts.logf("[livevoice] stopping OAuth pool failover after %d consecutive permission denied errors", permDeniedCount)
					break
				}
			} else {
				permDeniedCount = 0
			}
			if attempt+1 < maxAttempts && kind != FailureNone {
				opts.logf("[livevoice] aggregate live failover (%s, account=%q, attempt %d/%d): %v",
					reason, email, attempt+1, maxAttempts, err)
				if kind == FailureCapacityOrQuota {
					if waitErr := WaitBackoff(ctx, attempt, ""); waitErr != nil {
						return nil, waitErr
					}
				}
				continue
			}
			break
		}

		return &trackedLiveSession{
			LiveSession: sess,
			onErr: func(recvErr error) {
				if kind, reason := ClassifyError(recvErr); kind != FailureNone {
					ApplyFailure(router, email, family, kind, reason)
				}
			},
		}, nil
	}

	// If an explicit BearerToken was supplied and wasn't already tried via the pool, try it once.
	if explicitTok := strings.TrimSpace(opts.BearerToken); explicitTok != "" && !triedTokens[explicitTok] {
		backend := opts.buildOAuthBackend(explicitTok)
		sess, err := CreateSessionWithProbe(ctx, backend, cfg, opts.createTimeout(), opts.handshakeProbe())
		if err == nil {
			return sess, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = errors.New("no healthy accounts or credentials available in aggregate pool")
	}
	return nil, lastErr
}

type trackedLiveSession struct {
	voice.LiveSession
	onErr func(error)
}

func (s *trackedLiveSession) Receive() (*voice.LiveServerEvent, error) {
	ev, err := s.LiveSession.Receive()
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) && s.onErr != nil {
		s.onErr(err)
	}
	return ev, err
}

// PoolBackend implements voice.LiveBackend using the shared OAuth account pool
// with automatic failover and fallback to alt-backend or Gemini API key.
type PoolBackend struct {
	opts Options
}

// NewPoolBackend constructs a PoolBackend with the given options.
func NewPoolBackend(opts Options) *PoolBackend {
	return &PoolBackend{opts: opts}
}

// ResolveBackend validates that at least one live voice credential or endpoint
// is available and returns a shared voice.LiveBackend.
func ResolveBackend(opts Options) (voice.LiveBackend, error) {
	if ok, _ := isMockEnv(opts); ok {
		return NewPoolBackend(opts), nil
	}
	if strings.TrimSpace(opts.AltBackend) != "" {
		return NewPoolBackend(opts), nil
	}
	if opts.apiKey() != "" {
		return NewPoolBackend(opts), nil
	}
	if strings.TrimSpace(opts.BearerToken) != "" {
		return NewPoolBackend(opts), nil
	}
	if tok, err := opts.loadToken(); err == nil && tok != nil && (tok.Token.AccessToken != "" || tok.Token.RefreshToken != "") {
		return NewPoolBackend(opts), nil
	}
	if cands, err := opts.router().CandidateAccounts(context.Background()); err == nil {
		for _, acc := range cands {
			if !acc.Disabled && strings.TrimSpace(acc.Email) != "" {
				return NewPoolBackend(opts), nil
			}
		}
	}
	if ep := strings.TrimSpace(endpoints.Current().Voice); ep != "" && ep != endpoints.DefaultVoice {
		return NewPoolBackend(opts), nil
	}
	return nil, ErrNoCredentials
}

// CreateSession establishes a live voice session following the shared priority cascade:
// Mock -> ForceAPIKey -> ForceAlt -> Gemini API Key (unless PreferOAuth) -> OAuth Account Pool (with failover) -> AltBackend -> Custom Voice Endpoint.
func (b *PoolBackend) CreateSession(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
	if ok, mode := isMockEnv(b.opts); ok {
		return newMockBackend(mode, b.opts.MockInterim).CreateSession(ctx, cfg)
	}

	apiKey := b.opts.apiKey()
	alt := strings.TrimSpace(b.opts.AltBackend)

	if b.opts.ForceAPIKey && apiKey != "" {
		return voice.NewGeminiLiveBackend(apiKey).CreateSession(ctx, cfg)
	}

	if alt != "" && b.opts.ForceAlt {
		var altLive *voice.GeminiLiveBackend
		if tok := strings.TrimSpace(b.opts.BearerToken); tok != "" {
			altLive = voice.NewGeminiLiveBackendWithBearer(tok)
		} else if tok, err := b.opts.loadToken(); err == nil && tok != nil && strings.TrimSpace(tok.Token.AccessToken) != "" {
			altLive = voice.NewGeminiLiveBackendWithBearer(strings.TrimSpace(tok.Token.AccessToken))
		} else {
			altLive = voice.NewGeminiLiveBackend(apiKey)
		}
		altLive.SetEndpoint(alt)
		return altLive.CreateSession(ctx, cfg)
	}

	// Prefer configured Gemini API key by default (matching commit 8774ea39):
	// consumer OAuth accounts are rejected by aicode.googleapis.com StreamVoice
	// with PermissionDenied and lack generative-language scope for Gemini Live.
	var apiKeyErr error
	if !b.opts.PreferOAuth && apiKey != "" {
		sess, err := voice.NewGeminiLiveBackend(apiKey).CreateSession(ctx, cfg)
		if err == nil {
			return sess, nil
		}
		apiKeyErr = err
		b.opts.logf("[livevoice] Gemini API key session failed (%v); trying OAuth pool", err)
	}

	// Try OAuth account pool (with AICode StreamVoice + failover across accounts).
	sess, poolErr := OpenPoolOAuthSession(ctx, cfg, b.opts)
	if poolErr == nil {
		return sess, nil
	}

	// Fallback to explicit alt-backend if configured.
	if alt != "" {
		altLive := voice.NewGeminiLiveBackend(apiKey)
		altLive.SetEndpoint(alt)
		if s, err := altLive.CreateSession(ctx, cfg); err == nil {
			return s, nil
		}
	}

	// Fallback to Gemini API key if PreferOAuth tried OAuth pool first.
	if b.opts.PreferOAuth && apiKey != "" {
		b.opts.logf("[livevoice] OAuth pool unavailable (%v); falling back to Gemini API key", poolErr)
		return voice.NewGeminiLiveBackend(apiKey).CreateSession(ctx, cfg)
	}

	// Fallback to custom non-default Voice endpoint if configured.
	if ep := strings.TrimSpace(endpoints.Current().Voice); ep != "" && ep != endpoints.DefaultVoice {
		return voice.NewGeminiLiveBackend("").CreateSession(ctx, cfg)
	}

	if apiKeyErr != nil {
		return nil, fmt.Errorf("live voice session failed: api key: %v; oauth pool: %w", apiKeyErr, poolErr)
	}
	return nil, fmt.Errorf("live voice session failed: %w", poolErr)
}
