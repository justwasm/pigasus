package agyproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/atomicfile"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/models"
)

// Environment variables understood by the proxy. Each one overrides the
// persisted value in ~/.openagy/wrapper.json.
const (
	EnvWrapperConfig = "OPENAGY_WRAPPER_CONFIG"
	EnvAddr          = "OPENAGY_WRAPPER_ADDR"
	EnvCaptureDir    = "OPENAGY_CAPTURE_DIR"
	EnvUpstream      = "OPENAGY_UPSTREAM"
	EnvRealBin       = "AGY_REAL_BIN"
	EnvBalance       = "OPENAGY_ACCOUNT_BALANCE"
	EnvAccountPool   = "OPENAGY_ACCOUNT_POOL"
	EnvAggregate     = "OPENAGY_PROXY_AGGREGATE"
	EnvAccountPoolAggregate = "OPENAGY_ACCOUNT_POOL_AGGREGATE"
)

// fileConfig is the persisted proxy state. Pointers distinguish "absent"
// from "explicitly false" for booleans.
//
// It is called fileConfig because the proxy's own Config (server.go) is the
// server's runtime configuration.
type fileConfig struct {
	// Capture enables request/response capture. Default true: this is a
	// reverse-engineering proxy and a capture you forgot to switch on is
	// worse than a directory you have to clean up.
	Capture    *bool  `json:"capture,omitempty"`
	CaptureDir string `json:"capture_dir,omitempty"`
	Upstream   string `json:"upstream,omitempty"`
	Addr       string `json:"addr,omitempty"`
	Balance    *bool  `json:"balance,omitempty"`
	Aggregate  *bool  `json:"aggregate,omitempty"`
}

// defaultCaptureDir is where captures land: ~/.openagy/captures.
func defaultCaptureDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openagy", "captures")
}

// configPath returns $OPENAGY_WRAPPER_CONFIG or ~/.openagy/wrapper.json.
func configPath() string {
	if p := strings.TrimSpace(os.Getenv(EnvWrapperConfig)); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openagy", "wrapper.json")
}

// loadConfig reads the persisted config. A missing or unparseable file is not
// an error: defaults win, because a broken config must never stop agy from
// running.
func loadConfig() fileConfig {
	var c fileConfig
	p := configPath()
	if p == "" {
		return c
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return c
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		fmt.Fprintf(os.Stderr, "agy proxy: ignoring unreadable %s: %v\n", p, err)
		return fileConfig{}
	}
	return c
}

// saveConfig writes the config atomically with 0600 (it can carry a capture
// directory path, and it lives next to nothing that should be world-readable).
func saveConfig(c fileConfig) error {
	p := configPath()
	if p == "" {
		return errors.New("cannot locate the proxy config (no HOME)")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return atomicfile.WriteFile(p, buf, 0o600)
}

// settings is the resolved (env > file > default) view used by the launcher.
type settings struct {
	Addr       string
	Upstream   string
	CaptureOn  bool
	CaptureDir string
	Balance    bool
	Aggregate  bool
}

func resolveSettings(c fileConfig) settings {
	s := settings{
		Addr:       firstNonEmpty(os.Getenv(EnvAddr), c.Addr, "127.0.0.1:0"),
		Upstream:   firstNonEmpty(os.Getenv(EnvUpstream), c.Upstream, ""),
		CaptureDir: firstNonEmpty(os.Getenv(EnvCaptureDir), c.CaptureDir, defaultCaptureDir()),
		CaptureOn:  true,
		Balance:    resolveBool([]string{EnvBalance, EnvAccountPool}, c.Balance, false),
		Aggregate:  resolveBool([]string{EnvAggregate, EnvAccountPoolAggregate}, c.Aggregate, false),
	}
	if c.Capture != nil {
		s.CaptureOn = *c.Capture
	}
	if !s.CaptureOn {
		s.CaptureDir = ""
	}
	if p := endpoints.CurrentAccountPool(); p != nil {
		if p.ProxyBalance {
			s.Balance = true
		}
		if p.Aggregate || p.Strategy == accountpool.StrategyAggregate {
			s.Aggregate = true
			s.Balance = true
		}
	}
	if s.Aggregate {
		s.Balance = true
	}
	return s
}

func resolveBool(envVars []string, fileVal *bool, def bool) bool {
	for _, env := range envVars {
		if val, ok := os.LookupEnv(env); ok && strings.TrimSpace(val) != "" {
			if isTruthy(val) {
				return true
			}
			if isFalsy(val) {
				return false
			}
		}
	}
	if fileVal != nil {
		return *fileVal
	}
	return def
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true
	default:
		return false
	}
}

func isFalsy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off", "disable", "disabled":
		return true
	default:
		return false
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// loadRegistry reads the provider registry.
//
// Nothing is created here. A proxy that invents models would advertise
// endpoints the user never configured, and every first run would have to be
// undone by hand. The configuration is the user's; the proxy only reads it.
func loadRegistry() (*models.Registry, error) {
	reg, err := models.LoadAll()
	if err != nil {
		return nil, err
	}
	return reg, nil
}
