// The language-server file handlers own request validation, proto-JSON answers,
// read/display limits, and error mapping. The host only selects the filesystem.
package files

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// LSRoute dispatches the language server's file methods. ReadFile and ReadDir
// without a literal "uri" key fall through to the dashboard contract, exactly
// as in the original dispatcher. Unknown methods perform no filesystem access.
func LSRoute(s LSSeams, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var handler func(LSSeams, json.RawMessage) (any, *rpc.ErrorBody, bool)
	switch method {
	case "StatUri", "statUri":
		handler = handleLSStatUri
	case "ReadDir", "readDir":
		if strings.Contains(string(params), `"uri"`) {
			handler = handleLSReadDir
		}
	case "ReadFile", "readFile":
		if strings.Contains(string(params), `"uri"`) {
			handler = handleLSReadFile
		}
	case "WriteFile", "writeFile":
		handler = handleLSWriteFile
	case "CreateDirectory", "createDirectory":
		handler = handleLSCreateDirectory
	}
	if handler == nil {
		return nil, nil, false
	}
	if s.FS == nil {
		return nil, &rpc.ErrorBody{Code: -32603, Message: "Internal error", Data: "no language-server FS installed"}, true
	}
	return handler(s, params)
}

func handleLSStatUri(s LSSeams, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.URI == "" {
		return nil, &rpc.ErrorBody{Code: -32602, Message: "uri is required"}, true
	}
	p := ParseURIPath(req.URI)
	fi, err := s.FS.Stat(p)
	if err != nil {
		return nil, &rpc.ErrorBody{Code: -32004, Message: "Not found", Data: err.Error()}, true
	}
	// The reference's own answer is
	// {"fileType":"FILE_TYPE_FILE","modTime":"2026-10-26T13:47:00.907136440Z",
	//  "normalizedUri":"file:///…","size":"9293"}: the enum is spelled
	// by its name, the timestamp is a proto-JSON string, and the int64
	// is a string. See ls_wire.go.
	return map[string]any{
		"fileType":      lsFileTypeName(fi),
		"modTime":       lsTimestamp(fi.ModTime()),
		"normalizedUri": NormalizedURI(p),
		"size":          lsSize(fi.Size()),
	}, nil, true
}

func handleLSReadDir(s LSSeams, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.URI == "" {
		return nil, &rpc.ErrorBody{Code: -32602, Message: "uri is required"}, true
	}
	p := ParseURIPath(req.URI)
	entries, err := s.FS.ReadDir(p)
	if err != nil {
		return nil, &rpc.ErrorBody{Code: -32004, Message: "Not found", Data: err.Error()}, true
	}
	resEntries := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		childPath := filepath.Join(p, e.Name())
		fileType := fileTypeFile
		if e.IsDir() {
			fileType = fileTypeDirectory
		}
		resEntries = append(resEntries, map[string]any{
			"uri":      NormalizedURI(childPath),
			"fileType": fileType,
		})
	}
	if len(resEntries) == 0 {
		// proto-JSON omits an empty repeated field: the reference
		// answers an empty directory with {}.
		return map[string]any{}, nil, true
	}
	return map[string]any{
		"entries": resEntries,
	}, nil, true
}

const (
	lsReadChunkBytes         int64 = 20 << 20
	lsMediaDisplayLimitBytes int64 = 200 << 20
)

func isMediaDisplayFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3", ".wav", ".ogg", ".m4a", ".mp4", ".webm":
		return true
	default:
		return false
	}
}

func handleLSReadFile(s LSSeams, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var req struct {
		URI    string   `json:"uri"`
		Offset lsOffset `json:"offset"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.URI == "" {
		return nil, &rpc.ErrorBody{Code: -32602, Message: "uri is required"}, true
	}
	offset := int64(req.Offset)
	if offset < 0 {
		return nil, &rpc.ErrorBody{Code: -32602, Message: "offset must be >= 0"}, true
	}
	p := ParseURIPath(req.URI)
	f, err := s.FS.Open(p)
	if err != nil {
		return nil, &rpc.ErrorBody{Code: -32004, Message: "Not found", Data: err.Error()}, true
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, &rpc.ErrorBody{Code: -32004, Message: "Not found", Data: err.Error()}, true
	}
	size := fi.Size()
	limit := lsReadChunkBytes
	if isMediaDisplayFile(p) {
		limit = lsMediaDisplayLimitBytes
	}
	if size > limit {
		return nil, &rpc.ErrorBody{
			Code:    -32603,
			Message: fmt.Sprintf("file exceeds the %dMB display limit, so it can't be opened or downloaded here", limit>>20),
		}, true
	}
	if offset > size {
		return nil, &rpc.ErrorBody{Code: -32602, Message: "offset beyond file size"}, true
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, &rpc.ErrorBody{Code: -32603, Message: "Internal error", Data: err.Error()}, true
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, lsReadChunkBytes))
	if err != nil {
		return nil, &rpc.ErrorBody{Code: -32603, Message: "Internal error", Data: err.Error()}, true
	}
	truncated := offset+int64(len(data)) < size
	// `content` is a `bytes` field, and proto-JSON spells a bytes
	// field as base64. The raw text is not a legal answer: the Web
	// UI's decoder refuses it with "Cannot decode field
	// exa.language_server_pb.ReadFileResponse.content from JSON:
	// invalid base64 string", and the file never opens.
	answer := map[string]any{
		"content": lsEncodeBytes(data),
	}
	// `language` is exa.codeium_common_pb.Language, spelled by its
	// name. The reference reports one for the files it knows and none
	// at all for the files it does not.
	if lang := lsLanguageName(p); lang != "" {
		answer["language"] = lang
	}
	if truncated {
		answer["truncated"] = true
	}
	return answer, nil, true
}

func handleLSWriteFile(s LSSeams, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var req struct {
		URI     string `json:"uri"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.URI == "" {
		return nil, &rpc.ErrorBody{Code: -32602, Message: "uri is required"}, true
	}
	p := ParseURIPath(req.URI)
	// `content` is a `bytes` field on the request side too: the Web UI
	// sends it base64-encoded, and the reference refuses anything else
	// ("invalid value for bytes field content"). See ls_wire.go.
	content, err := lsDecodeBytes(req.Content)
	if err != nil {
		return nil, &rpc.ErrorBody{
			Code:    -32602,
			Message: "content is not base64",
			Data:    err.Error(),
		}, true
	}
	if err := s.FS.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, &rpc.ErrorBody{Code: -32603, Message: "MkdirAll failed", Data: err.Error()}, true
	}
	if err := s.FS.WriteFile(p, content, 0o644); err != nil {
		return nil, &rpc.ErrorBody{Code: -32603, Message: "WriteFile failed", Data: err.Error()}, true
	}
	return map[string]any{}, nil, true
}

func handleLSCreateDirectory(s LSSeams, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.URI == "" {
		return nil, &rpc.ErrorBody{Code: -32602, Message: "uri is required"}, true
	}
	p := ParseURIPath(req.URI)
	if err := s.FS.MkdirAll(p, 0o755); err != nil {
		return nil, &rpc.ErrorBody{Code: -32603, Message: "MkdirAll failed", Data: err.Error()}, true
	}
	return map[string]any{}, nil, true
}

// lsOffset preserves the language server's acceptance of a proto-JSON int64
// string or the Web UI's optimistic numeric copy (including null/empty zero).
type lsOffset int64

func (v *lsOffset) UnmarshalJSON(b []byte) error {
	text := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if text == "" || text == "null" {
		*v = 0
		return nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return err
	}
	*v = lsOffset(n)
	return nil
}
