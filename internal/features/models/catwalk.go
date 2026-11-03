// catwalk.go — Catwalk preset providers and model catalogs integration.
package models

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"charm.land/catwalk/v3/pkg/catwalk"
	"charm.land/catwalk/v3/pkg/embedded"
)

//go:embed hyper_provider.json
var embeddedHyperJSON []byte

//go:embed gaterouter_provider.json
var embeddedGateRouterJSON []byte

//go:embed stepfun_provider.json
var embeddedStepFunJSON []byte

//go:embed antigravity_provider.json
var embeddedAntigravityJSON []byte

// CatwalkPresetSearch holds pre-lowercased metadata for fast search without allocations.
type CatwalkPresetSearch struct {
	IDLower    string
	NameLower  string
	ModelIDs   []string
	ModelNames []string
}

var (
	catwalkOnce         sync.Once
	catwalkProviders    []catwalk.Provider
	catwalkByID         map[string]*catwalk.Provider
	catwalkByName       map[string]*catwalk.Provider
	catwalkPresetIDs    []string
	catwalkPresetConfig map[string]ProviderConfig
	catwalkSearchIndex  map[string]CatwalkPresetSearch
)

func initCatwalk() {
	catwalkOnce.Do(func() {
		all := embedded.GetAll()
		for _, data := range [][]byte{embeddedHyperJSON, embeddedGateRouterJSON, embeddedStepFunJSON, embeddedAntigravityJSON} {
			var cp catwalk.Provider
			if err := json.Unmarshal(data, &cp); err == nil && cp.ID != "" {
				all = append(all, cp)
			}
		}
		catwalkProviders = all
		catwalkByID = make(map[string]*catwalk.Provider, len(all))
		catwalkByName = make(map[string]*catwalk.Provider, len(all))
		catwalkPresetConfig = make(map[string]ProviderConfig, len(all))
		catwalkSearchIndex = make(map[string]CatwalkPresetSearch, len(all))
		ids := make([]string, 0, len(all))

		for i := range catwalkProviders {
			p := &catwalkProviders[i]
			id := strings.ToLower(strings.TrimSpace(string(p.ID)))
			if id == "" {
				continue
			}
			catwalkByID[id] = p
			ids = append(ids, id)

			name := strings.ToLower(strings.TrimSpace(p.Name))
			if name != "" {
				catwalkByName[name] = p
			}

			// Precompute ProviderConfig for PresetProviderConfig
			cfg := buildPresetConfig(p)
			catwalkPresetConfig[id] = cfg

			// Precompute search index with pre-lowercased model IDs and names
			mIDs := make([]string, 0, len(p.Models))
			mNames := make([]string, 0, len(p.Models))
			for _, m := range p.Models {
				if mid := strings.ToLower(strings.TrimSpace(m.ID)); mid != "" {
					mIDs = append(mIDs, mid)
				}
				if mname := strings.ToLower(strings.TrimSpace(m.Name)); mname != "" {
					mNames = append(mNames, mname)
				}
			}
			catwalkSearchIndex[id] = CatwalkPresetSearch{
				IDLower:    id,
				NameLower:  name,
				ModelIDs:   mIDs,
				ModelNames: mNames,
			}
		}

		sort.Strings(ids)
		catwalkPresetIDs = ids
	})
}

// CatwalkProviders returns all preset providers from the embedded Catwalk catalog.
func CatwalkProviders() []catwalk.Provider {
	initCatwalk()
	return catwalkProviders
}

// CatwalkSearchIndex returns the precomputed search index for all Catwalk presets.
func CatwalkSearchIndex() map[string]CatwalkPresetSearch {
	initCatwalk()
	return catwalkSearchIndex
}


func normalizePresetID(id string) string {
	needle := strings.ToLower(strings.TrimSpace(id))
	switch needle {
	case "antigravity":
		return "google-antigravity"
	default:
		return needle
	}
}

// FindCatwalkProvider looks up a Catwalk preset provider by ID or display name.
// The match is case-insensitive.
func FindCatwalkProvider(idOrName string) (*catwalk.Provider, bool) {
	initCatwalk()
	needle := normalizePresetID(idOrName)
	if needle == "" {
		return nil, false
	}
	if p, ok := catwalkByID[needle]; ok {
		return p, true
	}
	if p, ok := catwalkByName[needle]; ok {
		return p, true
	}
	return nil, false
}

