package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/justwasm/pigasus/internal/core/apierror"
)

type sseErrorPayload struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

func formatSSEErrorCode(code any) string {
	if code == nil {
		return ""
	}
	switch v := code.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

// sseErrorToHTTPError converts a mid-stream SSE error frame into a typed
// *apierror.HTTPError so apierror.IsQuotaReachedErr, IsRetryableStatusErr,
// IsAuthFailureErr, and failover.IsFailoverEligible classify it accurately.
func sseErrorToHTTPError(errType, errCode, errMsg string, rawPayload []byte, endpointURL string) *apierror.HTTPError {
	status := classifySSEErrorStatus(errType, errCode, errMsg)
	body := rawPayload
	if len(body) == 0 {
		envelope := map[string]any{
			"error": map[string]any{
				"message": errMsg,
				"type":    errType,
				"code":    errCode,
			},
		}
		if b, err := json.Marshal(envelope); err == nil {
			body = b
		}
	}
	return &apierror.HTTPError{
		StatusCode: status,
		Body:       body,
		URL:        endpointURL,
	}
}

func classifySSEErrorStatus(errType, errCode, errMsg string) int {
	t := strings.ToLower(strings.TrimSpace(errType))
	c := strings.ToLower(strings.TrimSpace(errCode))
	m := strings.ToLower(strings.TrimSpace(errMsg))

	for _, token := range []string{t, c} {
		switch token {
		case "rate_limit_error", "rate_limit_exceeded", "too_many_requests",
			"insufficient_quota", "quota_exceeded", "resource_exhausted", "429":
			return http.StatusTooManyRequests
		case "overloaded_error", "overloaded", "529":
			return 529
		case "service_unavailable", "503":
			return http.StatusServiceUnavailable
		case "authentication_error", "invalid_api_key", "unauthorized", "unauthenticated", "401":
			return http.StatusUnauthorized
		case "permission_error", "forbidden", "403":
			return http.StatusForbidden
		case "billing_hard_limit_reached", "payment_required", "402":
			return http.StatusPaymentRequired
		case "bad_gateway", "502":
			return http.StatusBadGateway
		case "gateway_timeout", "504":
			return http.StatusGatewayTimeout
		case "server_error", "api_error", "internal_error", "upstream_error", "500":
			return http.StatusInternalServerError
		}
	}

	switch {
	case strings.Contains(m, "insufficient_quota") || strings.Contains(m, "quota") ||
		strings.Contains(m, "rate limit") || strings.Contains(m, "too many requests") ||
		strings.Contains(m, "resource_exhausted"):
		return http.StatusTooManyRequests
	case strings.Contains(m, "overloaded"):
		return 529
	case strings.Contains(m, "invalid api key") || strings.Contains(m, "authentication") ||
		strings.Contains(m, "unauthorized") || strings.Contains(m, "unauthenticated"):
		return http.StatusUnauthorized
	case strings.Contains(m, "permission") || strings.Contains(m, "forbidden"):
		return http.StatusForbidden
	case strings.Contains(m, "spend cap") || strings.Contains(m, "prepaid balance"):
		return http.StatusPaymentRequired
	default:
		return http.StatusInternalServerError
	}
}
