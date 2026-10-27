// Package backend — gateway.go: a direct, registry-free Client that talks
// to one user-configured upstream endpoint.
//
// Background: the cycle 33 registry-backed path (`NewRealClient` ->
// models.Registry -> models.Provider) is the right answer for a stable
// user-managed config of multiple providers, but it has two rough
// edges:
//
//  1. The registry has to be set up by the user (write a providers.json
//     file with the gateway entry). That's a chicken-and-egg problem when
//     you're just trying to verify the agent loop wires up correctly.
//  2. The 7-provider Provider interface pulls in the entire models
//     package (validate, retry, registry, Anthropic, Google, ...)
//     when all we want is a single HTTPS POST.
//
// This file is that escape hatch: NewGateway() returns a backend.Client
// that talks to one endpoint, with the endpoint, the credential and the
// upstream model id all coming from the user's provider configuration
// (see internal/models). It is selected via `--backend gateway`, so the
// rest of the code path (mock, registry providers) keeps working
// unchanged.
//
// # Two wire protocols
//
// GatewayConfig.Type selects the dialect:
//
//	"openai"     OpenAI-compatible Chat Completions. The request is a
//	             single POST whose body is {model, messages, stream,
//	             tools}; the response is the standard "data: {…}\n\n"
//	             SSE framing. The credential is `Authorization: Bearer …`.
//
//	"anthropic"  Anthropic Messages. The request is a single POST whose
//	             body is {model, system, messages, stream, tools}; the
//	             response is the Anthropic event stream
//	             ("event: content_block_delta\ndata: {…}"). The
//	             credential is `x-api-key: …` and the request carries
//	             `anthropic-version: 2023-06-01`.
//
// Both dialects share the HTTP plumbing — the connection-drop retry
// budget, the *HTTPError classification, the idle-timeout reader —
// because those are properties of the transport, not of the protocol.
//
// The wire shape we send / receive is OpenAI-compatible Chat
// Completions with the standard "data: {…}\n\n" SSE framing. We reuse
// the same JSON tags the upstream returns, so the parser is robust
// against the "reasoning_tokens" subfield some gateways add.
package backend

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/models"
)

// GatewayTimeoutUnlimited disables the gateway client's per-request timeout
// so the caller's context is the only bound. The headless (`-p` / `--prompt`)
// path uses it: the reference changed the default headless timeout from 5
// minutes to unlimited, so a long agent turn must not be cut off by the
// transport.
const GatewayTimeoutUnlimited = -1

// The wire protocols a gateway endpoint can speak.
const (
	// GatewayProtocolOpenAI is the OpenAI-compatible Chat Completions
	// dialect. It is the default when a configuration states no type.
	GatewayProtocolOpenAI = "openai"
	// GatewayProtocolAnthropic is the Anthropic Messages dialect.
	GatewayProtocolAnthropic = "anthropic"
)

// GatewayConfig is the knob set for one configured upstream endpoint.
type GatewayConfig struct {
	URL              string
	APIKey           string
	ExternalModel    string
	Type             string
	Headers          map[string]string
	TimeoutSeconds   int
	DefaultMaxTokens int
	// ReasoningEffort is the reasoning level the model runs at — the
	// "tier" of a model that declares tiers. It is sent as the OpenAI
	// dialect's `reasoning_effort` hint.
	//
	// The Anthropic dialect has no such field, so the hint is dropped
	// there: the Messages API spells its thinking budget differently.
	ReasoningEffort string
	HTTPClient      *http.Client
}

// withDefaults fills in the values that can be defaulted without inventing
// an endpoint: the protocol, the timeout, and the environment overrides.
func (c GatewayConfig) withDefaults() GatewayConfig {
	if c.URL == "" {
		c.URL = strings.TrimSpace(os.Getenv("OPENAGY_GATEWAY_URL"))
	}
	if c.APIKey == "" {
		c.APIKey = strings.TrimSpace(os.Getenv("OPENAGY_GATEWAY_KEY"))
	}
	if c.Type == "" {
		c.Type = GatewayProtocolOpenAI
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 60
	}
	return c
}

