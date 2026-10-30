package agyproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/accountrouting"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
)

func writeTestRegistry(t *testing.T, path, active string, accs ...accounts.Account) {
	t.Helper()
	st := accounts.Store{Version: accounts.Version, Active: active, Accounts: accs}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatalf("encode registry: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}

func writeTestConfig(t *testing.T, path string, pool map[string]any) {
	t.Helper()
	doc := map[string]any{"endpoints": map[string]string{}}
	if pool != nil {
		doc["account_pool"] = pool
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("encode config: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func testToken(access string) *auth.AntigravityToken {
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

func testQuotaSnapshot(remaining float64) *auth.QuotaSnapshot {
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

func setupTestEnvironment(t *testing.T, pool map[string]any, accs ...accounts.Account) {
	t.Helper()
	dir := t.TempDir()
	registry := filepath.Join(dir, "accounts.json")
	config := filepath.Join(dir, "config.json")
	active := ""
	if len(accs) > 0 {
		active = accs[0].Email
	}
	writeTestRegistry(t, registry, active, accs...)
	writeTestConfig(t, config, pool)
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", registry)
	t.Setenv("OPENAGY_CONFIG", config)
}

func newTestRouter(accs ...accounts.Account) *accountrouting.Router {
	r := accountrouting.NewRouter()
	r.SetLoadShared(func() (*auth.AntigravityToken, string, error) {
		st, err := accounts.Load()
		if err == nil && st.Active != "" {
			for _, acc := range accs {
				if strings.EqualFold(acc.Email, st.Active) {
					return acc.Token, acc.Email, nil
				}
			}
		}
		if len(accs) > 0 {
			return accs[0].Token, accs[0].Email, nil
		}
		return nil, "", auth.ErrNoToken
	})
	r.SetFallback(func(context.Context) (string, error) {
		if len(accs) > 0 && accs[0].Token != nil {
			return accs[0].Token.Token.AccessToken, nil
		}
		return "fallback-token", nil
	})
	return r
}

func boolPtr(b bool) *bool { return &b }

func TestParseProxyFlags(t *testing.T) {
	cases := []struct {
		in          []string
		wantArgs    []string
		wantBalance *bool
	}{
		{
			in:          []string{"-p", "hello"},
			wantArgs:    []string{"-p", "hello"},
			wantBalance: nil,
		},
		{
			in:          []string{"--balance", "-p", "hello"},
			wantArgs:    []string{"-p", "hello"},
			wantBalance: boolPtr(true),
		},
		{
			in:          []string{"-balance", "--model", "gemini-2.5-pro"},
			wantArgs:    []string{"--model", "gemini-2.5-pro"},
			wantBalance: boolPtr(true),
		},
		{
			in:          []string{"--balance=true", "-p", "hi"},
			wantArgs:    []string{"-p", "hi"},
			wantBalance: boolPtr(true),
		},
		{
			in:          []string{"-balance=true"},
			wantArgs:    []string{},
			wantBalance: boolPtr(true),
		},
		{
			in:          []string{"--balance=false", "-p", "hello"},
			wantArgs:    []string{"-p", "hello"},
			wantBalance: boolPtr(false),
		},
		{
			in:          []string{"--no-balance", "-p", "hello"},
			wantArgs:    []string{"-p", "hello"},
			wantBalance: boolPtr(false),
		},
		{
			in:          []string{"--balance", "false", "-p", "hello"},
			wantArgs:    []string{"-p", "hello"},
			wantBalance: boolPtr(false),
		},
		{
			in:          []string{"-p", "prompt", "--", "--balance"},
			wantArgs:    []string{"-p", "prompt", "--", "--balance"},
			wantBalance: nil,
		},
		{
			in:          []string{"--balance", "1", "arg"},
			wantArgs:    []string{"1", "arg"},
			wantBalance: boolPtr(true),
		},
		{
			in:          []string{"--balance=1", "arg"},
			wantArgs:    []string{"arg"},
			wantBalance: boolPtr(true),
		},
		{
			in:          []string{"--balance", "on", "arg"},
			wantArgs:    []string{"arg"},
			wantBalance: boolPtr(true),
		},
		{
			in:          []string{"--aggregate", "arg"},
			wantArgs:    []string{"arg"},
			wantBalance: nil,
		},
	}

	for _, tc := range cases {
		gotArgs, gotBal, _ := parseProxyFlags(tc.in)
		if (gotBal == nil) != (tc.wantBalance == nil) || (gotBal != nil && *gotBal != *tc.wantBalance) {
			t.Errorf("parseProxyFlags(%v) balance = %v, want %v", tc.in, gotBal, tc.wantBalance)
		}
		if len(gotArgs) != len(tc.wantArgs) {
			t.Fatalf("parseProxyFlags(%v) args = %v, want %v", tc.in, gotArgs, tc.wantArgs)
		}
		for i := range gotArgs {
			if gotArgs[i] != tc.wantArgs[i] {
				t.Errorf("args[%d] = %q, want %q", i, gotArgs[i], tc.wantArgs[i])
			}
		}
	}
}

func TestBalanceFeatureFlagOptIn(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(0.2)}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta"), Quota: testQuotaSnapshot(0.9)}

	var receivedAuth string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	}))
	defer upstream.Close()

	// 1. Balance off: standard passthrough preserves client's inbound Authorization.
	setupTestEnvironment(t, map[string]any{
		"enabled":  true,
		"strategy": "quota_aware",
	}, acc1, acc2)

	router := newTestRouter(acc1, acc2)
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  false,
		Router:   router,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:generateContent", strings.NewReader(`{"contents":[]}`))
	req.Header.Set("Authorization", "Bearer client-original-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	mu.Lock()
	if receivedAuth != "Bearer client-original-token" {
		t.Errorf("balance off: expected original token, got %q", receivedAuth)
	}
	mu.Unlock()

	// 2. Balance on via Config: routes to beta (most quota: 0.9 vs 0.2).
	srvBal, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   newTestRouter(acc1, acc2),
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srvBal.Start(); err != nil {
		t.Fatal(err)
	}
	defer srvBal.Close()

	req2, _ := http.NewRequest(http.MethodPost, "http://"+srvBal.Addr()+"/v1internal:generateContent", strings.NewReader(`{"contents":[]}`))
	req2.Header.Set("Authorization", "Bearer client-original-token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()

	mu.Lock()
	if receivedAuth != "Bearer beta" {
		t.Errorf("balance on: expected beta token, got %q", receivedAuth)
	}
	mu.Unlock()

	// 3. Balance on via OPENAGY_ACCOUNT_BALANCE=1 env var.
	t.Setenv("OPENAGY_ACCOUNT_BALANCE", "1")
	srvEnv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  false, // config false, but env is 1
		Router:   newTestRouter(acc1, acc2),
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srvEnv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srvEnv.Close()

	req3, _ := http.NewRequest(http.MethodPost, "http://"+srvEnv.Addr()+"/v1internal:generateContent", strings.NewReader(`{"contents":[]}`))
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp3.Body.Close()

	mu.Lock()
	if receivedAuth != "Bearer beta" {
		t.Errorf("balance via env: expected beta token, got %q", receivedAuth)
	}
	mu.Unlock()
}

func TestBalanceFailoverOnQuotaError(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(0.9)}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta"), Quota: testQuotaSnapshot(0.5)}

	setupTestEnvironment(t, map[string]any{
		"enabled":                 true,
		"strategy":                "sticky", // starts on alpha (active)
		"max_attempts":            3,
		"switch_on_quota_error":   true,
	}, acc1, acc2)

	var authCalls []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth := r.Header.Get("Authorization")
		authCalls = append(authCalls, auth)
		mu.Unlock()

		if auth == "Bearer alpha" {
			// Alpha is out of quota
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":429,"message":"Individual quota reached. Please try again later.","status":"RESOURCE_EXHAUSTED"}}`)
			return
		}
		// Beta succeeds
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"success from beta"}]}}]}`)
	}))
	defer upstream.Close()

		router := newTestRouter(acc1, acc2)
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   router,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:generateContent", strings.NewReader(`{"contents":[]}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Client receives HTTP 200 without seeing the 429 error!
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after failover, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "success from beta") {
		t.Fatalf("unexpected body: %s", string(body))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authCalls) != 2 {
		t.Fatalf("expected 2 upstream calls (alpha then beta), got %d: %v", len(authCalls), authCalls)
	}
	if authCalls[0] != "Bearer alpha" || authCalls[1] != "Bearer beta" {
		t.Errorf("call sequence = %v, want [Bearer alpha, Bearer beta]", authCalls)
	}
}

