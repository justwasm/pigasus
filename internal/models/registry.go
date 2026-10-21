// Package models provides native (no-binary-patch) support for plugging
// third-party LLM providers into the chadgpt/openagy recovered binary.
//
// Background: the upstream v4 binary talks to the Antigravity cloudaicompanion
// gRPC endpoint. When that's not reachable (sandbox, dev, air-gapped) we
// instead route through user-configured providers defined in
// `~/.config/openagy/models.json` — the same JSON schema kelvinzer0's Node.js
// wrapper uses, but loaded natively in Go (no runtime patching).
//
// The two important types are:
//
//   - Model    — one configured endpoint (provider + creds + external model id).
//   - Registry — a slice of Model with helpers for load/save/lookup/mutate.
//
// File layout (matches kelvinzer0's `~/.free-antigravity/models.json`):
//
//	{
//	  "models": [
//	    {
//	      "name": "agw",
//	      "display_name": "Avalanche Gateway (dev sentinel)",
//	      "provider": "agw",
//	      "api_key": "-",
//	      "api_url": "https://agw.k0s.io/v1/chat/completions",
//	      "external_model_name": "MiniMax-M3",
//	      "context_window": 128000,
//	      "reasoning_effort": "default",
//	      "timeout_seconds": 60,
//	      "max_retries": 2
//	    }
//	  ]
//	}
package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Model is one user-configured model entry. Field names are JSON-aligned with
// the kelvinzer0 schema so existing config files drop in unchanged. All
// optional fields fall back to sane defaults when zero.
type Model struct {
	// Name is the internal handle used in `--model <name>` and the registry
	// lookups. Must be unique within a Registry.
	Name string `json:"name"`
	// DisplayName is what CLI `list` prints. Optional; defaults to Name.
	DisplayName string `json:"display_name,omitempty"`
	// Provider is one of: openai, anthropic, ollama, google, openrouter,
	// agw, custom. Case-insensitive on parse, lowercased on save.
	Provider string `json:"provider"`
	// APIKey is the bearer / x-api-key credential. For agw.k0s.io the dev
	// sentinel is literally "-".
	APIKey string `json:"api_key"`
	// APIURL is the full chat completions / messages URL. For openai-style
	// providers this is /v1/chat/completions; for anthropic-style it's
	// /v1/messages; we don't auto-append paths so users can point at proxies.
	APIURL string `json:"api_url"`
	// ExternalModelName is the model id to send upstream (e.g. "MiniMax-M3"
	// for agw, "gpt-4o-mini" for openai). Kept separate from `Name` so
	// internal handles don't have to mirror the upstream namespace.
	ExternalModelName string `json:"external_model_name"`
	// ContextWindow is the published context size, used by the TUI to know
	// when to truncate. 0 means "unknown" — the provider is asked to figure
	// it out from the response.
	ContextWindow int `json:"context_window,omitempty"`
	// ReasoningEffort is the OpenAI o-series hint: "low"/"medium"/"high"/
	// "default". Empty means "don't send the hint".
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// TimeoutSeconds is the per-request timeout. 0 defaults to 60s.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// MaxRetries is the number of retries on 5xx / network. 0 means 1 try.
	MaxRetries int `json:"max_retries,omitempty"`
}

// Validate checks the fields needed for a usable Model. It's called by Add
// and by NewProvider so an incomplete entry fails early instead of mid-stream.
func (m *Model) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("model: name is required")
	}
	if strings.TrimSpace(m.Provider) == "" {
		return fmt.Errorf("model %q: provider is required", m.Name)
	}
	if strings.TrimSpace(m.APIURL) == "" {
		return fmt.Errorf("model %q: api_url is required", m.Name)
	}
	if strings.TrimSpace(m.ExternalModelName) == "" {
		return fmt.Errorf("model %q: external_model_name is required", m.Name)
	}
	return nil
}

// Timeout returns the per-request timeout, defaulting to 60s.
func (m *Model) Timeout() int {
	if m.TimeoutSeconds <= 0 {
		return 60
	}
	return m.TimeoutSeconds
}

// Retries returns the max retry count, defaulting to 1 attempt total.
func (m *Model) Retries() int {
	if m.MaxRetries <= 0 {
		return 1
	}
	return m.MaxRetries
}

