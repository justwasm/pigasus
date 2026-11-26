// Package livevoice provides shared account-pool OAuth routing, failover,
// and credential resolution for real-time voice sessions across CLI (`livechat`),
// Web UI (`livechat --web`), and TUI (`/voice live`).
package livevoice

import (
	"bytes"
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/geminicode"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// FailureKind classifies upstream errors for account-pool exclusion and failover.
type FailureKind int

const (
	FailureNone FailureKind = iota
	FailureCapacityOrQuota
	FailureAccountDisabled
	FailurePermissionDenied
	FailureAuthExpired
)

var (
	sharedRouterMu sync.Mutex
	sharedRouter   *accountrouting.Router
)

// SharedRouter returns the shared quota-aware account router configured for
// aggregate mode across livechat TUI and Web UI.
func SharedRouter() *accountrouting.Router {
	sharedRouterMu.Lock()
	defer sharedRouterMu.Unlock()
	if sharedRouter == nil {
		r := accountrouting.NewRouter()
		r.SetPolicy(EffectivePolicy)
		sharedRouter = r
	}
	return sharedRouter
}

// ResetSharedRouterForTest replaces or clears the shared aggregate router in unit tests.
func ResetSharedRouterForTest(r *accountrouting.Router) {
	sharedRouterMu.Lock()
	defer sharedRouterMu.Unlock()
	if sharedRouter != nil && sharedRouter != r {
		sharedRouter.Close()
	}
	sharedRouter = r
}

// EffectivePolicy returns an account-pool policy with routing, aggregate mode,
// and quota-error failover enabled.
func EffectivePolicy() *accountpool.Config {
	p := accounts.CurrentAccountPool()
	var cp accountpool.Config
	if p != nil {
		cp = *p
	}
	cp.SetEnabled(true)
	if cp.Aggregate == nil {
		cp.Aggregate = &accountpool.Aggregate{}
	} else {
		agg := *cp.Aggregate
		cp.Aggregate = &agg
	}
	cp.Aggregate.Enabled = accountpool.Bool(true)
	cp.SwitchOnQuotaError = accountpool.Bool(true)
	if cp.MaxAttempts < 3 {
		cp.MaxAttempts = 3
	}
	return &cp
}

// PoolStatus summarizes backend pool availability without exposing individual
// account credentials.
func PoolStatus(ctx context.Context, apiKeyFn func() string) (enabled bool, total int, healthy int) {
	r := SharedRouter()
	cands, err := r.CandidateAccounts(ctx)
	if err == nil {
		for _, acc := range cands {
			if acc.Disabled || strings.TrimSpace(acc.Email) == "" {
				continue
			}
			total++
			if !r.IsExcluded(acc.Email) {
				healthy++
			}
		}
	}
	if total == 0 {
		if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil && (tok.Token.AccessToken != "" || tok.Token.RefreshToken != "") {
			total = 1
			healthy = 1
		}
	}
	if apiKeyFn == nil {
		apiKeyFn = endpoints.CurrentGeminiAPIKey
	}
	enabled = total > 0 || apiKeyFn() != ""
	return enabled, total, healthy
}

// BackoffFunc computes the backoff delay before retrying another account.
// Overridden in unit tests to avoid sleeping.
var BackoffFunc = defaultBackoff

func defaultBackoff(attempt int, retryAfterHeader string) time.Duration {
	if d := parseRetryAfterHeader(retryAfterHeader); d > 0 {
		if d > 3*time.Second {
			return 3 * time.Second
		}
		return d
	}
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 3 {
		attempt = 3
	}
	base := 200 * time.Millisecond * time.Duration(1<<attempt)
	jitter := time.Duration(rand.Intn(120)) * time.Millisecond
	delay := base + jitter
	if delay > 1500*time.Millisecond {
		delay = 1500 * time.Millisecond
	}
	return delay
}

func parseRetryAfterHeader(val string) time.Duration {
	val = strings.TrimSpace(val)
	if val == "" {
		return 0
	}
	if secs, err := strconv.Atoi(val); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// WaitBackoff sleeps for the computed backoff duration or returns early if ctx is canceled.
func WaitBackoff(ctx context.Context, attempt int, retryAfterHeader string) error {
	delay := BackoffFunc(attempt, retryAfterHeader)
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ClassifyHTTPFailure inspects an HTTP status code and response body to determine
// whether the failure should trigger account exclusion/quarantine and failover.
func ClassifyHTTPFailure(code int, body []byte) (FailureKind, string) {
	low := bytes.ToLower(body)

	if bytes.Contains(low, []byte("tos_violation")) ||
		bytes.Contains(low, []byte("terms of service")) ||
		bytes.Contains(low, []byte("service has been disabled")) ||
		bytes.Contains(low, []byte("account has been disabled")) ||
		bytes.Contains(low, []byte("account_disabled")) {
		return FailureAccountDisabled, "Terms of Service violation / account service disabled"
	}

	if code == http.StatusTooManyRequests {
		return FailureCapacityOrQuota, "quota exceeded (429)"
	}

	if code == http.StatusServiceUnavailable ||
		bytes.Contains(low, []byte("model_capacity_exhausted")) ||
		bytes.Contains(low, []byte("no capacity available")) ||
		bytes.Contains(low, []byte("overloaded_too_many_retries")) ||
		bytes.Contains(low, []byte("unavailable")) {
		return FailureCapacityOrQuota, "model capacity exhausted / unavailable (503)"
	}

	if bytes.Contains(low, []byte("quota")) ||
		bytes.Contains(low, []byte("resource_exhausted")) ||
		bytes.Contains(low, []byte("rate_limit")) ||
		bytes.Contains(low, []byte("ratelimit")) {
		return FailureCapacityOrQuota, "quota exceeded"
	}

	if code == http.StatusUnauthorized || bytes.Contains(low, []byte("unauthenticated")) {
		return FailureAuthExpired, "authentication failure / token expired"
	}

	if code == http.StatusForbidden ||
		bytes.Contains(low, []byte("permission_denied")) ||
		bytes.Contains(low, []byte("permissiondenied")) ||
		bytes.Contains(low, []byte("permission denied")) ||
		bytes.Contains(low, []byte("insufficient authentication scopes")) {
		return FailurePermissionDenied, "permission denied"
	}

	return FailureNone, ""
}

// ClassifyStreamError inspects a CloudCode SSE StreamError for pool failover.
func ClassifyStreamError(sErr *geminicode.StreamError) (FailureKind, string) {
	if sErr == nil {
		return FailureNone, ""
	}
	msg := strings.ToLower(sErr.Message)
	status := strings.ToLower(sErr.Status)

	if strings.Contains(msg, "tos_violation") ||
		strings.Contains(msg, "terms of service") ||
		strings.Contains(msg, "service has been disabled") ||
		strings.Contains(msg, "account has been disabled") ||
		strings.Contains(msg, "account_disabled") {
		return FailureAccountDisabled, "Terms of Service violation / account service disabled"
	}

	if sErr.Code == http.StatusTooManyRequests ||
		status == "resource_exhausted" ||
		strings.Contains(msg, "quota") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "rate_limit") ||
		strings.Contains(msg, "ratelimit") {
		return FailureCapacityOrQuota, "quota exceeded"
	}

	if sErr.Code == http.StatusServiceUnavailable ||
		status == "unavailable" ||
		strings.Contains(msg, "model_capacity_exhausted") ||
		strings.Contains(msg, "no capacity available") ||
		strings.Contains(msg, "overloaded") {
		return FailureCapacityOrQuota, "model capacity exhausted / unavailable (503)"
	}

	if sErr.Code == http.StatusUnauthorized || status == "unauthenticated" || strings.Contains(msg, "unauthenticated") {
		return FailureAuthExpired, "authentication failure / token expired"
	}

	if sErr.Code == http.StatusForbidden || status == "permission_denied" {
		return FailurePermissionDenied, "permission denied"
	}

	return FailureNone, ""
}

// ClassifyError inspects a generic Go error (e.g. gRPC or WebSocket error) for pool failover.
func ClassifyError(err error) (FailureKind, string) {
	if err == nil {
		return FailureNone, ""
	}
	return ClassifyHTTPFailure(0, []byte(err.Error()))
}

// ApplyFailure records an account failure on the router (quarantine or exclusion).
func ApplyFailure(r *accountrouting.Router, email, family string, kind FailureKind, reason string) {
	if r == nil {
		return
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	switch kind {
	case FailureAccountDisabled:
		r.Quarantine(email, reason)
	case FailureCapacityOrQuota:
		r.ExcludeForModel(email, family)
	case FailureAuthExpired, FailurePermissionDenied:
		r.Exclude(email)
	}
}
