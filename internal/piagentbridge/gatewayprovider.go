package piagentbridge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/modelctx"
)

// Provider is a native PiG provider (ai.Provider) that speaks the OpenAI
// Chat-Completions streaming dialect to an gateway-style gateway
// (https://gateway.example.com/v1/chat/completions and anything shaped like it).
//
// It is the "real" half of this bridge. BackendStreamFn adapts the clone's
// existing transport, which means the clone's SSE parser stays in charge of
// the wire format; Provider instead drives PiG's own streaming contract
// directly, so setting model.Provider is enough to run the loop with no
// StreamFn at all (agent_loop.go:412, streamWithProvider).
//
// Wire shape, both directions
// ---------------------------
// Request: OpenAI Chat Completions, `stream: true`, plus
// `stream_options.include_usage` so the gateway reports tokens on the final
// frame. Tools are advertised in the OpenAI `tools[].function` shape.
//
// Response: `data: {…}` SSE frames whose `choices[0].delta` carries `content`
// text and/or fragmented `tool_calls` keyed by `index` (the first fragment of
// a call carries `id` and `function.name`; `function.arguments` is delivered in
// pieces and must be concatenated). `data: [DONE]` terminates.
//
// This mirrors backend/gateway.go's parsing deliberately: the two must agree
// on the wire, and having the shapes written down twice is how a divergence
// gets caught. Unlike that parser, Provider emits PiG's full event union —
// including ToolCallStart/Delta/End as the fragments arrive — instead of a
// single assembled ToolCalls slice at the end.
type Provider struct {
	cfg    ProviderConfig
	client *http.Client
}

// ProviderConfig is the knob set for a gateway that speaks the OpenAI
// Chat-Completions dialect.
//
// A zero value is not usable: the endpoint, the credential and the upstream
// model id are configuration, and they come from the user's provider
// configuration. The environment variables are the escape hatch for a
// parity harness.
type ProviderConfig struct {
	// URL is the full chat-completions endpoint.
	URL string
	// APIKey is the bearer credential. Empty sends no credential header.
	APIKey string
	// Model is the wire model id used when a caller passes an empty id to
	// Model().
	Model string
	// ReasoningEffort is the reasoning level the model runs at — the tier
	// of a model that declares tiers. It is sent as the OpenAI dialect's
	// `reasoning_effort` hint.
	//
	// The Anthropic dialect has no such field, so the hint is dropped
	// there: the Messages API spells its thinking budget differently.
	ReasoningEffort string
	// Timeout bounds one request. Zero means 60s; a negative value disables
	// the client timeout so the caller's context is the only bound (this is
	// what the headless path wants — see backend.GatewayTimeoutUnlimited).
	Timeout time.Duration
	// HTTPClient is an optional pre-built client. When set, Timeout is
	// ignored and the caller owns the transport.
	HTTPClient *http.Client
	// Headers are extra request headers, applied after the defaults.
	Headers map[string]string
}

const (
	providerDefaultWait = 60 * time.Second
)

// NewProvider builds a provider. Unlike a PiG-native provider it is not
// model-scoped; use Model() to get a *ai.Model bound to one wire model id.
func NewProvider(cfg ProviderConfig) *Provider {
	if cfg.URL == "" {
		// Mirrors backend.GatewayConfig.withDefaults: a local proxy or dev
		// mirror can be selected without recompiling.
		cfg.URL = strings.TrimSpace(os.Getenv("OPENAGY_GATEWAY_URL"))
	}
	if cfg.APIKey == "" {
		cfg.APIKey = strings.TrimSpace(os.Getenv("OPENAGY_GATEWAY_KEY"))
	}
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		switch {
		case timeout == 0:
			timeout = providerDefaultWait
		case timeout < 0:
			timeout = 0 // http.Client{Timeout: 0} means no timeout
		}
		client = &http.Client{Timeout: timeout}
	}
	// Claim the same identity the clone's gateway client claims. The provider is
	// reached both from an explicit Options.Provider and from the switch
	// agent.nativeProvider performs, and the upstream should not be able to
	// tell which. Set it through cfg.Headers to override.
	if _, ok := cfg.Headers["User-Agent"]; !ok {
		headers := make(map[string]string, len(cfg.Headers)+1)
		for name, value := range cfg.Headers {
			headers[name] = value
		}
		headers["User-Agent"] = backend.GatewayUserAgent
		cfg.Headers = headers
	}
	return &Provider{cfg: cfg, client: client}
}

// ID implements ai.Provider.
func (p *Provider) ID() string { return ProviderID }

// Close implements ai.Provider. The provider owns no persistent resources —
// net/http pools connections per Transport, which it does not create — so this
// only honours the interface.
func (p *Provider) Close() error { return nil }

// Endpoint reports the URL requests are sent to. Useful in logs and tests.
func (p *Provider) Endpoint() string { return p.cfg.URL }

