package endpoints

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCloudCodeURLWinsOverTheFile pins the documented precedence: the
// variable the local-service walkthroughs and the transport already set
// beats the configuration file, and Current() is the one place that decides
// it.
//
// Found by tuialign: `pigasus` was pointed at the harness's mock with
// CLOUD_CODE_URL while its plan-tier call went to Google, because only the
// transport read the variable. The banner then had no account tier on a run
// where the reference had one.
func TestCloudCodeURLWinsOverTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"endpoints": {"cloudcode": "https://from-file.invalid"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv(ConfigPathEnvVar, path)

	t.Setenv("CLOUD_CODE_URL", "http://127.0.0.1:9999")
	if got := Current().CloudCode; got != "http://127.0.0.1:9999" {
		t.Errorf("CloudCode = %q, want the CLOUD_CODE_URL value", got)
	}

	t.Setenv("CLOUD_CODE_URL", "")
	if got := Current().CloudCode; got != "https://from-file.invalid" {
		t.Errorf("CloudCode = %q, want the file's value once the variable is empty", got)
	}
}