// CatwalkAuthMethod returns the preferred authentication method for a provider:
// "browser" (OAuth / device flow browser sign-in), "api_key", or "both".
func CatwalkAuthMethod(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case HyperProviderID, "copilot":
		return "browser" // Charm Device Flow / GitHub Device Flow
	case "google-antigravity", "antigravity":
		return "browser" // Google OAuth / CloudCode keychain
	case "openai":
		return "both" // Supports ChatGPT OAuth and OpenAI API Key
	case "xai":
		return "both" // xAI Grok Device Flow or API Key
	case "google", "gemini", "vertexai":
		return "both" // Google Cloud ADC / browser login or API Key
	default:
		return "api_key"
	}
}

// PresetProviderConfig converts a Catwalk provider into an openagy ProviderConfig
// pre-filled with endpoint URL, protocol wire type, and available models.
func PresetProviderConfig(id string) (ProviderConfig, bool) {
	initCatwalk()
	needle := normalizePresetID(id)
	cfg, ok := catwalkPresetConfig[needle]
	if ok {
		return cfg, true
	}
	if p, found := FindCatwalkProvider(needle); found {
		pid := strings.ToLower(strings.TrimSpace(string(p.ID)))
		if c, exists := catwalkPresetConfig[pid]; exists {
			return c, true
		}
	}
	return ProviderConfig{}, false
}

func buildPresetConfig(cp *catwalk.Provider) ProviderConfig {
	wireType := ProviderTypeOpenAI
	if cp.Type == catwalk.TypeMessages {
		wireType = ProviderTypeAnthropic
	}

	endpoint := strings.TrimSpace(cp.APIEndpoint)
	var baseURL, apiURL string
	if cp.ID == HyperProviderID || cp.Type == HyperProviderID {
		wireType = ProviderTypeOpenAI
		baseURL = HyperBaseURL() + "/v1"
		apiURL = ""
	} else if cp.ID == "google-antigravity" || cp.Type == "cloudcode" {
		wireType = "cloudcode"
		baseURL = "https://cloudcode-pa.googleapis.com"
		apiURL = ""
	} else if cp.ID == "gaterouter" {
		wireType = ProviderTypeOpenAI
		baseURL = "https://api.gaterouter.ai/openai/v1"
		apiURL = ""
	} else if cp.ID == "stepfun" {
		wireType = ProviderTypeOpenAI
		baseURL = "https://api.stepfun.com/step_plan/v1"
		apiURL = ""
	} else if strings.HasSuffix(endpoint, "/chat/completions") {
		baseURL = strings.TrimSuffix(endpoint, "/chat/completions")
		apiURL = endpoint
	} else if strings.HasSuffix(endpoint, "/v1/messages") {
		baseURL = strings.TrimSuffix(endpoint, "/v1/messages")
		apiURL = endpoint
	} else if strings.HasSuffix(endpoint, "/messages") {
		baseURL = strings.TrimSuffix(endpoint, "/messages")
		apiURL = endpoint
	} else {
		// When catwalk provides a base URL (e.g. https://api.minimaxi.com/anthropic or https://api.deepseek.com),
		// keep APIURL empty so ProviderConfig.Endpoint() derives the protocol path.
		baseURL = endpoint
		apiURL = ""
	}

	var modelsList []ProviderModel
	for _, m := range cp.Models {
		mid := strings.TrimSpace(m.ID)
		if mid == "" {
			continue
		}
		name := strings.TrimSpace(m.Name)
		if name == "" {
			name = mid
		}
		efforts := m.ReasoningEffortLevels()
		var vision *bool
		if m.Capabilities.Vision {
			v := true
			vision = &v
		}

		modelsList = append(modelsList, ProviderModel{
			ID:               mid,
			Name:             name,
			ContextWindow:    int(m.ContextWindow),
			DefaultMaxTokens: int(m.DefaultMaxTokens),
			ReasoningEffort:  m.Reasoning.DefaultEffortLevel,
			ReasoningEfforts: efforts,
			SupportsImages:   vision,
		})
	}

	return ProviderConfig{
		ID:       string(cp.ID),
		Name:     cp.Name,
		Type:     wireType,
		BaseURL:  baseURL,
		APIURL:   apiURL,
		Models:   modelsList,
	}
}

// FindCatwalkModel finds a model in Catwalk presets for a given provider and model ID.
// Returns a ProviderModel pre-filled with context window, default max tokens,
// reasoning efforts, vision support, and name from Catwalk metadata.
func FindCatwalkModel(providerID, modelID string) (*ProviderModel, bool) {
	preset, ok := PresetProviderConfig(providerID)
	if !ok {
		return nil, false
	}
	needle := strings.ToLower(strings.TrimSpace(modelID))
	for _, m := range preset.Models {
		if strings.ToLower(m.ID) == needle || strings.ToLower(m.Name) == needle {
			cm := m
			return &cm, true
		}
	}
	return nil, false
}