func TestBalanceStreamFailoverOnQuotaError(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(0.8)}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta"), Quota: testQuotaSnapshot(0.4)}

	setupTestEnvironment(t, map[string]any{
		"enabled":               true,
		"strategy":              "sticky",
		"max_attempts":          3,
		"switch_on_quota_error": true,
	}, acc1, acc2)

	var authCalls []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth := r.Header.Get("Authorization")
		authCalls = append(authCalls, auth)
		mu.Unlock()

		if auth == "Bearer alpha" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`)
			return
		}
		// Beta answers with an SSE stream
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"chunk\":1}\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: {\"chunk\":2}\n\n")
		fl.Flush()
	}))
	defer upstream.Close()

	router := newTestRouter(acc1, acc2)
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   router,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:streamGenerateContent?alt=sse", strings.NewReader(`{"contents":[]}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for stream, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}

	br := bufio.NewReader(resp.Body)
	var lines []string
	for {
		line, rErr := br.ReadString('\n')
		if len(line) > 0 {
			lines = append(lines, line)
		}
		if rErr != nil {
			break
		}
	}
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, `{"chunk":1}`) || !strings.Contains(joined, `{"chunk":2}`) {
		t.Fatalf("stream content missing chunks: %q", joined)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authCalls) != 2 {
		t.Fatalf("expected 2 calls, got %v", authCalls)
	}
}

