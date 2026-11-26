package surface

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/web"
	"github.com/justwasm/pigasus/internal/core/backend"
)

type wrappedTool struct {
	inner tools.Tool
}

func (w *wrappedTool) Name() string            { return w.inner.Name() }
func (w *wrappedTool) Schema() json.RawMessage { return w.inner.Schema() }
func (w *wrappedTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return w.inner.Run(ctx, args)
}
func (w *wrappedTool) Unwrap() tools.Tool { return w.inner }

type fakeImageBackend struct {
	gotReq backend.ImageGenRequest
	res    backend.ImageGenResult
	err    error
}

func (f *fakeImageBackend) GenerateImage(_ context.Context, req backend.ImageGenRequest) (backend.ImageGenResult, error) {
	f.gotReq = req
	return f.res, f.err
}

func TestConfigureImageGeneratorAndConversationThroughUnwrap(t *testing.T) {
	reg := Catalog(t.TempDir())
	wrapped := tools.NewRegistry()
	if orig, ok := reg.Get("generate_image"); ok {
		wrapped.Register(&wrappedTool{inner: orig})
	}

	fb := &fakeImageBackend{
		res: backend.ImageGenResult{MIMEType: "image/png", Data: []byte("png-bytes")},
	}
	ConfigureConversation(wrapped, "conv-surface-1")
	ConfigureImageGenerator(wrapped, ImageProviderFromBackend(fb))

	orig, ok := reg.Get("generate_image")
	if !ok {
		t.Fatal("generate_image missing from catalog")
	}
	gi, ok := unwrapTool(orig).(*web.GenerateImage)
	if !ok {
		t.Fatalf("unwrapped tool = %T, want *web.GenerateImage", unwrapTool(orig))
	}
	if gi.ConversationID != "conv-surface-1" {
		t.Errorf("ConversationID = %q, want conv-surface-1", gi.ConversationID)
	}
	if gi.Provider == nil {
		t.Fatal("Provider is nil after ConfigureImageGenerator")
	}
	out, err := gi.Provider(context.Background(), web.GenerateImageRequest{
		Prompt:      "a blue circle",
		AspectRatio: "4:3",
		InputImages: []web.InputImage{{MIMEType: "image/png", Data: []byte("in")}},
	})
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if fb.gotReq.Prompt != "a blue circle" || fb.gotReq.AspectRatio != "4:3" || len(fb.gotReq.InputImages) != 1 {
		t.Errorf("backend got unexpected request: %+v", fb.gotReq)
	}
	if out.MIMEType != "image/png" || string(out.Data) != "png-bytes" {
		t.Errorf("Provider returned %+v", out)
	}
}
