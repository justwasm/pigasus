package accountrouting

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
)

// The router reads two files: the account registry and the configuration
// document. Both have an environment override, so a test can point them
// at a scratch directory and stay hermetic.

// writeRegistry writes an account registry with the given accounts.
func writeRegistry(t *testing.T, path string, active string, accs ...accounts.Account) {
	t.Helper()
	st := accounts.Store{Version: accounts.Version, Active: active, Accounts: accs}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatalf("encode the registry: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write the registry: %v", err)
	}
}

// writeConfig writes a configuration document with the given
// "account_pool" section.
func writeConfig(t *testing.T, path string, section map[string]any) {
	t.Helper()
	doc := map[string]any{"endpoints": map[string]string{}}
	if section != nil {
		doc["account_pool"] = section
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("encode the configuration: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
}

// quotaSnapshot builds a cached quota with one bucket in one window.
func quotaSnapshot(remaining float64) *auth.QuotaSnapshot {
	return &auth.QuotaSnapshot{
		FetchedAt: time.Now(),
		Groups: []auth.QuotaGroupSnapshot{{
			Name: "Gemini",
			Buckets: []auth.QuotaBucketSnapshot{{
				DisplayName:       "Weekly Limit Remaining",
				Window:            "weekly",
				RemainingFraction: remaining,
			}},
		}},
	}
}

// token builds a credential that is not about to expire, so no refresh —
// and no network — happens.
func token(access string) *auth.AntigravityToken {
	return &auth.AntigravityToken{
		Token: auth.AntigravityAccessToken{
			AccessToken:  access,
			TokenType:    "Bearer",
			RefreshToken: "refresh-" + access,
			Expiry:       time.Now().Add(time.Hour).Format(time.RFC3339),
		},
		AuthMethod:   "consumer",
		IDTokenEmail: access + "@example.com",
	}
}

// setup points the registry and the configuration file at a scratch
// directory and returns a fresh router.
func setup(t *testing.T, section map[string]any, accs ...accounts.Account) *Router {
	t.Helper()
	dir := t.TempDir()
	registry := filepath.Join(dir, "accounts.json")
	config := filepath.Join(dir, "config.json")
	active := ""
	if len(accs) > 0 {
		active = accs[0].Email
	}
	writeRegistry(t, registry, active, accs...)
	writeConfig(t, config, section)
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", registry)
	t.Setenv("OPENAGY_CONFIG", config)
	// A fresh router per test: the shared one keeps the pool's state.
	r := &Router{
		remaining:  map[string]quota{},
		excluded:   map[string]time.Time{},
		lastUsed:   map[string]time.Time{},
		refreshing: map[string]bool{},
	}
	// The shared credential is the active account's own token in the
	// registry: a test must not reach for the machine's keychain.
	r.loadShared = func() (*auth.AntigravityToken, string, error) {
		for _, acc := range accs {
			if strings.EqualFold(acc.Email, active) {
				return acc.Token, acc.Email, nil
			}
		}
		return nil, "", auth.ErrNoToken
	}
	// The transport's own credential source answers when the routing is
	// off; in a test it is a stub, so no machine state is read.
	r.fallback = func(context.Context) (string, error) { return "shared", nil }
	return r
}

func TestDecidePicksTheAccountWithTheMostQuota(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":  true,
		"strategy": "quota_aware",
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.2), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: quotaSnapshot(0.8), Token: token("b")},
	)

	d, err := r.Decide(context.Background())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Email != "b@example.com" {
		t.Errorf("the decision chose %q, want the account with the most quota", d.Email)
	}
}

func TestDecideStickyFollowsTheActiveAccount(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":  true,
		"strategy": "sticky",
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: quotaSnapshot(0.1), Token: token("b")},
	)

	d, err := r.Decide(context.Background())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Email != "a@example.com" {
		t.Errorf("the decision chose %q, want the active account", d.Email)
	}
}

func TestDecideSkipsTheAccountsBelowTheThreshold(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":   true,
		"strategy":  "quota_aware",
		"threshold": 0.5,
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.2), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: quotaSnapshot(0.8), Token: token("b")},
	)

	d, err := r.Decide(context.Background())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Email != "b@example.com" {
		t.Errorf("the decision chose %q, want the account above the threshold", d.Email)
	}
}

