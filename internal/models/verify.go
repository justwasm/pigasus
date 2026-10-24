// verify.go — the provider verification probe.
//
// # What it answers
//
// "Does this endpoint accept this credential?" — the question a user asks
// when they type a key into the configuration. The answer is a network
// round trip, so it is a probe, not a local check.
//
// # Two probes
//
// The probe is a **minimal chat completion** when a model is named: it is
// the same request the clone sends later, so a pass means the endpoint,
// the credential *and* the model id all work. Without a model the probe
// is a **model listing** (`GET <base>/models`), which needs no model id
// and costs no tokens.
//
// # The credential is the caller's
//
// VerifyProvider takes the credential as an argument rather than reading
// it from the provider: the panel verifies a key the user has just typed
// and has not saved yet. An environment reference ("$NAME") is expanded
// here, so the caller may pass either form.
//
// The reference for the flow is crush's API-key dialog
// (internal/ui/dialog/api_key_input.go): it probes the endpoint before
// the key is committed and shows the outcome.
package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VerifyTimeout bounds a verification probe when the caller's context has
// no deadline of its own. It is short: a probe that has not answered in
// twenty seconds is not going to.
const VerifyTimeout = 20 * time.Second

// verifyProbeTokens is the output cap of the chat-completion probe. It is
// deliberately tiny: the probe only needs *an* answer, not a good one.
const verifyProbeTokens = 16

// VerifyResult is the outcome of one verification probe.
type VerifyResult struct {
	// OK is true when the endpoint answered and accepted the credential.
	OK bool
	// Status is the HTTP status the endpoint answered with. 0 when the
	// request never completed (a DNS or TLS failure, a timeout).
	Status int
	// Detail is the one-line, user-facing summary of what happened.
	Detail string
	// Err is the underlying error, when there was one.
	Err error
	// Model is the model the probe used, when it made a chat completion.
	Model string
}

// Summary renders the result as one line, with a marker the caller can
// print or paint.
func (r VerifyResult) Summary() string {
	mark := "✗"
	if r.OK {
		mark = "✓"
	}
	detail := r.Detail
	if detail == "" {
		detail = "no answer"
	}
	return mark + " " + detail
}

// VerifyProvider makes one request against the provider's endpoint with
// the given credential and reports what happened.
//
// The credential is expanded when it is an environment reference. The
// model id is optional: without one the probe is a model listing.
func VerifyProvider(ctx context.Context, p ProviderConfig, credential, modelID string) VerifyResult {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, VerifyTimeout)
		defer cancel()
	}
	if strings.TrimSpace(p.Endpoint()) == "" {
		return VerifyResult{
			Detail: "the provider has no endpoint",
			Err:    errors.New("models: the provider has no endpoint"),
		}
	}
	key := ExpandEnvRef(credential)
	wire := strings.TrimSpace(modelID)
	if wire == "" {
		return verifyListing(ctx, p, key)
	}
	return verifyChat(ctx, p, key, wire)
}

// verifyChat probes the endpoint with a minimal chat completion.
func verifyChat(ctx context.Context, p ProviderConfig, key, wire string) VerifyResult {
	m := Model{
		Name:              "verify",
		DisplayName:       "verify",
		Provider:          p.WireType(),
		ProviderID:        strings.TrimSpace(p.ID),
		Type:              p.WireType(),
		ExtraHeaders:      copyHeaders(p.ExtraHeaders),
		APIKey:            key,
		APIURL:            p.Endpoint(),
		ExternalModelName: wire,
		TimeoutSeconds:    p.TimeoutSeconds,
		MaxRetries:        p.MaxRetries,
	}
	prov, err := NewProvider(m)
	if err != nil {
		return VerifyResult{Detail: err.Error(), Err: err, Model: wire}
	}
	resp, err := prov.Chat(ctx, ChatRequest{
		Messages:  []ChatMessage{{Role: "user", Content: "ping"}},
		MaxTokens: verifyProbeTokens,
	})
	if err != nil {
		status := UpstreamStatus(err)
		return VerifyResult{
			Status: status,
			Detail: describeVerifyFailure(status, err),
			Err:    err,
			Model:  wire,
		}
	}
	detail := "the model answered"
	if resp.FinishReason != "" {
		detail = "the model answered (" + resp.FinishReason + ")"
	}
	return VerifyResult{OK: true, Status: http.StatusOK, Detail: detail, Model: wire}
}

