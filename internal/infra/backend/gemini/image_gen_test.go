package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

const imageResponseJSON = `{"candidates":[{"content":{"role":"model","parts":[{"text":"done"},{"inlineData":{"mimeType":"image/webp","data":"aW1hZ2U="}}]}}]}`

func TestGeminiGenerateImageRequest(t *testing.T) {
	for _, tc := range []struct {
		name, model, project, location, path string
	}{
		{"default", "", "", "", "/v1beta/models/" + geminicode.DefaultImageGenerationModel + ":generateContent"},
		{"explicit", " custom-image ", "", "", "/v1beta/models/custom-image:generateContent"},
		{"vertex default region", "vertex-image", "project", "", "/v1beta/projects/project/locations/us-central1/publishers/google/models/vertex-image:generateContent"},
		{"vertex region", "vertex-image", "project", "europe-west4", "/v1beta/projects/project/locations/europe-west4/publishers/google/models/vertex-image:generateContent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.RequestURI() != tc.path {
					t.Errorf("request = %s %s, want %s", r.Method, r.URL, tc.path)
				}
				for name, want := range map[string]string{"X-Goog-Api-Key": "real-key", "X-Goog-Api-Client": GeminiAPIClientHeader, "User-Agent": "image-test", "Content-Type": "application/json", "X-Test": "custom"} {
					if got := r.Header.Get(name); got != want {
						t.Errorf("header %s = %q, want %q", name, got, want)
					}
				}
				var wire map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
				}
				if len(wire) != 2 || wire["contents"] == nil || wire["generationConfig"] == nil {
					t.Errorf("unexpected fields: %v", wire)
				}
				var config map[string]any
				_ = json.Unmarshal(wire["generationConfig"], &config)
				wantConfig := map[string]any{"responseModalities": []any{"IMAGE"}, "imageConfig": map[string]any{"aspectRatio": "16:9"}}
				if !reflect.DeepEqual(config, wantConfig) {
					t.Errorf("config = %v, want %v", config, wantConfig)
				}
				var contents []geminiContent
				_ = json.Unmarshal(wire["contents"], &contents)
				if len(contents) != 1 || contents[0].Role != "user" || len(contents[0].Parts) != 3 {
					t.Errorf("contents = %+v", contents)
				} else {
					parts := contents[0].Parts
					if parts[0].InlineData == nil || parts[0].InlineData.MIMEType != "image/png" || parts[0].InlineData.Data != "b25l" || parts[1].InlineData == nil || parts[1].InlineData.MIMEType != "image/jpeg" || parts[2].Text != "edit it" {
						t.Errorf("parts = %+v", parts)
					}
				}
				_, _ = io.WriteString(w, imageResponseJSON)
			}))
			defer server.Close()
			client := NewGeminiWithConfig(GeminiConfig{BaseURL: server.URL + "/v1beta/", APIKey: "real-key", Headers: map[string]string{"X-Goog-Api-Key": "wrong", "X-Test": "custom"}, UserAgent: "image-test", Model: "chat-model", VertexProject: tc.project, VertexLocation: tc.location}).(backend.ImageGenerator)
			got, err := client.GenerateImage(context.Background(), backend.ImageGenRequest{Model: tc.model, Prompt: "edit it", AspectRatio: " 16:9 ", InputImages: []backend.ImagePart{{}, {Data: []byte("one")}, {MIMEType: " image/jpeg ", Data: []byte("two")}}})
			if err != nil || string(got.Data) != "image" || got.MIMEType != "image/webp" {
				t.Fatalf("image = %+v, error = %v", got, err)
			}
		})
	}
}

func TestGeminiGenerateImageValidation(t *testing.T) {
	for _, c := range []*GeminiClient{nil, {}, NewGeminiWithConfig(GeminiConfig{}).(*GeminiClient)} {
		if _, err := c.GenerateImage(context.Background(), backend.ImageGenRequest{Prompt: "draw"}); err == nil {
			t.Fatal("uninitialized client accepted")
		}
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = io.WriteString(w, imageResponseJSON) }))
	defer server.Close()
	c := NewGeminiWithConfig(GeminiConfig{BaseURL: server.URL}).(backend.ImageGenerator)
	for _, req := range []backend.ImageGenRequest{{}, {Prompt: " \n\t"}, {InputImages: []backend.ImagePart{{}}}} {
		if _, err := c.GenerateImage(context.Background(), req); err == nil {
			t.Errorf("empty request accepted: %+v", req)
		}
	}
	if calls != 0 {
		t.Fatalf("validation made %d HTTP calls", calls)
	}
	if _, err := c.GenerateImage(context.Background(), backend.ImageGenRequest{InputImages: []backend.ImagePart{{Data: []byte("input")}}}); err != nil {
		t.Fatalf("image-only edit: %v", err)
	}
}

