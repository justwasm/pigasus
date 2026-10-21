package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/models"
)

// Environment variables understood by the wrapper. Each one overrides the
// persisted value in ~/.openagy/wrapper.json.
const (
	EnvWrapperConfig = "OPENAGY_WRAPPER_CONFIG"
	EnvAddr          = "OPENAGY_WRAPPER_ADDR"
	EnvCaptureDir    = "OPENAGY_CAPTURE_DIR"
	EnvUpstream      = "OPENAGY_UPSTREAM"
	EnvRealBin       = "AGY_REAL_BIN"
)

// config is the persisted wrapper state. Pointers distinguish "absent" from
// "explicitly false" for booleans.
type config struct {
	// Capture enables request/response capture. Default true: this is a
	// reverse-engineering wrapper and a capture you forgot to switch on is
	// worse than a directory you have to clean up.
	Capture    *bool  `json:"capture,omitempty"`
	CaptureDir string `json:"capture_dir,omitempty"`
	Upstream   string `json:"upstream,omitempty"`
	Addr       string `json:"addr,omitempty"`
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
func loadConfig() config {
	var c config
	p := configPath()
	if p == "" {
		return c
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return c
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		fmt.Fprintf(os.Stderr, "agy wrapper: ignoring unreadable %s: %v\n", p, err)
		return config{}
	}
	return c
}

// saveConfig writes the config atomically with 0600 (it can carry a capture
// directory path, and it lives next to nothing that should be world-readable).
func saveConfig(c config) error {
	p := configPath()
	if p == "" {
		return errors.New("cannot locate wrapper config (no HOME)")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// settings is the resolved (env > file > default) view used by the launcher.
type settings struct {
	Addr       string
	Upstream   string
	CaptureOn  bool
	CaptureDir string
}

func resolveSettings(c config) settings {
	s := settings{
		Addr:       firstNonEmpty(os.Getenv(EnvAddr), c.Addr, "127.0.0.1:0"),
		Upstream:   firstNonEmpty(os.Getenv(EnvUpstream), c.Upstream, ""),
		CaptureDir: firstNonEmpty(os.Getenv(EnvCaptureDir), c.CaptureDir, defaultCaptureDir()),
		CaptureOn:  true,
	}
	if c.Capture != nil {
		s.CaptureOn = *c.Capture
	}
	if !s.CaptureOn {
		s.CaptureDir = ""
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// defaultModel is written on first run so MiniMax M3 works with zero setup.
func defaultModel() models.Model {
	return models.Model{
		Name:              "minimax-m3",
		DisplayName:       "MiniMax M3",
		Provider:          "agw",
		APIKey:            "-",
		APIURL:            "https://agw.k0s.io/v1/chat/completions",
		ExternalModelName: "MiniMax-M3",
		ContextWindow:     1000000,
	}
}

// ensureRegistry loads ~/.config/openagy/models.json, creating it with the
// single default entry when it does not exist. An existing file is never
// rewritten.
func ensureRegistry(path string) (*models.Registry, error) {
	if strings.TrimSpace(path) == "" {
		return &models.Registry{}, nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		reg := &models.Registry{}
		if err := reg.Add(defaultModel()); err != nil {
			return nil, err
		}
		if err := models.SaveToFile(path, reg); err != nil {
			return nil, fmt.Errorf("create %s: %w", path, err)
		}
		fmt.Fprintf(os.Stderr, "agy wrapper: created %s with the default MiniMax M3 entry\n", path)
		return models.LoadFromFile(path)
	}
	return models.LoadFromFile(path)
}