// Model returns a PiG model handle bound to this provider. Every returned
// model shares one HTTP client, so a conversation that switches models does not
// re-dial.
//
// The label is what PiG records as the model id and what a UI displays. It is
// NOT what goes on the wire: the wire model is always ProviderConfig.Model.
// That mirrors backend.GatewayConfig.ExternalModel, which overrides whatever
// model the caller asked for — cmd/wagyu passes Antigravity names like
// "claude-sonnet-4-6" and gateway serves its own ids. An empty label uses the
// configured model.
func (p *Provider) Model(label string) *pi.Model {
	if label == "" {
		label = p.wireModel()
	}
	return &pi.Model{
		Type:         pi.ModelTypeChat,
		ID:           label,
		DisplayName:  label,
		Provider:     &pinnedProvider{owner: p, label: label},
		ProviderMeta: pi.ProviderMetadata{ProviderID: ProviderID, API: ProviderAPI},
		Capabilities: pi.ModelCapabilities{ContextWindow: modelctx.ContextWindowFor(label)},
		Input:        []string{"text"},
	}
}

// WireModel is the model id this provider puts on the wire. It is the value to
// report alongside a response (gateway.GatewaySystemFingerprint takes it) and the value
// to compare against what the caller asked for.
func (p *Provider) WireModel() string { return p.wireModel() }

// wireModel is the configured model, trimmed. It deliberately does not fall
// back to a default: NewProvider already applies one, so an empty value here
// means the caller emptied it, and buildChatRequest should say so rather than
// silently sending a model nobody asked for.
func (p *Provider) wireModel() string {
	return strings.TrimSpace(p.cfg.Model)
}

// Stream implements ai.Provider for the configuration's default model. It
// exists so Provider itself is a valid ai.Provider; callers that want a
// specific model should go through Model(id).
func (p *Provider) Stream(ctx context.Context, transcript pi.TranscriptContext, opts pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
	return p.stream(ctx, transcript, opts)
}

// pinnedProvider is the model-scoped view of a Provider. PiG's
// ai.Provider.Stream does not receive the model (agent_loop.go:412 passes only
// the transcript), so a provider that can serve several models has to be
// pinned per model exactly as PiG's own providers are.
type pinnedProvider struct {
	owner *Provider
	label string
}

func (p *pinnedProvider) ID() string   { return p.owner.ID() }
func (p *pinnedProvider) Close() error { return nil }

func (p *pinnedProvider) Stream(ctx context.Context, transcript pi.TranscriptContext, opts pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
	return p.owner.stream(ctx, transcript, opts)
}

// stream opens the request and returns a live event stream.
//
// A failure that happens *before* the response body starts — a bad request
// body, a non-2xx status — is returned as an error, not encoded in the stream.
// That is deliberate and load-bearing: backend.GatewayClient.Stream does the
// same, returning a *backend.HTTPError, and cmd/wagyu/api_error.go classifies
// the turn's failure with errors.As. A `nil, err` return is fully supported by
// PiG (agent_loop.go:494 synthesizes the error assistant message), and it is
// the only shape that keeps the concrete error type alive — PiG copies only
// err.Error() into the assistant message.
//
// A failure *after* the body started (a bad frame, a read error, cancellation)
// has nowhere else to go, and stays an ErrorEvent.
func (p *Provider) stream(ctx context.Context, transcript pi.TranscriptContext, opts pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
	wireModel := p.wireModel()
	// The assistant message is stamped with the wire model, not the label:
	// that is the model that actually answered.
	partial := newAssistantMessage(&pi.Model{ID: wireModel}, ProviderAPI)

	body, err := buildChatRequest(wireModel, p.cfg.ReasoningEffort, transcript, opts)
	if err != nil {
		return nil, err
	}

	reader, status, err := p.open(ctx, body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		// Read the whole body, exactly as the recovered client does:
		// HTTPError.Message() owns the truncation rule (MaxErrorBody), and a
		// bounded read here would make this provider's user-facing message
		// differ from the clone's for a large error body.
		detail, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("gateway: read error body: %w", readErr)
		}
		return nil, backend.NewHTTPError(status, detail, p.cfg.URL)
	}

	out := pi.NewAssistantMessageEventStream()
	go pumpSSE(ctx, out, partial, reader)
	return out, nil
}

func (p *Provider) open(ctx context.Context, body []byte) (io.ReadCloser, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set("accept", "text/event-stream")
	request.Header.Set("authorization", "Bearer "+p.cfg.APIKey)
	for name, value := range p.cfg.Headers {
		request.Header.Set(name, value)
	}
	// AGY 1.3.1: a connection drop at the HTTP-request layer (dial
	// failure, TLS handshake, broken pipe before the first byte) is
	// retried up to backend.MaxConnectionRetries with the schedule in
	// backend/gemini_api_key_retry.go. A non-retryable failure
	// (4xx/5xx, caller-side cancel) returns the error directly.
	var response *http.Response
	for attempt := 0; attempt <= backend.MaxConnectionRetries; attempt++ {
		if attempt > 0 {
			delay := backend.NextConnectionRetryDelay(attempt - 1)
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(delay):
			}
		}
		response, err = p.client.Do(request)
		if err == nil {
			break
		}
		dropErr := backend.ClassifyTransportError(err, p.cfg.URL)
		if !backend.IsConnectionDropped(dropErr) {
			return nil, 0, err
		}
		if attempt == backend.MaxConnectionRetries {
			return nil, 0, dropErr
		}
	}
	return response.Body, response.StatusCode, nil
}

// firstNonEmpty returns the first of its arguments that is non-empty.
// Package-private; NewProvider uses it for URL/key defaults, and the
// variadic shape is the only reason this isn't a 2-arg helper.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
