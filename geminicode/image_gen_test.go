package geminicode

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
)

const generatedImageJSON = `{"candidates":[{"content":{"role":"model","parts":[{"text":"done"},{"inlineData":{"mimeType":"image/jpeg","data":"aW1hZ2U="}}]}}],"usageMetadata":{"totalTokenCount":9}}`

func TestBuildImageGenerationRequest(t *testing.T) {
	req, err := BuildImageGenerationRequest(ImageGenerationRequest{
		Prompt: "edit image", InputMedia: []TurnImage{{}, {Data: []byte("input")}}, RequestID: "fixed-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"project":"aicode-consumers","requestId":"fixed-id","request":{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"aW5wdXQ="}},{"text":"edit image"}]}],"generationConfig":{"candidateCount":1,"imageConfig":{"aspectRatio":"1:1"}}},"model":"gemini-3.1-flash-image","userAgent":"antigravity","requestType":"image_gen"}`
	if string(raw) != want {
		t.Fatalf("wire = %s\nwant   %s", raw, want)
	}
	custom, err := BuildImageGenerationRequest(ImageGenerationRequest{
		Model: " custom ", Project: " project ", AspectRatio: " 16:9 ",
		InputMedia: []TurnImage{{MIMEType: " image/webp ", Data: []byte("input")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if custom.Model != "custom" || custom.Project != "project" || custom.Request.GenerationConfig.ImageConfig.AspectRatio != "16:9" || custom.Request.Contents[0].Parts[0].InlineData.MIMEType != "image/webp" {
		t.Fatalf("overrides = %+v", custom)
	}
	if !regexp.MustCompile(`^image_gen/[0-9a-f-]{36}/[0-9]+/[0-9a-f-]{36}/1$`).MatchString(custom.RequestID) {
		t.Fatalf("request ID = %q", custom.RequestID)
	}
	if custom.RequestID == NewImageGenRequestID() {
		t.Fatal("request IDs reused")
	}
	for _, req := range []ImageGenerationRequest{{}, {Prompt: " \t\n"}, {InputMedia: []TurnImage{{MIMEType: "image/png"}}}} {
		if _, err := BuildImageGenerationRequest(req); err == nil {
			t.Fatalf("empty request accepted: %+v", req)
		}
	}
}

func TestExtractGeneratedImage(t *testing.T) {
	var image GenerateContentResponse
	if err := json.Unmarshal([]byte(generatedImageJSON), &image); err != nil {
		t.Fatal(err)
	}
	got, err := ExtractGeneratedImage(&image)
	if err != nil || string(got.Data) != "image" || got.MIMEType != "image/jpeg" || got.Usage != image.UsageMetadata {
		t.Fatalf("result = %+v, err = %v", got, err)
	}
	image.Candidates[0].Content.Parts[1].InlineData.MIMEType = ""
	got, err = ExtractGeneratedImage(&image)
	if err != nil || got.MIMEType != "image/png" {
		t.Fatalf("default MIME = %+v, %v", got, err)
	}
	for _, tc := range []struct {
		name string
		resp *GenerateContentResponse
		want string
	}{
		{"nil", nil, "no response"},
		{"empty", &GenerateContentResponse{}, "no image generated"},
		{"text only", &GenerateContentResponse{Candidates: []Candidate{{Content: Content{Parts: []Part{{Text: "blocked"}}}}}}, "no image generated"},
		{"invalid base64", &GenerateContentResponse{Candidates: []Candidate{{Content: Content{Parts: []Part{{InlineData: &InlineData{Data: "!"}}}}}}}, "decode generated image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ExtractGeneratedImage(tc.resp); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestClientGenerateImageResponseShapes(t *testing.T) {
	for _, tc := range []struct {
		name, response, wantErr string
	}{
		{"bare", generatedImageJSON, ""},
		{"wrapped", `{"response":` + generatedImageJSON + `,"traceId":"trace"}`, ""},
		{"wrapped empty", `{"response":{}}`, "no image generated"},
		{"wrapped null", `{"response":null}`, "no response"},
		{"null", `null`, "no response"},
		{"malformed", `{"response":`, "decode generateContent"},
		{"malformed wrapped", `{"response":"bad"}`, "decode generateContent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.RequestURI() != GenerateContentPath || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("User-Agent") != "test-agent" {
					t.Errorf("unexpected request: %s %s %v", r.Method, r.URL, r.Header)
				}
				var req GenerateContentRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if req.Model != DefaultImageGenerationModel || req.Project != "configured-project" || req.RequestType != "image_gen" {
					t.Errorf("envelope = %+v", req)
				}
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()
			c := New(Config{Endpoint: "http://unused.invalid", Project: "configured-project", AccessToken: "test-token", UserAgent: "test-agent", HTTPClient: server.Client()})
			got, err := c.GenerateImageAt(context.Background(), ImageGenerationRequest{Prompt: "draw"}, server.URL)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %s", err, tc.wantErr)
				}
			} else if err != nil || string(got.Data) != "image" || got.MIMEType != "image/jpeg" || got.Usage.TotalTokenCount != 9 {
				t.Fatalf("image = %+v, err = %v", got, err)
			}
		})
	}
}

func TestClientUnaryUnauthorized(t *testing.T) {
	for _, mode := range []string{"same token", "refresh error", "new token"} {
		t.Run(mode, func(t *testing.T) {
			var calls, tokens int
			var bodies []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				bodies = append(bodies, string(raw))
				if calls == 2 {
					if r.Header.Get("Authorization") != "Bearer fresh" {
						t.Error("refresh token not used")
					}
					_, _ = io.WriteString(w, generatedImageJSON)
					return
				}
				w.Header().Set("X-Error-Detail", "kept")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":{"message":"original unauthorized"}}`)
			}))
			defer server.Close()
			c := New(Config{Endpoint: server.URL, HTTPClient: server.Client(), TokenFunc: func(context.Context) (string, error) {
				tokens++
				if tokens > 1 {
					if mode == "refresh error" {
						return "", errors.New("refresh unavailable")
					}
					if mode == "new token" {
						return "fresh", nil
					}
				}
				return "old", nil
			}})
			_, err := c.GenerateImage(context.Background(), ImageGenerationRequest{Prompt: "draw"})
			if mode == "new token" {
				if err != nil || calls != 2 || bodies[0] != bodies[1] {
					t.Fatalf("retry calls = %d, err = %v, bodies = %v", calls, err, bodies)
				}
			} else {
				var he *HTTPError
				if !errors.As(err, &he) || he.StatusCode != 401 || !strings.Contains(string(he.Body), "original unauthorized") || he.Header.Get("X-Error-Detail") != "kept" || calls != 1 {
					t.Fatalf("error = %#v, calls = %d", err, calls)
				}
			}
		})
	}
}

func TestClientImageModelDiscoveryCacheScope(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		if r.URL.RequestURI() != FetchAvailableModelsPath {
			t.Errorf("path = %s", r.URL)
		}
		var req FetchAvailableModelsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(FetchAvailableModelsResponse{ImageGenerationModelIDs: []string{req.Project + "/" + r.Header.Get("Authorization")}})
	}))
	defer server.Close()
	token := "one"
	c := New(Config{Endpoint: server.URL, Project: "project", HTTPClient: server.Client(), TokenFunc: func(context.Context) (string, error) { return token, nil }})
	fetch := func(project, endpoint, want string) *FetchAvailableModelsResponse {
		t.Helper()
		out, err := c.FetchAvailableModelsAt(context.Background(), project, endpoint)
		if err != nil || len(out.ImageGenerationModelIDs) != 1 || out.ImageGenerationModelIDs[0] != want {
			t.Fatalf("discovery = %+v, %v; want %q", out, err, want)
		}
		return out
	}
	first := fetch("", server.URL, "project/Bearer one")
	first.ImageGenerationModelIDs[0] = "mutated"
	fetch(" project ", server.URL+"/", "project/Bearer one")
	if calls != 1 {
		t.Fatalf("cached discovery made %d calls", calls)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); fetch("", server.URL, "project/Bearer one") }()
	}
	wg.Wait()
	token = "two"
	fetch("", server.URL, "project/Bearer two")
	fetch("other", server.URL, "other/Bearer two")
	c2 := New(Config{Endpoint: server.URL, Project: "other", AccessToken: "two", HTTPClient: server.Client()})
	if _, err := c2.FetchAvailableModels(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("cross-token/project/client discovery calls = %d, want 4", calls)
	}
	mirror := httptest.NewServer(server.Config.Handler)
	defer mirror.Close()
	fetch("other", mirror.URL, "other/Bearer two")
	if calls != 5 {
		t.Fatalf("cross-endpoint discovery calls = %d, want 5", calls)
	}
}

