package agyproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdminHelpAndUnknown pins the admin dispatch: `help` prints the usage
// and succeeds, an unknown command is an error.
func TestAdminHelpAndUnknown(t *testing.T) {
	if code := Admin(DefaultOptions(), []string{"help"}); code != 0 {
		t.Errorf("proxy help = %d, want 0", code)
	}
	if code := Admin(DefaultOptions(), nil); code != 0 {
		t.Errorf("bare proxy = %d, want 0", code)
	}
	if code := Admin(DefaultOptions(), []string{"nope"}); code != 1 {
		t.Errorf("proxy nope = %d, want 1", code)
	}
}

// TestCaptureToggleRoundTrip checks that the capture toggle is persisted and
// that the directory is created.
func TestCaptureToggleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAGY_WRAPPER_CONFIG", filepath.Join(dir, "wrapper.json"))
	t.Setenv("OPENAGY_CAPTURE_DIR", "")
	t.Setenv("HOME", dir) // keep defaultCaptureDir inside the sandbox

	if code := Admin(DefaultOptions(), []string{"capture", "off"}); code != 0 {
		t.Fatalf("capture off = %d", code)
	}
	if c := loadConfig(); c.Capture == nil || *c.Capture {
		t.Fatalf("after off: capture = %v", c.Capture)
	}
	if code := Admin(DefaultOptions(), []string{"capture", "on"}); code != 0 {
		t.Fatalf("capture on = %d", code)
	}
	if c := loadConfig(); c.Capture == nil || !*c.Capture {
		t.Fatalf("after on: capture = %v", c.Capture)
	}

	custom := filepath.Join(dir, "captures")
	if code := Admin(DefaultOptions(), []string{"capture", "dir", custom}); code != 0 {
		t.Fatalf("capture dir = %d", code)
	}
	if got := loadConfig().CaptureDir; got != custom {
		t.Fatalf("capture dir = %q, want %q", got, custom)
	}
	if st, err := os.Stat(custom); err != nil || !st.IsDir() {
		t.Fatalf("capture dir not created: %v", err)
	}
}

// TestWithEnvReplacesNotDuplicates pins the launcher's environment rule: the
// child must see exactly one CLOUD_CODE_URL.
func TestWithEnvReplacesNotDuplicates(t *testing.T) {
	env := []string{"HOME=/h", "CLOUD_CODE_URL=http://old:1", "PATH=/bin"}
	got := withEnv(env, "CLOUD_CODE_URL", "http://127.0.0.1:2")
	want := []string{"HOME=/h", "PATH=/bin", "CLOUD_CODE_URL=http://127.0.0.1:2"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("withEnv = %q, want %q", got, want)
	}
}

// TestResolveSettingsPrecedence pins the resolution order: environment beats
// file, file beats default.
func TestResolveSettingsPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvWrapperConfig, filepath.Join(dir, "wrapper.json"))
	t.Setenv("HOME", dir)
	t.Setenv(EnvAddr, "")
	t.Setenv(EnvUpstream, "")
	t.Setenv(EnvCaptureDir, "")

	// Defaults.
	s := resolveSettings(fileConfig{})
	if s.Addr != "127.0.0.1:0" || s.Upstream != "" || !s.CaptureOn {
		t.Fatalf("defaults = %+v", s)
	}
	if !strings.HasPrefix(s.CaptureDir, dir) {
		t.Fatalf("default capture dir = %q, want under %q", s.CaptureDir, dir)
	}

	// File beats default.
	off := false
	s = resolveSettings(fileConfig{Capture: &off, Upstream: "https://u.example", Addr: "127.0.0.1:7"})
	if s.Addr != "127.0.0.1:7" || s.Upstream != "https://u.example" {
		t.Fatalf("file values = %+v", s)
	}
	if s.CaptureOn || s.CaptureDir != "" {
		t.Fatalf("capture off must clear the dir: %+v", s)
	}

	// Environment beats file.
	t.Setenv(EnvAddr, "127.0.0.1:9")
	t.Setenv(EnvUpstream, "https://env.example")
	t.Setenv(EnvCaptureDir, "/tmp/env-cap")
	s = resolveSettings(fileConfig{Addr: "127.0.0.1:7", Upstream: "https://u.example"})
	if s.Addr != "127.0.0.1:9" || s.Upstream != "https://env.example" || s.CaptureDir != "/tmp/env-cap" {
		t.Fatalf("env values = %+v", s)
	}
}
