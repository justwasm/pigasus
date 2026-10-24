// Package configtest isolates the provider configuration for tests.
//
// The clone's catalogue, its context-window table and its transport all read
// the user's provider configuration. A test that does not isolate it depends
// on what happens to be on the machine: a model the user configured would
// appear in the catalogue, change the resolved label and shift the expected
// output.
//
// Isolate points the provider configuration at an empty scratch file and
// restores the environment when the test ends.
package configtest

import (
	"os"
	"path/filepath"
	"testing"
)

// Isolate points the provider configuration at an empty scratch file, so the
// catalogue a test sees is the reference's own and nothing else.
func Isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OPENAGY_PROVIDERS_CONFIG", filepath.Join(dir, "providers.json"))
}

// WriteProviders writes a provider configuration document and points the
// configuration at it. The document is the provider-centric form:
//
//	{"providers": {"gateway": {"type": "openai", "base_url": "...",
//	  "models": [{"id": "...", "model": "..."}]}}}
func WriteProviders(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("configtest: write providers: %v", err)
	}
	t.Setenv("OPENAGY_PROVIDERS_CONFIG", path)
}
