package persistent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/branding"
)

func TestInstanceName_BrandingPrefix(t *testing.T) {
	defer branding.Reset()

	// Test Antigravity brand
	branding.SetCurrent(branding.BrandAntigravity)
	if got := OfficialDaemonLabel(); got != DefaultOfficialDaemonLabel {
		t.Errorf("Antigravity label = %q, want %q", got, DefaultOfficialDaemonLabel)
	}
	if got := OfficialPlistPath(); !strings.HasSuffix(got, DefaultOfficialDaemonLabel+".plist") {
		t.Errorf("Antigravity plist = %q, want suffix %q", got, DefaultOfficialDaemonLabel+".plist")
	}
	if got := OfficialLogPath(); !strings.HasSuffix(got, "antigravity-cli-daemon.log") || strings.Contains(got, "pigasus-") {
		t.Errorf("Antigravity log = %q, want suffix antigravity-cli-daemon.log without pigasus- prefix", got)
	}
	if got := ResolveInstanceName("my-host"); got != "my-host" {
		t.Errorf("Antigravity ResolveInstanceName = %q, want my-host", got)
	}

	// Test Pigasus brand
	branding.SetCurrent(branding.BrandPigasus)
	wantLabel := "pigasus-" + DefaultOfficialDaemonLabel
	if got := OfficialDaemonLabel(); got != wantLabel {
		t.Errorf("Pigasus label = %q, want %q", got, wantLabel)
	}
	if got := OfficialPlistPath(); !strings.HasSuffix(got, wantLabel+".plist") {
		t.Errorf("Pigasus plist = %q, want suffix %q", got, wantLabel+".plist")
	}
	if got := OfficialLogPath(); !strings.HasSuffix(got, "pigasus-antigravity-cli-daemon.log") {
		t.Errorf("Pigasus log = %q, want suffix pigasus-antigravity-cli-daemon.log", got)
	}
	if got := ResolveInstanceName("my-host"); got != "pigasus-my-host" {
		t.Errorf("Pigasus ResolveInstanceName = %q, want pigasus-my-host", got)
	}
	if got := ResolveInstanceName("pigasus-my-host"); got != "pigasus-my-host" {
		t.Errorf("Pigasus ResolveInstanceName (already prefixed) = %q, want pigasus-my-host", got)
	}
}

func TestSetInstanceName_PrefixNotPersisted(t *testing.T) {
	defer branding.Reset()
	branding.SetCurrent(branding.BrandPigasus)

	tmpDir := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", tmpDir)

	if err := SetInstanceName("pigasus-my-box"); err != nil {
		t.Fatalf("SetInstanceName failed: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(tmpDir, "config.json"))
	if err != nil {
		t.Fatalf("ReadFile config.json failed: %v", err)
	}
	var doc struct {
		UserSettings struct {
			CliRemoteControlHostname string `json:"cliRemoteControlHostname"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal config.json failed: %v", err)
	}

	// Crucial invariant: config.json must contain the base name without "pigasus-" prefix!
	if doc.UserSettings.CliRemoteControlHostname != "my-box" {
		t.Errorf("Persisted hostname in config.json = %q, want %q (prefix must not be persisted)", doc.UserSettings.CliRemoteControlHostname, "my-box")
	}

	// In Pigasus branding, runtime resolution adds the prefix
	if got := ResolveInstanceName(""); got != "pigasus-my-box" {
		t.Errorf("ResolveInstanceName under Pigasus = %q, want pigasus-my-box", got)
	}

	// In Antigravity branding, runtime resolution returns the un-prefixed base name
	branding.SetCurrent(branding.BrandAntigravity)
	if got := ResolveInstanceName(""); got != "my-box" {
		t.Errorf("ResolveInstanceName under Antigravity = %q, want my-box", got)
	}
}
