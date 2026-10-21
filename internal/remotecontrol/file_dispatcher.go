// Package remotecontrol — file_dispatcher.go: the dispatch
// table for the two read-only filesystem JSON-RPC methods
// introduced in cycle rc-file-ops:
//
//	ReadRemoteAgentFile  — read a file's content (with size cap)
//	ReadRemoteAgentDir   — list entries of a directory
//
// Why a dedicated file (rather than another switch arm in
// dispatch.go):
//
//   - Both methods share an FSProvider seam and a small
//     sentinel-error map (ErrPathNotAllowed → -32007,
//     ErrNotRegularFile → -32008, ErrTooLarge → -32603);
//     keeping the dispatch + sentinel handling in one file
//     avoids a second switch / second mapping in
//     dispatch.go.
//
//   - The dispatcher-level switch in dispatch.go grows by
//     two case entries; the per-method bodies live here.
//
//   - Future cycles (rc-list-projects / rc-update-project)
//     may need filesystem access too; they reuse the same
//     FSProvider seam rather than spawning their own.
//
// Sentinel mapping lives in mapFSError below: the brief's
// three custom codes (-32007 / -32008 / -32603) plus the
// regular -32602 for parameter validation. The dispatcher
// never propagates a Go error; it returns a JSON-RPC
// envelope built from the sentinel match.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"encoding/json"
	"errors"
	"fmt"
)

