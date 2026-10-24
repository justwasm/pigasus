package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// # The provider-centric configuration
//
// The registry is the centralised shape: one provider entry holds the
// endpoint, the credential and the protocol, and the models it serves are
// listed under it. That is the shape a user actually wants to maintain —
// the credential is written once, not once per model — and it is the only
// shape the code base reads.
//
//	{
//	  "providers": {
//	    "gateway": {
//	      "name": "Gateway",
//	      "type": "openai",
//	      "base_url": "https://gateway.example.com/v1",
//	      "api_key": "$OPENAGY_GATEWAY_KEY",
//	      "models": [
//	        {
//	          "id": "minimax-m3",
//	          "name": "MiniMax M3",
//	          "model": "MiniMax-M3",
//	          "context_window": 1048576,
//	          "default_max_tokens": 8192
//	        }
//	      ]
//	    }
//	  }
//	}
//
// # The two ids
//
// A model entry carries two names. The catalogue id ("id") is what the
// user types after --model and what the model list prints. The upstream id
// ("model") is what the provider is asked for on the wire. They differ
// whenever the provider spells its models differently from the CLI —
// "minimax-m3" here, "MiniMax-M3" upstream — and they are the same
// whenever the provider's own spelling is already usable. An empty
// "model" means "the catalogue id is the upstream id".

// ProviderConfig is one centrally configured upstream: an endpoint, a
// credential, a protocol, and the models it serves.
type ProviderConfig struct {
	// ID is the provider's key in the "providers" map. It is the handle the
	// CLI and the logs use ("gateway"), not a wire value. Optional in the
	// file: the map key wins.
	ID string `json:"id,omitempty"`
	// Name is the human-readable label. Optional; the ID is used when it is
	// empty.
	Name string `json:"name,omitempty"`
	// Type is the wire protocol: "openai" (Chat Completions) or "anthropic"
	// (Messages). Empty means "openai", which is the shape every
	// OpenAI-compatible gateway speaks.
	Type string `json:"type,omitempty"`
	// BaseURL is the API base. The endpoint is derived from it by the
	// protocol: "openai" appends "/chat/completions", "anthropic" appends
	// "/v1/messages". The base therefore carries the version segment
	// ("https://host/v1") for OpenAI and not for Anthropic
	// ("https://host/anthropic").
	BaseURL string `json:"base_url,omitempty"`
	// APIURL is the full endpoint, used verbatim when it is set. It exists
	// for proxies that do not follow the convention above.
	APIURL string `json:"api_url,omitempty"`
	// APIKey is the credential. A value of the form "$NAME" or "${NAME}" is
	// read from the environment; anything else is a literal.
	APIKey string `json:"api_key,omitempty"`
	// ExtraHeaders are added to every request to this provider.
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
	// TimeoutSeconds bounds one request. 0 means the transport's default.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// MaxRetries is the retry count for 5xx and network failures.
	MaxRetries int `json:"max_retries,omitempty"`
	// Models are the models this provider serves.
	Models []ProviderModel `json:"models,omitempty"`
}

// ProviderModel is one model a provider serves.
type ProviderModel struct {
	// ID is the catalogue id: what --model takes and what the model list
	// prints.
	ID string `json:"id"`
	// Name is the display label. Optional; the ID is used when it is empty.
	Name string `json:"name,omitempty"`
	// Model is the upstream id sent on the wire. Optional; the catalogue id
	// is used when it is empty.
	Model string `json:"model,omitempty"`
	// ContextWindow is the model's input context limit in tokens. 0 means
	// "not known".
	ContextWindow int `json:"context_window,omitempty"`
	// DefaultMaxTokens is the model's output cap in tokens. 0 means "not
	// known".
	DefaultMaxTokens int `json:"default_max_tokens,omitempty"`
	// ReasoningEffort is the sampling hint for models that take one
	// ("low", "medium", "high", "default"). Empty means "do not send it".
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// Provider types. The wire protocol of a provider decides which client
// speaks to it.
const (
	// ProviderTypeOpenAI is the Chat Completions dialect.
	ProviderTypeOpenAI = "openai"
	// ProviderTypeAnthropic is the Anthropic Messages dialect.
	ProviderTypeAnthropic = "anthropic"
)

// WireType returns the provider's protocol, lowercased, defaulting to the
// OpenAI dialect — the shape every OpenAI-compatible gateway speaks.
func (p ProviderConfig) WireType() string {
	t := strings.ToLower(strings.TrimSpace(p.Type))
	if t == "" {
		return ProviderTypeOpenAI
	}
	return t
}

// Endpoint returns the URL requests are sent to: the explicit api_url when
// it is set, otherwise the base_url with the protocol's path appended.
func (p ProviderConfig) Endpoint() string {
	if u := strings.TrimSpace(p.APIURL); u != "" {
		return u
	}
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		return ""
	}
	switch p.WireType() {
	case ProviderTypeAnthropic:
		return base + "/v1/messages"
	default:
		return base + "/chat/completions"
	}
}

// ResolvedAPIKey returns the credential, expanding an environment reference.
// A "$NAME" or "${NAME}" value is read from the environment; a literal is
// returned unchanged.
func (p ProviderConfig) ResolvedAPIKey() string {
	return ExpandEnvRef(p.APIKey)
}

// ExpandEnvRef expands a "$NAME" or "${NAME}" reference to the environment
// value. Anything else — including an empty string and a literal that
// merely contains a dollar sign — is returned unchanged.
func ExpandEnvRef(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
		return os.Getenv(strings.TrimSuffix(strings.TrimPrefix(v, "${"), "}"))
	}
	if strings.HasPrefix(v, "$") && len(v) > 1 {
		return os.Getenv(strings.TrimPrefix(v, "$"))
	}
	return value
}