func TestBalanceMaxAttemptsExhausted(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha")}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta")}

	setupTestEnvironment(t, map[string]any{
		"enabled":               true,
		"strategy":              "round_robin",
		"max_attempts":          2,
		"switch_on_quota_error": true,
	}, acc1, acc2)

	attempts := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":429,"message":"Individual quota reached."}}`)
	}))
	defer upstream.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   newTestRouter(acc1, acc2),
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:generateContent", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when all accounts exhausted, got %d", resp.StatusCode)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestBalanceSwitchOnQuotaErrorDisabled(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha")}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta")}

	switchOnQuota := false
	setupTestEnvironment(t, map[string]any{
		"enabled":               true,
		"strategy":              "round_robin",
		"max_attempts":          3,
		"switch_on_quota_error": &switchOnQuota,
	}, acc1, acc2)

	attempts := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":429,"message":"Individual quota reached."}}`)
	}))
	defer upstream.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   newTestRouter(acc1, acc2),
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:generateContent", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt when switch_on_quota_error=false, got %d", attempts)
	}
}

func TestBalanceRoundRobinRotation(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(1.0)}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta"), Quota: testQuotaSnapshot(1.0)}

	setupTestEnvironment(t, map[string]any{
		"enabled":  true,
		"strategy": "round_robin",
	}, acc1, acc2)

	var authCalls []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authCalls = append(authCalls, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[]}`)
	}))
	defer upstream.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   newTestRouter(acc1, acc2),
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	for i := 0; i < 4; i++ {
		req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:generateContent", strings.NewReader(`{}`))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"Bearer alpha", "Bearer beta", "Bearer alpha", "Bearer beta"}
	if len(authCalls) != 4 {
		t.Fatalf("expected 4 calls, got %v", authCalls)
	}
	for i := range want {
		if authCalls[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, authCalls[i], want[i])
		}
	}
}

func TestBalanceMidStreamQuotaExclusion(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(0.9)}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta"), Quota: testQuotaSnapshot(0.5)}

	setupTestEnvironment(t, map[string]any{
		"enabled":  true,
		"strategy": "sticky",
	}, acc1, acc2)

	router := newTestRouter(acc1, acc2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"text\":\"started ok\"}\n\n")
		fl.Flush()
		// Error chunk mid-stream
		_, _ = io.WriteString(w, "data: {\"error\":{\"code\":429,\"message\":\"Individual quota reached mid-stream\"}}\n\n")
		fl.Flush()
	}))
	defer upstream.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   router,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:streamGenerateContent?alt=sse", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	// Alpha should now be excluded because of mid-stream quota error!
	// Next Decide should pick beta!
	decision, err := router.Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Email != "beta@example.com" {
		t.Fatalf("expected next decision to pick beta after alpha excluded mid-stream, got %q", decision.Email)
	}
}

func TestDoctorBalanceOutput(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha")}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta")}

	setupTestEnvironment(t, map[string]any{
		"enabled":   true,
		"strategy":  "quota_aware",
		"threshold": 0.25,
	}, acc1, acc2)

	// Upstream test server so probe finishes immediately
	probeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer probeSrv.Close()
	t.Setenv("OPENAGY_UPSTREAM", probeSrv.URL)
	t.Setenv("AGY_REAL_BIN", "/nonexistent")
	t.Setenv(EnvWrapperConfig, filepath.Join(t.TempDir(), "wrapper.json"))

	// 1. Balance off by default
	t.Setenv("OPENAGY_ACCOUNT_BALANCE", "")
	var out bytes.Buffer
	var errOut bytes.Buffer
	opts := AdminOptions{
		Prog: "pigasus",
		Out:  &out,
		Err:  &errOut,
	}
	if err := cmdDoctor(opts, []string{"-probe-timeout=100ms"}); err != nil {
		t.Fatalf("cmdDoctor failed: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "balance     off") {
		t.Errorf("expected 'balance     off' in doctor output, got:\n%s", output)
	}
	if !strings.Contains(output, "pool        on (strategy: quota_aware, threshold: 25.0%)") {
		t.Errorf("expected pool info in doctor output, got:\n%s", output)
	}

	// 2. Balance on via env var
	t.Setenv("OPENAGY_ACCOUNT_BALANCE", "true")
	out.Reset()
	errOut.Reset()
	if err := cmdDoctor(opts, []string{"-probe-timeout=100ms"}); err != nil {
		t.Fatalf("cmdDoctor failed: %v", err)
	}
	output2 := out.String()
	if !strings.Contains(output2, "balance     on") {
		t.Errorf("expected 'balance     on' in doctor output, got:\n%s", output2)
	}
}

func TestBalanceModelCandidateTextNotExcluded(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(0.9)}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta"), Quota: testQuotaSnapshot(0.5)}

	setupTestEnvironment(t, map[string]any{
		"enabled":  true,
		"strategy": "sticky",
	}, acc1, acc2)

	router := newTestRouter(acc1, acc2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		// Model outputs text discussing a 429 quota error inside candidates:
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"To fix the 429 quota error, request more quota.\"}]}}]}\n\n")
		fl.Flush()
	}))
	defer upstream.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   router,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:streamGenerateContent?alt=sse", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	// Alpha MUST NOT be excluded because the text was candidate model output, NOT a server error frame!
	decision, err := router.Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Email != "alpha@example.com" {
		t.Fatalf("expected decision to remain alpha (sticky), but got %q (falsely excluded by model text)", decision.Email)
	}
}

func TestBalanceStreamImmediateQuotaErrorFailover(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(0.9)}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta"), Quota: testQuotaSnapshot(0.5)}

	setupTestEnvironment(t, map[string]any{
		"enabled":               true,
		"strategy":              "sticky",
		"max_attempts":          3,
		"switch_on_quota_error": true,
	}, acc1, acc2)

	var authCalls []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth := r.Header.Get("Authorization")
		authCalls = append(authCalls, auth)
		mu.Unlock()

		if auth == "Bearer alpha" {
			// Alpha returns HTTP 200 with immediate SSE error frame
			w.Header().Set("Content-Type", "text/event-stream")
			fl := w.(http.Flusher)
			_, _ = io.WriteString(w, "data: {\"error\":{\"code\":429,\"message\":\"Resource has been exhausted (check quota).\",\"status\":\"RESOURCE_EXHAUSTED\"}}\n\n")
			fl.Flush()
			return
		}

		// Beta succeeds
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"success from beta\"}]}}]}\n\n")
		fl.Flush()
	}))
	defer upstream.Close()

	router := newTestRouter(acc1, acc2)
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   router,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:streamGenerateContent?alt=sse", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "success from beta") {
		t.Fatalf("expected stream failover to beta, got: %s", string(body))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authCalls) != 2 {
		t.Fatalf("expected 2 calls, got %v", authCalls)
	}
}

func TestAdminBalanceToggle(t *testing.T) {
	setupTestEnvironment(t, nil)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "wrapper.json")
	t.Setenv(EnvWrapperConfig, cfgPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	opts := AdminOptions{Prog: "pigasus", Out: &out, Err: &errOut}

	// 1. Initially off
	if err := cmdBalance(opts, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "balance: off") {
		t.Errorf("expected balance: off, got %s", out.String())
	}

	// 2. Turn on
	out.Reset()
	if err := cmdBalance(opts, []string{"on"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "balance: on") {
		t.Errorf("expected balance: on, got %s", out.String())
	}

	// Verify persisted
	c := loadConfig()
	if c.Balance == nil || !*c.Balance {
		t.Fatalf("expected Balance true in config, got %v", c.Balance)
	}

	// 3. Turn off
	out.Reset()
	if err := cmdBalance(opts, []string{"off"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "balance: off") {
		t.Errorf("expected balance: off, got %s", out.String())
	}

	c = loadConfig()
	if c.Balance == nil || *c.Balance {
		t.Fatalf("expected Balance false in config, got %v", c.Balance)
	}
}

func TestBalanceEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "wrapper.json")
	t.Setenv(EnvWrapperConfig, cfgPath)

	// Persist balance: true in wrapper.json
	b := true
	_ = saveConfig(fileConfig{Balance: &b})

	// 1. Without env var, settings has Balance: true
	t.Setenv(EnvBalance, "")
	s := resolveSettings(loadConfig())
	if !s.Balance {
		t.Errorf("expected Balance true from file, got false")
	}

	// 2. OPENAGY_ACCOUNT_BALANCE=0 overrides file
	t.Setenv(EnvBalance, "0")
	s = resolveSettings(loadConfig())
	if s.Balance {
		t.Errorf("expected Balance false when env is 0, got true")
	}

	// 3. OPENAGY_ACCOUNT_BALANCE=false overrides file
	t.Setenv(EnvBalance, "false")
	s = resolveSettings(loadConfig())
	if s.Balance {
		t.Errorf("expected Balance false when env is false, got true")
	}
}

func TestBalanceWithoutAccountPoolSectionInConfig(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(0.8)}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta"), Quota: testQuotaSnapshot(0.4)}

	// No account_pool section at all in config.json!
	setupTestEnvironment(t, nil, acc1, acc2)

	var receivedAuth string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[]}`)
	}))
	defer upstream.Close()

	router := newTestRouter(acc1, acc2)
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true, // enabled via CLI / flag
		Router:   router,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:generateContent", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	// Should successfully route even with no account_pool in config.json!
	if receivedAuth != "Bearer alpha" && receivedAuth != "Bearer beta" {
		t.Errorf("expected routed token, got %q", receivedAuth)
	}
}

