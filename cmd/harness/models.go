// models.go — the model configuration the client hands to the harness.
//
// # Who owns the credentials
//
// The SDK does not put the model credentials in the environment. It puts
// them in `HarnessConfig.models`: each entry names a model and the
// endpoint the harness must dial to reach it.
//
//	models[i].gemini_api_endpoint{base_url, http_headers, api_key, options}
//	models[i].vertex_endpoint{base_url, http_headers, project, location, api_key, options}
//	models[i].gemma_endpoint{base_url}
//	models[i].custom_endpoint{backend_type, config_json}
//
// The reference harness reads that field and dials the Gemini API
// (`google.golang.org/genai`) with the key it was given — the request it
// sent was captured verbatim against a local fake endpoint (see
// `out/localharness/` and cycle 301):
//
//	POST {base_url}/v1beta/models/{model}:streamGenerateContent?alt=sse
//	X-Goog-Api-Key: {api_key}
//
// The clone does the same: an endpoint in the config wins over the clone's
// own provider dispatch, because the client's configuration is the
// authoritative one for a harness session. Without this the SDK's key
// would never reach a model and the session would silently fall back to
// whatever `~/.config/openagy/providers.json` happens to name.
//
// # Selection
//
// `HarnessConfig.models` may list several models with different `types`
// (text, image). The turn engine needs the text one; the first text entry
// wins, and a config with no text entry falls back to the clone's own
// dispatch.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// configModel is the model configuration a conversation runs on.
type configModel struct {
	// Client is the transport the endpoint names.
	Client backend.Client
	// Wire is the model id put on the request.
	Wire string
	// Label is the human-readable name (the config's model name).
	Label string
}

// configModelFor builds the transport the config's model endpoint names.
//
// It returns nil when the config names no usable endpoint — the caller
// then falls back to the clone's own dispatch. An error means the config
// named an endpoint the clone cannot serve; the caller surfaces it
// instead of silently talking to a different model.
func configModelFor(cfg *pb.HarnessConfig, client *pb.ClientInfo) (*configModel, error) {
	if cfg == nil {
		return nil, nil
	}
	model := pickTextModel(cfg.GetModels())
	if model == nil {
		return nil, nil
	}
	name := strings.TrimSpace(model.GetName())
	userAgent := userAgentFor(client)

	switch {
	case model.GetGeminiApiEndpoint() != nil:
		endpoint := model.GetGeminiApiEndpoint()
		options := endpoint.GetOptions()
		client := backend.NewGeminiWithConfig(backend.GeminiConfig{
			BaseURL:         endpoint.GetBaseUrl(),
			APIKey:          endpoint.GetApiKey(),
			Model:           name,
			Headers:         endpoint.GetHttpHeaders(),
			ThinkingLevel:   options.GetThinkingLevel(),
			MaxOutputTokens: int(options.GetMaxOutputTokens()),
			UserAgent:       userAgent,
			// The reference's harness declares its tools with a
			// `google.protobuf.Struct`, so every schema it sends travels
			// under `parametersJsonSchema`.
			JSONSchemaTools: true,
		})
		return &configModel{Client: client, Wire: name, Label: name}, nil

	case model.GetVertexEndpoint() != nil:
		endpoint := model.GetVertexEndpoint()
		options := endpoint.GetOptions()
		client := backend.NewGeminiWithConfig(backend.GeminiConfig{
			BaseURL:         endpoint.GetBaseUrl(),
			APIKey:          endpoint.GetApiKey(),
			Model:           name,
			Headers:         endpoint.GetHttpHeaders(),
			ThinkingLevel:   options.GetThinkingLevel(),
			MaxOutputTokens: int(options.GetMaxOutputTokens()),
			VertexProject:   endpoint.GetProject(),
			VertexLocation:  endpoint.GetLocation(),
			UserAgent:       userAgent,
			JSONSchemaTools: true,
		})
		return &configModel{Client: client, Wire: name, Label: name}, nil

	case model.GetGemmaEndpoint() != nil:
		endpoint := model.GetGemmaEndpoint()
		client := backend.NewGeminiWithConfig(backend.GeminiConfig{
			BaseURL:         endpoint.GetBaseUrl(),
			Model:           name,
			UserAgent:       userAgent,
			JSONSchemaTools: true,
		})
		return &configModel{Client: client, Wire: name, Label: name}, nil

	case model.GetCustomEndpoint() != nil:
		return customEndpointModel(model.GetCustomEndpoint(), name)
	}
	return nil, nil
}

// userAgentFor renders the User-Agent the reference sends.
//
// The reference builds it from the client's own description:
//
//	google-antigravity-sdk/0.1.22 python/3.13.0 client-os/darwin client-os-ver/25.3.0
//
// The SDK's name is fixed — it names the SDK the client is — and the rest
// comes from `InputConfig.client_info`. The captured request carries
// exactly this string.
func userAgentFor(client *pb.ClientInfo) string {
	if client == nil {
		return ""
	}
	parts := []string{"google-antigravity-sdk/" + client.GetVersion()}
	if language := client.GetLanguage(); language != "" {
		parts = append(parts, language+"/"+client.GetLanguageVersion())
	}
	if os := client.GetOs(); os != "" {
		parts = append(parts, "client-os/"+os)
	}
	if version := client.GetOsVersion(); version != "" {
		parts = append(parts, "client-os-ver/"+version)
	}
	return strings.Join(parts, " ")
}

