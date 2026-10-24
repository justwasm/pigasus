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
	// Mixed: positional "minimax-m3" first, then flags.
	got := reorderArgs([]string{
		"minimax-m3",
		"--provider=openai",
		"--api-url", "https://example.com",
		"--external-model", "m",
	}, fs)
	if got[0] != "--provider=openai" {
		t.Errorf("first arg = %q, want --provider=openai", got[0])
	}
	// "minimax-m3" should be the last arg.
	if got[len(got)-1] != "minimax-m3" {
		t.Errorf("last arg = %q, want 'minimax-m3' (positional last)", got[len(got)-1])
	}
}

func TestReorderArgsPreservesOrder(t *testing.T) {
	fs := makeAddFlagSet()
	got := reorderArgs([]string{
		"--provider=openai",
		"--api-url=https://example.com",
		"minimax-m3",
	}, fs)
	if got[0] != "--provider=openai" {
		t.Errorf("got[0] = %q", got[0])
	}
	if got[1] != "--api-url=https://example.com" {
		t.Errorf("got[1] = %q", got[1])
	}
	if got[2] != "minimax-m3" {
		t.Errorf("got[2] = %q, want 'minimax-m3' positional", got[2])
	}
}

func TestRunAddListRemoveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "providers.json")

	// Add.
	err := runAdd(cfg, []string{
		"--provider=openai",
		"--api-url=https://gateway.example.com/v1/chat/completions",
		"--external-model=MiniMax-M3",
		"--api-key=-",
		"minimax-m3",
	})
	if err != nil {
		t.Fatalf("runAdd: %v", err)
	}

	// List.
	if err := runList(cfg); err != nil {
		t.Fatalf("runList: %v", err)
	}

	// Remove.
	if err := runRemove(cfg, []string{"minimax-m3"}); err != nil {
		t.Fatalf("runRemove: %v", err)
	}

	// List should now report empty.
	if err := runList(cfg); err != nil {
		t.Fatalf("runList (post-remove): %v", err)
	}
}

func TestRunAddRejectsIncomplete(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "providers.json")
	err := runAdd(cfg, []string{"--provider=openai"})
	if err == nil {
		t.Fatal("expected error on incomplete add")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("err = %q, want 'required' substring", err.Error())
	}
}

func TestRunInstallContextWritesFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "providers.json")
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

// TestAddWritesTheProviderCentricForm pins the shape the CLI writes: the
// provider carries the endpoint and the credential, and the model is
// listed under it.
func TestAddWritesTheProviderCentricForm(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "providers.json")

	err := runAdd(cfg, []string{
		"--provider=gateway",
		"--api-url=https://gateway.example.com/v1/chat/completions",
		"--external-model=MiniMax-M3",
		"--api-key=$OPENAGY_GATEWAY_KEY",
		"minimax-m3",
	})
	if err != nil {
		t.Fatalf("runAdd: %v", err)
	}
	body, err := osReadFile(cfg)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{
		`"providers"`,
		`"gateway"`,
		`"api_url": "https://gateway.example.com/v1/chat/completions"`,
		`"api_key": "$OPENAGY_GATEWAY_KEY"`,
		`"id": "minimax-m3"`,
		`"model": "MiniMax-M3"`,
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("the file is missing %s:\n%s", want, body)
		}
	}
}

// TestAddMergesIntoAnExistingProvider pins the merge: a second add that
// only carries a new model must not drop the provider's credential.
func TestAddMergesIntoAnExistingProvider(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "providers.json")

	if err := runAdd(cfg, []string{
		"--provider=gateway",
		"--api-url=https://gateway.example.com/v1/chat/completions",
		"--external-model=One",
		"--api-key=$OPENAGY_GATEWAY_KEY",
		"one",
	}); err != nil {
		t.Fatalf("runAdd: %v", err)
	}
	if err := runAdd(cfg, []string{
		"--provider=gateway",
		"--external-model=Two",
		"two",
	}); err != nil {
		t.Fatalf("runAdd: %v", err)
	}
	body, err := osReadFile(cfg)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), `"api_key": "$OPENAGY_GATEWAY_KEY"`) {
		t.Fatalf("the credential was dropped:\n%s", body)
	}
	if !strings.Contains(string(body), `"id": "one"`) {
		t.Fatalf("the first model was dropped:\n%s", body)
	}
	if !strings.Contains(string(body), `"id": "two"`) {
		t.Fatalf("the second model was not added:\n%s", body)
	}
}

// TestRemoveKeepsTheProvider pins that removing a model leaves the
// provider (and its endpoint) in place.
func TestRemoveKeepsTheProvider(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "providers.json")

	if err := runAdd(cfg, []string{
		"--provider=gateway",
		"--api-url=https://gateway.example.com/v1/chat/completions",
		"--external-model=One",
		"one",
	}); err != nil {
		t.Fatalf("runAdd: %v", err)
	}
	if err := runRemove(cfg, []string{"one"}); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	body, err := osReadFile(cfg)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), `"gateway"`) {
		t.Fatalf("the provider was removed too:\n%s", body)
	}
	if strings.Contains(string(body), `"id": "one"`) {
		t.Fatalf("the model survived:\n%s", body)
	}
}
