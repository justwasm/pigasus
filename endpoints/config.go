// Package endpoints — config.go
//
// The configuration file the overrides come from.
package endpoints

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/atomicfile"
)

// ConfigPathEnvVar is the environment variable that overrides the
// configuration file's path. It is the sibling of
// $OPENAGY_PROVIDERS_CONFIG, which names the provider-centric file.
const ConfigPathEnvVar = "OPENAGY_CONFIG"

// Config is the configuration document.
//
// The document is a map of sections rather than a bare map of endpoints so
// that the file can grow a second section without a format change. There
// are two today:
//
//   - "endpoints" — the endpoint overrides.
//   - "account_pool" — the account-routing policy, the shape of which
//     belongs to the accountpool package. This package only carries it,
//     so the configuration file keeps a single reader.
type Config struct {
	// Endpoints maps an endpoint name (see the name constants in
	// endpoints.go) to the URL to use instead of the compiled-in
	// default.
	Endpoints map[string]string `json:"endpoints,omitempty"`

	// AccountPool is the account-routing policy: which account a turn
	// runs on, and how the pool rotates. It is nil when the file carries
	// no such section, which means "no routing" — the behaviour without
	// the policy.
	AccountPool *accountpool.Config `json:"account_pool,omitempty"`
}

// Path returns the configuration file's path:
//
//	$XDG_CONFIG_HOME/openagy/config.json, or
//	$HOME/.config/openagy/config.json if HOME is set.
//
// The env override is $OPENAGY_CONFIG.
func Path() string {
	if p := strings.TrimSpace(os.Getenv(ConfigPathEnvVar)); p != "" {
		return p
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "openagy", "config.json")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "openagy", "config.json")
	}
	return ""
}

// Load reads the configuration document at path.
//
// A missing file is an empty configuration, not an error: the defaults
// apply until the user writes one.
func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("endpoints: read %s: %w", path, err)
	}
	var doc Config
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Config{}, fmt.Errorf("endpoints: parse %s: %w", path, err)
	}
	return doc, nil
}

// overrides is one resolved snapshot of the file's endpoint overrides.
type overrides struct {
	stamp  string
	values map[string]string
}

// overridesCache holds the last snapshot, with the stamp of the file it
// was built from.
var overridesCache atomic.Pointer[overrides]

// currentOverrides returns the endpoint overrides, re-reading the file only
// when it changed on disk.
//
// The stamp is the file's size and modification time — the same identity
// the provider registry uses for its own cache. A file that does not exist
// has the stamp "-", which is stable: the resolver does not stat it again
// until it appears.
func currentOverrides() map[string]string {
	path := Path()
	stamp := fileStamp(path)

	if cached := overridesCache.Load(); cached != nil && cached.stamp == stamp {
		return cached.values
	}

	values := map[string]string{}
	if cfg, err := Load(path); err == nil {
		for name, url := range cfg.Endpoints {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			values[name] = strings.TrimSpace(url)
		}
	}
	snapshot := &overrides{stamp: stamp, values: values}
	overridesCache.Store(snapshot)
	return snapshot.values
}

// accountPool is one resolved snapshot of the file's account-routing
// policy.
type accountPool struct {
	stamp  string
	policy *accountpool.Config
}

// accountPoolCache holds the last snapshot, with the stamp of the file it
// was built from.
var accountPoolCache atomic.Pointer[accountPool]

// CurrentAccountPool returns the account-routing policy the configuration
// file carries, or nil when it carries none.
//
// The file is re-read only when it changed on disk — the same stamp the
// endpoint overrides use — so a policy the user edits mid-session takes
// effect on the next read.
//
// The returned policy is a copy: the caller may normalise it (fill in the
// defaults) without touching the cached one.
func CurrentAccountPool() *accountpool.Config {
	path := Path()
	stamp := fileStamp(path)

	if cached := accountPoolCache.Load(); cached != nil && cached.stamp == stamp {
		return cached.policy
	}

	var policy *accountpool.Config
	if cfg, err := Load(path); err == nil && cfg.AccountPool != nil {
		copied := *cfg.AccountPool
		policy = &copied
	}
	snapshot := &accountPool{stamp: stamp, policy: policy}
	accountPoolCache.Store(snapshot)
	return snapshot.policy
}

// SaveAccountPool writes the account-routing policy into the
// configuration file.
//
// It rewrites the "account_pool" section and leaves the rest of the
// document — the endpoint overrides, and any section a newer version
// wrote — byte for byte as it was: the edit is made on the raw document,
// so the key order survives. A nil policy removes the section.
//
// The write is atomic, and the cached snapshot is dropped, so the policy
// the routing reads is the one that was just written — the turn after the
// write runs on it.
func SaveAccountPool(policy *accountpool.Config) error {
	path := Path()
	if strings.TrimSpace(path) == "" {
		return errors.New("endpoints: no configuration file path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("endpoints: read %s: %w", path, err)
		}
		raw = []byte("{}")
	}
	document := bytes.TrimSpace(raw)
	if len(document) == 0 {
		document = []byte("{}")
	}
	if !gjson.ValidBytes(document) {
		return fmt.Errorf("endpoints: %s is not valid JSON", path)
	}

	var updated []byte
	if policy == nil {
		updated, err = sjson.DeleteBytes(document, accountPoolKey)
		if err != nil {
			return fmt.Errorf("endpoints: remove %s: %w", accountPoolKey, err)
		}
	} else {
		encoded, err := json.Marshal(policy)
		if err != nil {
			return fmt.Errorf("endpoints: encode the account pool: %w", err)
		}
		updated, err = sjson.SetRawBytes(document, accountPoolKey, encoded)
		if err != nil {
			return fmt.Errorf("endpoints: write %s: %w", accountPoolKey, err)
		}
	}

	formatted := append(pretty.Pretty(updated), '\n')
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("endpoints: create %s: %w", dir, err)
		}
	}
	if err := atomicfile.WriteFile(path, formatted, 0o600); err != nil {
		return fmt.Errorf("endpoints: write %s: %w", path, err)
	}
	// The cached snapshot describes the file before the write: drop it,
	// so the next read picks the new one up.
	accountPoolCache.Store(nil)
	return nil
}

// accountPoolKey is the section name the account-routing policy lives
// under in the configuration document.
const accountPoolKey = "account_pool"

// fileStamp renders the identity of a configuration file: its path, and
// its size and modification time when it exists.
func fileStamp(path string) string {
	if path == "" {
		return "-"
	}
	var b strings.Builder
	b.WriteString(path)
	b.WriteByte(0)
	if st, err := os.Stat(path); err == nil {
		fmt.Fprintf(&b, "%d:%d", st.Size(), st.ModTime().UnixNano())
	} else {
		b.WriteString("-")
	}
	return b.String()
}