func TestGeminiGenerateImageHTTPError(t *testing.T) {
	for _, status := range []int{302, 400, 401, 403, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"upstream failed"}}`)
			}))
			defer server.Close()
			c := NewGeminiWithConfig(GeminiConfig{BaseURL: server.URL}).(backend.ImageGenerator)
			_, err := c.GenerateImage(backend.ContextWithMaxRetries(context.Background(), 0), backend.ImageGenRequest{Prompt: "draw"})
			var he *apierror.HTTPError
			if !errors.As(err, &he) || he.StatusCode != status || !strings.Contains(string(he.Body), "upstream failed") || he.Header.Get("Retry-After") != "7" || !strings.Contains(he.URL, ":generateContent") || calls != 1 {
				t.Fatalf("error = %#v, calls = %d", err, calls)
			}
		})
	}
}

func TestGeminiGenerateImageResponseErrors(t *testing.T) {
	for _, tc := range []struct{ body, want string }{{`invalid`, "decode image response"}, {`null`, "no response"}, {`{}`, "no response"}, {`{"responseId":"id","candidates":[]}`, "no image generated"}, {`{"candidates":[{"content":{"parts":[{"inlineData":{"data":"!"}}]}}]}`, "decode generated image"}} {
		t.Run(tc.want+tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			defer server.Close()
			c := NewGeminiWithConfig(GeminiConfig{BaseURL: server.URL}).(backend.ImageGenerator)
			if _, err := c.GenerateImage(context.Background(), backend.ImageGenRequest{Prompt: "draw"}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}

type imageRoundTripper func(*http.Request) (*http.Response, error)

func (f imageRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type brokenImageBody struct{}

func (brokenImageBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenImageBody) Close() error             { return nil }

func TestGeminiGenerateImageRetriesTransportAndBodyErrors(t *testing.T) {
	initial, maximum := retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval = time.Millisecond, time.Millisecond
	defer func() { retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval = initial, maximum }()
	for _, failure := range []string{"transport", "body", "capacity", "status"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			var bodies []string
			httpClient := &http.Client{Transport: imageRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				body, _ := io.ReadAll(r.Body)
				bodies = append(bodies, string(body))
				if calls == 1 {
					if failure == "transport" {
						return nil, io.ErrUnexpectedEOF
					}
					if failure == "body" {
						return &http.Response{StatusCode: 200, Body: brokenImageBody{}, Header: make(http.Header)}, nil
					}
					if failure == "capacity" {
						return &http.Response{
							StatusCode: http.StatusTooManyRequests,
							Body:       io.NopCloser(strings.NewReader(`{"error":{"status":"RESOURCE_EXHAUSTED","message":"No capacity available, try again later"}}`)),
							Header:     http.Header{"Retry-After": []string{"1"}},
						}, nil
					}
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(imageResponseJSON)), Header: make(http.Header)}, nil
			})}
			c := NewGeminiWithConfig(GeminiConfig{BaseURL: "http://image.test", HTTPClient: httpClient}).(backend.ImageGenerator)
			out, err := c.GenerateImage(backend.ContextWithMaxRetries(context.Background(), 1), backend.ImageGenRequest{Prompt: "draw"})
			if err != nil || string(out.Data) != "image" || calls != 2 || bodies[0] != bodies[1] {
				t.Fatalf("image = %+v, error = %v; calls = %d", out, err, calls)
			}
		})
	}
}

func TestGeminiMultimodalToolResponseWire(t *testing.T) {
	c := NewGeminiWithConfig(GeminiConfig{BaseURL: "http://unused"}).(*GeminiClient)
	for _, failed := range []bool{false, true} {
		wire := c.buildRequest(backend.StreamRequest{Messages: []backend.Message{
			{Role: "assistant", ToolCalls: []backend.ToolCall{{ID: "call", Name: "view_file", Arguments: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolCallID: "call", Content: "image result", IsError: failed, Images: []backend.ImagePart{{}, {MIMEType: "image/png", Data: []byte("media")}}},
		}})
		var response *geminiFunctionResponse
		for _, content := range wire.Contents {
			for _, part := range content.Parts {
				if part.FunctionResponse != nil {
					if content.Role != "model" || part.InlineData != nil {
						t.Fatalf("invalid tool result nesting: %+v", content)
					}
					response = part.FunctionResponse
				}
			}
		}
		if response == nil || response.Name != "view_file" || len(response.Parts) != 1 {
			t.Fatalf("response = %+v", response)
		}
		key := "output"
		if failed {
			key = "error"
		}
		raw, _ := json.Marshal(response)
		want := `{"id":"call","name":"view_file","response":{"` + key + `":"image result"},"parts":[{"inlineData":{"mimeType":"image/png","data":"bWVkaWE="}}]}`
		if string(raw) != want {
			t.Errorf("wire = %s, want %s", raw, want)
		}
	}
}

func TestGeminiGenerateImageDoesNotRetryExhaustedQuota(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"individual quota exceeded"}}`)
	}))
	defer server.Close()
	c := NewGeminiWithConfig(GeminiConfig{BaseURL: server.URL}).(backend.ImageGenerator)
	_, err := c.GenerateImage(backend.ContextWithMaxRetries(context.Background(), 3), backend.ImageGenRequest{Prompt: "draw"})
	var he *apierror.HTTPError
	if !errors.As(err, &he) || he.StatusCode != http.StatusTooManyRequests || calls != 1 {
		t.Fatalf("quota error = %#v, calls = %d, want one non-retried request", err, calls)
	}
}
