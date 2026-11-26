package surface

import (
	"context"

	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/web"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// ConfigureImageGenerator binds an image transport without changing the tool's declaration.
func ConfigureImageGenerator(reg *tools.Registry, provider func(context.Context, web.GenerateImageRequest) (web.GenerateImageResult, error)) {
	if reg == nil {
		return
	}
	if t, ok := reg.Get("generate_image"); ok {
		if gi, ok := unwrapTool(t).(*web.GenerateImage); ok {
			gi.Provider = provider
		}
	}
}

// ImageProviderFromBackend adapts the optional backend image capability.
func ImageProviderFromBackend(generator backend.ImageGenerator) func(context.Context, web.GenerateImageRequest) (web.GenerateImageResult, error) {
	if generator == nil {
		return nil
	}
	return func(ctx context.Context, req web.GenerateImageRequest) (web.GenerateImageResult, error) {
		in := backend.ImageGenRequest{Prompt: req.Prompt, AspectRatio: req.AspectRatio}
		for _, image := range req.InputImages {
			in.InputImages = append(in.InputImages, backend.ImagePart{MIMEType: image.MIMEType, Data: image.Data})
		}
		out, err := generator.GenerateImage(ctx, in)
		return web.GenerateImageResult{MIMEType: out.MIMEType, Data: out.Data}, err
	}
}
