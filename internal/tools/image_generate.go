// Package tools — image_generate.go: the catalogue's `generate_image`
// backend.
//
// Cycle 346: this lived in stubs_tasks.go, next to five other task tools
// the catalogue never registered (`wait`, `create_task`, `send_message`,
// `notify_user`, `task_boundary`) — the catalogue has its own `WaitTool`
// and `SendMessageTool` in tools_tasks.go / tools_interaction.go.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// GenerateImage — Image generation (reference name `generate_image`).
type GenerateImage struct {
	Workspace string
	Provider  func(ctx context.Context, prompt, size string) (string, error)
}

func (s *GenerateImage) Name() string { return "generate_image" }
func (s *GenerateImage) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"Prompt":{"type":"string"},"ImageName":{"type":"string"}},"required":["Prompt","ImageName"]}`)
}
func (s *GenerateImage) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Prompt    string `json:"prompt"`
		Size      string `json:"size"`
		ImageName string `json:"image_name"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Prompt == "" {
		return nil, errors.New("generate_image: prompt is required")
	}
	if s.Provider != nil {
		out, err := s.Provider(ctx, p.Prompt, p.Size)
		if err != nil {
			return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
		}
		return resultStamp(start) + out, nil
	}
	return resultStamp(start) + "Encountered error in tool execution: no response in generate content result", nil
}
