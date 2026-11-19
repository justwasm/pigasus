package mdrender

import (
	"strings"
	"testing"
)

// TestCJKBold1512 verifies changelog 1.5.12 item 12:
// Bold text next to Chinese, Japanese, or Korean text no longer shows its ** markers
// when the bold part starts or ends with punctuation, such as `**注意：**这里`.
func TestCJKBold1512(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantBold string
		notWant  string
	}{
		{
			name:     "ends with colon punct",
			input:    "**注意：**这里",
			wantBold: "注意：",
			notWant:  "**注意：**",
		},
		{
			name:     "starts with bracket punct",
			input:    "这里**（注意）**",
			wantBold: "（注意）",
			notWant:  "**（注意）**",
		},
		{
			name:     "ends with exclamation punct",
			input:    "**重要！**请查看",
			wantBold: "重要！",
			notWant:  "**重要！**",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ANSI(tc.input, 80)
			if err != nil {
				t.Fatalf("ANSI(%q) error: %v", tc.input, err)
			}
			if strings.Contains(out, tc.notWant) {
				t.Errorf("ANSI(%q) unexpectedly contains raw markers %q: %q", tc.input, tc.notWant, out)
			}
			if !strings.Contains(out, tc.wantBold) {
				t.Errorf("ANSI(%q) missing bold text %q: %q", tc.input, tc.wantBold, out)
			}
		})
	}
}
