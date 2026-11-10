package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/atomicfile"
	"github.com/justwasm/pigasus/configdir"
	"github.com/justwasm/pigasus/envref"
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
	// Headers is an alias for ExtraHeaders in providers.json.
	Headers map[string]string `json:"headers,omitempty"`
	// HeaderOverrides is an alias for ExtraHeaders in providers.json.
	HeaderOverrides map[string]string `json:"header_overrides,omitempty"`
	// TimeoutSeconds bounds one request. 0 means the transport's default.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// MaxRetries is the retry count for 5xx and network failures.
	MaxRetries int `json:"max_retries,omitempty"`
	// Models are the models this provider serves.
	Models []ProviderModel `json:"models,omitempty"`
	// Accounts holds multiple accounts or API keys configured for this provider.
	Accounts []ProviderAccount `json:"accounts,omitempty"`
	// ActiveAccount names the currently active account ID.
	ActiveAccount string `json:"active_account,omitempty"`
}

// ProviderAccount represents one account, token, or API key under a multi-account provider.
type ProviderAccount struct {
	ID     string `json:"id"`
	Email  string `json:"email,omitempty"`
	Label  string `json:"label,omitempty"`
	APIKey string `json:"api_key,omitempty"`
	Token  string `json:"token,omitempty"`
	Active bool   `json:"active,omitempty"`
}

// ProviderModel is one model a provider serves.
type ProviderModel struct {
	// ID is the catalogue id: what --model takes and what the model list
	// prints. Optional if Model is provided.
	ID string `json:"id,omitempty"`
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
	//
	// It is also the tier the model runs at when the model declares
	// tiers and nothing else says otherwise (see ReasoningEfforts).
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// ReasoningEfforts are the reasoning tiers the model offers — the
	// levels it can be selected at ("low", "medium", "high", "xhigh",
	// "max").
	//
	// The reference spells the tier into the model's own catalogue id
	// ("gemini-3.8-flash-high"), so a model the user configured needs
	// its own list: the catalogue then lists the model once per tier,
	// and the tier rides the row all the way to the request.
	//
	// The list is the closed set the Web UI's dispatcher accepts
	// (EffortLevels); a tier outside it is rejected when it is written.
	ReasoningEfforts []string `json:"reasoning_efforts,omitempty"`
	// SupportsImages says the model takes pictures.
	//
	// It is a pointer because "not stated" is an answer of its own: the
	// catalogue's family table decides then, and the user's word decides
	// when it is there. A model that says nothing is a model the Web
	// UI's attachment check refuses to attach anything to, so the
	// marker is the difference between a working attachment button and
	// a dead one.
	//
	// crush's own model list carries the same marker per model
	// (catwalk.Model.SupportsImages, json "supports_attachments"), and
	// its UI reads it off the selected model the same way.
	//
	// SupportedMimeTypes is the fuller form of the same answer: a model
	// that takes PDF but not PNG says so with a list. The list wins;
	// this marker is its shorthand ("the two image types").
	SupportsImages *bool `json:"supports_images,omitempty"`
	// SupportedMimeTypes are the media types the model accepts.
	//
	// It is the reference's own shape: the picker's model config
	// carries a `supportedMimeTypes` *map* (a mime type and whether it
	// is allowed), and the IDE's ModelFeatures carries the same field.
	// A list here is the shorthand of that map.
	SupportedMimeTypes []string `json:"supported_mime_types,omitempty"`
	// Disabled takes the model out of the picker.
	//
	// The Web UI filters the disabled entries out of the model list
	// (`filter(b => !b.disabled)`), so a model the operator has
	// taken out of service disappears from the picker instead of
	// failing every turn. The reference uses the same field for a
	// model that is temporarily unavailable.
	Disabled bool `json:"disabled,omitempty"`
	// QuotaInfo is the model's quota state.
	//
	// The Web UI reads it to warn before a turn ("this model is out of
	// quota until …"), and the reference's own catalogue carries it on
	// the model it has run out of.
	QuotaInfo *QuotaInfo `json:"quota_info,omitempty"`
	// SupportsThoughtCirculation says the model circulates its
	// thoughts — the reference's own field (ClientModelConfig's field
	// 19), which the Web UI carries into the picker's model option.
	//
	// It is a pointer for the same reason SupportsImages is.
	SupportsThoughtCirculation *bool `json:"supports_thought_circulation,omitempty"`
	// ModelURL is a link to the model's own page — the reference's
	// field (ClientModelConfig's field 20), which the Web UI carries
	// into the picker's model option.
	ModelURL string `json:"model_url,omitempty"`
	// TagTitle and TagDescription are the picker's tag line under the
	// model's name. The clone guesses them from the model's id when
	// the configuration says nothing.
	TagTitle       string `json:"tag_title,omitempty"`
	TagDescription string `json:"tag_description,omitempty"`
	// EffortSupported says the model exposes a reasoning level (the
	// --effort hint). The clone's own model list carries the flag; the
	// Web UI's picker does not read it.
	//
	// It is a pointer for the same reason SupportsImages is.
	EffortSupported *bool `json:"effort_supported,omitempty"`
}

