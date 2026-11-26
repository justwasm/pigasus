package subagenttools

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
	builtinsubagent "github.com/justwasm/pigasus/internal/app/subagent/builtin"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/web"
)

func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{G: 200, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestInvokeSubagentImageGenerator_LLMDrivenWithInspection(t *testing.T) {
	ws := t.TempDir()
	agentDir := t.TempDir()
	const parentConvID = "parent-conv-img"
	pngBytes := testPNGBytes(t)

	reg := NewRegistry(parentConvID)
	var inboxMu sync.Mutex
	var delivered []string
	reg.SetNotify(func(_, content string) {
		inboxMu.Lock()
		delivered = append(delivered, content)
		inboxMu.Unlock()
	})

	var generatedPath string
	var sawInspectedImage bool
	var sawTools []string
	var sawSystemPrompt string

	provider := actor.NewMockProvider()
	// Turn 1: inspect tools & system prompt, call generate_image.
	provider.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		sawSystemPrompt = req.SystemPrompt
		for _, td := range req.Tools {
			sawTools = append(sawTools, td.Name)
		}
		return []actor.StreamChunk{{
			ToolCalls: []actor.ToolCall{{
				ID:    "call-gen",
				Name:  "generate_image",
				Input: `{"Prompt":"A green pixel icon","ImageName":"GreenIcon","AspectRatio":"1:1"}`,
			}},
			FinishReason: "tool_calls",
		}}, nil
	})
	// Turn 2: extract saved path from generate_image result, call view_file.
	provider.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		for _, m := range req.Messages {
			if m.Role == actor.RoleTool && m.ToolName == "generate_image" {
				const marker = "Generated image is saved at "
				if idx := strings.Index(m.Content, marker); idx >= 0 {
					generatedPath = strings.TrimSuffix(strings.TrimSpace(m.Content[idx+len(marker):]), ".")
				}
			}
		}
		if generatedPath == "" {
			t.Fatalf("generate_image result missing saved path in messages: %+v", req.Messages)
		}
		args, _ := json.Marshal(map[string]string{"AbsolutePath": generatedPath})
		return []actor.StreamChunk{{
			ToolCalls: []actor.ToolCall{{
				ID:    "call-view",
				Name:  "view_file",
				Input: string(args),
			}},
			FinishReason: "tool_calls",
		}}, nil
	})
	// Turn 3: verify view_file attached the PNG bytes, then send_message to parent.
	provider.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		for _, m := range req.Messages {
			if m.Role == actor.RoleTool && m.ToolName == "view_file" {
				for _, img := range m.Images {
					if img.MIMEType == "image/png" && bytes.Equal(img.Data, pngBytes) {
						sawInspectedImage = true
					}
				}
			}
		}
		msgArgs, _ := json.Marshal(map[string]string{
			"Recipient": parentConvID,
			"Message":   "Saved at " + generatedPath + "\nShows a green pixel icon.",
		})
		return []actor.StreamChunk{{
			ToolCalls: []actor.ToolCall{{
				ID:    "call-send",
				Name:  "send_message",
				Input: string(msgArgs),
			}},
			FinishReason: "tool_calls",
		}}, nil
	})
	// Turn 4: finish.
	provider.EnqueueResponse("Done.")

	tool := &invokeTool{deps: Deps{
		Model:          "gemini-3-pro-preview",
		Provider:       provider,
		CWD:            ws,
		AgentDir:       agentDir,
		Registry:       reg,
		ConversationID: parentConvID,
		ImageGenerator: func(_ context.Context, req web.GenerateImageRequest) (web.GenerateImageResult, error) {
			return web.GenerateImageResult{MIMEType: "image/png", Data: pngBytes}, nil
		},
	}}

	args, _ := json.Marshal(invokeArgs{
		TypeName: string(builtinsubagent.KindImageGenerator),
		Role:     "Image Generator",
		Prompt:   "Create a green pixel icon",
	})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("Execute error: %s", res.Error)
	}

	// Wait for the background subagent to finish and deliver its message to parent.
	deadline := time.Now().Add(5 * time.Second)
	var gotDelivered []string
	for time.Now().Before(deadline) {
		inboxMu.Lock()
		gotDelivered = append([]string(nil), delivered...)
		inboxMu.Unlock()
		if len(gotDelivered) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(gotDelivered) == 0 {
		t.Fatalf("parent inbox received no message from image-generator subagent")
	}
	if !strings.Contains(gotDelivered[0], generatedPath) {
		t.Errorf("delivered message %q does not contain generated path %q", gotDelivered[0], generatedPath)
	}
	if !strings.Contains(sawSystemPrompt, builtinsubagent.ImageGeneratorInstructions) {
		t.Errorf("subagent system prompt missing ImageGeneratorInstructions:\n%s", sawSystemPrompt)
	}
	wantTools := map[string]bool{"generate_image": false, "view_file": false, "send_message": false}
	for _, name := range sawTools {
		if _, ok := wantTools[name]; ok {
			wantTools[name] = true
		} else {
			t.Errorf("unexpected tool %q exposed to image-generator subagent", name)
		}
	}
	for name, seen := range wantTools {
		if !seen {
			t.Errorf("missing expected tool %q on image-generator subagent", name)
		}
	}
	if !sawInspectedImage {
		t.Errorf("view_file did not attach PNG bytes to subagent model request")
	}
	wantBrainDir := filepath.Join(tools.ScheduleHome(""), "brain", parentConvID)
	if !strings.HasPrefix(generatedPath, wantBrainDir) {
		t.Errorf("generatedPath = %q, want under parent brain dir %q", generatedPath, wantBrainDir)
	}
	if _, err := os.Stat(generatedPath); err != nil {
		t.Errorf("generated file missing at %q: %v", generatedPath, err)
	}
}
