package printmode

import (
	"flag"
	"strings"
	"testing"
)

func TestPrintSlash_GeminiAPIKeyUsageQuotaCredits(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-api-key-12345")

	for _, cmd := range []string{"/usage", "/quota", "/credits"} {
		t.Run(cmd, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			res := dispatchPrintSlash(cmd, fs)
			if !res.Handled {
				t.Fatalf("%s not handled", cmd)
			}
			if res.Err == nil {
				t.Fatalf("%s returned nil err, want code 2", cmd)
			}
			wantMsg := cmd + " failed: not available with a Gemini API key; see https://aistudio.google.com/usage for the key's usage"
			if !strings.Contains(res.Err.Error(), wantMsg) {
				t.Errorf("%s error = %q, want containing %q", cmd, res.Err.Error(), wantMsg)
			}
		})
	}

	t.Run("help omits usage quota credits", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		res := dispatchPrintSlash("/help", fs)
		if !res.Handled {
			t.Fatal("/help not handled")
		}
		if strings.Contains(res.Output, "/usage") || strings.Contains(res.Output, "/quota") || strings.Contains(res.Output, "/credits") {
			t.Errorf("/help output should omit /usage, /quota, /credits under GEMINI_API_KEY:\n%s", res.Output)
		}
	})
}
