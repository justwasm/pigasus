package apierror

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestIsAccountDisabledErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "standard error without keywords",
			err:  errors.New("connection reset by peer"),
			want: false,
		},
		{
			name: "text error with terms of service",
			err:  errors.New("This service has been disabled in this account for violation of Terms of Service. Please submit an appeal"),
			want: true,
		},
		{
			name: "wrapped text error",
			err:  fmt.Errorf("stream failed: %w", errors.New("upstream rejected: account has been disabled")),
			want: true,
		},
		{
			name: "HTTPError with tos_violation in body",
			err: &HTTPError{
				StatusCode: http.StatusForbidden,
				Body:       []byte(`{"error":{"code":403,"message":"Account suspended: tos_violation"}}`),
			},
			want: true,
		},
		{
			name: "HTTPError with service has been disabled in body",
			err: &HTTPError{
				StatusCode: http.StatusForbidden,
				Body:       []byte(`{"error":{"code":403,"message":"This service has been disabled in this account for violation of Terms of Service."}}`),
			},
			want: true,
		},
		{
			name: "HTTPError with quota error",
			err: &HTTPError{
				StatusCode: http.StatusTooManyRequests,
				Body:       []byte(`{"error":{"code":429,"message":"Resource exhausted: quota exceeded"}}`),
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsAccountDisabledErr(tc.err)
			if got != tc.want {
				t.Errorf("IsAccountDisabledErr() = %v, want %v", got, tc.want)
			}
			if tc.want {
				reason := AccountDisabledReason(tc.err)
				if reason == "" {
					t.Errorf("AccountDisabledReason() is empty for disabled error")
				}
			}
		})
	}
}

func TestIsAuthFailureErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "401 HTTPError",
			err:  &HTTPError{StatusCode: http.StatusUnauthorized},
			want: true,
		},
		{
			name: "KindAuthExpired",
			err:  &HTTPError{Kind: KindAuthExpired},
			want: true,
		},
		{
			name: "invalid_grant error",
			err:  errors.New("oauth2: cannot fetch token: 400 Bad Request: {\"error\": \"invalid_grant\"}"),
			want: true,
		},
		{
			name: "unauthenticated error",
			err:  errors.New("Request unauthenticated: invalid credentials"),
			want: true,
		},
		{
			name: "normal error",
			err:  errors.New("connection timed out"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsAuthFailureErr(tc.err)
			if got != tc.want {
				t.Errorf("IsAuthFailureErr() = %v, want %v", got, tc.want)
			}
		})
	}
}
