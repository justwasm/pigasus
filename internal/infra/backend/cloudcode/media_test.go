// media_test.go — the CloudCode dialect carries a picture.
//
// The neutral shape is the mime type and the bytes
// (backend.ImagePart); every transport spells it its own way:
//
//	CloudCode  {"inlineData":{"mimeType":"image/png","data":"<base64>"}}
//	OpenAI     {"type":"image_url","image_url":{"url":"data:image/png;base64,…"}}
//	Anthropic  {"type":"image","source":{"type":"base64","media_type":"image/png",…}}
//	Gemini     {"inlineData":{"mimeType":"image/png","data":"<base64>"}}
//
// 【一手】 the first and the last are the reference's own spelling —
// its JSON wire types (the geminicode package) name the member
// `inlineData` with a `mimeType` and a `data` member.
//
// 【一手】 the middle two are the shapes PiG's own dialects write
// (ai/openai.go, ai/anthropic.go) — the upstream the clone's gateway
// transport mirrors.
package cloudcode

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/geminicode"
)

// mediaBytes is the picture the tests attach.
var mediaBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// cloudCodeMediaMessage is the same message in the dialect's own terms.
func cloudCodeMediaMessage() geminicode.TurnMessage {
	return geminicode.TurnMessage{
		Role:    "user",
		Content: "what is this?",
		Images: []geminicode.TurnImage{
			{MIMEType: "image/png", Data: mediaBytes},
		},
	}
}

// TestTheCloudCodeRequestCarriesThePicture pins the CloudCode
// dialect's own spelling of an inline image.
func TestTheCloudCodeRequestCarriesThePicture(t *testing.T) {
	envelope := geminicode.BuildRequest(geminicode.Turn{
		Model:    "gemini-3.1-pro",
		Messages: []geminicode.TurnMessage{cloudCodeMediaMessage()},
	})
	if len(envelope.Request.Contents) != 1 {
		t.Fatalf("the request carries %d contents, want 1", len(envelope.Request.Contents))
	}
	parts := envelope.Request.Contents[0].Parts
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

	// The wire spells it the way the reference does.
	raw, err := json.Marshal(parts[1])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"inlineData":{"mimeType":"image/png"`) {
		t.Errorf("the part on the wire = %s, want the reference's inlineData spelling", raw)
	}
}

// TestATextOnlyMessageKeepsItsShape pins that the change did not
// touch the messages that carry no picture.
func TestATextOnlyMessageKeepsItsShape(t *testing.T) {
	envelope := geminicode.BuildRequest(geminicode.Turn{
		Model:    "gemini-3.1-pro",
		Messages: []geminicode.TurnMessage{{Role: "user", Content: "hello"}},
	})
	parts := envelope.Request.Contents[0].Parts
	if len(parts) != 1 || parts[0].Text != "hello" || parts[0].InlineData != nil {
		t.Fatalf("a text-only message's parts = %+v, want the one text part", parts)
	}
}