// Registry is the in-memory collection of Models with a file-backed
// persistence layer. It's safe for concurrent reads; mutations (Add/Remove)
// hold an exclusive lock so we don't lose updates from a parallel CLI.
type Registry struct {
	mu     sync.RWMutex
	models []Model
	path   string // file this was loaded from, "" if not from a file
}

// LoadFromFile reads a JSON file at path and returns the parsed Registry.
// Missing file is treated as an empty registry (not an error), so first-run
// users don't see a scary "no such file" message. Malformed JSON still fails
// because silently dropping entries would be worse than the error.
func LoadFromFile(path string) (*Registry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Registry{path: path}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc struct {
		Models []Model `json:"models"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	r := &Registry{models: doc.Models, path: path}
	// Lowercase the provider field on load so "OpenAI" / "openai" / "OPENAI"
	// all behave the same downstream. Same for trim of whitespace.
	for i := range r.models {
		r.models[i].Provider = strings.ToLower(strings.TrimSpace(r.models[i].Provider))
		r.models[i].Name = strings.TrimSpace(r.models[i].Name)
	}
	return r, nil
}

// SaveToFile writes the registry atomically (write to a sibling temp file,
// rename into place) so a crash mid-write doesn't truncate the existing
// config. Directory is created if missing.
func SaveToFile(path string, r *Registry) error {
	if r == nil {
		return errors.New("registry is nil")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	// Make a copy so we can lowercase provider without mutating the caller's
	// memory (the in-memory model can still be uppercase if they want).
	cp := r.clone()
	for i := range cp.models {
		cp.models[i].Provider = strings.ToLower(strings.TrimSpace(cp.models[i].Provider))
	}
	doc := struct {
		Models []Model `json:"models"`
	}{Models: cp.models}
	buf, err := json.MarshalIndent(&doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Path returns the file this registry was loaded from ("" if constructed in
// memory). Useful for the CLI's "save back to where I loaded from" path.
func (r *Registry) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

// GetByName returns a pointer to the matching Model (not a copy — the caller
// must not mutate it) and whether it was found. The second return is the
// canonical bool-ok pattern, not an error, because "not configured" is
// expected when --model points at something the user hasn't set up yet.
func (r *Registry) GetByName(name string) (*Model, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := range r.models {
		if r.models[i].Name == name {
			// Return a copy so caller mutation doesn't race with concurrent
			// list iteration. The pointer-to-slice-element trick in Go is
			// famously easy to get wrong.
			m := r.models[i]
			return &m, true
		}
	}
	return nil, false
}

// Add inserts (or replaces, on name collision) a Model. Returns an error if
// the model is invalid.
func (r *Registry) Add(m Model) error {
	if err := m.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.models {
		if r.models[i].Name == m.Name {
			r.models[i] = m
			return nil
		}
	}
	r.models = append(r.models, m)
	return nil
}

// Remove deletes a Model by name. Returns whether something was actually
// removed (false ⇒ caller already deleted it / typo in name).
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.models {
		if r.models[i].Name == name {
			r.models = append(r.models[:i], r.models[i+1:]...)
			return true
		}
	}
	return false
}

// List returns a sorted copy of the configured models. Sorted so the CLI's
// `list` output is stable across runs (Go map iteration is randomized, but
// we use a slice — still sorting for determinism).
func (r *Registry) List() []Model {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Model, len(r.models))
	copy(out, r.models)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Len returns the number of configured models. O(1) — kept in sync under
// the same mutex as the slice.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.models)
}

// clone is the internal helper used by SaveToFile. Not exported because
// callers should not be hand-cloning the registry.
func (r *Registry) clone() *Registry {
	if r == nil {
		return &Registry{}
	}
	cp := &Registry{path: r.path, models: make([]Model, len(r.models))}
	copy(cp.models, r.models)
	return cp
}

// DefaultConfigPath returns the conventional config path:
//   $XDG_CONFIG_HOME/openagy/models.json, or
//   $HOME/.config/openagy/models.json if HOME is set.
//
// We intentionally don't return an error here — "no home dir" is a legitimate
// state in CI / containers, and the caller (CLI) can fall back to a
// per-process temp path.
func DefaultConfigPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "openagy", "models.json")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "openagy", "models.json")
	}
	return ""
}
