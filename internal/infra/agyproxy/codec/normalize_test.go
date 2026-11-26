package codec_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/infra/agyproxy/codec"
)

func TestNormalizeModelName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "gemini-3.8-flash-high"},
		{"gemini-3.8-flash", "gemini-3.8-flash-high"},
		{"gemini-3.8-flash-medium", "gemini-3.8-flash-medium"},
		{"gemini-3.1-pro-high", "gemini-3.1-pro-high"},
		{"gemini-3.1-pro-low", "gemini-3.1-pro-low"},
		{"gemini-2.5-pro", "gemini-2.5-pro"},
		{"gemini-2.5-flash", "gemini-2.5-flash"},
		{"gpt-6.1-sol", "gemini-3.8-flash-high"},
		{"gpt-6-astra", "gemini-3.8-flash-high"},
		{"gpt-5.6-luna", "gemini-3.8-flash-high"},
		{"gpt-5.6-terra", "gemini-3.8-flash-high"},
		{"gpt-4o", "gemini-3.8-flash-high"},
		{"sol", "gemini-3.8-flash-high"},
		{"luna", "gemini-3.8-flash-high"},
		{"o1-preview", "gemini-3.8-flash-high"},
		{"o3-mini", "gemini-3.8-flash-high"},
		{"o4-mini", "gemini-3.8-flash-high"},
		{"gpt-oss-120b-medium", "gpt-oss-120b-medium"},
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
	}
	for _, tc := range cases {
		got := codec.NormalizeModelName(tc.in)
		if got != tc.want {
			t.Errorf("codec.NormalizeModelName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}