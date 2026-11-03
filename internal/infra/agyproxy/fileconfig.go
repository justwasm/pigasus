package agyproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/configdir"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/features/models"
)

// Environment variables understood by the proxy. Each one overrides the
// persisted value in the configuration document's "wrapper" section.
const (
	EnvAddr                 = "OPENAGY_WRAPPER_ADDR"
	EnvCaptureDir           = "OPENAGY_CAPTURE_DIR"
	EnvCaptureMax           = "OPENAGY_CAPTURE_MAX_BYTES"
	EnvUpstream             = "OPENAGY_UPSTREAM"
	EnvRealBin              = "AGY_REAL_BIN"
	EnvBalance              = "OPENAGY_ACCOUNT_BALANCE"
	EnvAccountPool          = "OPENAGY_ACCOUNT_POOL"
	EnvAggregate            = "OPENAGY_PROXY_AGGREGATE"
	EnvAccountPoolAggregate = "OPENAGY_ACCOUNT_POOL_AGGREGATE"
)

// fileConfig is the persisted proxy state. Pointers distinguish "absent"
// from "explicitly false" for booleans.
//
// It is called fileConfig because the proxy's own Config (server.go) is the
// server's runtime configuration.
//
// The state is carried in the configuration document's "wrapper" section,
// next to the endpoint overrides and the account-routing policy. There is
// one configuration file now, not two.
type fileConfig struct {
	// Capture enables request/response capture. Default true: this is a
	// reverse-engineering proxy and a capture you forgot to switch on is
	// worse than a directory you have to clean up.
	Capture    *bool  `json:"capture,omitempty"`
	CaptureDir string `json:"capture_dir,omitempty"`
	// CaptureMaxBytes is the ceiling the capture directory is held to.
	// Absent means the default; 0 means no ceiling.
	CaptureMaxBytes *int64 `json:"capture_max_bytes,omitempty"`
	Upstream        string `json:"upstream,omitempty"`
	Addr            string `json:"addr,omitempty"`
	Balance         *bool  `json:"balance,omitempty"`
	Aggregate       *bool  `json:"aggregate,omitempty"`
}

// defaultCaptureDir is where captures land: <the configuration home>/captures.
func defaultCaptureDir() string {
	return configdir.Path("captures")
}

// configPath returns the configuration document the proxy's state lives
// in: $OPENAGY_CONFIG, or <the configuration home>/config.json.
func configPath() string {
	return endpoints.Path()
}

// loadConfig reads the persisted config. A missing or unparseable section is
// not an error: defaults win, because a broken config must never stop agy
// from running.
func loadConfig() fileConfig {
	var c fileConfig
	raw := endpoints.CurrentWrapper()
	if len(raw) == 0 {
		return c
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		fmt.Fprintf(os.Stderr, "agy proxy: ignoring the unreadable wrapper section: %v\n", err)
		return fileConfig{}
	}
	return c
}

// saveConfig writes the proxy's state into the configuration document's
// "wrapper" section.
//
// The rest of the document — the endpoint overrides, the account-routing
// policy — is left as it was, and the write is atomic and 0600: the
// document can carry a capture directory path, and it lives next to
// nothing that should be world-readable.
func saveConfig(c fileConfig) error {
	buf, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return endpoints.SaveWrapper(buf)
}

// settings is the resolved (env > file > default) view used by the launcher.
type settings struct {
	Addr            string
	Upstream        string
	CaptureOn       bool
	CaptureDir      string
	CaptureMaxBytes int64
	Balance         bool
	Aggregate       bool
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
	s.CaptureMaxBytes = resolveCaptureMax(c)
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

// resolveCaptureMax resolves the capture ceiling: env > file > default.
//
// The environment wins because that is how a one-off run overrides a
// persisted setting. Zero — from either source — disables the ceiling and
// restores the old unbounded behaviour.
func resolveCaptureMax(c fileConfig) int64 {
	if v, ok := os.LookupEnv(EnvCaptureMax); ok && strings.TrimSpace(v) != "" {
		n, err := parseBytes(v)
		if err == nil {
			return n
		}
		fmt.Fprintf(os.Stderr, "agy proxy: ignoring %s=%q: %v\n", EnvCaptureMax, v, err)
	}
	if c.CaptureMaxBytes != nil {
		return *c.CaptureMaxBytes
	}
	return DefaultCaptureMaxBytes
}

// parseBytes reads a byte count.
//
// It accepts a plain number of bytes ("1048576"), the decimal suffixes
// (KB, MB, GB, TB — powers of 1000), the binary suffixes (KiB, MiB, GiB,
// TiB — powers of 1024) and the bare single letters (K, M, G, T — binary,
// the way `ls -h` means them). "0" is zero: no ceiling.
func parseBytes(s string) (int64, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return 0, errors.New("empty byte count")
	}
	lower := strings.ToLower(raw)
	mult := int64(1)
	for _, suffix := range byteSuffixOrder {
		if strings.HasSuffix(lower, suffix) {
			mult = byteSuffixes[suffix]
			lower = strings.TrimSpace(strings.TrimSuffix(lower, suffix))
			break
		}
	}
	if lower == "" {
		return 0, fmt.Errorf("byte count %q has no number", raw)
	}
	for _, r := range lower {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("byte count %q is not a number", raw)
		}
	}
	n, err := strconv.ParseInt(lower, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("byte count %q is out of range", raw)
	}
	if mult > 1 && n > math.MaxInt64/mult {
		return 0, fmt.Errorf("byte count %q is out of range", raw)
	}
	return n * mult, nil
}

// byteSuffixes are the multipliers parseBytes understands.
var byteSuffixes = map[string]int64{
	"kib": 1 << 10,
	"mib": 1 << 20,
	"gib": 1 << 30,
	"tib": 1 << 40,
	"kb":  1000,
	"mb":  1000 * 1000,
	"gb":  1000 * 1000 * 1000,
	"tb":  1000 * 1000 * 1000 * 1000,
	"k":   1 << 10,
	"m":   1 << 20,
	"g":   1 << 30,
	"t":   1 << 40,
	"b":   1,
}

// byteSuffixOrder is the order the suffixes are tried in. The binary
// spellings come first because "kib" also ends in "b" and "k".
var byteSuffixOrder = []string{"kib", "mib", "gib", "tib", "kb", "mb", "gb", "tb", "k", "m", "g", "t", "b"}

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