func TestDecideRoundRobinRotates(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":  true,
		"strategy": "round_robin",
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: quotaSnapshot(0.9), Token: token("b")},
	)

	seen := []string{}
	for i := 0; i < 4; i++ {
		d, err := r.Decide(context.Background())
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		seen = append(seen, d.Email)
	}
	want := []string{"a@example.com", "b@example.com", "a@example.com", "b@example.com"}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("the rotation was %v, want %v", seen, want)
		}
	}
}

func TestDecideIsOffWithoutThePolicy(t *testing.T) {
	r := setup(t, nil,
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
	)
	if _, err := r.Decide(context.Background()); err == nil {
		t.Fatal("a policy-less configuration should not route")
	}
}

func TestDecideIsOffWhenThePolicyIsDisabled(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":  false,
		"strategy": "quota_aware",
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
	)
	if _, err := r.Decide(context.Background()); err == nil {
		t.Fatal("a disabled policy should not route")
	}
}

func TestTokenFuncAnswersTheRoutedAccountsCredential(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":  true,
		"strategy": "quota_aware",
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.2), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: quotaSnapshot(0.8), Token: token("b")},
	)

	got, err := r.TokenFunc(context.Background())
	if err != nil {
		t.Fatalf("TokenFunc: %v", err)
	}
	if got != "b" {
		t.Errorf("the credential = %q, want the routed account's access token", got)
	}
	if r.LastRouted() != "b@example.com" {
		t.Errorf("the routed account = %q, want b@example.com", r.LastRouted())
	}
}

func TestTokenFuncAnswersTheSharedCredentialForTheActiveAccount(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":  true,
		"strategy": "sticky",
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: quotaSnapshot(0.1), Token: token("b")},
	)

	got, err := r.TokenFunc(context.Background())
	if err != nil {
		t.Fatalf("TokenFunc: %v", err)
	}
	if got != "a" {
		t.Errorf("the credential = %q, want the active account's access token", got)
	}
}

func TestTokenFuncAnswersTheTransportsOwnCredentialWhenTheRoutingIsOff(t *testing.T) {
	r := setup(t, nil,
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
	)

	got, err := r.TokenFunc(context.Background())
	if err != nil {
		t.Fatalf("TokenFunc: %v", err)
	}
	if got != "shared" {
		t.Errorf("the credential = %q, want the transport's own credential", got)
	}
}

// TestSetFallbackInstallsTheTransportsOwnSource pins that a rebuilt
// transport installs a fresh source: the credential the clone uses
// without a policy is the one the transport would have used by itself,
// at that moment — a sign-in between the two builds is seen.
func TestSetFallbackInstallsTheTransportsOwnSource(t *testing.T) {
	r := setup(t, nil,
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
	)

	r.SetFallback(func(context.Context) (string, error) { return "rebuilt", nil })

	got, err := r.TokenFunc(context.Background())
	if err != nil {
		t.Fatalf("TokenFunc: %v", err)
	}
	if got != "rebuilt" {
		t.Errorf("the credential = %q, want the rebuilt transport's own credential", got)
	}
}

// TestSetFallbackIgnoresANilSource pins that the router keeps a working
// answer: a nil source is not an answer.
func TestSetFallbackIgnoresANilSource(t *testing.T) {
	r := setup(t, nil,
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
	)

	r.SetFallback(nil)

	got, err := r.TokenFunc(context.Background())
	if err != nil {
		t.Fatalf("TokenFunc: %v", err)
	}
	if got != "shared" {
		t.Errorf("the credential = %q, want the previous source's answer", got)
	}
}

func TestExcludeKeepsAnAccountOutForTheCooldown(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":  true,
		"strategy": "quota_aware",
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: quotaSnapshot(0.1), Token: token("b")},
	)

	r.Exclude("b@example.com")
	d, err := r.Decide(context.Background())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d.Email != "a@example.com" {
		t.Errorf("the decision chose %q, want the account that is not in cooldown", d.Email)
	}
}

