// Package core — auth.go: plugin auth.json parser, validator and environment resolver.
//
// Plugins can declare authentication providers in <plugin_dir>/auth.json.
// The reference supports two provider types: "api_key" and "oauth2".
//
// In agy 1.4.12, "optional": true was introduced for API key providers:
// for services that work without a key but raise rate limits with one:
// when the user hasn't set the key, commands see the variable as unset
// and make unauthenticated requests at the free rate limit, instead of
// failing or prompting the user to sign in.
//
// 【一手】 Recovered from agy 1.4.12 symbols and strings:
//   google3/third_party/jetski/cortex/utils/utils.readPluginAuthFile
//   google3/third_party/jetski/cortex/utils/utils.TranslatePluginAuth
//   google3/third_party/jetski/cortex/utils/utils.translateProvider
//   google3/third_party/jetski/cortex/utils/utils.PluginAuthProviderInfo
package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

var validEnvVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// PluginOAuth2Config declares the OAuth2 client configuration in auth.json.
type PluginOAuth2Config struct {
	ClientID string `json:"clientId"`
	AuthURL  string `json:"authUrl,omitempty"`
	TokenURL string `json:"tokenUrl,omitempty"`
}

// PluginAuthProvider represents one auth provider declaration in auth.json.
type PluginAuthProvider struct {
	Type       string              `json:"type"` // "api_key" or "oauth2"
	Optional   bool                `json:"optional,omitempty"`
	Env        map[string]string   `json:"env,omitempty"`
	Domains    []string            `json:"domains,omitempty"`
	Scopes     []string            `json:"scopes,omitempty"`
	CLIs       []string            `json:"clis,omitempty"`
	MCPServers []string            `json:"mcpServers,omitempty"`
	OAuth2     *PluginOAuth2Config `json:"oauth2,omitempty"`
}

// PluginAuthDoc is the root structure of <plugin_dir>/auth.json.
type PluginAuthDoc struct {
	Providers map[string]PluginAuthProvider `json:"providers"`
}

// PluginAuthProviderInfo summarizes an auth provider's runtime state for UI and marketplace.
// Matches the exact struct shape recovered from agy 1.4.12:
//
//	struct { Plugin string; Provider string; Type string; Domains []string; Scopes []string; StorageKey string; Connected bool; CLIs []string; Accounts []...; Optional bool }
type PluginAuthProviderInfo struct {
	Plugin     string   `json:"plugin"`
	Provider   string   `json:"provider"`
	Type       string   `json:"type"`
	Domains    []string `json:"domains,omitempty"`
	Scopes     []string `json:"scopes,omitempty"`
	StorageKey string   `json:"storageKey,omitempty"`
	Connected  bool     `json:"connected"`
	CLIs       []string `json:"clis,omitempty"`
	Optional   bool     `json:"optional,omitempty"`
}

// Validate checks that the provider configuration conforms to the reference rules.
func (p *PluginAuthProvider) Validate(providerName string) error {
	switch p.Type {
	case "api_key":
		// valid
	case "oauth2":
		if p.OAuth2 == nil {
			return errors.New("type \"oauth2\" requires an \"oauth2\" section")
		}
		if p.OAuth2.ClientID == "" {
			return errors.New("oauth2.clientId is required")
		}
	default:
		return fmt.Errorf("unsupported provider type %q (want \"oauth2\" or \"api_key\")", p.Type)
	}

	for _, cli := range p.CLIs {
		if cli == "" {
			return errors.New("empty entry in \"clis\"")
		}
	}

	for envVar, secretName := range p.Env {
		if !validEnvVarName.MatchString(envVar) {
			return fmt.Errorf("provider %q: %q is not a valid environment variable name", providerName, envVar)
		}
		if secretName != "" && !validEnvVarName.MatchString(secretName) {
			return fmt.Errorf("provider %q: secret name %q is not a valid environment variable name", providerName, secretName)
		}
	}

	return nil
}

// ReadPluginAuth reads and parses <pluginDir>/auth.json.
// If the file does not exist, it returns (nil, nil).
// If the file is malformed, it returns an error wrapping "invalid auth.json".
func ReadPluginAuth(pluginDir string) (*PluginAuthDoc, error) {
	path := filepath.Join(pluginDir, "auth.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid auth.json in %s: %w", pluginDir, err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc PluginAuthDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid auth.json: %w", err)
	}

	if len(doc.Providers) == 0 {
		return nil, errors.New("auth.json declares no providers")
	}

	for name, provider := range doc.Providers {
		if err := provider.Validate(name); err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
	}

	return &doc, nil
}

// ResolvePluginAuthEnv resolves environment variable injections for a provider.
//
// For API key providers:
//   - If provider.Optional is false and a secret is missing/empty, it returns
//     an authentication error:
//     "%s needs authentication (%s). Ask the user to sign in to %s in the Plugins Manager, then try again"
//   - If provider.Optional is true (new in agy 1.4.12) and a secret is missing/empty,
//     the variable is left unset in the returned map without error, allowing
//     unauthenticated calls at the service's free rate limit.
func ResolvePluginAuthEnv(
	pluginName string,
	commandName string,
	providerName string,
	provider *PluginAuthProvider,
	getSecret func(key string) (string, bool),
) (map[string]string, error) {
	if provider == nil || len(provider.Env) == 0 {
		return nil, nil
	}

	out := make(map[string]string)
	for envVar, secretKey := range provider.Env {
		key := secretKey
		if key == "" {
			key = envVar
		}

		secretVal, found := "", false
		if getSecret != nil {
			secretVal, found = getSecret(key)
		}

		if !found || secretVal == "" {
			if provider.Optional {
				// agy 1.4.12: "optional": true leaves variable unset and does not fail.
				continue
			}
			return nil, fmt.Errorf("%s needs authentication (%s). Ask the user to sign in to %s in the Plugins Manager, then try again", commandName, providerName, pluginName)
		}

		out[envVar] = secretVal
	}

	return out, nil
}

// ListPluginAuthProviders loads auth.json from pluginDir and returns sorted provider metadata.
func ListPluginAuthProviders(pluginDir string, pluginName string) ([]PluginAuthProviderInfo, error) {
	doc, err := ReadPluginAuth(pluginDir)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, nil
	}

	keys := make([]string, 0, len(doc.Providers))
	for k := range doc.Providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result []PluginAuthProviderInfo
	for _, k := range keys {
		p := doc.Providers[k]
		result = append(result, PluginAuthProviderInfo{
			Plugin:     pluginName,
			Provider:   k,
			Type:       p.Type,
			Domains:    p.Domains,
			Scopes:     p.Scopes,
			CLIs:       p.CLIs,
			Optional:   p.Optional,
			Connected:  false,
			StorageKey: fmt.Sprintf("%s:%s", pluginName, k),
		})
	}
	return result, nil
}