func TestClientImageModelDiscoveryDoesNotCacheFailureOrEmpty(t *testing.T) {
	for _, initial := range []string{`{}`, `{"imageGenerationModelIds":[" ",""]}`, `invalid`, `failure`} {
		t.Run(initial, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					if initial == "failure" {
						w.WriteHeader(503)
					}
					_, _ = io.WriteString(w, initial)
					return
				}
				_, _ = io.WriteString(w, `{"imageGenerationModelIds":["available"]}`)
			}))
			defer server.Close()
			c := New(Config{Endpoint: server.URL, AccessToken: "test", HTTPClient: server.Client()})
			_, _ = c.FetchAvailableModels(context.Background(), "")
			out, err := c.FetchAvailableModels(context.Background(), "")
			if err != nil || !reflect.DeepEqual(out.ImageGenerationModelIDs, []string{"available"}) || calls != 2 {
				t.Fatalf("discovery = %+v, %v; calls = %d", out, err, calls)
			}
		})
	}
}

func TestMultimodalToolResponseWire(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			req := BuildRequest(Turn{Model: "gemini-test", Messages: []TurnMessage{
				{Role: "assistant", ToolCalls: []TurnToolCall{{ID: "call-1", Name: "view_file", Arguments: json.RawMessage(`{}`)}}},
				{Role: "tool", ToolCallID: "call-1", Content: "tool result", IsError: failed, Images: []TurnImage{{}, {MIMEType: "image/png", Data: []byte("media")}}},
			}})
			var found *FunctionResponse
			for _, content := range req.Request.Contents {
				for _, part := range content.Parts {
					if part.FunctionResponse != nil {
						if content.Role != "model" || part.InlineData != nil {
							t.Errorf("media outside functionResponse: %+v", content)
						}
						found = part.FunctionResponse
					}
				}
			}
			if found == nil || found.ID != "call-1" || found.Name != "view_file" || len(found.Parts) != 1 {
				t.Fatalf("function response = %+v", found)
			}
			key := "output"
			if failed {
				key = "error"
			}
			raw, _ := json.Marshal(found)
			want := `{"id":"call-1","name":"view_file","response":{"` + key + `":"tool result"},"parts":[{"inlineData":{"mimeType":"image/png","data":"` + base64.StdEncoding.EncodeToString([]byte("media")) + `"}}]}`
			if string(raw) != want {
				t.Fatalf("wire = %s, want %s", raw, want)
			}
		})
	}
}

