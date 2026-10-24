// Package models provides native (no-binary-patch) support for plugging
// third-party LLM providers into the chadgpt/openagy recovered binary.
//
// Background: the upstream v4 binary talks to the Antigravity cloudaicompanion
// gRPC endpoint. When that's not reachable (sandbox, dev, air-gapped) we
// instead route through user-configured providers defined in
// `~/.config/openagy/providers.json`.
//
// The two important types are:
//
//   - Model    — one configured endpoint (provider + creds + external model id).
//   - Registry — the providers the user configured, with helpers for
//     load/lookup/mutate.
//
// # File layout — the provider-centric form is the only one
//
//	{
//	  "providers": {
//	    "gateway": {
//	      "type": "openai",
//	      "base_url": "https://gateway.example.com/v1",
//	      "api_key": "$OPENAGY_GATEWAY_KEY",
//	      "models": [
//	        {
//	          "id": "minimax-m3",
//	          "name": "MiniMax M3",
//	          "model": "MiniMax-M3",
//	          "context_window": 1048576
//	        }
//	      ]
//	    }
//	  }
//	}
//
// A provider holds the endpoint, the credential and the protocol; the models
// it serves are listed under it. The credential is written once, not once per
// model, and the protocol is decided once.
//
// The earlier *flat* form — a list of self-contained model entries, each with
// its own endpoint and credential — was written by the first versions of
// this file. It has been removed: there is one configuration shape now, and
// `internal/models` reads and writes only this one.
package models

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Model is one user-configured model entry. It is the shape the rest of the
// code base works with: one entry per model, carrying the endpoint and the
// credential it was resolved from. The configuration file itself is
// provider-centric (see ProviderConfig); a provider's models are flattened
// into this shape by ProviderConfig.Flatten.
type Model struct {
	// Name is the internal handle used in `--model <name>` and the registry
	// lookups. Must be unique within a Registry.
	Name string `json:"name"`
	// DisplayName is what the CLI list prints and what the picker shows.
	// Optional; defaults to Name.
	DisplayName string `json:"display_name,omitempty"`
	// Provider is the *wire protocol* the endpoint speaks: openai,
	// anthropic, ollama, google or openrouter. It is not the provider's
	// handle in the configuration file — that is ProviderID.
	Provider string `json:"provider"`
	// ProviderID is the key of the provider entry this model was listed
	// under. It is empty for a model that was built in memory.
	ProviderID string `json:"provider_id,omitempty"`
	// Type is the wire protocol, spelled out. It duplicates Provider for
	// the entries the provider-centric section produced.
	Type string `json:"type,omitempty"`
	// ExtraHeaders are added to every request to this model's endpoint.
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
	// APIKey is the bearer / x-api-key credential. It may be written as
	// "$NAME" or "${NAME}" to read it from the environment.
	APIKey string `json:"api_key"`
	// APIURL is the full chat completions / messages URL. For openai-style
	// providers this is /v1/chat/completions; for anthropic-style it's
	// /v1/messages; we don't auto-append paths so users can point at proxies.
	APIURL string `json:"api_url"`
	// ExternalModelName is the model id to send upstream (e.g. "MiniMax-M3"
	// for one vendor, "gpt-4o-mini" for another). Kept separate from `Name`
	// so internal handles don't have to mirror the upstream namespace.
	ExternalModelName string `json:"external_model_name"`
	// ContextWindow is the published context size, used by the TUI to know
	// when to truncate. 0 means "unknown" — the provider is asked to figure
	// it out from the response.
	ContextWindow int `json:"context_window,omitempty"`
	// DefaultMaxTokens is the model's output cap in tokens. It is the
	// reserve the context budget holds back for the next step's answer.
	// 0 means "not known".
	DefaultMaxTokens int `json:"default_max_tokens,omitempty"`
	// ReasoningEffort is the OpenAI o-series hint: "low"/"medium"/"high"/
	// "default". Empty means "don't send the hint".
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// TimeoutSeconds is the per-request timeout. 0 defaults to 60s.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// MaxRetries is the number of retries on 5xx / network. 0 means 1 try.
	MaxRetries int `json:"max_retries,omitempty"`
}

// Validate checks the fields needed for a usable Model. It's called by
// NewProvider so an incomplete entry fails early instead of mid-stream.
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

// Registry is the in-memory collection of the configured providers. It's
// safe for concurrent reads; mutations hold an exclusive lock so we don't
// lose updates from a parallel CLI.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]ProviderConfig
	path      string // file this was loaded from, "" if not from a file
}

// LoadRegistry reads the provider section of the configuration file at path
// and returns the parsed Registry. A missing file is an empty registry (not
// an error), so first-run users don't see a scary "no such file" message.
// Malformed JSON still fails, because silently dropping entries would be
// worse than the error.
func LoadRegistry(path string) (*Registry, error) {
	providers, err := LoadProviders(path)
	if err != nil {
		return nil, err
	}
	return &Registry{providers: providers, path: path}, nil
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
	for _, m := range r.providerModels() {
		if m.Name == name {
			cp := m
			return &cp, true
		}
	}
	return nil, false
}

// List returns a sorted copy of the configured models: every model listed
// under a provider, flattened.
//
// Sorted so the CLI's `list` output is stable across runs (Go map iteration
// is randomized, but we use a slice — still sorting for determinism).
func (r *Registry) List() []Model {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := r.providerModels()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// providerModels flattens the provider section. The caller holds the lock.
func (r *Registry) providerModels() []Model {
	var out []Model
	for _, p := range r.providers {
		out = append(out, p.Flatten()...)
	}
	return out
}

// Providers returns a copy of the provider section, keyed by provider id.
func (r *Registry) Providers() map[string]ProviderConfig {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]ProviderConfig, len(r.providers))
	for id, p := range r.providers {
		out[id] = p
	}
	return out
}

// SetProvider stores a provider entry, replacing one with the same id.
func (r *Registry) SetProvider(id string, p ProviderConfig) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("models: provider id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = map[string]ProviderConfig{}
	}
	if strings.TrimSpace(p.ID) == "" {
		p.ID = id
	}
	r.providers[id] = p
	return nil
}

// RemoveProvider deletes a provider entry and every model listed under it.
// Returns whether the provider existed.
func (r *Registry) RemoveProvider(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[id]; !ok {
		return false
	}
	delete(r.providers, id)
	return true
}

// AddProviderModel lists a model under a provider, replacing an entry with
// the same id. The provider must already exist.
func (r *Registry) AddProviderModel(providerID string, m ProviderModel) error {
	m.ID = strings.TrimSpace(m.ID)
	if m.ID == "" {
		return errors.New("models: model id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.providers[providerID]
	if !ok {
		return fmt.Errorf("models: unknown provider %q", providerID)
	}
	for i := range p.Models {
		if p.Models[i].ID == m.ID {
			p.Models[i] = m
			r.providers[providerID] = p
			return nil
		}
	}
	p.Models = append(p.Models, m)
	r.providers[providerID] = p
	return nil
}

// RemoveProviderModel deletes a model listed under a provider. Returns
// whether it existed.
func (r *Registry) RemoveProviderModel(providerID, modelID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.providers[providerID]
	if !ok {
		return false
	}
	for i := range p.Models {
		if p.Models[i].ID == modelID {
			p.Models = append(p.Models[:i], p.Models[i+1:]...)
			r.providers[providerID] = p
			return true
		}
	}
	return false
}

// Len returns the number of configured models: every model listed under a
// provider.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, p := range r.providers {
		n += len(p.Models)
	}
	return n
}