// protocol returns the normalised wire protocol.
func (c GatewayConfig) protocol() string {
	switch strings.ToLower(strings.TrimSpace(c.Type)) {
	case GatewayProtocolAnthropic:
		return GatewayProtocolAnthropic
	default:
		return GatewayProtocolOpenAI
	}
}

// GatewayClient is a backend.Client that streams from one configured
// endpoint. It is safe for concurrent Stream() calls (each call spawns its
// own goroutine and its own HTTP request).
type GatewayClient struct {
	cfg        GatewayConfig
	httpDo     func(*http.Request) (*http.Response, error) // overridable for tests
	baseURL    string                                      // for status / logging
	apiKey     string                                      // for status / logging
	model      string
	defaultMax int
}

// NewGateway returns a Client for the endpoint named by the environment
// ($OPENAGY_GATEWAY_URL / $OPENAGY_GATEWAY_KEY) and the user's provider
// configuration.
func NewGateway() Client {
	return NewGatewayWithConfig(GatewayConfig{})
}

// NewGatewayWithConfig returns a Client with the supplied config. Zero
// values trigger defaults; a non-nil HTTPClient is preserved (used by
// tests to inject a stub server).
func NewGatewayWithConfig(cfg GatewayConfig) Client {
	cfg = cfg.withDefaults()
	c := &GatewayClient{
		cfg:        cfg,
		baseURL:    cfg.URL,
		apiKey:     cfg.APIKey,
		model:      cfg.ExternalModel,
		defaultMax: cfg.DefaultMaxTokens,
	}
	if cfg.HTTPClient != nil {
		c.httpDo = cfg.HTTPClient.Do
	} else if cfg.TimeoutSeconds < 0 {
		c.httpDo = (&http.Client{}).Do
	} else {
		c.httpDo = (&http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}).Do
	}
	return c
}

// Endpoint returns a short, loggable description of where this client
// is pointing. Used by /healthz and operator tooling. The API key is
// not echoed.
func (c *GatewayClient) Endpoint() string {
	return fmt.Sprintf("%s model=%s protocol=%s", endpointHost(c.cfg.URL), c.model, c.cfg.protocol())
}

// endpointHost renders the host of a configured endpoint for logs.
func endpointHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "(unset)"
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// GatewayConfig returns the resolved configuration this client is using, defaults
// included.
func (c *GatewayClient) GatewayConfig() GatewayConfig { return c.cfg }

// GatewaySystemFingerprint is the value this client stamps on every usage record.
func GatewaySystemFingerprint(baseURL, externalModel string) string {
	return endpointHost(baseURL) + "/" + externalModel
}

// GatewayUserAgent is the identity this transport presents upstream.
const GatewayUserAgent = "openagy-recovered/1 (gateway)"

// Stream implements backend.Client. It translates the wire-neutral
// StreamRequest into an OpenAI-shaped or Anthropic-shaped POST, parses the
// server-sent event stream, and emits one StreamChunk per non-empty delta.content.
func (c *GatewayClient) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	externalModel := strings.TrimSpace(c.model)
	if externalModel == "" {
		return nil, fmt.Errorf("gateway: no upstream model configured: set the model's `model` field in the provider configuration (%s)", models.ProvidersPath())
	}
	if strings.TrimSpace(c.baseURL) == "" {
		return nil, fmt.Errorf("gateway: no endpoint configured: set the provider's base URL in the provider configuration (%s)", models.ProvidersPath())
	}
	raw, err := c.buildRequest(req, externalModel)
	if err != nil {
		return nil, err
	}

	resp, err := c.openGatewayWithRetry(ctx, raw)
	if err != nil {
		return nil, err
	}

	out := make(chan StreamChunk, 16)
	go func() {
		defer close(out)
		c.runGatewayStreamBodyWithRetry(ctx, resp, raw, externalModel, out)
	}()
	return out, nil
}

// truncateGateway is a small helper that keeps error messages short enough
// to fit in a single log line.
func truncateGateway(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Compile-time check GatewayClient satisfies Client.
var _ Client = (*GatewayClient)(nil)
