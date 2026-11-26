package cloudcode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

const cloudCodeImageResponse = `{"response":{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}}`

func TestCloudCodeGenerateImageDiscoveryAndCacheIsolation(t *testing.T) {
	discoveries, generations := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var wire geminicode.GenerateContentRequest
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case geminicode.FetchAvailableModelsPath:
			discoveries++
			_ = json.NewEncoder(w).Encode(geminicode.FetchAvailableModelsResponse{ImageGenerationModelIDs: []string{" ", " image-for-" + r.Header.Get("Authorization") + " "}})
		case geminicode.GenerateContentPath:
			generations++
			if wire.Model != "explicit" && wire.Model != "image-for-"+r.Header.Get("Authorization") {
				t.Errorf("model = %q, authorization = %q", wire.Model, r.Header.Get("Authorization"))
			}
			if wire.RequestType != "image_gen" || wire.Request == nil || wire.Request.GenerationConfig.ImageConfig.AspectRatio != "16:9" || wire.Project == "" || wire.RequestID == "" {
				t.Errorf("wire = %+v", wire)
			}
			_, _ = io.WriteString(w, cloudCodeImageResponse)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	token := "account-a"
	cfg := CloudCodeConfig{Endpoint: server.URL, Project: "project-a", HTTPClient: server.Client(), TokenFunc: func(context.Context) (string, error) { return token, nil }}
	c := NewCloudCodeWithConfig(cfg)
	generate := func(c *CloudCodeClient, model string) {
		t.Helper()
		out, err := c.GenerateImage(context.Background(), backend.ImageGenRequest{Model: model, Prompt: "draw", AspectRatio: "16:9"})
		if err != nil || string(out.Data) != "image" || out.MIMEType != "image/png" {
			t.Fatalf("image = %+v, error = %v", out, err)
		}
	}
	generate(c, "")
	generate(c, "")
	if discoveries != 1 {
		t.Fatalf("discovery calls = %d", discoveries)
	}
	generate(c, "explicit")
	if discoveries != 1 {
		t.Fatal("explicit model performed discovery")
	}
	token = "account-b"
	generate(c, "")
	cfg.Project = "project-b"
	generate(NewCloudCodeWithConfig(cfg), "")
	if discoveries != 3 || generations != 5 {
		t.Fatalf("discovery/generation calls = %d/%d", discoveries, generations)
	}
}

func TestCloudCodeImageDiscoveryFallbackDoesNotPoisonCache(t *testing.T) {
	for _, response := range []string{`{}`, `{"imageGenerationModelIds":[" "]}`, `unavailable`} {
		t.Run(response, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					if response == "unavailable" {
						w.WriteHeader(503)
					}
					_, _ = io.WriteString(w, response)
					return
				}
				_, _ = io.WriteString(w, `{"imageGenerationModelIds":["available"]}`)
			}))
			defer server.Close()
			c := NewCloudCodeWithConfig(CloudCodeConfig{Endpoint: server.URL, AccessToken: "test", HTTPClient: server.Client()})
			if got := c.ImageGenerationModelID(context.Background()); got != geminicode.DefaultImageGenerationModel {
				t.Fatalf("fallback = %q", got)
			}
			if got := c.ImageGenerationModelID(context.Background()); got != "available" {
				t.Fatalf("second lookup = %q", got)
			}
			if got := c.ImageGenerationModelID(context.Background()); got != "available" || calls != 2 {
				t.Fatalf("cached lookup = %q, calls = %d", got, calls)
			}
		})
	}
}

func TestCloudCodeGenerateImageValidation(t *testing.T) {
	for _, c := range []*CloudCodeClient{nil, {}} {
		if _, err := c.GenerateImage(context.Background(), backend.ImageGenRequest{Prompt: "draw"}); err == nil {
			t.Fatal("uninitialized client returned nil error")
		}
		if got := c.ImageGenerationModelID(context.Background()); got != geminicode.DefaultImageGenerationModel {
			t.Fatalf("nil discovery = %q", got)
		}
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, cloudCodeImageResponse)
	}))
	defer server.Close()
	c := NewCloudCodeWithConfig(CloudCodeConfig{Endpoint: server.URL, AccessToken: "test", HTTPClient: server.Client()})
	for _, req := range []backend.ImageGenRequest{{}, {Prompt: " \n"}, {InputImages: []backend.ImagePart{{}}}} {
		if _, err := c.GenerateImage(context.Background(), req); err == nil {
			t.Errorf("empty request accepted: %+v", req)
		}
	}
	if calls != 0 {
		t.Fatalf("validation made %d HTTP calls", calls)
	}
	if _, err := c.GenerateImage(context.Background(), backend.ImageGenRequest{Model: "image-model", InputImages: []backend.ImagePart{{Data: []byte("input")}}}); err != nil {
		t.Fatalf("image-only edit: %v", err)
	}
}

