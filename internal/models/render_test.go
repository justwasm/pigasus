// Tests for the shared renderers (render.go).
//
// The model detail view is where the operator reads what the Web UI's
// model picker will see about a model. A model that states none of the
// per-model attributes prints none of them — the lines are the model's
// own words, and a default in the view would be a statement the
// operator never made.
package models

import (
	"strings"
	"testing"
)

// TestRenderModelDetailShowsTheAttributes pins the attribute lines: a
// model that states them shows them, and a model that says nothing
// shows none.
func TestRenderModelDetailShowsTheAttributes(t *testing.T) {
	yes, no := true, false
	fraction := 0.25
	full := Model{
		Name:                       "minimax-m3",
		DisplayName:                "MiniMax M3",
		Provider:                   "openai",
		ExternalModelName:          "MiniMax-M3",
		SupportsImages:             &yes,
		SupportedMimeTypes:         []string{"image/png", "audio/webm"},
		Disabled:                   true,
		QuotaInfo:                  &QuotaInfo{RemainingFraction: &fraction, ResetTime: "2026-10-27T00:00:00Z"},
		SupportsThoughtCirculation: &yes,
		ModelURL:                   "https://example.com/models/minimax-m3",
		TagTitle:                   "NEW",
		TagDescription:             "The newest model",
		EffortSupported:            &no,
	}
	var out strings.Builder
	RenderModelDetail(&out, full)
	body := out.String()
	for _, want := range []string{
		"images:       true",
		"mime types:   image/png, audio/webm",
		"disabled:     true",
		"quota:        0.25 left",
		"quota reset:  2026-10-27T00:00:00Z",
		"thoughts:     circulated",
		"model url:    https://example.com/models/minimax-m3",
		"tag:          NEW",
		"tag note:     The newest model",
		"effort level: false",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the detail view does not carry %q:\n%s", want, body)
		}
	}

	out.Reset()
	RenderModelDetail(&out, Model{Name: "plain", Provider: "openai"})
	body = out.String()
	for _, unwanted := range []string{
		"images:", "mime types:", "disabled:", "quota:", "thoughts:",
		"model url:", "tag:", "tag note:", "effort level:",
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("a model that says nothing carries %q:\n%s", unwanted, body)
		}
	}
}