// readRemoteAgentFileParams is the JSON shape the
// dashboard posts for ReadRemoteAgentFile. Field names
// mirror the agy decompiled ReadRemoteAgentFile
// envelope; offset is optional, maxBytes is optional
// (default DefaultReadMaxBytes = 1 MiB).
type readRemoteAgentFileParams struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"maxBytes"`
	Offset   int64  `json:"offset"`
}

// readRemoteAgentFileResult is the JSON shape the
// dispatcher returns on a successful read. Path is the
// canonical absolute path (ResolvePolicy's output); Size
// is the file's total size in bytes; Truncated is true
// iff the returned content is fewer than (size - offset)
// bytes; Encoding is "utf-8" for text or "binary" for
// anything containing a NUL byte in the first 8 KiB;
// Content is the utf-8 string verbatim for text, or the
// base64 encoding of the bytes for binary.
type readRemoteAgentFileResult struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Encoding  string `json:"encoding"`
	Content   string `json:"content"`
}

// readRemoteAgentDirParams is the JSON shape the
// dashboard posts for ReadRemoteAgentDir. maxEntries is
// optional (default DefaultReadDirMaxEntries = 1000).
type readRemoteAgentDirParams struct {
	Path       string `json:"path"`
	MaxEntries int    `json:"maxEntries"`
}

// readRemoteAgentDirResult is the JSON shape the
// dispatcher returns on a successful listing. Path is
// the canonical absolute path; TotalEntries is the full
// entry count (NOT capped by maxEntries); Truncated is
// true iff the returned entries were capped.
type readRemoteAgentDirResult struct {
	Path         string     `json:"path"`
	Entries      []DirEntry `json:"entries"`
	TotalEntries int        `json:"total_entries"`
	Truncated    bool       `json:"truncated"`
}

// handleReadRemoteAgentFile is the JSON-RPC handler for
// "ReadRemoteAgentFile". Cycle rc-file-ops.
//
// Param validation:
//
//   - path: required, non-empty. Absent or empty returns
//     -32602 (matches the dispatcher's general
//     "required-field missing" pattern).
//   - maxBytes: optional, capped at MaxReadMaxBytes
//     (16 MiB). Anything above → -32602. Zero / unset
//     → DefaultReadMaxBytes (1 MiB).
//   - offset: optional, >= 0. Negative → -32602.
//
// FS resolution:
//
//   - cfg.FS == nil → -32603 (no FS provider installed).
//   - cfg.FS.ResolvePolicy(path) returning
//     ErrPathNotAllowed → -32007.
//   - cfg.FS.ResolvePolicy(path) returning any other
//     error → -32603.
//
// FS read:
//
//   - ErrNotRegularFile → -32008.
//   - ErrTooLarge → -32603 (the brief: "ErrTooLarge →
//     -32603 (returns an error result envelope saying
//     so)").
//   - other I/O errors (offset > size, etc.) → -32603.
//
// On success the result envelope carries the canonical
// path, the file's total size, the truncated flag, the
// encoding label ("utf-8" / "binary"), and the content
// (string for utf-8, base64 for binary).
func (d *Dispatcher) handleReadRemoteAgentFile(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.FS == nil {
		return nil, internalError("no FS provider installed")
	}
	p, perr := decodeReadFileParams(params)
	if perr != nil {
		return nil, perr
	}
	canonical, err := d.cfg.FS.ResolvePolicy(p.Path)
	if err != nil {
		return nil, mapFSError(err, "resolve policy")
	}
	data, total, truncated, isBinary, err := d.cfg.FS.ReadFile(canonical, p.MaxBytes, p.Offset)
	if err != nil {
		return nil, mapFSError(err, "read file")
	}
	encoding := "utf-8"
	if isBinary {
		encoding = "binary"
	}
	return map[string]any{
		"path":      canonical,
		"size":      total,
		"truncated": truncated,
		"encoding":  encoding,
		"content":   encodeContent(data, isBinary),
	}, nil
}

// decodeReadFileParams unmarshals
// ReadRemoteAgentFile params, applies the validation
// chain (non-string path, trim, required path,
// bounded offset / maxBytes, default maxBytes), and
// returns the populated struct + nil (or zero +
// *rpcErrorBody on failure). Extracted from
// handleReadRemoteAgentFile to keep the handler body
// under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeReadFileParams(params json.RawMessage) (*readRemoteAgentFileParams, *rpcErrorBody) {
	var p readRemoteAgentFileParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "path", p.Path) {
		return nil, invalidParams("path must be a string")
	}
	p.Path = stringsTrimSpace(p.Path)
	if p.Path == "" {
		return nil, invalidParams("path is required")
	}
	if p.Offset < 0 {
		return nil, invalidParams("offset must be >= 0")
	}
	if p.MaxBytes < 0 {
		return nil, invalidParams("maxBytes must be >= 0")
	}
	if p.MaxBytes == 0 {
		p.MaxBytes = DefaultReadMaxBytes
	}
	if p.MaxBytes > MaxReadMaxBytes {
		return nil, invalidParams(fmt.Sprintf("maxBytes must be <= %d", MaxReadMaxBytes))
	}
	return &p, nil
}

// handleReadRemoteAgentDir is the JSON-RPC handler for
// "ReadRemoteAgentDir". Cycle rc-file-ops.
//
// Param validation:
//
//   - path: required, non-empty. Absent or empty →
//     -32602.
//   - maxEntries: optional, capped at
//     MaxReadDirMaxEntries (10000). Anything above →
//     -32602. Zero / unset → DefaultReadDirMaxEntries
//     (1000).
//
// FS resolution:
//
//   - cfg.FS == nil → -32603.
//   - cfg.FS.ResolvePolicy(path) returning
//     ErrPathNotAllowed → -32007.
//   - cfg.FS.ResolvePolicy(path) returning any other
//     error → -32603.
//
// FS read:
//
//   - other I/O errors → -32603.
//   - No "not a directory" sentinel: the production
//     WorkdirScope rejects non-directories via
//     ErrNotRegularFile if you call ReadFile on them;
//     ReadDir's stdlib os.File.Readdirnames returns an
//     error that the dispatcher maps to -32603 (the
//     brief: "Errors: -32602, -32007, -32603 only").
//
// On success the result envelope carries the canonical
// path, the entries array, the full total count, and the
// truncated flag.
func (d *Dispatcher) handleReadRemoteAgentDir(params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if d.cfg.FS == nil {
		return nil, internalError("no FS provider installed")
	}
	p, perr := decodeReadDirParams(params)
	if perr != nil {
		return nil, perr
	}
	canonical, err := d.cfg.FS.ResolvePolicy(p.Path)
	if err != nil {
		return nil, mapFSError(err, "resolve policy")
	}
	entries, total, truncated, err := d.cfg.FS.ReadDir(canonical, p.MaxEntries)
	if err != nil {
		return nil, mapFSError(err, "read dir")
	}
	if entries == nil {
		entries = []DirEntry{}
	}
	return map[string]any{
		"path":          canonical,
		"entries":       entries,
		"total_entries": total,
		"truncated":     truncated,
	}, nil
}

// decodeReadDirParams unmarshals ReadRemoteAgentDir
// params, applies the validation chain (non-string
// path, trim, required path, bounded maxEntries,
// default maxEntries), and returns the populated
// struct + nil (or zero + *rpcErrorBody on failure).
// Extracted from handleReadRemoteAgentDir to keep
// the handler body under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func decodeReadDirParams(params json.RawMessage) (*readRemoteAgentDirParams, *rpcErrorBody) {
	var p readRemoteAgentDirParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "path", p.Path) {
		return nil, invalidParams("path must be a string")
	}
	p.Path = stringsTrimSpace(p.Path)
	if p.Path == "" {
		return nil, invalidParams("path is required")
	}
	if p.MaxEntries < 0 {
		return nil, invalidParams("maxEntries must be >= 0")
	}
	if p.MaxEntries == 0 {
		p.MaxEntries = DefaultReadDirMaxEntries
	}
	if p.MaxEntries > MaxReadDirMaxEntries {
		return nil, invalidParams(fmt.Sprintf("maxEntries must be <= %d", MaxReadDirMaxEntries))
	}
	return &p, nil
}

// mapFSError maps the FSProvider sentinel errors to the
// JSON-RPC error envelopes the brief calls for:
//
//   - ErrPathNotAllowed → -32007 "Path not allowed"
//   - ErrNotRegularFile → -32008 "Not a regular file"
//   - ErrTooLarge        → -32603 "Internal error"
//     with the message prefixed by op so a dashboard
//     sees "read file: file too large" etc.
//   - any other error    → -32603 "Internal error"
//
// op is the operation label ("resolve policy", "read
// file", "read dir") so a dashboard log shows which
// stage failed.
func mapFSError(err error, op string) *rpcErrorBody {
	switch {
	case errors.Is(err, ErrPathNotAllowed):
		return &rpcErrorBody{Code: -32007, Message: "Path not allowed", Data: err.Error()}
	case errors.Is(err, ErrNotRegularFile):
		return &rpcErrorBody{Code: -32008, Message: "Not a regular file", Data: err.Error()}
	default:
		return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: fmt.Sprintf("%s: %v", op, err)}
	}
}
