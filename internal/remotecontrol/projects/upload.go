// Package projects — upload.go: the UploadProjectFile JSON-RPC
// method.
//
// The reference's JetskiRemoteAgentProxyService carries
// UploadProjectFile(UploadProjectFileRequest) →
// UploadProjectFileResponse (【反编译】, the 1.4.8 descriptor at
// out/local-macos/protos/remote_agent_proxy_1.4.8.txt). The request is
// `project_id`, `file_name`, `content` (bytes), `mime_type`,
// `size_bytes`, and — for large files — the `inline_content` /
// `blob_handle` oneof. The response is a ProjectResource:
// type, resource_id, display_name, url, project_id, mime_type,
// size_bytes, mount_path.
//
// This file owns the wire half: parameter validation, content decoding
// (proto JSON bytes are base64, but a dashboard may also send a raw
// string or a byte array), the per-chunk size cap that keeps a request
// under the daemon's 16 MiB line limit, and the resource envelope.
// upload_fs.go owns where the bytes land.
//
// Chunked upload
// --------------
// A single request carries one chunk. `offset` is the byte position the
// chunk starts at; a request without `offset` writes from the start and
// truncates the file. That makes an 8 MiB+ binary (the AGY 1.5.2 fix —
// "uploading PDFs, other binary files, or files larger than 8MB in a
// remote-agent session now works") a sequence of bounded requests
// instead of one oversized line, and it keeps the transport's base64
// expansion (4/3) safely inside the 16 MiB line budget.
//
// Stdlib-only. No third-party deps.
package projects

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// MethodUploadProjectFile is the reference RPC name.
const MethodUploadProjectFile = "UploadProjectFile"

// LegacyMethodUploadProjectFile is the clone's RemoteAgent-prefixed
// spelling, kept so dashboards written against the earlier naming
// keep working.
const LegacyMethodUploadProjectFile = "UploadRemoteAgentProjectFile"

// MaxUploadChunkSize caps the decoded bytes one request may carry.
// 10 MiB of binary becomes ~13.4 MiB of base64, which — with the JSON
// envelope — stays under the daemon's 16 MiB line limit. The previous
// frontend limit was 8 MiB, so a larger file is sent as several
// chunks of at most this size.
const MaxUploadChunkSize int64 = 10 << 20

// MaxUploadFileSize caps the assembled file. A project resource is
// meant to be opened as an attachment; 1 GiB is far above any real
// PDF or archive while still refusing an unbounded write.
const MaxUploadFileSize int64 = 1 << 30

// ErrInvalidUploadPath is returned by UploadStore.WriteChunk when a
// project id or file name would escape the store's root. The handler
// maps it to -32602.
var ErrInvalidUploadPath = errors.New("rc-projects: invalid upload path")

// UploadStore is the seam the upload handler writes through. The
// production implementation (FSUploadStore) roots project uploads
// under $ANTIGRAVITY_HOME/projects/<project-id>/uploads/.
//
// WriteChunk writes content at offset and returns the file's mount
// path and its total size after the write. offset == 0 truncates the
// file first, so a fresh upload starts clean while later chunks append
// at their offsets. Implementations must reject path traversal.
type UploadStore interface {
	WriteChunk(projectID, fileName string, offset int64, content []byte) (mountPath string, total int64, err error)
}

// uploadParams is the JSON shape of UploadProjectFile. Snake-case and
// proto-JSON camelCase spellings are both accepted: the clone's
// dashboard uses snake_case, while the reference's proto JSON uses
// camelCase.
type uploadParams struct {
	ProjectID      string          `json:"project_id"`
	ProjectIDCamel string          `json:"projectId"`
	FileName       string          `json:"file_name"`
	FileNameCamel  string          `json:"fileName"`
	Content        json.RawMessage `json:"content"`
	InlineContent  json.RawMessage `json:"inline_content"`
	InlineCamel    json.RawMessage `json:"inlineContent"`
	MimeType       string          `json:"mime_type"`
	MimeTypeCamel  string          `json:"mimeType"`
	SizeBytes      int64           `json:"size_bytes"`
	SizeCamel      int64           `json:"sizeBytes"`
	Offset         int64           `json:"offset"`
	// ContentEncoding is "base64" or "raw". Empty tries base64
	// first and falls back to raw text.
	ContentEncoding      string `json:"content_encoding"`
	ContentEncodingCamel string `json:"contentEncoding"`
}

