// Package remotecontrol — media_test.go: the Web UI's media surface.
//
// The pictures a user attaches reach the model through three hops, and
// each one is pinned here:
//
//   - the catalogue says which mime types a model accepts, so the Web
//     UI's attachment check lets the picture through at all;
//   - the upload RPC hands the picture back an artifact uri;
//   - the message request carries the picture's bytes, and the turn
//     they belong to.
//
// 【一手】 2026-10-26, the reference's own frontend bundle
// (internal/rcfrontend/assets/main.js) is the source of the error the
// clone's users saw:
//
//	function aOa(a,b,c){if(b&&a.length!==0){
//	    if(ZNa(a,b.supportedMimeTypes))
//	        return"Selected model does not support this media type";…}}
//
// and the upload it retries three times over:
//
//	for(let B=1;B<=3;B++)try{let D=v(await u(c({media:t})));…
//
// — the failure the user saw as "Media upload failed after 3
// attempts." beside the message above.
package remotecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// lsMediaBackend is a MessageBackend that records the prompts it was
// given, so a test can assert what a turn carried.
type lsMediaBackend struct {
	mu      sync.Mutex
	prompts []MessagePrompt
}

func (b *lsMediaBackend) Send(ctx context.Context, prompt MessagePrompt) (MessageResult, error) {
	b.mu.Lock()
	b.prompts = append(b.prompts, prompt)
	b.mu.Unlock()
	return MessageResult{Reply: "ok"}, nil
}

func (b *lsMediaBackend) seen() []MessagePrompt {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]MessagePrompt(nil), b.prompts...)
}

// pngBytes is a one-pixel PNG — the smallest thing that is really a
// picture.
var pngBytes = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

// TestTheCatalogueDeclaresTheMimeTypesOfAModelThatTakesPictures pins
// the field the Web UI's attachment check reads.
//
// 【一手】 the reference's own live catalogue
// (out/ide-language-server/live/reference/GetUserStatus.json) names
// exactly two types on the model it enables:
//
//	"supportedMimeTypes":{"image/jpeg":true,"image/png":true}
func TestTheCatalogueDeclaresTheMimeTypesOfAModelThatTakesPictures(t *testing.T) {
	mimes := modelSupportedMimeTypes("Gemini 3.8 Flash (High)", "gemini-3.8-flash-high", nil)
	if mimes == nil {
		t.Fatalf("a Gemini model must declare its supported mime types")
	}
	for _, mime := range []string{"image/jpeg", "image/png"} {
		if mimes[mime] != true {
			t.Errorf("supportedMimeTypes[%q] = %v, want true", mime, mimes[mime])
		}
	}
	if len(mimes) != 2 {
		t.Errorf("the map holds %d types, want the reference's two: %v", len(mimes), mimes)
	}
}

// TestATextOnlyModelDeclaresNoMimeTypes pins the other half: a model
// that takes no picture says nothing, because an empty map is a map's
// default and proto-JSON omits it.
func TestATextOnlyModelDeclaresNoMimeTypes(t *testing.T) {
	// The open-weight OpenAI models are text-only.
	if mimes := modelSupportedMimeTypes("GPT-OSS 120B Medium", "gpt-oss-120b-medium", nil); mimes != nil {
		t.Errorf("gpt-oss must declare no mime types, got %v", mimes)
	}
	// A model of an unknown family is text-only until it says otherwise.
	if mimes := modelSupportedMimeTypes("Some Local Model", "some-local-model", nil); mimes != nil {
		t.Errorf("an unknown model must declare no mime types, got %v", mimes)
	}
}

// TestAModelThatTakesPicturesDeclaresItsMimeTypes pins the MiniMax M3
// generation: it reads pictures, so it declares them.
//
// 【一手】 2026-10-26, a live request through the configured `agw`
// endpoint (https://agw.k0s.io/v1/chat/completions) carrying a 64x64
// PNG split red/blue down the middle: MiniMax-M3 and
// MiniMax-M3.1-Flash both answered "Red, blue" — the picture reached
// the model and the model read it.
func TestAModelThatTakesPicturesDeclaresItsMimeTypes(t *testing.T) {
	for _, m := range []struct{ label, id string }{
		{"MiniMax M3", "minimax-m3"},
		{"MiniMax M3.1 Flash", "minimax-m3.1-flash"},
	} {
		mimes := modelSupportedMimeTypes(m.label, m.id, nil)
		if mimes == nil {
			t.Errorf("%s must declare its supported mime types", m.id)
			continue
		}
		if mimes["image/png"] != true {
			t.Errorf("%s: supportedMimeTypes = %v, want image/png among them", m.id, mimes)
		}
	}
}

