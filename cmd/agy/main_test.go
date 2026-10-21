package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/models"
)

// dispatch captures which path run() chose without exec'ing anything.
func dispatch(t *testing.T, args []string) (which string, got []string) {
	t.Helper()
	oldL, oldW := launcherFunc, wrapperFunc
	t.Cleanup(func() { launcherFunc, wrapperFunc = oldL, oldW })
	launcherFunc = func(a []string) int { which, got = "launcher", a; return 0 }
	wrapperFunc = func(a []string) int { which, got = "wrapper", a; return 0 }
	run(args)
	return which, got
}

func TestRunPassesEverythingButWrapperToLauncher(t *testing.T) {
	cases := []struct {
		args  []string
		which string
		want  []string
	}{
		{[]string{"-p", "hi"}, "launcher", []string{"-p", "hi"}},
		{[]string{"--version"}, "launcher", []string{"--version"}},
		{[]string{"models"}, "launcher", []string{"models"}},
		{[]string{}, "launcher", nil},
		// "wrapper" must be the literal first argument: as an argument value
		// it belongs to agy.
		{[]string{"-p", "wrapper"}, "launcher", []string{"-p", "wrapper"}},
		{[]string{"--wrapper"}, "launcher", []string{"--wrapper"}},
		{[]string{"wrapper"}, "wrapper", nil},
		{[]string{"wrapper", "doctor"}, "wrapper", []string{"doctor"}},
		{[]string{"wrapper", "models", "add", "x"}, "wrapper", []string{"models", "add", "x"}},
	}
	for _, tc := range cases {
		which, got := dispatch(t, tc.args)
		if which != tc.which {
			t.Errorf("%v: routed to %s, want %s", tc.args, which, tc.which)
			continue
		}
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("%v: args %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestWrapperHelpAndUnknown(t *testing.T) {
	if code := runWrapper([]string{"help"}); code != 0 {
		t.Errorf("wrapper help = %d, want 0", code)
	}
	if code := runWrapper(nil); code != 0 {
		t.Errorf("bare wrapper = %d, want 0", code)
	}
	if code := runWrapper([]string{"nope"}); code != 1 {
		t.Errorf("wrapper nope = %d, want 1", code)
	}
}

func TestCaptureToggleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAGY_WRAPPER_CONFIG", filepath.Join(dir, "wrapper.json"))
	t.Setenv("OPENAGY_CAPTURE_DIR", "")
	t.Setenv("HOME", dir) // keep defaultCaptureDir inside the sandbox

	if code := runWrapper([]string{"capture", "off"}); code != 0 {
		t.Fatalf("capture off = %d", code)
	}
	if c := loadConfig(); c.Capture == nil || *c.Capture {
		t.Fatalf("after off: capture = %v", c.Capture)
	}
	if code := runWrapper([]string{"capture", "on"}); code != 0 {
		t.Fatalf("capture on = %d", code)
	}
	if c := loadConfig(); c.Capture == nil || !*c.Capture {
		t.Fatalf("after on: capture = %v", c.Capture)
	}

	custom := filepath.Join(dir, "captures")
	if code := runWrapper([]string{"capture", "dir", custom}); code != 0 {
		t.Fatalf("capture dir = %d", code)
	}
	if got := loadConfig().CaptureDir; got != custom {
		t.Fatalf("capture dir = %q, want %q", got, custom)
	}
	if st, err := os.Stat(custom); err != nil || !st.IsDir() {
		t.Fatalf("capture dir not created: %v", err)
	}
}

func TestModelsAddRemove(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "models.json")
	t.Setenv("XDG_CONFIG_HOME", dir) // DefaultConfigPath -> dir/openagy/models.json
	t.Setenv("AGY_REAL_BIN", "")

	// First run: the default MiniMax M3 entry appears, from ensureRegistry.
	if code := runWrapper([]string{"models"}); code != 0 {
		t.Fatalf("models = %d", code)
	}
	reg, err := models.LoadFromFile(models.DefaultConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.GetByName("minimax-m3"); !ok {
		t.Fatalf("default model missing; registry = %+v", reg.List())
	}

	add := []string{
		"models", "add", "local-qwen",
		"-provider", "custom",
		"-api-url", "http://127.0.0.1:11434/v1/chat/completions",
		"-external-model", "qwen3:8b",
		"-display-name", "Qwen 3 8B",
	}
	if code := runWrapper(add); code != 0 {
		t.Fatalf("models add = %d", code)
	}
	reg, err = models.LoadFromFile(models.DefaultConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	m, ok := reg.GetByName("local-qwen")
	if !ok {
		t.Fatalf("added model missing: %+v", reg.List())
	}
	if m.ExternalModelName != "qwen3:8b" || m.DisplayName != "Qwen 3 8B" {
		t.Fatalf("model round-trip = %+v", m)
	}

	if code := runWrapper([]string{"models", "remove", "local-qwen"}); code != 0 {
		t.Fatalf("models remove = %d", code)
	}
	reg, _ = models.LoadFromFile(models.DefaultConfigPath())
	if _, ok := reg.GetByName("local-qwen"); ok {
		t.Fatal("model still present after remove")
	}
	if code := runWrapper([]string{"models", "remove", "local-qwen"}); code != 1 {
		t.Fatalf("removing a missing model = %d, want 1", code)
	}
	_ = regPath
}

func TestWithEnvReplacesNotDuplicates(t *testing.T) {
	env := []string{"HOME=/h", "CLOUD_CODE_URL=http://old:1", "PATH=/bin"}
	got := withEnv(env, "CLOUD_CODE_URL", "http://127.0.0.1:2")
	want := []string{"HOME=/h", "PATH=/bin", "CLOUD_CODE_URL=http://127.0.0.1:2"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("withEnv = %q, want %q", got, want)
	}
}

func TestResolveSettingsPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvWrapperConfig, filepath.Join(dir, "wrapper.json"))
	t.Setenv("HOME", dir)
	t.Setenv(EnvAddr, "")
	t.Setenv(EnvUpstream, "")
	t.Setenv(EnvCaptureDir, "")

	// Defaults.
	s := resolveSettings(config{})
	if s.Addr != "127.0.0.1:0" || s.Upstream != "" || !s.CaptureOn {
		t.Fatalf("defaults = %+v", s)
	}
	if !strings.HasPrefix(s.CaptureDir, dir) {
		t.Fatalf("default capture dir = %q, want under %q", s.CaptureDir, dir)
	}

	// File beats default.
	off := false
	s = resolveSettings(config{Capture: &off, Upstream: "https://u.example", Addr: "127.0.0.1:7"})
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
	s = resolveSettings(config{Addr: "127.0.0.1:7", Upstream: "https://u.example"})
	if s.Addr != "127.0.0.1:9" || s.Upstream != "https://env.example" || s.CaptureDir != "/tmp/env-cap" {
		t.Fatalf("env values = %+v", s)
	}
}
