package agentturn_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/filesystem"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/app/tools/web"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
)

// This crosses the real tool runner, actor context, backend adapter and HTTP
// wire. A readable path alone is not sufficient: the next model request must
// actually contain the generated image in the view_file function response.
func TestImageGenerationInspectionHTTPRoundTrip(t *testing.T) {
	var encoded bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	imageBytes := encoded.Bytes()
	home := t.TempDir()
	workspace := t.TempDir()
	const conversationID = "parent-image-conversation"
	var mu sync.Mutex
	var streams, discoveries, generations int
	var savedPath string
	var inspected bool
	var serverErrors []string
	recordError := func(format string, args ...any) { serverErrors = append(serverErrors, fmt.Sprintf(format, args...)) }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
			recordError("unexpected request %s %s", r.Method, r.URL)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if r.URL.Path == geminicode.FetchAvailableModelsPath {
			discoveries++
			_, _ = fmt.Fprint(w, `{"imageGenerationModelIds":["discovered-image-model"]}`)
			return
		}
		var req geminicode.GenerateContentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			recordError("decode request: %v", err)
			http.Error(w, "bad JSON", http.StatusBadRequest)
			return
		}
		if r.URL.Path == geminicode.GenerateContentPath {
			generations++
			if req.RequestType != "image_gen" || req.Model != "discovered-image-model" || req.Inner().GenerationConfig == nil || req.Inner().GenerationConfig.ImageConfig == nil || req.Inner().GenerationConfig.ImageConfig.AspectRatio != "16:9" {
				recordError("image request envelope: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(geminicode.CloudCodeResponse{Response: geminicode.GenerateContentResponse{Candidates: []geminicode.Candidate{{Content: geminicode.Content{Role: "model", Parts: []geminicode.Part{{InlineData: &geminicode.InlineData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(imageBytes)}}}}}}}})
			return
		}
		if r.URL.Path != "/v1internal:streamGenerateContent" {
			recordError("unexpected endpoint %s", r.URL)
			http.NotFound(w, r)
			return
		}
		streams++
		part := geminicode.Part{}
		switch streams {
		case 1:
			part.FunctionCall = &geminicode.FunctionCall{ID: "make-image", Name: "generate_image", Args: json.RawMessage(`{"Prompt":"A red square on transparent background","ImageName":"Red Square","AspectRatio":"16:9"}`)}
		case 2:
			for _, content := range req.Inner().Contents {
				for _, p := range content.Parts {
					if p.FunctionResponse == nil || p.FunctionResponse.Name != "generate_image" {
						continue
					}
					response, _ := p.FunctionResponse.Response.(map[string]any)
					text, _ := response["output"].(string)
					const marker = "Generated image is saved at "
					if at := strings.Index(text, marker); at >= 0 {
						savedPath = strings.TrimSuffix(strings.TrimSpace(text[at+len(marker):]), ".")
					}
				}
			}
			if savedPath == "" {
				recordError("generated path missing from tool response")
			}
			args, _ := json.Marshal(map[string]string{"Path": savedPath})
			part.FunctionCall = &geminicode.FunctionCall{ID: "inspect-image", Name: "view_file", Args: args}
		case 3:
			for _, content := range req.Inner().Contents {
				for _, p := range content.Parts {
					if p.FunctionResponse == nil || p.FunctionResponse.Name != "view_file" {
						continue
					}
					for _, media := range p.FunctionResponse.Parts {
						if media.InlineData == nil {
							continue
						}
						data, err := base64.StdEncoding.DecodeString(media.InlineData.Data)
						if err == nil && media.InlineData.MIMEType == "image/png" && bytes.Equal(data, imageBytes) {
							inspected = true
						}
					}
				}
			}
			if !inspected {
				recordError("view_file returned no matching function-response image")
			}
			part.Text = "The generated image has been visually inspected."
		default:
			recordError("unexpected model continuation %d", streams)
			part.Text = "Stop."
		}
		w.Header().Set("Content-Type", "text/event-stream")
		response := geminicode.CloudCodeResponse{Response: geminicode.GenerateContentResponse{Candidates: []geminicode.Candidate{{Content: geminicode.Content{Role: "model", Parts: []geminicode.Part{part}}, FinishReason: "STOP"}}}}
		payload, _ := json.Marshal(response)
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
	}))
	defer server.Close()

	client := cloudcode.NewCloudCodeWithConfig(cloudcode.CloudCodeConfig{Endpoint: server.URL, Model: "test-chat", Project: "test-project", AccessToken: "test-token", HTTPClient: server.Client()})
	registry := tools.NewRegistry()
	registry.Register(&web.GenerateImage{Workspace: workspace, Home: home, ConversationID: conversationID, Provider: surface.ImageProviderFromBackend(client)})
	registry.Register(&filesystem.ViewFile{Workspace: workspace})
	policy := &permissions.Policy{SkipPermissions: true}
	runner := &toolrunner.PermissionRunner{Inner: &toolrunner.RegistryRunner{Reg: registry}, Policy: policy}
	engine := agentturn.NewLocal(func(context.Context, string) (agentturn.Env, func()) {
		return agentturn.Env{Backend: client, Model: "test-chat", Tools: toolrunner.ToolDeclsFromRegistry(registry), ToolRunner: runner, Policy: policy}, nil
	})
	defer engine.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	frames, err := engine.Turn(ctx, agentturn.Turn{History: []agentlink.Message{{Role: "user", Content: "Generate and inspect a red square."}}})
	if err != nil {
		t.Fatal(err)
	}
	var answer strings.Builder
	for frame := range frames {
		if frame.Kind == agentlink.KindText {
			answer.WriteString(frame.Text)
		}
	}
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, err := range serverErrors {
		t.Error(err)
	}
	if streams != 3 || discoveries != 1 || generations != 1 || !inspected {
		t.Fatalf("streams=%d discoveries=%d generations=%d inspected=%t", streams, discoveries, generations, inspected)
	}
	if !strings.Contains(answer.String(), "visually inspected") {
		t.Errorf("answer = %q", answer.String())
	}
	if filepath.Dir(savedPath) != filepath.Join(home, "brain", conversationID) || !strings.HasPrefix(filepath.Base(savedPath), "red_square_") || filepath.Ext(savedPath) != ".png" {
		t.Fatalf("generated path is not a parent conversation artifact: %q", savedPath)
	}
	data, err := os.ReadFile(savedPath)
	if err != nil || !bytes.Equal(data, imageBytes) {
		t.Fatalf("saved bytes differ: %v", err)
	}
}