func TestBalanceRespectsConfigJson(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(0.8)}
	setupTestEnvironment(t, map[string]any{
		"enabled":       false,
		"proxy_balance": true,
		"strategy":      "quota_aware",
		"threshold":     0.25,
	}, acc1)

	// 1. In config.json, proxy_balance: true enables proxy balancing
	s := resolveSettings(loadConfig())
	if !s.Balance {
		t.Fatalf("expected s.Balance to be true when proxy_balance: true in config.json, got false")
	}

	// 2. effectivePolicy respects strategy and threshold from config.json
	srv, err := New(Config{
		Addr:    "127.0.0.1:0",
		Balance: true,
		Logf:    t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	pol := srv.effectivePolicy()
	if pol.Strategy != "quota_aware" {
		t.Errorf("expected effectivePolicy strategy quota_aware, got %s", pol.Strategy)
	}
	if pol.Threshold != 0.25 {
		t.Errorf("expected effectivePolicy threshold 0.25, got %f", pol.Threshold)
	}
}

func TestBalanceStreamLeadingKeepaliveAndNewlinesFailover(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(0.9)}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta"), Quota: testQuotaSnapshot(0.5)}

	setupTestEnvironment(t, map[string]any{
		"enabled":               true,
		"strategy":              "sticky",
		"max_attempts":          3,
		"switch_on_quota_error": true,
	}, acc1, acc2)

	var authCalls []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth := r.Header.Get("Authorization")
		authCalls = append(authCalls, auth)
		mu.Unlock()

		if auth == "Bearer alpha" {
			// Alpha returns keepalive pings and leading newlines before quota error frame
			w.Header().Set("Content-Type", "text/event-stream")
			fl := w.(http.Flusher)
			_, _ = io.WriteString(w, "\r\n: keepalive\r\n\r\ndata: {\"error\":{\"code\":429,\"message\":\"Resource has been exhausted\",\"status\":\"RESOURCE_EXHAUSTED\"}}\r\n\r\n")
			fl.Flush()
			return
		}

		// Beta succeeds
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"success from beta\"}]}}]}\n\n")
		fl.Flush()
	}))
	defer upstream.Close()

	router := newTestRouter(acc1, acc2)
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   router,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:streamGenerateContent?alt=sse", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "success from beta") {
		t.Fatalf("expected stream failover to beta, got: %s", string(body))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authCalls) != 2 {
		t.Fatalf("expected 2 calls, got %v", authCalls)
	}
}

