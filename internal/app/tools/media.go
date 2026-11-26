package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// MediaPart is one binary attachment returned by a tool.
type MediaPart struct {
	MIMEType string `json:"mime_type"`
	Data     []byte `json:"data"`
}

// MediaOutput carries the readable result text alongside binary attachments.
type MediaOutput struct {
	Text   string      `json:"text"`
	Images []MediaPart `json:"images,omitempty"`
}

// DispatchMedia runs a tool and preserves any media attachments it returns.
// Text is formatted and budgeted using the same rules as Dispatch.
func (r *Registry) DispatchMedia(ctx context.Context, name string, args json.RawMessage) (MediaOutput, error) {
	t, ok := r.Get(name)
	if !ok {
		return MediaOutput{}, fmt.Errorf("unknown tool %q", name)
	}
	out, err := t.Run(ctx, args)
	if err != nil {
		return MediaOutput{}, err
	}

	var result MediaOutput
	switch v := out.(type) {
	case nil:
		return result, nil
	case MediaOutput:
		result = v
	case *MediaOutput:
		if v != nil {
			result = *v
		}
	case string:
		result.Text = v
	default:
		b, marshalErr := json.Marshal(v)
		if marshalErr != nil {
			result.Text = fmt.Sprint(v)
		} else {
			result.Text = string(b)
		}
	}

	if ShouldStripCitations(name) {
		result.Text = StripCitationFootnotes(result.Text)
	}
	result.Text = ApplyOutputBudget(result.Text, "", DefaultMaxResultTokens).Content
	return result, nil
}