// EffectiveID returns the catalogue id: ID if set, otherwise Model.
func (m ProviderModel) EffectiveID() string {
	if id := strings.TrimSpace(m.ID); id != "" {
		return id
	}
	return strings.TrimSpace(m.Model)
}

// EffectiveWire returns the upstream wire model name: Model if set, otherwise ID.
func (m ProviderModel) EffectiveWire() string {
	if w := strings.TrimSpace(m.Model); w != "" {
		return w
	}
	return strings.TrimSpace(m.ID)
}

// EffectiveName returns the display label: Name if set, otherwise EffectiveID.
func (m ProviderModel) EffectiveName() string {
	if n := strings.TrimSpace(m.Name); n != "" {
		return n
	}
	return m.EffectiveID()
}

// UnmarshalJSON supports unmarshaling a ProviderModel from either a JSON string
// (e.g. "MiniMax-M3.1-Flash") or a JSON object ({"id": "...", "model": "..."}).
// It normalizes ID, Model, and Name so that if either is omitted, it defaults gracefully.
func (m *ProviderModel) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		m.ID = s
		m.Name = s
		m.Model = s
		return nil
	}

	type alias ProviderModel
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = ProviderModel(raw)

	if strings.TrimSpace(m.ID) == "" && strings.TrimSpace(m.Model) != "" {
		m.ID = strings.TrimSpace(m.Model)
	}
	if strings.TrimSpace(m.Model) == "" && strings.TrimSpace(m.ID) != "" {
		m.Model = strings.TrimSpace(m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		m.Name = m.EffectiveID()
	}

	return nil
}

// MarshalJSON serializes ProviderModel, omitting "model" and "name" when they
// are identical to "id" to keep providers.json clean and close to catwalk/crush format.
func (m ProviderModel) MarshalJSON() ([]byte, error) {
	type alias ProviderModel
	cp := m
	id := m.EffectiveID()
	cp.ID = id
	if strings.TrimSpace(cp.Name) == id {
		cp.Name = ""
	}
	if strings.TrimSpace(cp.Model) == id {
		cp.Model = ""
	}
	return json.Marshal((alias)(cp))
}