// pickTextModel returns the first text model of the config.
func pickTextModel(models []*pb.ModelConfig) *pb.ModelConfig {
	for _, model := range models {
		if model == nil {
			continue
		}
		for _, kind := range model.GetTypes() {
			if kind == pb.ModelType_MODEL_TYPE_TEXT {
				return model
			}
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return nil
}

// customEndpointModel builds the transport a `custom_endpoint` names.
//
// The endpoint carries a `backend_type` and a `config_json` blob. The
// clone maps the type onto its own transports:
//
//	gemini / gemini_api / google    → the Gemini API transport
//	openai / openai_compatible      → the configured gateway (OpenAI dialect)
//	cloudcode / codeassist          → the Antigravity gRPC (CloudCode)
//
// An unknown type is an error, not a silent fallback: the client asked
// for a specific backend and quietly serving a different one would be
// worse than failing.
func customEndpointModel(endpoint *pb.CustomEndpoint, model string) (*configModel, error) {
	kind := strings.ToLower(strings.TrimSpace(endpoint.GetBackendType()))
	raw := strings.TrimSpace(endpoint.GetConfigJson())

	switch kind {
	case "gemini", "gemini_api", "gemini-api", "google", "gemma":
		var cfg struct {
			BaseURL      string            `json:"base_url"`
			APIKey       string            `json:"api_key"`
			Headers      map[string]string `json:"http_headers"`
			ThinkingLvl  string            `json:"thinking_level"`
			MaxOutTokens int               `json:"max_output_tokens"`
		}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
				return nil, fmt.Errorf("harness: custom endpoint %q: %w", kind, err)
			}
		}
		client := backend.NewGeminiWithConfig(backend.GeminiConfig{
			BaseURL:         cfg.BaseURL,
			APIKey:          cfg.APIKey,
			Model:           model,
			Headers:         cfg.Headers,
			ThinkingLevel:   cfg.ThinkingLvl,
			MaxOutputTokens: cfg.MaxOutTokens,
			JSONSchemaTools: true,
		})
		return &configModel{Client: client, Wire: model, Label: model}, nil

	case "openai", "openai_compatible", "openai-compatible", "gateway":
		var cfg struct {
			URL            string            `json:"url"`
			BaseURL        string            `json:"base_url"`
			APIKey         string            `json:"api_key"`
			ExternalModel  string            `json:"external_model"`
			Headers        map[string]string `json:"headers"`
			TimeoutSeconds int               `json:"timeout_seconds"`
		}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
				return nil, fmt.Errorf("harness: custom endpoint %q: %w", kind, err)
			}
		}
		url := strings.TrimSpace(cfg.URL)
		if url == "" {
			url = strings.TrimSpace(cfg.BaseURL)
		}
		wire := strings.TrimSpace(cfg.ExternalModel)
		if wire == "" {
			wire = model
		}
		client := backend.NewGatewayWithConfig(backend.GatewayConfig{
			URL:              url,
			APIKey:           cfg.APIKey,
			ExternalModel:    wire,
			Headers:          cfg.Headers,
			TimeoutSeconds:   cfg.TimeoutSeconds,
			DefaultMaxTokens: 0,
			HTTPClient:       &http.Client{Timeout: backendTimeout(cfg.TimeoutSeconds)},
		})
		return &configModel{Client: client, Wire: wire, Label: model}, nil

	case "cloudcode", "codeassist", "antigravity":
		var cfg struct {
			Endpoint        string `json:"endpoint"`
			Project         string `json:"project"`
			AccessToken     string `json:"access_token"`
			TimeoutSeconds  int    `json:"timeout_seconds"`
			MaxOutputTokens int    `json:"max_output_tokens"`
		}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
				return nil, fmt.Errorf("harness: custom endpoint %q: %w", kind, err)
			}
		}
		client := backend.NewCloudCodeWithConfig(backend.CloudCodeConfig{
			Endpoint:        cfg.Endpoint,
			Project:         cfg.Project,
			Model:           model,
			AccessToken:     cfg.AccessToken,
			TimeoutSeconds:  cfg.TimeoutSeconds,
			MaxOutputTokens: cfg.MaxOutputTokens,
		})
		return &configModel{Client: client, Wire: model, Label: model}, nil
	}
	return nil, fmt.Errorf("harness: unsupported custom endpoint backend type %q", kind)
}

// backendTimeout turns a seconds count into a client timeout. Zero means
// "no timeout" — the gateway's own default would otherwise impose 60s.
func backendTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// describeConfigModels renders the config's model list for a log line.
func describeConfigModels(cfg *pb.HarnessConfig) string {
	models := cfg.GetModels()
	if len(models) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(models))
	for _, model := range models {
		kind := "gemini"
		switch {
		case model.GetVertexEndpoint() != nil:
			kind = "vertex"
		case model.GetGemmaEndpoint() != nil:
			kind = "gemma"
		case model.GetCustomEndpoint() != nil:
			kind = "custom:" + model.GetCustomEndpoint().GetBackendType()
		}
		parts = append(parts, fmt.Sprintf("%s(%s)", model.GetName(), kind))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