func TestBalanceStreamMultilineDataErrorFailover(t *testing.T) {
	acc1 := accounts.Account{Email: "alpha@example.com", Token: testToken("alpha"), Quota: testQuotaSnapshot(0.9)}
	acc2 := accounts.Account{Email: "beta@example.com", Token: testToken("beta"), Quota: testQuotaSnapshot(0.5)}

	setupTestEnvironment(t, map[string]any{
		"enabled":               true,
		"strategy":              "sticky",
		"max_attempts":          3,
		"switch_on_quota_error": true,
	}, acc1, acc2)

	var authCalls []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth := r.Header.Get("Authorization")
		authCalls = append(authCalls, auth)
		mu.Unlock()

		if auth == "Bearer alpha" {
			// Alpha returns multiline SSE data error frame
			w.Header().Set("Content-Type", "text/event-stream")
			fl := w.(http.Flusher)
			_, _ = io.WriteString(w, "data: {\"error\":\ndata: {\"code\":429,\"message\":\"Resource has been exhausted (check quota).\",\"status\":\"RESOURCE_EXHAUSTED\"}}\n\n")
			fl.Flush()
			return
		}

		// Beta succeeds
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"success from beta multiline\"}]}}]}\n\n")
		fl.Flush()
	}))
	defer upstream.Close()

	router := newTestRouter(acc1, acc2)
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   router,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr()+"/v1internal:streamGenerateContent?alt=sse", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "success from beta multiline") {
		t.Fatalf("expected stream failover to beta, got: %s", string(body))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authCalls) != 2 {
		t.Fatalf("expected 2 calls, got %v", authCalls)
	}
}


