package printmode

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/agenttools"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
)

type fakePrintImageBackend struct {
	gotReq backend.ImageGenRequest
	png    []byte
}

func (f *fakePrintImageBackend) Stream(context.Context, backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	return nil, nil
}

func (f *fakePrintImageBackend) GenerateImage(_ context.Context, req backend.ImageGenRequest) (backend.ImageGenResult, error) {
	f.gotReq = req
	return backend.ImageGenResult{MIMEType: "image/png", Data: f.png}, nil
}

func TestBuildPrintToolsWiresImageGeneratorAndConversation(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{B: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	for _, agentBody := range []string{"", "Custom agent instructions"} {
		name := "default"
		if agentBody != "" {
			name = "custom_agent"
		}
		t.Run(name, func(t *testing.T) {
			ws := t.TempDir()
			cat := surface.Catalog(ws)
			if agentBody != "" {
				cat = agenttools.Registry(cat)
			}
			fb := &fakePrintImageBackend{png: buf.Bytes()}
			convID := "print-conv-" + name
			cfg := actorPrintConfig{
				Backend:        fb,
				Model:          "gemini-3-pro-preview",
				Catalog:        cat,
				Policy:         &permissions.Policy{ToolPermission: permissions.ToolPermissionAlwaysProceed},
				Workspace:      ws,
				AgentDir:       t.TempDir(),
				ConversationID: convID,
				AgentBody:      agentBody,
			}
			toolsList, cleanup, err := buildPrintTools(cfg, actor.NewMockProvider(), func() string { return "sys" }, nil)
			if err != nil {
				t.Fatalf("buildPrintTools: %v", err)
			}
			if cleanup != nil {
				defer cleanup()
			}

			var genTool actor.Tool
			for _, tl := range toolsList {
				if tl.Name() == "generate_image" {
					genTool = tl
					break
				}
			}
			if genTool == nil {
				t.Fatal("generate_image tool missing from buildPrintTools output")
			}
			res, err := genTool.Execute(context.Background(), `{"Prompt":"A blue square","ImageName":"BlueSquare","AspectRatio":"1:1"}`)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.Error != "" {
				t.Fatalf("Execute error: %s", res.Error)
			}
			if fb.gotReq.Prompt != "A blue square" {
				t.Errorf("backend Prompt = %q, want A blue square", fb.gotReq.Prompt)
			}
			const marker = "Generated image is saved at "
			idx := strings.Index(res.Output, marker)
			if idx < 0 {
				t.Fatalf("unexpected output %q", res.Output)
			}
			savedPath := strings.TrimSuffix(strings.TrimSpace(res.Output[idx+len(marker):]), ".")
			wantBrainDir := filepath.Join(tools.ScheduleHome(""), "brain", convID)
			if !strings.HasPrefix(savedPath, wantBrainDir) {
				t.Errorf("savedPath = %q, want under %q", savedPath, wantBrainDir)
			}
			if _, err := os.Stat(savedPath); err != nil {
				t.Errorf("saved file missing at %q: %v", savedPath, err)
			}
		})
	}
}
