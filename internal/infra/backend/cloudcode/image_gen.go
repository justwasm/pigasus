package cloudcode

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/clilog"
	"github.com/justwasm/pigasus/internal/infra/backend/streampipe"
)

// ImageGenerationModelID resolves the image generation model ID from
// /v1internal:fetchAvailableModels (cached per client, endpoint, project and
// credential), falling back to
// geminicode.DefaultImageGenerationModel ("gemini-3.1-flash-image").
func (c *CloudCodeClient) ImageGenerationModelID(ctx context.Context) string {
	if c == nil || c.cc == nil {
		return geminicode.DefaultImageGenerationModel
	}

	resp, err := c.cc.FetchAvailableModels(ctx, c.cfg.Project)
	if err != nil && isSandboxFailoverEligible(c.cfg.Endpoint, toBackendError(err, c.cfg.Endpoint)) {
		resp, err = c.cc.FetchAvailableModelsAt(ctx, c.cfg.Project, endpoints.Current().CloudCodeSandbox)
	}
	if err == nil && resp != nil {
		for _, id := range resp.ImageGenerationModelIDs {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				return trimmed
			}
		}
	}
	return geminicode.DefaultImageGenerationModel
}

// GenerateImage performs a non-streaming /v1internal:generateContent image
// generation or edit call matching the reference (1.5.16 shape, still present in 1.5.17).
func (c *CloudCodeClient) GenerateImage(ctx context.Context, req backend.ImageGenRequest) (backend.ImageGenResult, error) {
	if c == nil || c.cc == nil {
		return backend.ImageGenResult{}, fmt.Errorf("cloudcode: image generation client is not initialized")
	}
	media := make([]geminicode.TurnImage, 0, len(req.InputImages))
	for _, img := range req.InputImages {
		if len(img.Data) == 0 {
			continue
		}
		media = append(media, geminicode.TurnImage{
			MIMEType: img.MIMEType,
			Data:     img.Data,
		})
	}
	if strings.TrimSpace(req.Prompt) == "" && len(media) == 0 {
		return backend.ImageGenResult{}, fmt.Errorf("cloudcode: image generation requires a prompt or input media")
	}
	if c.cfg.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = c.ImageGenerationModelID(ctx)
	}
	if model == "" {
		model = geminicode.DefaultImageGenerationModel
	}
	ccReq := geminicode.ImageGenerationRequest{
		Model:       model,
		Project:     c.cfg.Project,
		Prompt:      req.Prompt,
		AspectRatio: req.AspectRatio,
		InputMedia:  media,
		RequestID:   geminicode.NewImageGenRequestID(),
	}
	res, err := streampipe.ExecuteWithRetry(ctx, c.cfg.Endpoint, func(ctx context.Context) (geminicode.ImageGenerationResult, error) {
		out, err := c.cc.GenerateImage(ctx, ccReq)
		if err != nil {
			clilog.Logf("cloudcode", "generate_image failed model=%s endpoint=%s err=%v", model, c.cfg.Endpoint, err)
			if isSandboxFailoverEligible(c.cfg.Endpoint, toBackendError(err, c.cfg.Endpoint)) {
				sandbox := endpoints.Current().CloudCodeSandbox
				clilog.Logf("cloudcode", "generate_image trying sandbox mirror model=%s sandbox=%s", model, sandbox)
				sOut, sErr := c.cc.GenerateImageAt(ctx, ccReq, sandbox)
				if sErr == nil {
					return sOut, nil
				}
				clilog.Logf("cloudcode", "generate_image sandbox mirror failed model=%s sandbox=%s err=%v", model, sandbox, sErr)
				return geminicode.ImageGenerationResult{}, toBackendError(sErr, sandbox)
			}
			return geminicode.ImageGenerationResult{}, toBackendError(err, c.cfg.Endpoint)
		}
		return out, nil
	})
	if err != nil {
		return backend.ImageGenResult{}, err
	}
	return backend.ImageGenResult{
		MIMEType: res.MIMEType,
		Data:     res.Data,
	}, nil
}

var _ backend.ImageGenerator = (*CloudCodeClient)(nil)
