// Package models provides native (no-binary-patch) support for plugging
// third-party LLM providers into the chadgpt/openagy recovered binary.
//
// Background: the upstream v4 binary talks to the Antigravity cloudaicompanion
// gRPC endpoint. When that's not reachable (sandbox, dev, air-gapped) we
// instead route through user-configured providers defined in
// `~/.openagy/providers.json`.
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
	//
	// On a tier row (see ReasoningEfforts) it is the tier itself: the
	// row's level is what the request carries.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// ReasoningEfforts are the reasoning tiers the model offers — the
	// levels it can be selected at. Empty means the model has no tiers
	// of its own and is listed as one row.
	ReasoningEfforts []string `json:"reasoning_efforts,omitempty"`
	// SupportsImages is the model's own word about pictures: nil means
	// "not stated", and the catalogue's family table decides then. A
	// stated value is the user's answer, and nothing overrides it.
	SupportsImages *bool `json:"supports_images,omitempty"`
	// SupportedMimeTypes are the media types the model accepts — the
	// fuller form of the same answer. The list wins over
	// SupportsImages.
	SupportedMimeTypes []string `json:"supported_mime_types,omitempty"`
	// Disabled takes the model out of the Web UI's picker.
	Disabled bool `json:"disabled,omitempty"`
	// QuotaInfo is how much of the model's quota is left, and when it
	// comes back.
	QuotaInfo *QuotaInfo `json:"quota_info,omitempty"`
	// SupportsThoughtCirculation says the model circulates its
	// thoughts: nil means "not stated".
	SupportsThoughtCirculation *bool `json:"supports_thought_circulation,omitempty"`
	// ModelURL is a link to the model's own page.
	ModelURL string `json:"model_url,omitempty"`
	// TagTitle and TagDescription are the picker's tag line under the
	// model's name.
	TagTitle       string `json:"tag_title,omitempty"`
	TagDescription string `json:"tag_description,omitempty"`
	// EffortSupported says the model exposes a reasoning level: nil
	// means "not stated".
	EffortSupported *bool `json:"effort_supported,omitempty"`
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

// QualifiedName returns the provider-scoped model name in the form
// "<provider_id>/<name>". If ProviderID is empty, it returns Name.
func (m *Model) QualifiedName() string {
	if m == nil {
		return ""
	}
	pid := strings.TrimSpace(m.ProviderID)
	name := strings.TrimSpace(m.Name)
	if pid == "" {
		return name
	}
	return pid + "/" + name
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
	mu              sync.RWMutex
	providers       map[string]ProviderConfig
	path            string // file this was loaded from, "" if not from a file
	cachedFlattened []Model
	cachedSorted    []Model
	cacheValid      bool
	gen             uint64
}

// Gen returns the generation count of the registry. Incremented on any mutation.
func (r *Registry) Gen() uint64 {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.gen
}

func (r *Registry) rebuildCacheLocked() {
	var flat []Model
	for _, p := range r.providers {
		flat = append(flat, p.Flatten()...)
	}
	sorted := make([]Model, len(flat))
	copy(sorted, flat)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	r.cachedFlattened = flat
	r.cachedSorted = sorted
	r.cacheValid = true
	r.gen++
}

