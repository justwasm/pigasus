// Package endpoints — config_test.go
//
// The configuration file's own contract: where it is, and what happens
// when it is missing or broken.
package endpoints

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tidwall/gjson"
)

// TestPathPrefersTheEnvOverride pins that $OPENAGY_CONFIG wins over
// every convention — it is the escape hatch a test or a wrapper needs.
func TestPathPrefersTheEnvOverride(t *testing.T) {
	t.Setenv(ConfigPathEnvVar, "/tmp/explicit.json")
	t.Setenv("OPENAGY_CONFIG_HOME", "/tmp/home")
	t.Setenv("HOME", "/tmp/home")

	if got := Path(); got != "/tmp/explicit.json" {
		t.Errorf("Path() = %q, want %q", got, "/tmp/explicit.json")
	}
}

// TestPathFollowsTheConfigurationHome pins the one convention: the
// configuration home, which $OPENAGY_CONFIG_HOME overrides. There is no
// second home — the retired $HOME/.config/openagy is migrated away.
func TestPathFollowsTheConfigurationHome(t *testing.T) {
	t.Setenv(ConfigPathEnvVar, "")
	t.Setenv("OPENAGY_CONFIG_HOME", "/tmp/home")
	t.Setenv("HOME", "/tmp/other")

	want := filepath.Join("/tmp/home", "config.json")
	if got := Path(); got != want {
		t.Errorf("Path() with OPENAGY_CONFIG_HOME = %q, want %q", got, want)
	}

	t.Setenv("OPENAGY_CONFIG_HOME", "")
	want = filepath.Join("/tmp/other", ".openagy", "config.json")
	if got := Path(); got != want {
		t.Errorf("Path() with HOME = %q, want %q", got, want)
	}
}

// TestPathIsEmptyWithoutAHome pins that a process with no home has no
// configuration file — rather than a path under "/.config".
func TestPathIsEmptyWithoutAHome(t *testing.T) {
	t.Setenv(ConfigPathEnvVar, "")
	t.Setenv("OPENAGY_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if got := Path(); got != "" {
		t.Errorf("Path() = %q, want the empty string", got)
	}
}

// TestLoadReadsTheEndpointsSection pins the document's shape.
func TestLoadReadsTheEndpointsSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"endpoints": {"cloudcode": "http://127.0.0.1:8080"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Endpoints["cloudcode"] != "http://127.0.0.1:8080" {
		t.Errorf("the endpoints are %v, want the configured host", cfg.Endpoints)
	}
}

// TestLoadOfAMissingFileIsEmpty pins that a user who never wrote a
// configuration file has an empty configuration, not an error.
func TestLoadOfAMissingFileIsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Endpoints) != 0 {
		t.Errorf("the endpoints are %v, want none", cfg.Endpoints)
	}
}

// TestLoadOfAMalformedFileIsAnError pins that the loader reports a
// broken file — the resolver is the layer that swallows it.
func TestLoadOfAMalformedFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"endpoints": {`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Error("Load of a malformed file returned no error")
	}
}

// TestLoadOfAnEmptyPathIsEmpty pins the degenerate case: no path, no
// configuration, no error.
func TestLoadOfAnEmptyPathIsEmpty(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Endpoints) != 0 {
		t.Errorf("the endpoints are %v, want none", cfg.Endpoints)
	}
}

// TestSaveWrapperWritesTheSection pins that the proxy's own state is
// carried verbatim in the document's "wrapper" section — the section's
// shape belongs to internal/agyproxy, not to this package.
func TestSaveWrapperWritesTheSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv(ConfigPathEnvVar, path)

	if err := SaveWrapper([]byte(`{"balance":true}`)); err != nil {
		t.Fatalf("SaveWrapper: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := gjson.GetBytes(raw, "wrapper.balance").Bool(); !got {
		t.Errorf("the wrapper section was not written: %s", raw)
	}
	if got := CurrentWrapper(); !gjson.ValidBytes(got) || !gjson.GetBytes(got, "balance").Bool() {
		t.Errorf("CurrentWrapper() = %q, want the section verbatim", got)
	}
}

// TestSaveWrapperLeavesTheRestOfTheDocument pins the one-writer rule for
// the new section: the endpoint overrides and the account-routing policy
// survive a wrapper write byte for byte.
func TestSaveWrapperLeavesTheRestOfTheDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := []byte(`{"endpoints":{"a":"b"}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigPathEnvVar, path)

	if err := SaveWrapper([]byte(`{"capture":true}`)); err != nil {
		t.Fatalf("SaveWrapper: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := gjson.GetBytes(raw, "endpoints.a").String(); got != "b" {
		t.Errorf("the endpoint override = %q, want it kept", got)
	}
	if got := gjson.GetBytes(raw, "wrapper.capture").Bool(); !got {
		t.Errorf("the wrapper section was not written: %s", raw)
	}
}

// TestSaveWrapperRemovesTheSection pins that an empty value takes the
// section out of the document.
func TestSaveWrapperRemovesTheSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"wrapper":{"balance":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigPathEnvVar, path)

	if err := SaveWrapper(nil); err != nil {
		t.Fatalf("SaveWrapper: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(raw, "wrapper").Exists() {
		t.Errorf("the section was not removed: %s", raw)
	}
	if got := CurrentWrapper(); len(got) != 0 {
		t.Errorf("CurrentWrapper() = %q, want nothing", got)
	}
}

func TestCurrentGeminiAPIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv(ConfigPathEnvVar, path)

	t.Setenv("GEMINI_API_KEY", "env-key-1")
	t.Setenv("OPENAGY_API_KEY", "env-key-2")
	if got := CurrentGeminiAPIKey(); got != "env-key-1" {
		t.Errorf("CurrentGeminiAPIKey() = %q, want env-key-1", got)
	}

	t.Setenv("GEMINI_API_KEY", "")
	if got := CurrentGeminiAPIKey(); got != "env-key-2" {
		t.Errorf("CurrentGeminiAPIKey() = %q, want env-key-2", got)
	}

	t.Setenv("OPENAGY_API_KEY", "")
	if err := os.WriteFile(path, []byte(`{"gemini_api_key":"file-key-123"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CurrentGeminiAPIKey(); got != "file-key-123" {
		t.Errorf("CurrentGeminiAPIKey() = %q, want file-key-123", got)
	}
}

func TestCurrentGeminiLiveModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv(ConfigPathEnvVar, path)

	t.Setenv("GEMINI_LIVE_MODEL", "models/custom-live-1")
	t.Setenv("OPENAGY_LIVE_MODEL", "models/custom-live-2")
	if got := CurrentGeminiLiveModel(); got != "models/custom-live-1" {
		t.Errorf("CurrentGeminiLiveModel() = %q, want models/custom-live-1", got)
	}

	t.Setenv("GEMINI_LIVE_MODEL", "")
	if got := CurrentGeminiLiveModel(); got != "models/custom-live-2" {
		t.Errorf("CurrentGeminiLiveModel() = %q, want models/custom-live-2", got)
	}

	t.Setenv("OPENAGY_LIVE_MODEL", "")
	if err := os.WriteFile(path, []byte(`{"gemini_live_model":"models/gemini-3.8-live-extended-thinking"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CurrentGeminiLiveModel(); got != "models/gemini-3.8-live-extended-thinking" {
		t.Errorf("CurrentGeminiLiveModel() = %q, want models/gemini-3.8-live-extended-thinking", got)
	}
}
