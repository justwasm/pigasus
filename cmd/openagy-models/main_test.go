// Tests for the openagy-models CLI. These exercise the public surface
// (runAdd / runList / runRemove) by stubbing out the file I/O via
// t.TempDir(). We don't exec the binary; the helpers are exported
// internally (runXxx) but the test is in the same package so it can
// reach them.

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReorderArgsSticksFlagsFirst(t *testing.T) {
	fs := makeAddFlagSet()
	// Mixed: positional "agw" first, then flags.
	got := reorderArgs([]string{
		"agw",
		"--provider=openai",
		"--api-url", "https://example.com",
		"--external-model", "m",
	}, fs)
	if got[0] != "--provider=openai" {
		t.Errorf("first arg = %q, want --provider=openai", got[0])
	}
	// "agw" should be the last arg.
	if got[len(got)-1] != "agw" {
		t.Errorf("last arg = %q, want 'agw' (positional last)", got[len(got)-1])
	}
}

func TestReorderArgsPreservesOrder(t *testing.T) {
	fs := makeAddFlagSet()
	got := reorderArgs([]string{
		"--provider=openai",
		"--api-url=https://example.com",
		"agw",
	}, fs)
	if got[0] != "--provider=openai" {
		t.Errorf("got[0] = %q", got[0])
	}
	if got[1] != "--api-url=https://example.com" {
		t.Errorf("got[1] = %q", got[1])
	}
	if got[2] != "agw" {
		t.Errorf("got[2] = %q, want 'agw' positional", got[2])
	}
}

func TestRunAddListRemoveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "models.json")

	// Add.
	err := runAdd(cfg, []string{
		"--provider=agw",
		"--api-url=https://agw.k0s.io/v1/chat/completions",
		"--external-model=MiniMax-M3",
		"--api-key=-",
		"agw",
	})
	if err != nil {
		t.Fatalf("runAdd: %v", err)
	}

	// List.
	if err := runList(cfg); err != nil {
		t.Fatalf("runList: %v", err)
	}

	// Remove.
	if err := runRemove(cfg, []string{"agw"}); err != nil {
		t.Fatalf("runRemove: %v", err)
	}

	// List should now report empty.
	if err := runList(cfg); err != nil {
		t.Fatalf("runList (post-remove): %v", err)
	}
}

func TestRunAddRejectsIncomplete(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "models.json")
	err := runAdd(cfg, []string{"--provider=agw"})
	if err == nil {
		t.Fatal("expected error on incomplete add")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("err = %q, want 'required' substring", err.Error())
	}
}

func TestRunInstallContextWritesFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "models.json")
	if err := runInstallContext(cfg, []string{"lite"}); err != nil {
		t.Fatalf("runInstallContext lite: %v", err)
	}
	// Both files should exist.
	for _, name := range []string{"agent-context.md", "agent-context-lite.md"} {
		if _, err := readFile(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	// Passthrough is a no-op.
	if err := runInstallContext(cfg, []string{"passthrough"}); err != nil {
		t.Fatalf("runInstallContext passthrough: %v", err)
	}
}

// readFile is a tiny helper so we don't import os in this file just for
// one call. Returns the byte count for the test.
func readFile(path string) (int, error) {
	data, err := osReadFile(path)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