func TestRemainingOfTakesTheLowestBucket(t *testing.T) {
	snap := &auth.QuotaSnapshot{Groups: []auth.QuotaGroupSnapshot{
		{Buckets: []auth.QuotaBucketSnapshot{
			{DisplayName: "Weekly Limit Remaining", RemainingFraction: 0.9},
			{DisplayName: "Five Hour Limit Remaining", RemainingFraction: 0.3},
		}},
	}}
	got, ok := remainingOf(snap)
	if !ok {
		t.Fatal("the snapshot has buckets")
	}
	if got != 0.3 {
		t.Errorf("the remaining figure = %v, want the lowest bucket's 0.3", got)
	}
	if _, ok := remainingOf(nil); ok {
		t.Error("an empty snapshot has no figure")
	}
}

// quotaClient is a transport that fails with a quota error as many times
// as the test asks it to, and succeeds after that.
//
// It asks the router for its credential first, exactly as the real
// transport does: the credential and the routing decision are one step.
type quotaClient struct {
	failures int
	calls    int
	router   *Router
}

func (c *quotaClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	c.calls++
	if c.router != nil {
		if _, err := c.router.TokenFunc(ctx); err != nil {
			return nil, err
		}
	}
	if c.calls <= c.failures {
		return nil, backend.NewHTTPError(429, []byte(`{"error":{"message":"Individual quota reached."}}`), "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent")
	}
	out := make(chan backend.StreamChunk, 1)
	out <- backend.StreamChunk{Text: "ok", Done: true}
	close(out)
	return out, nil
}

func TestWrapRetriesTheTurnOnAnotherAccountAfterAQuotaError(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":      true,
		"strategy":     "round_robin",
		"max_attempts": 3,
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: quotaSnapshot(0.9), Token: token("b")},
	)

	inner := &quotaClient{failures: 1, router: r}
	client := &routedClient{inner: inner, router: r}
	out, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gemini"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range out {
		if chunk.Text != "ok" {
			t.Errorf("the turn's text = %q, want ok", chunk.Text)
		}
	}
	if inner.calls != 2 {
		t.Errorf("the transport was called %d times, want 2", inner.calls)
	}
	// The account that failed is out of the pool, so the next turn goes
	// to the other one.
	if r.LastRouted() != "b@example.com" {
		t.Errorf("the routed account = %q, want b@example.com", r.LastRouted())
	}
}

func TestWrapGivesUpAfterThePolicysAttemptCount(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":      true,
		"strategy":     "round_robin",
		"max_attempts": 2,
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: quotaSnapshot(0.9), Token: token("b")},
	)

	inner := &quotaClient{failures: 5, router: r}
	client := &routedClient{inner: inner, router: r}
	if _, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gemini"}); err == nil {
		t.Fatal("a turn that keeps failing should fail")
	}
	if inner.calls != 2 {
		t.Errorf("the transport was called %d times, want the policy's 2 attempts", inner.calls)
	}
}

func TestWrapDoesNotRetryWhenThePolicySaysNotTo(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":               true,
		"strategy":              "round_robin",
		"max_attempts":          3,
		"switch_on_quota_error": false,
	},
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: quotaSnapshot(0.9), Token: token("b")},
	)

	inner := &quotaClient{failures: 5, router: r}
	client := &routedClient{inner: inner, router: r}
	if _, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gemini"}); err == nil {
		t.Fatal("a turn that fails should fail")
	}
	if inner.calls != 1 {
		t.Errorf("the transport was called %d times, want 1", inner.calls)
	}
}

func TestWrapPassesTheTurnThroughWhenTheRoutingIsOff(t *testing.T) {
	r := setup(t, nil,
		accounts.Account{Email: "a@example.com", Quota: quotaSnapshot(0.9), Token: token("a")},
	)

	inner := &quotaClient{failures: 1}
	client := &routedClient{inner: inner, router: r}
	if _, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gemini"}); err == nil {
		t.Fatal("the turn should fail")
	}
	if inner.calls != 1 {
		t.Errorf("the transport was called %d times, want 1: the routing is off", inner.calls)
	}
}

func TestUnwrapHandsBackTheInnerTransport(t *testing.T) {
	inner := &quotaClient{}
	client := Wrap(inner, nil)
	if Unwrap(client) != backend.Client(inner) {
		t.Error("Unwrap did not hand back the inner transport")
	}
	if Unwrap(inner) != backend.Client(inner) {
		t.Error("Unwrap changed a transport that was never wrapped")
	}
}