// QuotaInfo is how much of a model's quota is left, and when it comes
// back.
//
// The shape is the reference's own (codeium_common_pb.QuotaInfo):
// `remaining_fraction` (1) and `reset_time` (2).
type QuotaInfo struct {
	// RemainingFraction is what is left of the quota: 1 is a full
	// quota, 0 is none. The Web UI warns below a fifth of it.
	RemainingFraction *float64 `json:"remaining_fraction,omitempty"`
	// ResetTime is when the quota comes back, in RFC 3339. The
	// reference's own answer spells it the way protojson spells a
	// timestamp: "2026-10-25T22:33:00.530076Z".
	ResetTime string `json:"reset_time,omitempty"`
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
// it is set and distinct from base_url, otherwise the base_url with the protocol's
// path appended.
func (p ProviderConfig) Endpoint() string {
	u := strings.TrimRight(strings.TrimSpace(p.APIURL), "/")
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if u != "" && u != base {
		return u
	}
	if base == "" {
		return u
	}
	switch p.WireType() {
	case ProviderTypeAnthropic:
		return base + "/v1/messages"
	default:
		return base + "/chat/completions"
	}
}

// EffectiveAPIKey returns the active account's credential if accounts are configured,
// or falls back to the top-level APIKey.
func (p ProviderConfig) EffectiveAPIKey() string {
	if len(p.Accounts) > 0 {
		if p.ActiveAccount != "" {
			for _, acc := range p.Accounts {
				if acc.ID == p.ActiveAccount {
					if acc.APIKey != "" {
						return acc.APIKey
					}
					return acc.Token
				}
			}
		}
		for _, acc := range p.Accounts {
			if acc.Active {
				if acc.APIKey != "" {
					return acc.APIKey
				}
				return acc.Token
			}
		}
		if p.Accounts[0].APIKey != "" {
			return p.Accounts[0].APIKey
		}
		return p.Accounts[0].Token
	}
	return p.APIKey
}

// EffectiveModels returns the models configured for this provider.
// If no explicit models are configured, it automatically falls back to the
// embedded Catwalk preset models if this provider is a known preset (e.g. hyper, deepseek, etc.).
func (p ProviderConfig) EffectiveModels() []ProviderModel {
	if len(p.Models) > 0 {
		return p.Models
	}
	id := strings.ToLower(strings.TrimSpace(p.ID))
	if preset, ok := PresetProviderConfig(id); ok && len(preset.Models) > 0 {
		return preset.Models
	}
	return nil
}

// ResolvedAPIKey returns the credential, expanding an environment reference.
// A "$NAME" or "${NAME}" value is read from the environment; a literal is
// returned unchanged.
func (p ProviderConfig) ResolvedAPIKey() string {
	return ExpandEnvRef(p.EffectiveAPIKey())
}

// ExpandEnvRef expands a "$NAME" or "${NAME}" reference to the environment
// value. Anything else — including an empty string and a literal that
// merely contains a dollar sign — is returned unchanged.
//
// The scanner is envref's, the same one the MCP header
// substitution uses.
func ExpandEnvRef(value string) string {
	return envref.ExpandWhole(value, os.Getenv)
}

// ParseModelRef parses a model string into provider filter and model ID.
// Format: "model-name" or "provider/model-name" or "synthetic/moonshot/kimi-k2".
// Following crush's model resolution convention: if knownProviders is provided,
// the first component is checked against it. If it matches a known provider,
// the first component is treated as the provider and the remainder as the model ID.
// If knownProviders is nil, any reference with a slash splits at the first slash.
func ParseModelRef(ref string, knownProviders map[string]bool) (provider, modelID string) {
	ref = strings.TrimSpace(ref)
	parts := strings.Split(ref, "/")
	if len(parts) <= 1 {
		return "", ref
	}
	if knownProviders != nil {
		if knownProviders[parts[0]] {
			return parts[0], strings.Join(parts[1:], "/")
		}
		return "", ref
	}
	return parts[0], strings.Join(parts[1:], "/")
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
	modelsList := p.EffectiveModels()
	out := make([]Model, 0, len(modelsList))
	for _, m := range modelsList {
		id := m.EffectiveID()
		if id == "" {
			continue
		}
		wire := m.EffectiveWire()
		label := m.EffectiveName()
		out = append(out, Model{
			Name:                       id,
			DisplayName:                label,
			Provider:                   p.WireType(),
			ProviderID:                 strings.TrimSpace(p.ID),
			Type:                       p.WireType(),
			APIKey:                     p.ResolvedAPIKey(),
			APIURL:                     p.Endpoint(),
			ExternalModelName:          wire,
			ContextWindow:              m.ContextWindow,
			DefaultMaxTokens:           m.DefaultMaxTokens,
			ReasoningEffort:            strings.TrimSpace(m.ReasoningEffort),
			ReasoningEfforts:           copyStrings(NormalizeEfforts(m.ReasoningEfforts)),
			SupportsImages:             m.SupportsImages,
			SupportedMimeTypes:         copyStrings(m.SupportedMimeTypes),
			Disabled:                   m.Disabled,
			QuotaInfo:                  m.QuotaInfo,
			SupportsThoughtCirculation: m.SupportsThoughtCirculation,
			ModelURL:                   strings.TrimSpace(m.ModelURL),
			TagTitle:                   strings.TrimSpace(m.TagTitle),
			TagDescription:             strings.TrimSpace(m.TagDescription),
			EffortSupported:            m.EffortSupported,
			TimeoutSeconds:             p.TimeoutSeconds,
			MaxRetries:                 p.MaxRetries,
			ExtraHeaders:               copyHeaders(p.EffectiveHeaders()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// EffectiveHeaders returns the merged headers from Headers, HeaderOverrides, and ExtraHeaders.
func (p ProviderConfig) EffectiveHeaders() map[string]string {
	if len(p.ExtraHeaders) == 0 && len(p.Headers) == 0 && len(p.HeaderOverrides) == 0 {
		return nil
	}
	out := make(map[string]string, len(p.Headers)+len(p.HeaderOverrides)+len(p.ExtraHeaders))
	for k, v := range p.Headers {
		out[k] = v
	}
	for k, v := range p.HeaderOverrides {
		out[k] = v
	}
	for k, v := range p.ExtraHeaders {
		out[k] = v
	}
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

// copyStrings copies a list of strings, so a caller cannot change the
// configuration's own slice through the registry.
func copyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// ProvidersPath returns the conventional path of the provider-centric
// configuration file:
//
//	<the configuration home>/providers.json — $HOME/.openagy/providers.json
//	unless $OPENAGY_CONFIG_HOME says otherwise.
//
// The env override is OPENAGY_PROVIDERS_CONFIG.
func ProvidersPath() string {
	if p := strings.TrimSpace(os.Getenv("OPENAGY_PROVIDERS_CONFIG")); p != "" {
		return p
	}
	return configdir.Path("providers.json")
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
	if err := atomicfile.WriteFile(path, buf, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
