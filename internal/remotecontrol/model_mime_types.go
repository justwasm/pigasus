// Package remotecontrol — model_mime_types.go: the mime types a
// model accepts.
//
// Why a file of its own: the Web UI's attachment button and its
// attachment check both read one field — the model config's
// `supportedMimeTypes` — and the clone used to leave it out. The
// frontend's own check then rejected every picture with the message
// "Selected model does not support this media type", and the upload
// that follows failed three times over.
//
// 【一手】 2026-10-26, the reference's own live GetUserStatus capture
// (out/ide-language-server/live/reference/GetUserStatus.json) — the
// enabled entry carries the map, and the disabled ones do not:
//
//	{"label":"gemini-3.1-pro",
//	 "modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"},
//	 "provider":"MODEL_PROVIDER_GOOGLE",
//	 "supportedMimeTypes":{"image/jpeg":true,"image/png":true}}
//
// 【反编译】 the field is
// `exa.codeium_common_pb.ClientModelConfig.supported_mime_types`,
// field 18, a `map<string, bool>` — read out of the
// FileDescriptorProto the reference's own frontend bundle embeds
// (internal/rcfrontend/assets/main.js, the base64 blob for
// `third_party/jetski/codeium_common_pb/codeium_common.proto`).
//
// 【一手】 the frontend's check, from the same bundle:
//
//	function ZNa(a,b){return b&&a?a.some(c=>{c=c.mimeType;
//	    return!(RA(c)||b.get(c)===!0)}):!1}
//
// and the composer calls it with the model config's map:
//
//	function aOa(a,b,c){if(b&&a.length!==0){
//	    if(ZNa(a,b.supportedMimeTypes))
//	        return"Selected model does not support this media type";…}}
//
// The map the frontend builds is
// `new Map(Object.entries(a.supportedMimeTypes))` — an *empty* map
// when the server omits the field, and an empty map is truthy. Every
// picture therefore failed the check: `RA` only knows the office
// document types, and an empty map knows no mime type at all.
//
// So the field is not optional in practice: a model that takes
// pictures has to say so.
package remotecontrol

import "strings"

// imageMimeTypes are the mime types the reference's catalogue names for
// a model that takes images.
//
// 【一手】 the reference's own capture names exactly these two —
// "image/jpeg" and "image/png" — on the model it enables. The Web UI
// offers the pictures it accepts in this list (its own
// `["image/png","image/jpeg","image/gif","image/svg+xml","image/webp"]`
// is the *file picker*'s list, wider than the model's).
var imageMimeTypes = []string{"image/jpeg", "image/png"}

// modelSupportedMimeTypes returns the `supportedMimeTypes` map a
// model config carries, or nil for a model that takes no media.
//
// The model's own word decides, in this order:
//
//  1. the configuration's list of media types (`supported_mime_types`)
//     — the full answer: a model that takes PDF but not PNG says so;
//  2. the configuration's `supports_images` marker — the shorthand for
//     "the two image types";
//  3. the family table — the guess, for a model that says neither.
//
// stated is the model's own word about pictures — the user's
// `supports_images` in the provider configuration. nil means "not
// stated", and the family table decides then.
//
// proto-JSON omits a field that holds its default, and an empty map is
// a map's default: the reference's own catalogue omits the field on
// the entries that declare no images, and the clone does the same.
func modelSupportedMimeTypes(label, modelID string, stated *bool, listed ...string) map[string]any {
	mimes := imageMimeTypes
	if len(listed) > 0 {
		mimes = listed
	} else if !modelTakesImages(label, modelID, stated) {
		return nil
	}
	out := make(map[string]any, len(mimes))
	for _, mime := range mimes {
		mime = strings.TrimSpace(mime)
		if mime == "" {
			continue
		}
		out[mime] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// modelTakesImages reports whether a model accepts a picture.
//
// stated wins: the provider configuration's per-model
// `supports_images` is the user's answer, and the family table never
// overrides it. A stated false is as much an answer as a stated true.
//
// 【推测】 Without a stated value the rule is read off the model's own
// name, the way `userStatusModelProvider` reads the vendor off it: the
// reference's catalogue says nothing about a model it disables, so the
// only first-hand evidence is the one entry it enables — a Gemini model
// with the two image types.
//
// Gemini and Claude take pictures; the open-weight GPT-OSS models do
// not, and neither does a text-only model of any other family. A model
// whose name says none of these is treated as text-only: a wrong
// "yes" shows an attachment button that the model cannot answer, and
// a wrong "no" hides a capability the model has.
//
// 【一手】 2026-10-26, a live request through the configured `agw`
// endpoint (https://agw.k0s.io/v1/chat/completions) carrying a 64x64
// PNG split red/blue down the middle: MiniMax-M3 and
// MiniMax-M3.1-Flash both answered "Red, blue" — the picture reached
// the model and the model read it. The MiniMax M3 generation therefore
// takes pictures.
//
// 【推测】 The older MiniMax generations are not in the table: the
// text-only ones (Text-01, abab) and the M1/M2 ones are not verified
// either way. A model outside the table states the marker itself —
// `agy model add <id> --provider <p> --supports-images`, or the
// `supports_images` key in the provider configuration.
func modelTakesImages(label, modelID string, stated *bool) bool {
	if stated != nil {
		return *stated
	}
	name := strings.ToLower(label + " " + modelID)
	switch {
	case strings.Contains(name, "gpt-oss"):
		// The open-weight OpenAI models are text-only.
		return false
	case strings.Contains(name, "gemini"), strings.Contains(name, "claude"):
		return true
	case strings.Contains(name, "gpt-4o"), strings.Contains(name, "gpt-5"):
		// The hosted OpenAI models take pictures.
		return true
	case strings.Contains(name, "minimax m3"), strings.Contains(name, "minimax-m3"):
		// The MiniMax M3 generation takes pictures (M3, M3.1 Flash).
		return true
	}
	return false
}
