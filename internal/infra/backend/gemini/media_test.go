// media_test.go — the Gemini dialect carries a picture.
//
// The neutral shape is the mime type and the bytes
// (backend.ImagePart); the Gemini dialect spells it
//
//	Gemini     {"inlineData":{"mimeType":"image/png","data":"<base64>"}}
//
// 【一手】 which is the reference's own spelling — its JSON wire types
// (the geminicode package) name the member `inlineData` with a
// `mimeType` and a `data` member.
package gemini

import (
	"encoding/base64"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// mediaBytes is the picture the tests attach.
var mediaBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// mediaMessage is a user message with one picture in it.
func mediaMessage() backend.Message {
	return backend.Message{
		Role:    "user",
		Content: "what is this?",
		Images: []backend.ImagePart{
			{MIMEType: "image/png", Data: mediaBytes},
		},
	}
}

// TestTheGeminiRequestCarriesThePicture pins the Gemini dialect's
// inline data part.
func TestTheGeminiRequestCarriesThePicture(t *testing.T) {
	contents := geminiContentsFor(mediaMessage(), nil)
	if len(contents) != 1 {
		t.Fatalf("the request carries %d contents, want 1", len(contents))
	}
	parts := contents[0].Parts
	if len(parts) != 2 {
		t.Fatalf("the user content carries %d parts, want the text and the picture: %+v", len(parts), parts)
	}
	if parts[0].Text != "what is this?" {
		t.Errorf("the first part = %q, want the text", parts[0].Text)
	}
	if parts[1].InlineData == nil {
		t.Fatalf("the second part carries no inline data: %+v", parts[1])
	}
	if parts[1].InlineData.MIMEType != "image/png" {
		t.Errorf("the inline data's mime type = %q, want image/png", parts[1].InlineData.MIMEType)
	}
	if parts[1].InlineData.Data != base64.StdEncoding.EncodeToString(mediaBytes) {
		t.Errorf("the inline data = %q, want the picture's base64", parts[1].InlineData.Data)
	}
}

// TestAPictureWithoutTextIsAMessage pins the image-only case: the
// text part is dropped, so the request carries the picture alone.
func TestAPictureWithoutTextIsAMessage(t *testing.T) {
	contents := geminiContentsFor(backend.Message{
		Role:   "user",
		Images: []backend.ImagePart{{MIMEType: "image/png", Data: mediaBytes}},
	}, nil)
	if len(contents) != 1 {
		t.Fatalf("the request carries %d contents, want 1", len(contents))
	}
	parts := contents[0].Parts
	if len(parts) != 1 || parts[0].InlineData == nil {
		t.Fatalf("the user content's parts = %+v, want the picture alone", parts)
	}
}
