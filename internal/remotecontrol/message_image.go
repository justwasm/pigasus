// Package remotecontrol — message_image.go: the pictures a user
// attached to a message.
//
// The Web UI hands the pictures over with the message: the request's
// `media` field carries them inline — the mime type and the bytes —
// so the turn does not have to read them back from the artifact store.
//
// 【反编译】 the request field is
// `exa.language_server_pb.SendUserCascadeMessageRequest.media`,
// field 14, a repeated `exa.codeium_common_pb.Media` — read out of
// the descriptor closure the reference's frontend bundle embeds
// (out/ide-language-server/protos/language_server.protoset).
//
// 【一手】 the frontend's own send path passes the media on: the
// composer builds `media` from the attachments and the send call
// carries it (`vZ` in internal/rcfrontend/assets/main.js).
package remotecontrol

import (
	"github.com/justwasm/pigasus/imagery"
)

// MessageImage is one picture a user attached to a message.
//
// The mime type is the one the frontend read off the file, and the
// bytes are the file's own.
type MessageImage struct {
	// MIMEType is the picture's media type — "image/png", "image/jpeg".
	MIMEType string
	// Data is the picture's bytes.
	Data []byte
}

// messageImagesFromMedia reads the pictures out of a request's media
// list.
//
// A media that carries no bytes is not a picture the turn can send —
// the frontend uploads it first and the bytes ride along — so it is
// dropped rather than sent as an empty image.
//
// 【一手】 agy 1.4.10, the vendor's own release notes: "images are now
// capped at 2000 pixels per side and large PNGs are sent as JPEGs when
// that is much smaller, and an image saved as JPEG but labeled PNG,
// such as a pasted image or browser screenshot, is no longer rejected
// for a mismatched type."
//
// So the media type follows the **bytes**, not the name the frontend
// read off the file: a JPEG the browser labelled "image/png" goes out
// as "image/jpeg", and a picture over the pixel cap (or a large PNG)
// goes out through the imagery pipeline.
func messageImagesFromMedia(media []lsMediaItem) []MessageImage {
	var images []MessageImage
	for _, item := range media {
		data, ok := item.mediaBytes()
		if !ok {
			continue
		}
		mimeType := item.MIMEType
		if out, format, _, err := imagery.Prepare(data); err == nil && format != imagery.FormatUnknown {
			data = out
			mimeType = format.MIMEType()
		}
		images = append(images, MessageImage{MIMEType: mimeType, Data: data})
	}
	return images
}
