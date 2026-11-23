package livechat

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
	"github.com/justwasm/pigasus/geminicode"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
)

type aggregateFailureKind int

const (
	aggFailureNone aggregateFailureKind = iota
	aggFailureCapacityOrQuota
	aggFailureAccountDisabled
	aggFailurePermissionDenied
	aggFailureAuthExpired
)

var (
	aggregateRouterMu sync.Mutex
	aggregateRouter   *accountrouting.Router
)

// getAggregateRouter returns the shared quota-aware account router configured
// for aggregate mode (mirroring agyproxy's effectivePolicy).
func getAggregateRouter() *accountrouting.Router {
	aggregateRouterMu.Lock()
	defer aggregateRouterMu.Unlock()
	if aggregateRouter == nil {
		r := accountrouting.NewRouter()
		r.SetPolicy(effectiveAggregatePolicy)
		aggregateRouter = r
	}
	return aggregateRouter
}

// resetAggregateRouterForTest replaces or clears the shared aggregate router in unit tests.
func resetAggregateRouterForTest(r *accountrouting.Router) {
	aggregateRouterMu.Lock()
	defer aggregateRouterMu.Unlock()
	if aggregateRouter != nil && aggregateRouter != r {
		aggregateRouter.Close()
	}
	aggregateRouter = r
}

// effectiveAggregatePolicy returns an account-pool policy with routing,
// aggregate mode, and quota-error failover enabled.
func effectiveAggregatePolicy() *accountpool.Config {
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

// aggregatePoolStatus summarizes the backend pool availability without exposing
// individual account credentials to the frontend.
func aggregatePoolStatus(ctx context.Context) (enabled bool, total int, healthy int) {
	r := getAggregateRouter()
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
	enabled = total > 0 || currentGeminiAPIKeyFn() != ""
	return enabled, total, healthy
}

// aggregateBackoffFn computes the backoff delay before retrying another account.
// Overridden in unit tests to avoid sleeping.
var aggregateBackoffFn = defaultAggregateBackoff

func defaultAggregateBackoff(attempt int, retryAfterHeader string) time.Duration {
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

func waitAggregateBackoff(ctx context.Context, attempt int, retryAfterHeader string) error {
	delay := aggregateBackoffFn(attempt, retryAfterHeader)
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

// classifyAggregateFailure inspects an HTTP status code and response body to
// determine whether the failure should trigger account exclusion/quarantine and failover.
func classifyAggregateFailure(code int, body []byte) (aggregateFailureKind, string) {
	low := bytes.ToLower(body)

	if bytes.Contains(low, []byte("tos_violation")) ||
		bytes.Contains(low, []byte("terms of service")) ||
		bytes.Contains(low, []byte("service has been disabled")) ||
		bytes.Contains(low, []byte("account has been disabled")) ||
		bytes.Contains(low, []byte("account_disabled")) {
		return aggFailureAccountDisabled, "Terms of Service violation / account service disabled"
	}

	if code == http.StatusTooManyRequests {
		return aggFailureCapacityOrQuota, "quota exceeded (429)"
	}

	if code == http.StatusServiceUnavailable ||
		bytes.Contains(low, []byte("model_capacity_exhausted")) ||
		bytes.Contains(low, []byte("no capacity available")) ||
		bytes.Contains(low, []byte("overloaded_too_many_retries")) ||
		bytes.Contains(low, []byte("unavailable")) {
		return aggFailureCapacityOrQuota, "model capacity exhausted / unavailable (503)"
	}

	if bytes.Contains(low, []byte("quota")) ||
		bytes.Contains(low, []byte("resource_exhausted")) ||
		bytes.Contains(low, []byte("rate_limit")) ||
		bytes.Contains(low, []byte("ratelimit")) {
		return aggFailureCapacityOrQuota, "quota exceeded"
	}

	if code == http.StatusUnauthorized || bytes.Contains(low, []byte("unauthenticated")) {
		return aggFailureAuthExpired, "authentication failure / token expired"
	}

	if code == http.StatusForbidden || bytes.Contains(low, []byte("permission_denied")) {
		return aggFailurePermissionDenied, "permission denied"
	}

	return aggFailureNone, ""
}

func classifyAggregateStreamError(sErr *geminicode.StreamError) (aggregateFailureKind, string) {
	if sErr == nil {
		return aggFailureNone, ""
	}
	msg := strings.ToLower(sErr.Message)
	status := strings.ToLower(sErr.Status)

	if strings.Contains(msg, "tos_violation") ||
		strings.Contains(msg, "terms of service") ||
		strings.Contains(msg, "service has been disabled") ||
		strings.Contains(msg, "account has been disabled") ||
		strings.Contains(msg, "account_disabled") {
		return aggFailureAccountDisabled, "Terms of Service violation / account service disabled"
	}

	if sErr.Code == http.StatusTooManyRequests ||
		status == "resource_exhausted" ||
		strings.Contains(msg, "quota") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "rate_limit") ||
		strings.Contains(msg, "ratelimit") {
		return aggFailureCapacityOrQuota, "quota exceeded"
	}

	if sErr.Code == http.StatusServiceUnavailable ||
		status == "unavailable" ||
		strings.Contains(msg, "model_capacity_exhausted") ||
		strings.Contains(msg, "no capacity available") ||
		strings.Contains(msg, "overloaded") {
		return aggFailureCapacityOrQuota, "model capacity exhausted / unavailable (503)"
	}

	if sErr.Code == http.StatusUnauthorized || status == "unauthenticated" || strings.Contains(msg, "unauthenticated") {
		return aggFailureAuthExpired, "authentication failure / token expired"
	}

	if sErr.Code == http.StatusForbidden || status == "permission_denied" {
		return aggFailurePermissionDenied, "permission denied"
	}

	return aggFailureNone, ""
}

func classifyAggregateErr(err error) (aggregateFailureKind, string) {
	if err == nil {
		return aggFailureNone, ""
	}
	return classifyAggregateFailure(0, []byte(err.Error()))
}

func applyAggregateFailure(r *accountrouting.Router, email, family string, kind aggregateFailureKind, reason string) {
	if r == nil {
		return
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	switch kind {
	case aggFailureAccountDisabled:
		r.Quarantine(email, reason)
	case aggFailureCapacityOrQuota:
		r.ExcludeForModel(email, family)
	case aggFailureAuthExpired, aggFailurePermissionDenied:
		r.Exclude(email)
	}
}
