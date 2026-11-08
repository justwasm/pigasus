package agyproxy

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/justwasm/pigasus/geminicode"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
)

func TestClassifyAccountFailure(t *testing.T) {
	tosBody := []byte(`{
  "error": {
    "code": 403,
    "message": "This service has been disabled in this account for violation of Terms of Service. Please submit an appeal to continue using this product.",
    "status": "PERMISSION_DENIED",
    "details": [
      {
        "@type": "type.googleapis.com/google.rpc.ErrorInfo",
        "reason": "TOS_VIOLATION",
        "domain": "cloudcode-pa.googleapis.com",
        "metadata": {
          "uiMessage": "true",
          "appeal_url": "https://forms.gle/hGzM9MEUv2azZsrb9",
          "appeal_url_link_text": "Submit Appeal"
        }
      }
    ]
  }
}`)

	kind, reason := classifyAccountFailure(403, tosBody)
	if kind != failureAccountDisabled {
		t.Fatalf("expected failureAccountDisabled, got %v", kind)
	}
	if !strings.Contains(reason, "Terms of Service") {
		t.Errorf("expected reason to mention Terms of Service, got %q", reason)
	}

	// 429 quota
	quotaBody := []byte(`{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota)."}}`)
	kind, _ = classifyAccountFailure(429, quotaBody)
	if kind != failureQuotaExceeded {
		t.Fatalf("expected failureQuotaExceeded for 429, got %v", kind)
	}

	// 403 quota
	kind, _ = classifyAccountFailure(403, quotaBody)
	if kind != failureQuotaExceeded {
		t.Fatalf("expected failureQuotaExceeded for 403 with quota body, got %v", kind)
	}

	// 401 unauthenticated
	authBody := []byte(`{"error":{"code":401,"status":"UNAUTHENTICATED","message":"Request had invalid authentication credentials."}}`)
	kind, _ = classifyAccountFailure(401, authBody)
	if kind != failureAuthExpired {
		t.Fatalf("expected failureAuthExpired for 401, got %v", kind)
	}

	// 403 generic permission denied
	permBody := []byte(`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"Caller does not have required permission."}}`)
	kind, _ = classifyAccountFailure(403, permBody)
	if kind != failurePermissionDenied {
		t.Fatalf("expected failurePermissionDenied for generic 403, got %v", kind)
	}

	// 400 bad request (client error - should not trigger account failover)
	badReqBody := []byte(`{"error":{"code":400,"message":"Invalid argument provided."}}`)
	kind, _ = classifyAccountFailure(400, badReqBody)
	if kind != failureNone {
		t.Fatalf("expected failureNone for 400 bad request, got %v", kind)
	}
}

func TestExtractErrorInfo(t *testing.T) {
	tosBody := []byte(`{
  "error": {
    "code": 403,
    "message": "This service has been disabled in this account for violation of Terms of Service. Please submit an appeal to continue using this product.",
    "status": "PERMISSION_DENIED",
    "details": [
      {
        "@type": "type.googleapis.com/google.rpc.ErrorInfo",
        "reason": "TOS_VIOLATION",
        "domain": "cloudcode-pa.googleapis.com",
        "metadata": {
          "uiMessage": "true",
          "appeal_url": "https://forms.gle/hGzM9MEUv2azZsrb9",
          "appeal_url_link_text": "Submit Appeal"
        }
      }
    ]
  }
}`)

	reason, msg, appealURL := extractErrorInfo(tosBody)
	if reason != "TOS_VIOLATION" {
		t.Errorf("reason = %q, want TOS_VIOLATION", reason)
	}
	if !strings.Contains(msg, "disabled in this account") {
		t.Errorf("msg = %q", msg)
	}
	if appealURL != "https://forms.gle/hGzM9MEUv2azZsrb9" {
		t.Errorf("appealURL = %q", appealURL)
	}
}

func TestIsStreamAccountError(t *testing.T) {
	sErr := &geminicode.StreamError{
		Code:    403,
		Status:  "PERMISSION_DENIED",
		Message: "This service has been disabled in this account for violation of Terms of Service.",
	}
	isAcc, isGlobal, reason := isStreamAccountError(sErr)
	if !isAcc || !isGlobal {
		t.Fatalf("expected isAcc=true, isGlobal=true, got isAcc=%v, isGlobal=%v", isAcc, isGlobal)
	}
	if !strings.Contains(reason, "Terms of Service") {
		t.Errorf("unexpected reason: %q", reason)
	}

	qErr := &geminicode.StreamError{
		Code:    429,
		Status:  "RESOURCE_EXHAUSTED",
		Message: "Resource has been exhausted (e.g. check quota).",
	}
	isAcc, isGlobal, _ = isStreamAccountError(qErr)
	if !isAcc || isGlobal {
		t.Fatalf("expected isAcc=true, isGlobal=false for quota, got isAcc=%v, isGlobal=%v", isAcc, isGlobal)
	}
}

