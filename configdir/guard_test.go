package configdir

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRefuseTestWriteRefusesTheStartupHome pins the rule the configuration
// writers rely on: a test binary may not write the home this process started
// with, whatever the environment says now.
//
// The test binary *is* a test binary, so this is the production code path, and
// the startup home is the one this process really began with — resolving it
// again after t.Setenv would answer the scratch directory, which is exactly the
// mistake the guard exists to catch.
func TestRefuseTestWriteRefusesTheStartupHome(t *testing.T) {
	startup := StartupDir()
	if startup == "" {
		t.Skip("this process started without a configuration home")
	}
	// The environment now points somewhere else — this is the state a test
	// leaves behind when its t.Setenv is restored.
	t.Setenv(EnvHome, t.TempDir())

	if !IsTestBinary() {
		t.Fatal("IsTestBinary() = false in a test binary")
	}
	for _, name := range []string{"accounts.json", "config.json", "providers.json"} {
		path := filepath.Join(startup, name)
		err := RefuseTestWrite(path)
		if err == nil {
			t.Fatalf("RefuseTestWrite(%q) = nil, want a refusal", path)
		}
		if !strings.Contains(err.Error(), "refusing to write") {
			t.Fatalf("RefuseTestWrite(%q) = %v, want the refusal to say so", path, err)
		}
	}
}

// TestRefuseTestWriteAllowsAScratchHome pins the other half: a write into a
// test's own directory is the normal case and must go through.
func TestRefuseTestWriteAllowsAScratchHome(t *testing.T) {
	scratch := t.TempDir()
	t.Setenv(EnvHome, scratch)
	if err := RefuseTestWrite(filepath.Join(scratch, "accounts.json")); err != nil {
		t.Fatalf("RefuseTestWrite into a scratch home = %v, want nil", err)
	}
	if err := RefuseTestWrite(""); err != nil {
		t.Fatalf("RefuseTestWrite(\"\") = %v, want nil", err)
	}
}