// TestTheModelsOwnWordOverridesTheFamilyTable pins the precedence:
// the provider configuration's per-model `supports_images` is the
// user's answer, and the family table never overrides it — not with a
// "no", and not with a "yes" for a family the table knows nothing
// about.
func TestTheModelsOwnWordOverridesTheFamilyTable(t *testing.T) {
	yes, no := true, false
	if mimes := modelSupportedMimeTypes("Gemini 3.8 Flash (High)", "gemini-3.8-flash-high", &no); mimes != nil {
		t.Errorf("a stated false must win over the family table, got %v", mimes)
	}
	mimes := modelSupportedMimeTypes("Some Local Model", "some-local-model", &yes)
	if mimes == nil {
		t.Fatalf("a stated true must win over the family table")
	}
	if mimes["image/png"] != true {
		t.Errorf("supportedMimeTypes = %v, want image/png among them", mimes)
	}
}

// TestTheCatalogueCarriesTheMimeTypes pins the field's place in the
// model config the Web UI reads.
func TestTheCatalogueCarriesTheMimeTypes(t *testing.T) {
	models := []ModelInfo{
		{ID: "gemini-3.8-flash-high", Label: "Gemini 3.8 Flash (High)"},
		{ID: "gpt-oss-120b-medium", Label: "GPT-OSS 120B Medium"},
	}
	configs, _ := userStatusClientModelConfigs(models)
	mimes, ok := configs[0]["supportedMimeTypes"].(map[string]any)
	if !ok {
		t.Fatalf("the Gemini model config carries no supportedMimeTypes: %v", configs[0])
	}
	if mimes["image/png"] != true {
		t.Errorf("supportedMimeTypes = %v, want image/png among them", mimes)
	}
	if _, ok := configs[1]["supportedMimeTypes"]; ok {
		t.Errorf("a text-only model config must carry no supportedMimeTypes")
	}
}

// TestTheCatalogueCarriesTheModelsOwnWord pins the marker travelling
// from the provider configuration to the Web UI's model config: a
// model that states it takes pictures declares the mime types even
// when the family table knows nothing about it.
func TestTheCatalogueCarriesTheModelsOwnWord(t *testing.T) {
	yes := true
	configs, _ := userStatusClientModelConfigs([]ModelInfo{
		{ID: "some-local-model", Label: "Some Local Model", SupportsImages: &yes},
	})
	mimes, ok := configs[0]["supportedMimeTypes"].(map[string]any)
	if !ok {
		t.Fatalf("the model config carries no supportedMimeTypes: %v", configs[0])
	}
	if mimes["image/jpeg"] != true || mimes["image/png"] != true {
		t.Errorf("supportedMimeTypes = %v, want the reference's two image types", mimes)
	}
}