func (r *Registry) ensureCache() {
	r.mu.RLock()
	if r.cacheValid {
		r.mu.RUnlock()
		return
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.cacheValid {
		r.rebuildCacheLocked()
	}
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
//
// A catalogue id that carries a tier ("minimax-m3.1-flash-xhigh") names
// the base model at that tier: the reference spells the tier into the id,
// so a configured model is listed the same way. The model that comes back
// then carries the tier as its reasoning effort — the tier rides the row
// into the request.
// AmbiguousModelError is returned when a bare model ID matches models across multiple providers.
type AmbiguousModelError struct {
	ModelID   string
	Providers []string
}

func (e *AmbiguousModelError) Error() string {
	return fmt.Sprintf("model %q found in multiple providers: %s. Please specify provider using 'provider/model' format",
		e.ModelID, strings.Join(e.Providers, ", "))
}

func (r *Registry) GetByName(name string) (*Model, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	provMap := make(map[string]bool, len(r.providers))
	for id := range r.providers {
		provMap[id] = true
	}
	targetProv, targetModel := ParseModelRef(name, provMap)

	// 1. If provider prefix is present, strictly match that provider.
	if targetProv != "" {
		for _, m := range r.providerModels() {
			if strings.EqualFold(m.ProviderID, targetProv) && m.Name == targetModel {
				cp := m
				return &cp, true
			}
		}
		base := BaseModelID(targetModel)
		if base != targetModel {
			tier := TierFromID(targetModel)
			for _, m := range r.providerModels() {
				if strings.EqualFold(m.ProviderID, targetProv) && m.Name == base {
					if modelHasTier(m, tier) {
						cp := m
						cp.Name = name
						cp.ReasoningEffort = tier
						return &cp, true
					}
				}
			}
		}
		return nil, false
	}

	// 2. Bare name exact match
	for _, m := range r.providerModels() {
		if m.Name == name {
			cp := m
			return &cp, true
		}
	}
	base := BaseModelID(name)
	if base == name {
		return nil, false
	}
	tier := TierFromID(name)
	for _, m := range r.providerModels() {
		if m.Name != base {
			continue
		}
		if !modelHasTier(m, tier) {
			continue
		}
		cp := m
		cp.Name = name
		cp.ReasoningEffort = tier
		return &cp, true
	}
	return nil, false
}

// ResolveModel looks up a model by reference ("<id>" or "<provider>/<id>").
// If a bare model id matches multiple providers, AmbiguousModelError is returned
// instructing the caller to qualify with provider/model.
func (r *Registry) ResolveModel(name string) (*Model, error) {
	if r == nil {
		return nil, fmt.Errorf("models: nil registry")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("models: model name is required")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	provMap := make(map[string]bool, len(r.providers))
	for id := range r.providers {
		provMap[id] = true
	}
	targetProv, targetModel := ParseModelRef(name, provMap)

	if targetProv != "" {
		m, ok := r.getByNameLocked(name, targetProv, targetModel)
		if !ok {
			return nil, fmt.Errorf("model %q not found under provider %q", targetModel, targetProv)
		}
		return m, nil
	}

	// Bare model: check across all models for ambiguity
	var matches []Model
	seenProviders := make(map[string]bool)
	for _, m := range r.providerModels() {
		if m.Name == name {
			if !seenProviders[m.ProviderID] {
				seenProviders[m.ProviderID] = true
				matches = append(matches, m)
			}
		}
	}
	if len(matches) == 0 {
		base := BaseModelID(name)
		if base != name {
			tier := TierFromID(name)
			for _, m := range r.providerModels() {
				if m.Name == base && modelHasTier(m, tier) {
					if !seenProviders[m.ProviderID] {
						seenProviders[m.ProviderID] = true
						cp := m
						cp.Name = name
						cp.ReasoningEffort = tier
						matches = append(matches, cp)
					}
				}
			}
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("model %q not found", name)
	case 1:
		cp := matches[0]
		return &cp, nil
	default:
		var pnames []string
		for _, m := range matches {
			pnames = append(pnames, m.ProviderID)
		}
		sort.Strings(pnames)
		return nil, &AmbiguousModelError{
			ModelID:   name,
			Providers: pnames,
		}
	}
}

func (r *Registry) getByNameLocked(fullName, targetProv, targetModel string) (*Model, bool) {
	for _, m := range r.providerModels() {
		if strings.EqualFold(m.ProviderID, targetProv) && m.Name == targetModel {
			cp := m
			return &cp, true
		}
	}
	base := BaseModelID(targetModel)
	if base != targetModel {
		tier := TierFromID(targetModel)
		for _, m := range r.providerModels() {
			if strings.EqualFold(m.ProviderID, targetProv) && m.Name == base {
				if modelHasTier(m, tier) {
					cp := m
					cp.Name = fullName
					cp.ReasoningEffort = tier
					return &cp, true
				}
			}
		}
	}
	return nil, false
}

// modelHasTier reports whether the model offers the tier.
func modelHasTier(m Model, tier string) bool {
	for _, t := range NormalizeEfforts(m.ReasoningEfforts) {
		if t == tier {
			return true
		}
	}
	return false
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
	r.ensureCache()
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Model, len(r.cachedSorted))
	copy(out, r.cachedSorted)
	return out
}

// providerModels flattens the provider section. The caller holds the lock.
func (r *Registry) providerModels() []Model {
	if !r.cacheValid {
		r.rebuildCacheLocked()
	}
	return r.cachedFlattened
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
	r.rebuildCacheLocked()
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
	r.rebuildCacheLocked()
	return true
}

// AddProviderModel lists a model under a provider, replacing an entry with
// the same id. The provider must already exist.
func (r *Registry) AddProviderModel(providerID string, m ProviderModel) error {
	id := m.EffectiveID()
	if id == "" {
		return errors.New("models: model id is required")
	}
	m.ID = id
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.providers[providerID]
	if !ok {
		return fmt.Errorf("models: unknown provider %q", providerID)
	}
	for i := range p.Models {
		if p.Models[i].EffectiveID() == id {
			p.Models[i] = m
			r.providers[providerID] = p
			r.rebuildCacheLocked()
			return nil
		}
	}
	p.Models = append(p.Models, m)
	r.providers[providerID] = p
	r.rebuildCacheLocked()
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
	needle := strings.TrimSpace(modelID)
	for i := range p.Models {
		if p.Models[i].EffectiveID() == needle {
			p.Models = append(p.Models[:i], p.Models[i+1:]...)
			r.providers[providerID] = p
			r.rebuildCacheLocked()
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
