package agyproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/justwasm/pigasus/geminicode"
)

type accountFailureKind int

const (
	failureNone accountFailureKind = iota
	failureQuotaExceeded
	failureAccountDisabled
	failurePermissionDenied
	failureAuthExpired
)

// classifyAccountFailure inspects the HTTP status code and response body to determine
// whether the failure is tied to the specific account credential (and thus should trigger
// account exclusion and failover), or is a general/request-level failure (e.g. 400 bad request).
func classifyAccountFailure(code int, body []byte) (accountFailureKind, string) {
	low := bytes.ToLower(body)

	// Check for TOS violation / account disabled
	if bytes.Contains(low, []byte("tos_violation")) ||
		bytes.Contains(low, []byte("terms of service")) ||
		bytes.Contains(low, []byte("service has been disabled")) ||
		bytes.Contains(low, []byte("account has been disabled")) ||
		bytes.Contains(low, []byte("account_disabled")) {
		return failureAccountDisabled, "Terms of Service violation / account service disabled"
	}

	// 429 is always quota / rate limit
	if code == http.StatusTooManyRequests {
		return failureQuotaExceeded, "quota exceeded"
	}

	// Check for Quota / Resource exhausted in 403 or other codes
	if bytes.Contains(low, []byte("quota")) ||
		bytes.Contains(low, []byte("resource_exhausted")) ||
		bytes.Contains(low, []byte("rate_limit")) ||
		bytes.Contains(low, []byte("ratelimit")) {
		return failureQuotaExceeded, "quota exceeded"
	}

	// 401 is auth failure (token expired, revoked, unauthenticated)
	if code == http.StatusUnauthorized || bytes.Contains(low, []byte("unauthenticated")) {
		return failureAuthExpired, "authentication failure / token expired"
	}

	// 403 Forbidden is account permission denied
	if code == http.StatusForbidden {
		return failurePermissionDenied, "permission denied"
	}

	return failureNone, ""
}

// extractErrorInfo parses Google CloudCode / RPC error details if present.
func extractErrorInfo(body []byte) (reason, message, appealURL string) {
	var parsed struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
			Details []struct {
				Type     string            `json:"@type"`
				Reason   string            `json:"reason"`
				Metadata map[string]string `json:"metadata"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		message = parsed.Error.Message
		for _, d := range parsed.Error.Details {
			if d.Reason != "" && reason == "" {
				reason = d.Reason
			}
			if d.Metadata != nil {
				if u := d.Metadata["appeal_url"]; u != "" && appealURL == "" {
					appealURL = u
				}
			}
		}
	}
	return reason, message, appealURL
}

// isStreamAccountError classifies an in-stream error frame.
func isStreamAccountError(sErr *geminicode.StreamError) (isAccountErr bool, isGlobal bool, reason string) {
	if sErr == nil {
		return false, false, ""
	}
	msg := strings.ToLower(sErr.Message)
	if strings.Contains(msg, "tos_violation") ||
		strings.Contains(msg, "terms of service") ||
		strings.Contains(msg, "service has been disabled") ||
		strings.Contains(msg, "account has been disabled") ||
		strings.Contains(msg, "account_disabled") {
		return true, true, "Terms of Service violation / account service disabled"
	}
	if sErr.Code == 429 || strings.EqualFold(sErr.Status, "RESOURCE_EXHAUSTED") ||
		strings.Contains(msg, "quota") || strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "rate_limit") || strings.Contains(msg, "ratelimit") {
		return true, false, "quota exceeded"
	}
	if sErr.Code == 401 || strings.EqualFold(sErr.Status, "UNAUTHENTICATED") || strings.Contains(msg, "unauthenticated") {
		return true, true, "authentication failure / token expired"
	}
	if sErr.Code == 403 || strings.EqualFold(sErr.Status, "PERMISSION_DENIED") {
		return true, true, "permission denied"
	}
	return false, false, ""
}

// formatClientErrorMessage ensures client-facing error payloads identify which account was attempted.
func formatClientErrorMessage(email string, err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if email != "" && !strings.Contains(msg, email) {
		return fmt.Sprintf("[%s] %s", email, msg)
	}
	return msg
}