// Label returns the provider's human-readable name, falling back to its id.
func (p ProviderConfig) Label() string {
	if n := strings.TrimSpace(p.Name); n != "" {
		return n
	}
	return strings.TrimSpace(p.ID)
}

// Flatten turns the provider section into the flat model list the rest of
// the package works with. Every provider model becomes one Model entry:
// the catalogue id becomes the entry name, the upstream id becomes the
// external model name, and the provider's endpoint, credential and protocol
// are copied onto the entry.
//
// The entries are sorted by name so the registry's List is stable.
func (p ProviderConfig) Flatten() []Model {
	out := make([]Model, 0, len(p.Models))
	for _, m := range p.Models {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		wire := strings.TrimSpace(m.Model)
		if wire == "" {
			wire = id
		}
		label := strings.TrimSpace(m.Name)
		if label == "" {
			label = id
		}
		out = append(out, Model{
			Name:              id,
			DisplayName:       label,
			Provider:          p.WireType(),
			ProviderID:        strings.TrimSpace(p.ID),
			Type:              p.WireType(),
			APIKey:            p.ResolvedAPIKey(),
			APIURL:            p.Endpoint(),
			ExternalModelName: wire,
			ContextWindow:     m.ContextWindow,
			DefaultMaxTokens:  m.DefaultMaxTokens,
			ReasoningEffort:   strings.TrimSpace(m.ReasoningEffort),
			TimeoutSeconds:    p.TimeoutSeconds,
			MaxRetries:        p.MaxRetries,
			ExtraHeaders:      copyHeaders(p.ExtraHeaders),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func copyHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ProvidersPath returns the conventional path of the provider-centric
// configuration file:
//
//	$XDG_CONFIG_HOME/openagy/providers.json, or
//	$HOME/.config/openagy/providers.json if HOME is set.
//
// The env override is OPENAGY_PROVIDERS_CONFIG.
func ProvidersPath() string {
	if p := strings.TrimSpace(os.Getenv("OPENAGY_PROVIDERS_CONFIG")); p != "" {
		return p
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "openagy", "providers.json")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "openagy", "providers.json")
	}
	return ""
}

// LoadProviders reads the provider-centric configuration file. A missing
// file is an empty configuration, not an error, so a first run does not
// have to create it before the CLI works.
func LoadProviders(path string) (map[string]ProviderConfig, error) {
	if strings.TrimSpace(path) == "" {
		return map[string]ProviderConfig{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]ProviderConfig{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseProviders(raw)
}

// LoadAll loads the configuration into one registry.
//
// There is one configuration file — the provider-centric one — so "all" is
// just that file. A malformed file is reported; the caller decides whether
// to fall back to the last good registry.
func LoadAll() (*Registry, error) {
	path := ProvidersPath()
	providers, err := LoadProviders(path)
	if err != nil {
		return nil, err
	}
	return &Registry{providers: providers, path: path}, nil
}

// registryCache holds the last registry CachedRegistry built, with the
// stamp of the files it was built from.
type registryCache struct {
	stamp string
	reg   *Registry
}

var (
	cacheMu sync.Mutex
	cache   *registryCache
)

// CachedRegistry returns the merged registry, re-reading the configuration
// only when one of the files changed on disk.
//
// The catalogue, the context-window table and the transport all consult the
// user's configuration, and they consult it in a loop — one lookup per
// candidate model. Re-reading the file each time would be wasteful, so the
// registry is cached and invalidated by the files' size and modification
// time.
//
// A configuration that does not parse falls back to the last good registry,
// and to an empty one when there is none: a broken edit to the file must
// not take the CLI down.
func CachedRegistry() *Registry {
	paths := []string{ProvidersPath()}
	stamp := configStamp(paths)

	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cache != nil && cache.stamp == stamp {
		return cache.reg
	}
	reg, err := LoadAll()
	if err != nil {
		if cache != nil {
			return cache.reg
		}
		return &Registry{providers: map[string]ProviderConfig{}}
	}
	cache = &registryCache{stamp: stamp, reg: reg}
	return reg
}

// configStamp renders the identity of the configuration files: their paths,
// and their size and modification time when they exist.
func configStamp(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte(0)
		if st, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%d:%d", st.Size(), st.ModTime().UnixNano())
		} else {
			b.WriteString("-")
		}
		b.WriteByte(0)
	}
	return b.String()
}

// ParseProviders decodes the provider section of a configuration document.
func ParseProviders(raw []byte) (map[string]ProviderConfig, error) {
	var doc struct {
		Providers map[string]ProviderConfig `json:"providers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse providers: %w", err)
	}
	out := make(map[string]ProviderConfig, len(doc.Providers))
	for id, p := range doc.Providers {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if strings.TrimSpace(p.ID) == "" {
			p.ID = id
		}
		out[id] = p
	}
	return out, nil
}

// SaveProviders writes the provider section to path, atomically.
func SaveProviders(path string, providers map[string]ProviderConfig) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("models: no provider config path")
	}
	if len(providers) == 0 {
		providers = map[string]ProviderConfig{}
	}
	doc := struct {
		Providers map[string]ProviderConfig `json:"providers"`
	}{Providers: providers}
	return writeJSONAtomic(path, doc)
}

// writeJSONAtomic writes v as indented JSON to path: a sibling temp file
// first, then a rename, so a crash mid-write cannot truncate the existing
// configuration.
func writeJSONAtomic(path string, v any) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	buf = append(buf, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