// TestSaveMediaAsArtifactAnswersTheArtifactURI pins the upload the
// Web UI retries three times before it gives up.
//
// 【一手】 the frontend's own upload path (internal/rcfrontend/assets/
// main.js):
//
//	for(let B=1;B<=3;B++)try{let D=v(await u(c({media:t})));
//	    if(D.uri){w(D.uri);return}}
//
// — the answer has to carry a uri, or the upload failed.
//
// 【一手】 and the frontend recognises the scheme the uri uses, so it
// keeps the picture's bytes inline:
//
//	function ZA(a){return!!a?.startsWith("remote-artifact://")}
func TestSaveMediaAsArtifactAnswersTheArtifactURI(t *testing.T) {
	d, err := NewDispatcher(Config{Session: NewSessionState("test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	payload, err := json.Marshal(map[string]any{
		"media": map[string]any{
			"mimeType":   "image/png",
			"inlineData": base64.StdEncoding.EncodeToString(pngBytes),
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resText, err := d.DispatchDirect(context.Background(), "SaveMediaAsArtifact", string(payload))
	if err != nil {
		t.Fatalf("SaveMediaAsArtifact: %v", err)
	}
	var res struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("unmarshal %q: %v", resText, err)
	}
	if !strings.HasPrefix(res.URI, "remote-artifact://") {
		t.Fatalf("the artifact uri = %q, want the remote-artifact:// scheme", res.URI)
	}
	// The store holds the picture the uri names.
	artifact, ok := mediaArtifacts.get(res.URI)
	if !ok {
		t.Fatalf("the store does not hold the artifact %q", res.URI)
	}
	if artifact.MIMEType != "image/png" {
		t.Errorf("the artifact's mime type = %q, want image/png", artifact.MIMEType)
	}
	if len(artifact.Data) != len(pngBytes) {
		t.Errorf("the artifact holds %d bytes, want %d", len(artifact.Data), len(pngBytes))
	}
}

// TestSaveMediaAsArtifactRejectsAMediaWithoutBytes pins the answer to
// a media that carries no picture: an error, so the frontend's retry
// ends in a message the user can read.
func TestSaveMediaAsArtifactRejectsAMediaWithoutBytes(t *testing.T) {
	d, err := NewDispatcher(Config{Session: NewSessionState("test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if _, err := d.DispatchDirect(context.Background(), "SaveMediaAsArtifact",
		`{"media":{"mimeType":"image/png","uri":"/tmp/picture.png"}}`); err == nil {
		t.Fatalf("a media without bytes must be refused")
	}
}

// TestDeleteMediaArtifact pins the second RPC: the answer carries a
// success flag, and a second delete of the same uri fails.
//
// 【反编译】 exa.language_server_pb.DeleteMediaArtifactResponse —
// success = 1 (bool), error_message = 2 (string).
func TestDeleteMediaArtifact(t *testing.T) {
	d, err := NewDispatcher(Config{Session: NewSessionState("test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	payload, err := json.Marshal(map[string]any{
		"media": map[string]any{
			"mimeType":   "image/png",
			"inlineData": base64.StdEncoding.EncodeToString(pngBytes),
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resText, err := d.DispatchDirect(context.Background(), "SaveMediaAsArtifact", string(payload))
	if err != nil {
		t.Fatalf("SaveMediaAsArtifact: %v", err)
	}
	var saved struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal([]byte(resText), &saved); err != nil {
		t.Fatalf("unmarshal %q: %v", resText, err)
	}

	deletePayload, err := json.Marshal(map[string]any{"uri": saved.URI})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resText, err = d.DispatchDirect(context.Background(), "DeleteMediaArtifact", string(deletePayload))
	if err != nil {
		t.Fatalf("DeleteMediaArtifact: %v", err)
	}
	var res struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("unmarshal %q: %v", resText, err)
	}
	if !res.Success {
		t.Fatalf("deleting the artifact answered %q, want a success", resText)
	}
	if _, ok := mediaArtifacts.get(saved.URI); ok {
		t.Errorf("the store still holds the deleted artifact")
	}

	// A second delete has nothing to delete.
	resText, err = d.DispatchDirect(context.Background(), "DeleteMediaArtifact", string(deletePayload))
	if err != nil {
		t.Fatalf("DeleteMediaArtifact: %v", err)
	}
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("unmarshal %q: %v", resText, err)
	}
	if res.Success {
		t.Errorf("deleting an artifact that is gone answered %q, want a failure", resText)
	}
}

// TestSendUserCascadeMessageCarriesTheAttachedImages pins the last
// hop: the picture the user attached rides with the turn.
//
// 【反编译】 the request's field is
// exa.language_server_pb.SendUserCascadeMessageRequest.media — field
// 14, a repeated exa.codeium_common_pb.Media.
func TestSendUserCascadeMessageCarriesTheAttachedImages(t *testing.T) {
	backend := &lsMediaBackend{}
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	d, err := NewDispatcher(Config{Session: NewSessionState("test"), MessageBackend: backend})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	lsStartConversation(t, d, "conv-media")

	payload, err := json.Marshal(map[string]any{
		"cascadeId": "conv-media",
		"items":     []any{map[string]any{"text": "what is this?"}},
		"media": []any{map[string]any{
			"mimeType":   "image/png",
			"inlineData": base64.StdEncoding.EncodeToString(pngBytes),
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := d.DispatchDirect(context.Background(), "SendUserCascadeMessage", string(payload)); err != nil {
		t.Fatalf("SendUserCascadeMessage: %v", err)
	}
	prompts := backend.seen()
	if len(prompts) != 1 {
		t.Fatalf("the turn ran %d times, want 1", len(prompts))
	}
	images := prompts[0].Images
	if len(images) != 1 {
		t.Fatalf("the turn carried %d images, want 1", len(images))
	}
	if images[0].MIMEType != "image/png" {
		t.Errorf("the image's mime type = %q, want image/png", images[0].MIMEType)
	}
	if len(images[0].Data) != len(pngBytes) {
		t.Errorf("the image holds %d bytes, want %d", len(images[0].Data), len(pngBytes))
	}
}

// TestAMessageWithOnlyAPictureIsAValidMessage pins the empty-text
// case: a user who attaches a picture and says nothing has still sent
// a message.
func TestAMessageWithOnlyAPictureIsAValidMessage(t *testing.T) {
	backend := &lsMediaBackend{}
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	d, err := NewDispatcher(Config{Session: NewSessionState("test"), MessageBackend: backend})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	lsStartConversation(t, d, "conv-media-only")

	payload, err := json.Marshal(map[string]any{
		"cascadeId": "conv-media-only",
		"items":     []any{},
		"media": []any{map[string]any{
			"mimeType":   "image/png",
			"inlineData": base64.StdEncoding.EncodeToString(pngBytes),
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := d.DispatchDirect(context.Background(), "SendUserCascadeMessage", string(payload)); err != nil {
		t.Fatalf("SendUserCascadeMessage: %v", err)
	}
	prompts := backend.seen()
	if len(prompts) != 1 {
		t.Fatalf("the turn ran %d times, want 1", len(prompts))
	}
	if len(prompts[0].Images) != 1 {
		t.Errorf("the turn carried %d images, want 1", len(prompts[0].Images))
	}
}

// TestMessageImagesFromMedia pins the conversion: a media that
// carries no bytes is not a picture the turn can send.
func TestMessageImagesFromMedia(t *testing.T) {
	media := []lsMediaItem{
		{MIMEType: "image/png", InlineData: base64.StdEncoding.EncodeToString(pngBytes)},
		{MIMEType: "image/jpeg", URI: "/tmp/picture.jpg"},
		{MIMEType: "image/gif", InlineData: "not base64 at all"},
	}
	images := messageImagesFromMedia(media)
	if len(images) != 1 {
		t.Fatalf("read %d images, want 1: %v", len(images), images)
	}
	if images[0].MIMEType != "image/png" {
		t.Errorf("the image's mime type = %q, want image/png", images[0].MIMEType)
	}
	if len(images[0].Data) != len(pngBytes) {
		t.Errorf("the image holds %d bytes, want %d", len(images[0].Data), len(pngBytes))
	}
}

// TestTheMediaStoreEvictsTheOldestArtifact pins the store's bounds: a
// picture the user attached ten minutes ago and never sent is worth
// less than the one they are attaching now.
func TestTheMediaStoreEvictsTheOldestArtifact(t *testing.T) {
	store := newMediaStore()
	var first string
	for i := 0; i < mediaStoreMaxItems+1; i++ {
		uri := store.save("image/png", pngBytes)
		if i == 0 {
			first = uri
		}
	}
	if _, ok := store.get(first); ok {
		t.Errorf("the oldest artifact survived the store's bound")
	}
	if got := len(store.items); got > mediaStoreMaxItems {
		t.Errorf("the store holds %d artifacts, want at most %d", got, mediaStoreMaxItems)
	}
}

// TestAMediaArtifactURIIsOurs pins the uri's shape: an id of ours, and
// nothing else.
func TestAMediaArtifactURIIsOurs(t *testing.T) {
	if _, ok := mediaArtifactID("remote-artifact://abc"); !ok {
		t.Errorf("our own uri must be recognised")
	}
	if _, ok := mediaArtifactID("/tmp/picture.png"); ok {
		t.Errorf("a local path is not one of our artifacts")
	}
	if _, ok := mediaArtifactID("remote-artifact://"); ok {
		t.Errorf("a uri with no id is not one of our artifacts")
	}
}
