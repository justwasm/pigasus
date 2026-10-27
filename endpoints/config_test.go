// Package endpoints — config_test.go
//
// The configuration file's own contract: where it is, and what happens
// when it is missing or broken.
package endpoints

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/justwasm/pigasus/accountpool"
)

// TestPathPrefersTheEnvOverride pins that $OPENAGY_CONFIG wins over
// every convention — it is the escape hatch a test or a wrapper needs.
func TestPathPrefersTheEnvOverride(t *testing.T) {
	t.Setenv(ConfigPathEnvVar, "/tmp/explicit.json")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	t.Setenv("HOME", "/tmp/home")

	if got := Path(); got != "/tmp/explicit.json" {
		t.Errorf("Path() = %q, want %q", got, "/tmp/explicit.json")
	}
}

// TestPathFollowsXDGThenHome pins the two conventions, in order.
func TestPathFollowsXDGThenHome(t *testing.T) {
	t.Setenv(ConfigPathEnvVar, "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	t.Setenv("HOME", "/tmp/home")

	want := filepath.Join("/tmp/xdg", "openagy", "config.json")
	if got := Path(); got != want {
		t.Errorf("Path() with XDG = %q, want %q", got, want)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	want = filepath.Join("/tmp/home", ".config", "openagy", "config.json")
	if got := Path(); got != want {
		t.Errorf("Path() with HOME = %q, want %q", got, want)
	}
}

// TestPathIsEmptyWithoutAHome pins that a process with no home has no
// configuration file — rather than a path under "/.config".
func TestPathIsEmptyWithoutAHome(t *testing.T) {
	t.Setenv(ConfigPathEnvVar, "")
	t.Setenv("XDG_CONFIG_HOME", "")
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

// TestLoadReadsTheAccountPoolSection pins that the document carries the
// account-routing policy beside the endpoint overrides, and that a
// document without the section carries no policy.
func TestLoadReadsTheAccountPoolSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
	  "endpoints": {"cloudcode": "http://127.0.0.1:8080"},
	  "account_pool": {
	    "enabled": true,
	    "strategy": "quota_aware",
	    "threshold": 0.15,
	    "refresh_interval": "2m",
	    "accounts": ["a@example.com"]
	  }
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AccountPool == nil {
		t.Fatal("the account_pool section was not read")
	}
	if !cfg.AccountPool.Enabled {
		t.Error("the policy should be enabled")
	}
	if cfg.AccountPool.Strategy != "quota_aware" {
		t.Errorf("the strategy = %q, want quota_aware", cfg.AccountPool.Strategy)
	}
	if cfg.AccountPool.Threshold != 0.15 {
		t.Errorf("the threshold = %v, want 0.15", cfg.AccountPool.Threshold)
	}
	if cfg.AccountPool.RefreshInterval != "2m" {
		t.Errorf("the refresh interval = %q, want 2m", cfg.AccountPool.RefreshInterval)
	}
	if len(cfg.AccountPool.Accounts) != 1 {
		t.Errorf("the allow-list = %v, want one address", cfg.AccountPool.Accounts)
	}

	// A document without the section carries no policy: the routing is
	// off, not "on with the defaults".
	plain := filepath.Join(dir, "plain.json")
	if err := os.WriteFile(plain, []byte(`{"endpoints": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(plain)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AccountPool != nil {
		t.Errorf("the policy = %+v, want none", cfg.AccountPool)
	}
}

// TestCurrentAccountPoolReadsTheFile pins that the resolver hands the
// policy out, and that a file without the section hands out nothing.
func TestCurrentAccountPoolReadsTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"account_pool": {"enabled": true, "strategy": "failover"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigPathEnvVar, path)

	policy := CurrentAccountPool()
	if policy == nil {
		t.Fatal("the policy was not read")
	}
	if !policy.Enabled || policy.Strategy != "failover" {
		t.Errorf("the policy = %+v, want an enabled failover", policy)
	}

	// The file changes: the resolver picks the change up.
	if err := os.WriteFile(path, []byte(`{"account_pool": {"enabled": false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	policy = CurrentAccountPool()
	if policy == nil {
		t.Fatal("the policy was not read after the change")
	}
	if policy.Enabled {
		t.Error("the changed policy should be disabled")
	}
}

// TestSaveAccountPoolWritesTheSection pins that the writer puts the
// policy where the reader looks for it, and that the write is seen at
// once — the cached snapshot is dropped, so the next read answers with
// the policy that was just written.
func TestSaveAccountPoolWritesTheSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv(ConfigPathEnvVar, path)

	if err := SaveAccountPool(&accountpool.Config{Enabled: true, Strategy: "quota_aware"}); err != nil {
		t.Fatalf("SaveAccountPool: %v", err)
	}

	policy := CurrentAccountPool()
	if policy == nil {
		t.Fatal("the written policy was not read back")
	}
	if !policy.Enabled || policy.Strategy != "quota_aware" {
		t.Errorf("the policy = %+v, want an enabled quota_aware", policy)
	}
}

// TestSaveAccountPoolLeavesTheRestOfTheDocument pins the one-writer
// rule: the other sections of the file survive the write byte for byte,
// and the key order survives too.
func TestSaveAccountPoolLeavesTheRestOfTheDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := `{
  "endpoints": {
    "cloud_code": "http://127.0.0.1:8080"
  },
  "future_section": {
    "kept": true
  }
}
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigPathEnvVar, path)

	if err := SaveAccountPool(&accountpool.Config{Enabled: true}); err != nil {
		t.Fatalf("SaveAccountPool: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !gjson.ValidBytes(raw) {
		t.Fatalf("the written file is not valid JSON: %s", raw)
	}
	if got := gjson.GetBytes(raw, "endpoints.cloud_code").String(); got != "http://127.0.0.1:8080" {
		t.Errorf("the endpoint override = %q, want it kept", got)
	}
	if !gjson.GetBytes(raw, "future_section.kept").Bool() {
		t.Error("the unknown section was not kept")
	}
	if !gjson.GetBytes(raw, "account_pool.enabled").Bool() {
		t.Error("the policy was not written")
	}
	// The endpoints section still comes first: the edit was made on the
	// raw document, not by re-marshalling it.
	if bytes.Index(raw, []byte(`"endpoints"`)) > bytes.Index(raw, []byte(`"account_pool"`)) {
		t.Error("the key order was not kept")
	}
}

// TestSaveAccountPoolRemovesTheSection pins that a nil policy takes the
// section away rather than blanking it.
func TestSaveAccountPoolRemovesTheSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"endpoints": {"a": "b"}, "account_pool": {"enabled": true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigPathEnvVar, path)

	if err := SaveAccountPool(nil); err != nil {
		t.Fatalf("SaveAccountPool: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(raw, "account_pool").Exists() {
		t.Errorf("the section was not removed: %s", raw)
	}
	if got := gjson.GetBytes(raw, "endpoints.a").String(); got != "b" {
		t.Errorf("the endpoint override = %q, want it kept", got)
	}
	if CurrentAccountPool() != nil {
		t.Error("the removed policy is still read")
	}
}

// TestSaveAccountPoolRefusesAMalformedDocument pins that the writer
// never overwrites a file it cannot read: a broken document is an error,
// not a reason to replace it.
func TestSaveAccountPoolRefusesAMalformedDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := []byte(`{"endpoints": {`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigPathEnvVar, path)

	if err := SaveAccountPool(&accountpool.Config{Enabled: true}); err == nil {
		t.Fatal("a malformed document should be an error")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, original) {
		t.Errorf("the malformed document was rewritten: %s", raw)
	}
}
