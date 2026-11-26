package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/justwasm/pigasus/internal/app/tools"
)

// InputImage is a source image passed to the image-generation transport.
type InputImage struct {
	MIMEType string
	Data     []byte
}

type GenerateImageRequest struct {
	Prompt      string
	ImageName   string
	ImagePaths  []string
	AspectRatio string
	InputImages []InputImage
}

type GenerateImageResult struct {
	MIMEType string
	Data     []byte
}

// GenerateImage creates artifacts in the conversation that requested them.
type GenerateImage struct {
	Workspace      string
	ConversationID string
	Home           string
	Provider       func(context.Context, GenerateImageRequest) (GenerateImageResult, error)
}

func (s *GenerateImage) Name() string        { return "generate_image" }
func (s *GenerateImage) Description() string { return tools.RefDesc("generate_image") }
func (s *GenerateImage) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"Prompt":{"type":"string","minLength":1},"ImageName":{"type":"string","minLength":1},"ImagePaths":{"type":"array","items":{"type":"string"},"maxItems":3},"AspectRatio":{"type":"string","description":"Defaults to 1:1.","enum":["1:1","2:3","3:2","3:4","4:3","9:16","16:9"]}},"required":["Prompt","ImageName"]}`)
}

func (s *GenerateImage) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Prompt      string   `json:"prompt"`
		ImageName   string   `json:"image_name"`
		ImagePaths  []string `json:"image_paths"`
		AspectRatio string   `json:"aspect_ratio"`
	}
	if err := tools.ArgsOf(args, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return nil, errors.New("generate_image: prompt is required")
	}
	if strings.TrimSpace(p.ImageName) == "" {
		return nil, errors.New("generate_image: image_name is required")
	}
	if len(p.ImagePaths) > 3 {
		return nil, errors.New("generate_image: at most 3 input images are allowed")
	}
	if p.AspectRatio == "" {
		p.AspectRatio = "1:1"
	}
	switch p.AspectRatio {
	case "1:1", "2:3", "3:2", "3:4", "4:3", "9:16", "16:9":
	default:
		return nil, fmt.Errorf("generate_image: unsupported aspect ratio %q", p.AspectRatio)
	}
	fail := func(err error) (any, error) {
		return tools.ResultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
	}
	if s.Provider == nil {
		return fail(errors.New("no response in generate content result"))
	}
	req := GenerateImageRequest{Prompt: p.Prompt, ImageName: p.ImageName, ImagePaths: p.ImagePaths, AspectRatio: p.AspectRatio}
	for _, path := range p.ImagePaths {
		full, err := tools.ReadPath(s.Workspace, path)
		if err != nil {
			return fail(err)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return fail(err)
		}
		mimeType := http.DetectContentType(data)
		if !strings.HasPrefix(mimeType, "image/") {
			return fail(fmt.Errorf("input file %q is not a supported image", path))
		}
		req.InputImages = append(req.InputImages, InputImage{MIMEType: mimeType, Data: data})
	}
	out, err := s.Provider(ctx, req)
	if err != nil {
		return fail(err)
	}
	if len(out.Data) == 0 {
		return fail(errors.New("no image generated in response"))
	}
	ext := ""
	switch strings.ToLower(strings.TrimSpace(out.MIMEType)) {
	case "image/png":
		ext = ".png"
	case "image/jpeg", "image/jpg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	case "":
		switch http.DetectContentType(out.Data) {
		case "image/jpeg":
			ext = ".jpg"
		case "image/webp":
			ext = ".webp"
		case "image/gif":
			ext = ".gif"
		default:
			ext = ".png"
		}
	default:
		return fail(fmt.Errorf("unsupported generated image MIME type %q", out.MIMEType))
	}
	convID := s.ConversationID
	if convID == "" {
		convID = os.Getenv("OPENAGY_CONVERSATION_ID")
	}
	if convID == "" {
		convID = tools.ProcessConversationID()
	}
	if filepath.Base(convID) != convID || convID == "." || convID == ".." || strings.ContainsAny(convID, `/\\`) {
		return fail(errors.New("invalid conversation id"))
	}
	dir := filepath.Join(tools.ScheduleHome(s.Home), "brain", convID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(err)
	}
	name := imageFileName(p.ImageName)
	stamp := time.Now().UnixMilli()
	var savedPath string
	for {
		savedPath = filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, stamp, ext))
		f, err := os.OpenFile(savedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			stamp++
			continue
		}
		if err != nil {
			return fail(err)
		}
		_, writeErr := f.Write(out.Data)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(savedPath)
			if writeErr != nil {
				return fail(writeErr)
			}
			return fail(closeErr)
		}
		break
	}
	return tools.ResultStamp(start) + fmt.Sprintf("Generated image is saved at %s.", savedPath), nil
}

func imageFileName(name string) string {
	name = tools.CamelToSnake(name)
	var b strings.Builder
	separator := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if separator && b.Len() > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			separator = false
		} else {
			separator = true
		}
	}
	if b.Len() == 0 {
		return "image"
	}
	out := b.String()
	if len(out) > 160 {
		out = string([]rune(out)[:min(40, len([]rune(out)))])
	}
	return out
}
