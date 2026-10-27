// Package remotecontrol — dispatch_media.go: the Web UI's two media
// RPCs.
//
// The Web UI uploads a picture before it sends the message that
// carries it. The upload is one RPC — SaveMediaAsArtifact — and the
// frontend retries it three times before it gives up:
//
//	for(let B=1;B<=3;B++)try{let D=v(await u(c({media:t})));
//	    if(D.uri){w(D.uri);return}}catch(D){…}
//
// and the failure is what the user sees as "Media upload failed after
// 3 attempts." The clone had no route for the call at all, so every
// upload failed.
//
// 【反编译】 the two methods are the reference's own language-server
// service — read out of the descriptor closure the reference's
// frontend bundle embeds
// (out/ide-language-server/protos/language_server.protoset):
//
//	LanguageServerService.SaveMediaAsArtifact(
//	    exa.language_server_pb.SaveMediaAsArtifactRequest)
//	    → exa.language_server_pb.SaveMediaAsArtifactResponse
//	LanguageServerService.DeleteMediaArtifact(
//	    exa.language_server_pb.DeleteMediaArtifactRequest)
//	    → exa.language_server_pb.DeleteMediaArtifactResponse
//
// and the messages are:
//
//	SaveMediaAsArtifactRequest  { media = 1 : exa.codeium_common_pb.Media }
//	SaveMediaAsArtifactResponse { uri = 1 : string }
//	DeleteMediaArtifactRequest  { uri = 1 : string }
//	DeleteMediaArtifactResponse { success = 1 : bool, error_message = 2 : string }
//
// The service itself is the IDE's language server — the reference's
// own handlers are at 0x101bc4c80 (SaveMediaAsArtifact) and
// 0x101bc4ed0 (DeleteMediaArtifact) in that binary
// (out/local-macos/gosrc/language_server_pb/language_server_go_grpc/
// api_generated.go).
package remotecontrol

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// lsMediaItem is the proto-JSON shape of exa.codeium_common_pb.Media —
// the message the Web UI sends and the one it receives.
//
// 【反编译】 the fields, from the same descriptor closure:
//
//	mime_type = 1, inline_data = 2 (bytes), blobref = 3 (message),
//	description = 4, uri = 5, thumbnail = 6, duration_seconds = 7,
//	media_id = 8, display_name = 9, …
//
// The payload is a oneof — `inlineData` and `blobref` are its members
// — and proto-JSON spells a oneof by its member, so the bytes ride in
// `inlineData` as base64.
type lsMediaItem struct {
	MIMEType    string `json:"mimeType"`
	Description string `json:"description"`
	URI         string `json:"uri"`
	InlineData  string `json:"inlineData"`
}

// mediaBytes returns the picture's bytes, and whether it carries any.
//
// The bytes arrive base64-encoded (proto-JSON spells a bytes field
// that way) — but a frontend that sends the picture by *path* sends no
// bytes at all, only the path in `uri`.
func (m lsMediaItem) mediaBytes() ([]byte, bool) {
	if strings.TrimSpace(m.InlineData) == "" {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(m.InlineData)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// handleSaveMediaAsArtifact answers SaveMediaAsArtifact.
//
// The answer is the artifact's uri: the handle the frontend keeps
// beside the picture it already holds.
func (d *Dispatcher) handleSaveMediaAsArtifact(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		Media lsMediaItem `json:"media"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	data, ok := req.Media.mediaBytes()
	if !ok {
		return nil, &rpcErrorBody{
			Code:    -32602,
			Message: "Invalid params",
			Data:    "media.inlineData is required",
		}, true
	}
	uri := mediaArtifacts.save(req.Media.MIMEType, data)
	return map[string]any{"uri": uri}, nil, true
}

// handleDeleteMediaArtifact answers DeleteMediaArtifact.
//
// The reference's response carries a success flag and an error
// message; the clone answers the flag and leaves the message out when
// there was nothing to report — proto-JSON omits a field that holds
// its default.
func (d *Dispatcher) handleDeleteMediaArtifact(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		URI string `json:"uri"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	uri := strings.TrimSpace(req.URI)
	if uri == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "uri is required"}, true
	}
	if !mediaArtifacts.delete(uri) {
		return map[string]any{
			"success":      false,
			"errorMessage": "no such media artifact",
		}, nil, true
	}
	return map[string]any{"success": true}, nil, true
}