func TestFormatClientErrorMessage(t *testing.T) {
	err := errors.New("upstream rejected with status 403: permission denied")
	formatted := formatClientErrorMessage("user@example.com", err)
	if formatted != "[user@example.com] upstream rejected with status 403: permission denied" {
		t.Errorf("formatted = %q", formatted)
	}

	// Already contains email
	already := errors.New("[user@example.com] some error")
	if formatClientErrorMessage("user@example.com", already) != "[user@example.com] some error" {
		t.Errorf("double prefix: %q", formatClientErrorMessage("user@example.com", already))
	}
}

func TestBridgeFailoverOnTOSViolation(t *testing.T) {
	acc1 := accounts.Account{Email: "banned@example.com", Token: testToken("banned"), Quota: testQuotaSnapshot(0.9)}
	acc2 := accounts.Account{Email: "healthy@example.com", Token: testToken("healthy"), Quota: testQuotaSnapshot(0.8)}

	setupTestEnvironment(t, map[string]any{
		"enabled":               true,
		"strategy":              "sticky", // starts on banned
		"max_attempts":          2,
		"switch_on_quota_error": true,
	}, acc1, acc2)

	var authCalls []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth := r.Header.Get("Authorization")
		authCalls = append(authCalls, auth)
		mu.Unlock()

		if auth == "Bearer banned" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{
  "error": {
    "code": 403,
    "message": "This service has been disabled in this account for violation of Terms of Service.",
    "status": "PERMISSION_DENIED",
    "details": [{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"TOS_VIOLATION","metadata":{"appeal_url":"https://forms.gle/appeal"}}]
  }
}`)
			return
		}

		// healthy account succeeds
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hello from healthy\"}]}}]}}\n\n")
	}))
	defer upstream.Close()

	router := newTestRouter(acc1, acc2)
	var logs []string
	var logMu sync.Mutex
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   router,
		Logf: func(f string, a ...any) {
			logMu.Lock()
			logs = append(logs, fmt.Sprintf(f, a...))
			logMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	reqBody := `{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post("http://"+srv.Addr()+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 OK after TOS failover, got %d: %s", resp.StatusCode, string(b))
	}

	// Verify header has healthy account
	if accHeader := resp.Header.Get("X-OpenAGY-Account"); accHeader != "healthy@example.com" {
		t.Errorf("X-OpenAGY-Account header = %q, want healthy@example.com", accHeader)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authCalls) != 2 {
		t.Fatalf("expected 2 upstream calls, got %d: %v", len(authCalls), authCalls)
	}
	if authCalls[0] != "Bearer banned" || authCalls[1] != "Bearer healthy" {
		t.Errorf("calls = %v, want [Bearer banned, Bearer healthy]", authCalls)
	}

	// Check router excluded banned@example.com globally
	if !router.IsExcluded("banned@example.com") {
		t.Errorf("expected banned@example.com to be excluded globally")
	}

	// Verify log contains warning with appeal URL
	logMu.Lock()
	defer logMu.Unlock()
	foundWarning := false
	for _, l := range logs {
		if strings.Contains(l, "banned@example.com") && strings.Contains(l, "Terms of Service") && strings.Contains(l, "https://forms.gle/appeal") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("expected log to contain TOS warning with appeal URL, got: %v", logs)
	}
}

func TestBridgeErrorLogsIncludeAccountOnFailure(t *testing.T) {
	acc1 := accounts.Account{Email: "failing@example.com", Token: testToken("failing"), Quota: testQuotaSnapshot(0.9)}

	setupTestEnvironment(t, map[string]any{
		"enabled":      true,
		"strategy":     "sticky",
		"max_attempts": 1,
	}, acc1)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{
  "error": {
    "code": 403,
    "message": "This service has been disabled in this account for violation of Terms of Service.",
    "status": "PERMISSION_DENIED",
    "details": [{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"TOS_VIOLATION"}]
  }
}`)
	}))
	defer upstream.Close()

	router := newTestRouter(acc1)
	var logs []string
	var logMu sync.Mutex
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstream.URL,
		Balance:  true,
		Router:   router,
		Logf: func(f string, a ...any) {
			logMu.Lock()
			logs = append(logs, fmt.Sprintf(f, a...))
			logMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	reqBody := `{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post("http://"+srv.Addr()+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	// Verify error response body includes account email
	if !strings.Contains(string(body), "failing@example.com") {
		t.Errorf("expected response body to contain failing@example.com, got: %s", string(body))
	}

	// Verify X-OpenAGY-Account header
	if h := resp.Header.Get("X-OpenAGY-Account"); h != "failing@example.com" {
		t.Errorf("X-OpenAGY-Account = %q, want failing@example.com", h)
	}

	logMu.Lock()
	defer logMu.Unlock()

	foundUpstreamErrLog := false
	foundAccessLog := false
	for _, l := range logs {
		if strings.Contains(l, "[bridge] [failing@example.com] upstream error:") {
			foundUpstreamErrLog = true
		}
		if strings.Contains(l, "POST /v1/chat/completions [failing@example.com] -> 502") {
			foundAccessLog = true
		}
	}

	if !foundUpstreamErrLog {
		t.Errorf("expected log to contain '[bridge] [failing@example.com] upstream error:', got: %v", logs)
	}
	if !foundAccessLog {
		t.Errorf("expected log to contain 'POST /v1/chat/completions [failing@example.com] -> 502', got: %v", logs)
	}
}