func TestClientImageModelCacheUsesRefreshedCredential(t *testing.T) {
	requests, tokens := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") == "Bearer old" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, "expired")
			return
		}
		_, _ = io.WriteString(w, `{"imageGenerationModelIds":["fresh-model"]}`)
	}))
	defer server.Close()
	c := New(Config{Endpoint: server.URL, HTTPClient: server.Client(), TokenFunc: func(context.Context) (string, error) {
		tokens++
		if tokens == 1 {
			return "old", nil
		}
		return "fresh", nil
	}})
	for range 2 {
		out, err := c.FetchAvailableModels(context.Background(), "")
		if err != nil || out.ImageGenerationModelIDs[0] != "fresh-model" {
			t.Fatalf("discovery = %+v, error = %v", out, err)
		}
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want initial 401 + refresh; final lookup must be cached", requests)
	}
}

type unaryRoundTripper func(*http.Request) (*http.Response, error)

func (f unaryRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientUnaryHonorsTimeoutAndCancellation(t *testing.T) {
	sentinel := errors.New("missing credential")
	c := New(Config{Endpoint: "http://image.test", TokenFunc: func(context.Context) (string, error) { return "", sentinel }})
	if _, err := c.GenerateImage(context.Background(), ImageGenerationRequest{Prompt: "draw"}); !errors.Is(err, sentinel) {
		t.Fatalf("credential error = %v", err)
	}
	for _, discovery := range []bool{false, true} {
		c = New(Config{Endpoint: "http://image.test", AccessToken: "test", TimeoutSeconds: 1, HTTPClient: &http.Client{Transport: unaryRoundTripper(func(r *http.Request) (*http.Response, error) {
			if _, ok := r.Context().Deadline(); !ok {
				t.Error("configured timeout missing")
			}
			return nil, context.Canceled
		})}})
		var err error
		if discovery {
			_, err = c.FetchAvailableModels(context.Background(), "")
		} else {
			_, err = c.GenerateImage(context.Background(), ImageGenerationRequest{Prompt: "draw"})
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	}
}