// HandleUpload is the JSON-RPC handler for UploadProjectFile.
//
// Validation:
//
//   - project_id / file_name: required, non-empty after trim → -32602.
//   - offset: >= 0 → -32602 otherwise.
//   - content (or inline_content): at most MaxUploadChunkSize decoded
//     bytes → -32602 otherwise.
//   - size_bytes, when present: at most MaxUploadFileSize, and the
//     assembled position must not exceed it → -32602 otherwise.
//
// Store calls:
//
//   - no project store → -32603; no upload store → -32603.
//   - unknown project → -32004 (shared with the other project methods).
//   - path traversal / unreadable target → -32602 / -32603.
//
// Success carries the reference's ProjectResource envelope.
func HandleUpload(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no project store installed")
	}
	if s.Uploads == nil {
		return nil, rpc.InternalError("no upload store installed")
	}
	p, perr := decodeUploadParams(params)
	if perr != nil {
		return nil, perr
	}

	if _, err := s.Store.Get(nil, p.ProjectID); err != nil {
		return nil, MapError(err, "upload project file")
	}

	content, perr := decodeUploadContent(p)
	if perr != nil {
		return nil, perr
	}
	if int64(len(content)) > MaxUploadChunkSize {
		return nil, rpc.InvalidParams("content chunk is too large")
	}
	size := p.SizeBytes
	if size < 0 || size > MaxUploadFileSize {
		return nil, rpc.InvalidParams("size_bytes is out of range")
	}
	if p.Offset+int64(len(content)) > MaxUploadFileSize {
		return nil, rpc.InvalidParams("uploaded file is too large")
	}

	mountPath, total, err := s.Uploads.WriteChunk(p.ProjectID, p.FileName, p.Offset, content)
	if err != nil {
		if errors.Is(err, ErrInvalidUploadPath) {
			return nil, rpc.InvalidParams(err.Error())
		}
		return nil, rpc.InternalError("upload project file: " + err.Error())
	}

	return map[string]any{
		"resource": resourceOf(p, mountPath, total),
	}, nil
}

// decodeUploadParams unmarshals and validates the request fields shared
// by both spellings.
func decodeUploadParams(params json.RawMessage) (*uploadParams, *rpc.ErrorBody) {
	var p uploadParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if p.ProjectID == "" {
		p.ProjectID = p.ProjectIDCamel
	}
	if p.FileName == "" {
		p.FileName = p.FileNameCamel
	}
	if p.MimeType == "" {
		p.MimeType = p.MimeTypeCamel
	}
	if p.SizeBytes == 0 {
		p.SizeBytes = p.SizeCamel
	}
	if p.ContentEncoding == "" {
		p.ContentEncoding = p.ContentEncodingCamel
	}
	p.ProjectID = strings.TrimSpace(p.ProjectID)
	p.FileName = strings.TrimSpace(p.FileName)
	if p.ProjectID == "" {
		return nil, rpc.InvalidParams("project_id is required")
	}
	if p.FileName == "" {
		return nil, rpc.InvalidParams("file_name is required")
	}
	if p.Offset < 0 {
		return nil, rpc.InvalidParams("offset must be >= 0")
	}
	return &p, nil
}

// decodeUploadContent picks the content field and decodes it. Proto
// JSON carries `bytes` as base64; a raw string (a text attachment sent
// by an older dashboard) and a JSON byte array are accepted too.
func decodeUploadContent(p *uploadParams) ([]byte, *rpc.ErrorBody) {
	raw := p.Content
	if len(raw) == 0 || string(raw) == "null" {
		raw = p.InlineContent
	}
	if len(raw) == 0 || string(raw) == "null" {
		raw = p.InlineCamel
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	data, err := decodeBytes(raw, p.ContentEncoding)
	if err != nil {
		return nil, rpc.InvalidParams("content: " + err.Error())
	}
	return data, nil
}

// decodeBytes decodes one JSON value into bytes:
//
//   - a string is base64 (standard or URL alphabet, padded or raw);
//     with encoding "raw" it is taken as UTF-8 bytes instead, and with
//     encoding "base64" an invalid string is an error. The default
//     tries base64 first and falls back to raw text;
//   - an array of numbers is a byte array;
//   - anything else is an error.
func decodeBytes(raw json.RawMessage, encoding string) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, err
		}
		switch strings.ToLower(strings.TrimSpace(encoding)) {
		case "raw", "utf8", "utf-8", "text":
			return []byte(s), nil
		case "base64":
			if data, ok := decodeBase64(s); ok {
				return data, nil
			}
			return nil, errors.New("invalid base64")
		default:
			if data, ok := decodeBase64(s); ok {
				return data, nil
			}
			return []byte(s), nil
		}
	case '[':
		var data []byte
		if err := json.Unmarshal(trimmed, &data); err != nil {
			return nil, err
		}
		return data, nil
	default:
		return nil, errors.New("must be a base64 string or a byte array")
	}
}

// decodeBase64 tries the four base64 spellings a JSON producer may use.
func decodeBase64(s string) ([]byte, bool) {
	if s == "" {
		return nil, true
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if data, err := enc.DecodeString(s); err == nil {
			return data, true
		}
	}
	return nil, false
}

// resourceOf builds the ProjectResource the reference returns.
func resourceOf(p *uploadParams, mountPath string, total int64) map[string]any {
	mimeType := p.MimeType
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(p.FileName))
	}
	if mimeType == "" {
		if total > 0 && strings.EqualFold(filepath.Ext(p.FileName), ".pdf") {
			mimeType = "application/pdf"
		} else {
			mimeType = "application/octet-stream"
		}
	}
	return map[string]any{
		"type":         "file",
		"resource_id":  "project-file:" + p.ProjectID + "/" + p.FileName,
		"display_name": p.FileName,
		"url":          "project-file://" + p.ProjectID + "/" + url.PathEscape(p.FileName),
		"project_id":   p.ProjectID,
		"mime_type":    mimeType,
		"size_bytes":   total,
		"mount_path":   mountPath,
	}
}