func TestCloudCodeGenerateImagePreservesHTTPError(t *testing.T) {
	for _, status := range []int{302, 400, 401, 403, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "9")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"upstream image failure"}}`)
			}))
			defer server.Close()
			c := NewCloudCodeWithConfig(CloudCodeConfig{Endpoint: server.URL, AccessToken: "test", HTTPClient: server.Client()})
			_, err := c.GenerateImage(backend.ContextWithMaxRetries(context.Background(), 0), backend.ImageGenRequest{Model: "image-model", Prompt: "draw"})
			var he *apierror.HTTPError
			if !errors.As(err, &he) || he.StatusCode != status || !strings.Contains(string(he.Body), "upstream image failure") || he.Header.Get("Retry-After") != "9" || !strings.HasSuffix(he.URL, geminicode.GenerateContentPath) || calls != 1 {
				t.Fatalf("error = %#v, calls = %d", err, calls)
			}
		})
	}
}

func TestCloudCodeGenerateImageRetriesSameRequest(t *testing.T) {
	initial, maximum := retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval = time.Millisecond, time.Millisecond
	defer func() {
		retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval = initial, maximum
	}()
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var bodies []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				bodies = append(bodies, string(body))
				if len(bodies) == 1 {
					// Capacity/rate limits are transient, unlike exhausted quota.
					w.Header().Set("Retry-After", "1")
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"No capacity available, try again later"}}`)
					return
				}
				_, _ = io.WriteString(w, cloudCodeImageResponse)
			}))
			defer server.Close()
			c := NewCloudCodeWithConfig(CloudCodeConfig{Endpoint: server.URL, AccessToken: "test", HTTPClient: server.Client()})
			out, err := c.GenerateImage(backend.ContextWithMaxRetries(context.Background(), 1), backend.ImageGenRequest{Model: "image-model", Prompt: "draw"})
			if err != nil || string(out.Data) != "image" || len(bodies) != 2 || bodies[0] != bodies[1] {
				t.Fatalf("out = %+v, error = %v, bodies = %v", out, err, bodies)
			}
		})
	}
}

type imageRoundTripper func(*http.Request) (*http.Response, error)

func (f imageRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCloudCodeImageSandboxFailover(t *testing.T) {
	for _, failure := range []string{"503", "network"} {
		for _, sandboxStatus := range []int{200, 401} {
			t.Run(failure+http.StatusText(sandboxStatus), func(t *testing.T) {
				primary, mirror := 0, 0
				var firstBody string
				httpClient := &http.Client{Transport: imageRoundTripper(func(r *http.Request) (*http.Response, error) {
					body, _ := io.ReadAll(r.Body)
					if r.URL.Host == strings.TrimPrefix(endpoints.Current().CloudCode, "https://") {
						primary++
						firstBody = string(body)
						if failure == "network" {
							return nil, io.ErrUnexpectedEOF
						}
						return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("primary unavailable")), Header: make(http.Header)}, nil
					}
					mirror++
					if r.URL.Host != strings.TrimPrefix(endpoints.Current().CloudCodeSandbox, "https://") || firstBody != string(body) {
						t.Errorf("mirror request = %s %s", r.URL, body)
					}
					return &http.Response{StatusCode: sandboxStatus, Body: io.NopCloser(strings.NewReader(cloudCodeImageResponse)), Header: make(http.Header)}, nil
				})}
				c := NewCloudCodeWithConfig(CloudCodeConfig{Endpoint: endpoints.Current().CloudCode, AccessToken: "test", HTTPClient: httpClient})
				out, err := c.GenerateImage(backend.ContextWithMaxRetries(context.Background(), 0), backend.ImageGenRequest{Model: "image-model", Prompt: "draw"})
				if primary != 1 || mirror != 1 {
					t.Fatalf("calls primary/mirror = %d/%d", primary, mirror)
				}
				if sandboxStatus == 200 {
					if err != nil || string(out.Data) != "image" {
						t.Fatalf("result = %+v, error = %v", out, err)
					}
				} else {
					var he *apierror.HTTPError
					if !errors.As(err, &he) || he.StatusCode != 401 || !strings.HasPrefix(he.URL, endpoints.Current().CloudCodeSandbox) {
						t.Fatalf("sandbox auth error = %#v", err)
					}
				}
			})
		}
	}
}

func TestCloudCodeMultimodalToolResponseReachesWire(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req geminicode.GenerateContentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		var response *geminicode.FunctionResponse
		for _, content := range req.Request.Contents {
			for _, part := range content.Parts {
				if part.FunctionResponse != nil {
					response = part.FunctionResponse
				}
			}
		}
		if response == nil || response.Name != "view_file" || len(response.Parts) != 1 || response.Parts[0].InlineData.Data != "bWVkaWE=" {
			t.Errorf("function response = %+v", response)
		}
		_, _ = io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}}\n\n")
	}))
	defer server.Close()
	c := NewCloudCodeWithConfig(CloudCodeConfig{Endpoint: server.URL, AccessToken: "test", HTTPClient: server.Client(), Model: "gemini-test"})
	ch, err := c.Stream(context.Background(), backend.StreamRequest{Messages: []backend.Message{
		{Role: "assistant", ToolCalls: []backend.ToolCall{{ID: "call", Name: "view_file", Arguments: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolCallID: "call", Content: "image result", Images: []backend.ImagePart{{MIMEType: "image/png", Data: []byte("media")}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
	}
}

func TestCloudCodeGenerateImageDoesNotRetryExhaustedQuota(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"individual quota exceeded"}}`)
	}))
	defer server.Close()
	c := NewCloudCodeWithConfig(CloudCodeConfig{Endpoint: server.URL, AccessToken: "test", HTTPClient: server.Client()})
	_, err := c.GenerateImage(backend.ContextWithMaxRetries(context.Background(), 3), backend.ImageGenRequest{Model: "image-model", Prompt: "draw"})
	var he *apierror.HTTPError
	if !errors.As(err, &he) || he.StatusCode != http.StatusTooManyRequests || calls != 1 {
		t.Fatalf("quota error = %#v, calls = %d, want one non-retried request", err, calls)
	}
}