// verifyListing probes the endpoint with a model listing. It needs no
// model id, so it is the probe for a provider that has no model yet.
func verifyListing(ctx context.Context, p ProviderConfig, key string) VerifyResult {
	url := listingURL(p)
	if url == "" {
		return VerifyResult{
			Detail: "the provider has no endpoint",
			Err:    errors.New("models: the provider has no endpoint"),
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return VerifyResult{Detail: err.Error(), Err: err}
	}
	switch p.WireType() {
	case ProviderTypeAnthropic:
		if key != "" {
			req.Header.Set("x-api-key", key)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	for name, value := range p.ExtraHeaders {
		req.Header.Set(name, value)
	}
	client := &http.Client{Timeout: providerTimeout(p)}
	resp, err := client.Do(req)
	if err != nil {
		return VerifyResult{
			Detail: "the endpoint could not be reached: " + err.Error(),
			Err:    err,
		}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		detail := "the endpoint answered"
		if n := countListedModels(raw); n > 0 {
			detail = fmt.Sprintf("the endpoint lists %d model(s)", n)
		}
		return VerifyResult{OK: true, Status: resp.StatusCode, Detail: detail}
	}
	err = &UpstreamError{Status: resp.StatusCode, Body: truncate(string(raw), 240)}
	return VerifyResult{
		Status: resp.StatusCode,
		Detail: describeVerifyFailure(resp.StatusCode, err),
		Err:    err,
	}
}

// providerTimeout is the probe's per-request timeout, in seconds.
func providerTimeout(p ProviderConfig) time.Duration {
	if p.TimeoutSeconds > 0 {
		return time.Duration(p.TimeoutSeconds) * time.Second
	}
	return VerifyTimeout
}

// listingURL is the model-listing URL of a provider.
//
// The base carries the version segment for the OpenAI dialect
// ("https://host/v1" → "https://host/v1/models") and not for the Anthropic
// one ("https://host/anthropic" → "https://host/anthropic/v1/models"),
// which is the same convention Endpoint() follows.
func listingURL(p ProviderConfig) string {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		// The endpoint was given verbatim. Recover the base by stripping
		// the protocol's path.
		endpoint := strings.TrimRight(strings.TrimSpace(p.Endpoint()), "/")
		switch p.WireType() {
		case ProviderTypeAnthropic:
			base = strings.TrimSuffix(endpoint, "/v1/messages")
		default:
			base = strings.TrimSuffix(endpoint, "/chat/completions")
		}
	}
	if base == "" {
		return ""
	}
	if p.WireType() == ProviderTypeAnthropic {
		return base + "/v1/models"
	}
	return base + "/models"
}

// countListedModels counts the models in a listing body. It is a hint for
// the message, not a check: a body it cannot parse counts as zero.
func countListedModels(raw []byte) int {
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return 0
	}
	if len(body.Data) > 0 {
		return len(body.Data)
	}
	return len(body.Models)
}

// describeVerifyFailure turns an upstream failure into a one-line,
// user-facing summary. The status decides the wording, because "the
// credential was rejected" and "the endpoint could not be reached" call
// for different fixes.
func describeVerifyFailure(status int, err error) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("the credential was rejected (HTTP %d)", status)
	case http.StatusNotFound:
		return "the endpoint has nothing there (HTTP 404)"
	case http.StatusTooManyRequests:
		return "the endpoint is rate-limiting (HTTP 429)"
	}
	if status > 0 {
		return fmt.Sprintf("the endpoint answered HTTP %d", status)
	}
	if err == nil {
		return "the endpoint could not be reached"
	}
	return "the endpoint could not be reached: " + err.Error()
}
