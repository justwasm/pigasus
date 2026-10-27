// Tests for the rc-file-ops cycle. Covers:
//
//   - the FSProvider seam at the in-process level:
//     MemFS happy paths and rejection paths (sentinel
//     errors map correctly);
//   - the WorkdirScope production seam: workspace /
//     --add-dir / sandbox resolution;
//   - the dispatcher-level entry points for
//     ReadRemoteAgentFile and ReadRemoteAgentDir;
//   - the JSON-RPC envelope shape the dashboard sees.
//
// Wire-level round-trips (ws adapter + JSON marshalling)
// live in remotetransport/tcp_test.go, mirroring
// the split the rc-list-readonly cycle established.
package remotecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newFSDispatcher returns a Dispatcher with the supplied
// FSProvider pre-installed and a SessionState that satisfies
// NewDispatcher's "Session is required" precondition. The
// helper is intentionally small (no fixture cascades, no
// list backend) because the two file methods are pure reads
// over the FSProvider seam.
func newFSDispatcher(t *testing.T, fs FSProvider) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session: sess,
		FS:      fs,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// fsMemFS is a small in-memory FSProvider fixture the
// tests below drive. It exposes an "allowed" directory
// with a few files (text, binary, a 100-KiB text file
// for the truncation test) plus an isolated "secret"
// directory the dispatcher must reject via the production
// policy. The fixture deliberately has no policy of its
// own — the rejection tests pair MemFS with a WorkdirScope
// whose Workspace is the allowed dir.
func fsMemFS(t *testing.T) *MemFS {
	t.Helper()
	bin := []byte{0x00, 0x01, 0x02, 0x03, 0x00, 0x05}
	big := make([]byte, 100*1024)
	for i := range big {
		big[i] = byte('A' + (i % 26))
	}
	return NewMemFS(map[string][]MemFSEntry{
		"/allowed": {
			{Name: "main.go", IsDir: false, Data: []byte("package main\n\nfunc main() {}\n")},
			{Name: "binary.bin", IsDir: false, Data: bin},
			{Name: "big.txt", IsDir: false, Data: big},
			{Name: "sub", IsDir: true, Data: nil},
		},
		"/secret": {
			{Name: "secret.txt", IsDir: false, Data: []byte("top secret\n")},
		},
	})
}

// dispatchFile sends a ReadRemoteAgentFile or
// ReadRemoteAgentDir request through the dispatcher and
// returns the (result, error) pair. The helper centralises
// the boilerplate every test would otherwise repeat.
func dispatchFile(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	if params != nil {
		var err error
		raw, err = json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
	}
	req := jsonRPCFor(1, method, raw)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"].(map[string]any); ok {
		return nil, errObj
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: no result, got %#v", method, resp)
	}
	return res, nil
}

// codeAsInt coerces an error code from the dispatcher
// envelope to an int. In-process, the dispatcher stores
// the code as a Go int; over the wire, json.Unmarshal
// coerces it to a float64. Tests should use this helper
// rather than asserting a concrete numeric type so they
// pass in both shapes.
func codeAsInt(t *testing.T, v any) int {
	t.Helper()
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	default:
		t.Fatalf("error code = %T, want int or float64", v)
		return 0
	}
}

// sizeAsInt coerces a numeric field (size / total_entries)
// from the dispatcher envelope to an int. Same shape as
// codeAsInt: in-process is int, over the wire is float64.
func sizeAsInt(t *testing.T, v any) int {
	t.Helper()
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		t.Fatalf("numeric field = %T, want int / int64 / float64", v)
		return 0
	}
}

// TestFSProvider_MemFS_AllowsAllowed covers the
// happy-path ResolvePolicy. MemFS has no policy of its
// own (it returns the path verbatim); the test pins
// that the dispatcher accepts the canonical path the
// MemFS surfaces without modification.
func TestFSProvider_MemFS_AllowsAllowed(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	res, _ := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path": "/allowed/main.go",
	})
	want := "package main\n\nfunc main() {}\n"
	if res["path"] != "/allowed/main.go" {
		t.Errorf("path = %v, want /allowed/main.go", res["path"])
	}
	if res["encoding"] != "utf-8" {
		t.Errorf("encoding = %v, want utf-8", res["encoding"])
	}
	if res["content"] != want {
		t.Errorf("content = %v, want %q", res["content"], want)
	}
	if res["truncated"] != false {
		t.Errorf("truncated = %v, want false", res["truncated"])
	}
	if size := sizeAsInt(t, res["size"]); size != len(want) {
		t.Errorf("size = %v, want %d", size, len(want))
	}
}

// TestFSProvider_MemFS_RejectsSecret covers the
// "production WorkdirScope rejects an out-of-workspace
// path" path. The MemFS has a /secret directory; the
// WorkdirScope's Workspace is /allowed, so the policy
// returns ErrPathNotAllowed and the dispatcher maps
// it to -32007.
func TestFSProvider_MemFS_RejectsSecret(t *testing.T) {
	m := fsMemFS(t)
	scope := &WorkdirScope{Workspace: "/allowed"}
	d := newFSDispatcher(t, newPolicyOverFS(m, scope))

	res, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path": "/secret/secret.txt",
	})
	if res != nil {
		t.Fatalf("secret read: got result %#v, want error", res)
	}
	if codeAsInt(t, errObj["code"]) != -32007 {
		t.Errorf("code = %v, want -32007 (Path not allowed)", errObj["code"])
	}
	if errObj["message"] != "Path not allowed" {
		t.Errorf("message = %v, want 'Path not allowed'", errObj["message"])
	}
}

// policyOverFS is a small adapter that routes
// ResolvePolicy through a policy (typically
// WorkdirScope) and ReadFile / ReadDir through a
// fixture FSProvider (typically MemFS). The
// production pairing is "policy + real fs"; the
// test pairing is "policy + MemFS" so the rejection
// path can be exercised without a real filesystem.
type policyOverFS struct {
	policy FSProvider
	fs     FSProvider
}

func newPolicyOverFS(fs, policy FSProvider) *policyOverFS {
	return &policyOverFS{policy: policy, fs: fs}
}

func (p *policyOverFS) ResolvePolicy(path string) (string, error) {
	return p.policy.ResolvePolicy(path)
}

func (p *policyOverFS) ReadFile(canonical string, maxBytes int64, offset int64) (data []byte, total int64, truncated bool, isBinary bool, err error) {
	return p.fs.ReadFile(canonical, maxBytes, offset)
}

func (p *policyOverFS) ReadDir(canonical string, maxEntries int) (entries []DirEntry, total int, truncated bool, err error) {
	return p.fs.ReadDir(canonical, maxEntries)
}

// TestReadRemoteAgentFile_Ok_UTF8 covers the happy
// path through the dispatcher: a small text file comes
// back with encoding=utf-8 and the original bytes.
func TestReadRemoteAgentFile_Ok_UTF8(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	res, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path": "/allowed/main.go",
	})
	if errObj != nil {
		t.Fatalf("utf-8 read: error = %#v", errObj)
	}
	if res["encoding"] != "utf-8" {
		t.Errorf("encoding = %v, want utf-8", res["encoding"])
	}
	if res["content"] != "package main\n\nfunc main() {}\n" {
		t.Errorf("content mismatch: %v", res["content"])
	}
}

// TestReadRemoteAgentFile_Binary covers the binary
// path: synthetic binary content (NUL bytes in the
// first 8 KiB) comes back with encoding=binary and
// the base64-decoded content equals the original
// bytes.
func TestReadRemoteAgentFile_Binary(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	res, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path": "/allowed/binary.bin",
	})
	if errObj != nil {
		t.Fatalf("binary read: error = %#v", errObj)
	}
	if res["encoding"] != "binary" {
		t.Errorf("encoding = %v, want binary", res["encoding"])
	}
	got, err := base64.StdEncoding.DecodeString(res["content"].(string))
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	want := []byte{0x00, 0x01, 0x02, 0x03, 0x00, 0x05}
	if string(got) != string(want) {
		t.Errorf("decoded content = %v, want %v", got, want)
	}
	if size := sizeAsInt(t, res["size"]); size != 6 {
		t.Errorf("size = %v, want 6", size)
	}
}

// TestReadRemoteAgentFile_Truncated covers the cap path:
// file is 100 KiB; client requests maxBytes=4096; result
// has truncated=true and content length 4096.
func TestReadRemoteAgentFile_Truncated(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	res, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path":     "/allowed/big.txt",
		"maxBytes": 4096,
	})
	if errObj != nil {
		t.Fatalf("truncated read: error = %#v", errObj)
	}
	if res["truncated"] != true {
		t.Errorf("truncated = %v, want true", res["truncated"])
	}
	if size := sizeAsInt(t, res["size"]); size != 100*1024 {
		t.Errorf("size = %v, want %d", size, 100*1024)
	}
	if content, _ := res["content"].(string); len(content) != 4096 {
		t.Errorf("content length = %d, want 4096", len(content))
	}
}

// TestReadRemoteAgentFile_OffsetBeyondSize covers the
// "offset > size" path: the dispatcher returns -32603
// with an explanatory message.
func TestReadRemoteAgentFile_OffsetBeyondSize(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	_, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path":   "/allowed/main.go",
		"offset": 1000,
	})
	if errObj == nil {
		t.Fatalf("offset beyond size: got result, want error")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %v, want -32603 (Internal error)", errObj["code"])
	}
	if !strings.Contains(string(errObj["message"].(string)), "Internal error") {
		t.Errorf("message = %v, want 'Internal error'", errObj["message"])
	}
}

// TestReadRemoteAgentFile_PathNotAllowed covers the
// policy rejection path: a path outside the
// WorkdirScope's workspace returns -32007.
func TestReadRemoteAgentFile_PathNotAllowed(t *testing.T) {
	m := fsMemFS(t)
	scope := &WorkdirScope{Workspace: "/allowed"}
	d := newFSDispatcher(t, newPolicyOverFS(m, scope))

	_, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path": "/secret/secret.txt",
	})
	if errObj == nil {
		t.Fatalf("forbidden path: got result, want error")
	}
	if codeAsInt(t, errObj["code"]) != -32007 {
		t.Errorf("code = %v, want -32007 (Path not allowed)", errObj["code"])
	}
}

// TestReadRemoteAgentFile_NotRegularFile covers the
// directory-rejection path: requesting a directory
// returns -32008 "Not a regular file".
func TestReadRemoteAgentFile_NotRegularFile(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	_, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path": "/allowed/sub",
	})
	if errObj == nil {
		t.Fatalf("directory read: got result, want error")
	}
	if codeAsInt(t, errObj["code"]) != -32008 {
		t.Errorf("code = %v, want -32008 (Not a regular file)", errObj["code"])
	}
}

// TestReadRemoteAgentFile_MaxBytesTooLarge covers the
// "maxBytes > 16 MiB" rejection at the param-validation
// step (no FS call happens).
func TestReadRemoteAgentFile_MaxBytesTooLarge(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	_, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path":     "/allowed/main.go",
		"maxBytes": MaxReadMaxBytes + 1,
	})
	if errObj == nil {
		t.Fatalf("maxBytes too large: got result, want error")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %v, want -32602 (Invalid params)", errObj["code"])
	}
}

// TestReadRemoteAgentFile_NegativeOffset covers the
// "offset < 0" rejection.
func TestReadRemoteAgentFile_NegativeOffset(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	_, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path":   "/allowed/main.go",
		"offset": -1,
	})
	if errObj == nil {
		t.Fatalf("negative offset: got result, want error")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %v, want -32602 (Invalid params)", errObj["code"])
	}
}

// TestReadRemoteAgentFile_NoFSProvider covers the
// "FS not installed" path: cfg.FS == nil surfaces as
// -32603 (the dispatcher can't reach a filesystem
// without a provider).
func TestReadRemoteAgentFile_NoFSProvider(t *testing.T) {
	d := newFSDispatcher(t, nil)
	_, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path": "/allowed/main.go",
	})
	if errObj == nil {
		t.Fatalf("no FS: got result, want error")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %v, want -32603 (Internal error)", errObj["code"])
	}
}

// TestReadRemoteAgentDir_Ok covers the happy path:
// the dispatcher returns the entry list for the
// /allowed directory.
func TestReadRemoteAgentDir_Ok(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	res, errObj := dispatchFile(t, d, "ReadRemoteAgentDir", map[string]any{
		"path": "/allowed",
	})
	if errObj != nil {
		t.Fatalf("dir list: error = %#v", errObj)
	}
	if res["path"] != "/allowed" {
		t.Errorf("path = %v, want /allowed", res["path"])
	}
	if total := sizeAsInt(t, res["total_entries"]); total != 4 {
		t.Errorf("total_entries = %v, want 4", total)
	}
	if res["truncated"] != false {
		t.Errorf("truncated = %v, want false", res["truncated"])
	}
	entries, ok := res["entries"].([]DirEntry)
	if !ok {
		t.Fatalf("entries type = %T, want []DirEntry", res["entries"])
	}
	if len(entries) != 4 {
		t.Fatalf("entries length = %d, want 4", len(entries))
	}
	// Entries are sorted by name (case-sensitive,
	// uppercase letters sort before lowercase in
	// ASCII). big.txt first, then binary.bin, then
	// main.go, then sub.
	want := []string{"big.txt", "binary.bin", "main.go", "sub"}
	for i, w := range want {
		if entries[i].Name != w {
			t.Errorf("entries[%d].Name = %q, want %q", i, entries[i].Name, w)
		}
	}
	// Type coverage: sub is a directory; the other three are files.
	gotTypes := map[string]string{}
	for _, e := range entries {
		gotTypes[e.Name] = e.Type
	}
	if gotTypes["sub"] != "dir" {
		t.Errorf("sub.type = %v, want dir", gotTypes["sub"])
	}
	for _, n := range []string{"binary.bin", "big.txt", "main.go"} {
		if gotTypes[n] != "file" {
			t.Errorf("%s.type = %v, want file", n, gotTypes[n])
		}
	}
}

// TestReadRemoteAgentDir_TruncatedByMaxEntries covers
// the cap-by-maxEntries path: maxEntries=2 on a
// 4-entry directory produces truncated=true and
// only the first 2 (alphabetical) entries.
func TestReadRemoteAgentDir_TruncatedByMaxEntries(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	res, errObj := dispatchFile(t, d, "ReadRemoteAgentDir", map[string]any{
		"path":       "/allowed",
		"maxEntries": 2,
	})
	if errObj != nil {
		t.Fatalf("capped dir list: error = %#v", errObj)
	}
	if res["truncated"] != true {
		t.Errorf("truncated = %v, want true", res["truncated"])
	}
	if total := sizeAsInt(t, res["total_entries"]); total != 4 {
		t.Errorf("total_entries = %v, want 4", total)
	}
	entries, ok := res["entries"].([]DirEntry)
	if !ok {
		t.Fatalf("entries type = %T, want []DirEntry", res["entries"])
	}
	if len(entries) != 2 {
		t.Errorf("entries length = %d, want 2", len(entries))
	}
}

// TestReadRemoteAgentDir_PathNotAllowed covers the
// policy rejection path for the directory method:
// a path outside the WorkdirScope's workspace
// returns -32007.
func TestReadRemoteAgentDir_PathNotAllowed(t *testing.T) {
	m := fsMemFS(t)
	scope := &WorkdirScope{Workspace: "/allowed"}
	d := newFSDispatcher(t, newPolicyOverFS(m, scope))

	_, errObj := dispatchFile(t, d, "ReadRemoteAgentDir", map[string]any{
		"path": "/secret",
	})
	if errObj == nil {
		t.Fatalf("forbidden dir: got result, want error")
	}
	if codeAsInt(t, errObj["code"]) != -32007 {
		t.Errorf("code = %v, want -32007 (Path not allowed)", errObj["code"])
	}
}

// TestReadRemoteAgentDir_MaxEntriesTooLarge covers
// the "maxEntries > 10000" rejection.
func TestReadRemoteAgentDir_MaxEntriesTooLarge(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	_, errObj := dispatchFile(t, d, "ReadRemoteAgentDir", map[string]any{
		"path":       "/allowed",
		"maxEntries": MaxReadDirMaxEntries + 1,
	})
	if errObj == nil {
		t.Fatalf("maxEntries too large: got result, want error")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %v, want -32602 (Invalid params)", errObj["code"])
	}
}

// TestReadRemoteAgentFile_EmptyPath covers the
// "missing path" param-validation path.
func TestReadRemoteAgentFile_EmptyPath(t *testing.T) {
	m := fsMemFS(t)
	d := newFSDispatcher(t, m)

	_, errObj := dispatchFile(t, d, "ReadRemoteAgentFile", map[string]any{
		"path": "",
	})
	if errObj == nil {
		t.Fatalf("empty path: got result, want error")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %v, want -32602 (Invalid params)", errObj["code"])
	}
}

// TestWorkdirScope_AllowList covers the --add-dir
// path: a path inside an allowed dir is permitted
// even when outside the workspace.
func TestWorkdirScope_AllowList(t *testing.T) {
	s := &WorkdirScope{
		Workspace: "/workspace",
		Allow:     []string{"/extra"},
	}
	canon, err := s.ResolvePolicy("/extra/data.txt")
	if err != nil {
		t.Fatalf("allow-list path: error = %v", err)
	}
	if canon != "/extra/data.txt" {
		t.Errorf("canonical = %q, want /extra/data.txt", canon)
	}
	// /elsewhere still rejected.
	if _, err := s.ResolvePolicy("/elsewhere/x"); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("elsewhere: err = %v, want ErrPathNotAllowed", err)
	}
	// workspace subdir still allowed.
	canon, err = s.ResolvePolicy("/workspace/src/main.go")
	if err != nil {
		t.Fatalf("workspace subdir: error = %v", err)
	}
	if canon != "/workspace/src/main.go" {
		t.Errorf("canonical = %q, want /workspace/src/main.go", canon)
	}
}

// TestWorkdirScope_RelativePath covers the
// "relative path" path: "src/main.go" resolves
// against the workspace.
func TestWorkdirScope_RelativePath(t *testing.T) {
	s := &WorkdirScope{Workspace: "/workspace"}
	canon, err := s.ResolvePolicy("src/main.go")
	if err != nil {
		t.Fatalf("relative path: error = %v", err)
	}
	if canon != "/workspace/src/main.go" {
		t.Errorf("canonical = %q, want /workspace/src/main.go", canon)
	}
}

// TestWorkdirScope_EmptyPath covers the "empty path"
// path: returns ErrPathNotAllowed.
func TestWorkdirScope_EmptyPath(t *testing.T) {
	s := &WorkdirScope{Workspace: "/workspace"}
	if _, err := s.ResolvePolicy(""); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("empty path: err = %v, want ErrPathNotAllowed", err)
	}
}

// TestMemFS_ReadFile_Binary covers the MemFS-level
// binary-detection path: bytes with NULs are
// classified as binary.
func TestMemFS_ReadFile_Binary(t *testing.T) {
	m := NewMemFS(map[string][]MemFSEntry{
		"/x": {
			{Name: "b", IsDir: false, Data: []byte{0x00, 0x01, 0x02}},
		},
	})
	data, total, truncated, isBinary, err := m.ReadFile("/x/b", 1024, 0)
	if err != nil {
		t.Fatalf("MemFS.ReadFile: %v", err)
	}
	if !isBinary {
		t.Errorf("isBinary = false, want true")
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if truncated {
		t.Errorf("truncated = true, want false")
	}
	if len(data) != 3 {
		t.Errorf("data length = %d, want 3", len(data))
	}
}

// TestMemFS_ReadFile_Directory covers the
// MemFS-level "not a regular file" path: reading a
// directory returns ErrNotRegularFile.
func TestMemFS_ReadFile_Directory(t *testing.T) {
	m := NewMemFS(map[string][]MemFSEntry{
		"/x": {
			{Name: "sub", IsDir: true, Data: nil},
		},
	})
	_, _, _, _, err := m.ReadFile("/x/sub", 1024, 0)
	if !errors.Is(err, ErrNotRegularFile) {
		t.Errorf("MemFS.ReadFile on directory: err = %v, want ErrNotRegularFile", err)
	}
}

// TestMemFS_ReadFile_OffsetBeyondSize covers the
// MemFS-level offset > size path.
func TestMemFS_ReadFile_OffsetBeyondSize(t *testing.T) {
	m := NewMemFS(map[string][]MemFSEntry{
		"/x": {
			{Name: "f", IsDir: false, Data: []byte("hi")},
		},
	})
	_, total, _, _, err := m.ReadFile("/x/f", 1024, 100)
	if err == nil {
		t.Fatalf("offset beyond size: got nil error")
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
}

// TestWorkdirScope_ReadFile covers the production
// ReadFile path against a real temporary file. The
// test creates a tmp file, configures a WorkdirScope
// whose workspace is the tmp dir's parent, and
// exercises the read / truncation / binary paths.
func TestWorkdirScope_ReadFile(t *testing.T) {
	dir := t.TempDir()
	// text file
	textPath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(textPath, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// binary file
	binPath := filepath.Join(dir, "bin.bin")
	if err := os.WriteFile(binPath, []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// big file
	big := make([]byte, 8*1024)
	for i := range big {
		big[i] = byte('A' + (i % 26))
	}
	bigPath := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(bigPath, big, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := &WorkdirScope{Workspace: dir}

	// text → utf-8
	data, total, truncated, isBinary, err := s.ReadFile(textPath, 1024, 0)
	if err != nil {
		t.Fatalf("text ReadFile: %v", err)
	}
	if isBinary || truncated {
		t.Errorf("text: isBinary=%v truncated=%v, want false false", isBinary, truncated)
	}
	if total != 12 {
		t.Errorf("text total = %d, want 12", total)
	}
	if string(data) != "hello world\n" {
		t.Errorf("text data = %q, want hello world\\n", data)
	}

	// binary → binary
	_, _, _, isBinary, err = s.ReadFile(binPath, 1024, 0)
	if err != nil {
		t.Fatalf("bin ReadFile: %v", err)
	}
	if !isBinary {
		t.Errorf("bin: isBinary=false, want true")
	}

	// big → truncated
	_, total, truncated, _, err = s.ReadFile(bigPath, 1024, 0)
	if err != nil {
		t.Fatalf("big ReadFile: %v", err)
	}
	if total != 8*1024 {
		t.Errorf("big total = %d, want %d", total, 8*1024)
	}
	if !truncated {
		t.Errorf("big: truncated=false, want true")
	}
}

// TestWorkdirScope_ReadFile_Directory covers the
// production ReadFile path's directory rejection:
// ErrNotRegularFile.
func TestWorkdirScope_ReadFile_Directory(t *testing.T) {
	dir := t.TempDir()
	s := &WorkdirScope{Workspace: dir}
	_, _, _, _, err := s.ReadFile(dir, 1024, 0)
	if !errors.Is(err, ErrNotRegularFile) {
		t.Errorf("ReadFile on dir: err = %v, want ErrNotRegularFile", err)
	}
}

// TestWorkdirScope_ReadDir covers the production
// ReadDir path against a real temporary directory.
func TestWorkdirScope_ReadDir(t *testing.T) {
	dir := t.TempDir()
	// Two files + a subdirectory
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	s := &WorkdirScope{Workspace: dir}
	entries, total, truncated, err := s.ReadDir(dir, 0)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if truncated {
		t.Errorf("truncated = true, want false")
	}
	if len(entries) != 3 {
		t.Fatalf("entries length = %d, want 3", len(entries))
	}
	// Sorted: a.txt, b.txt, sub.
	want := []string{"a.txt", "b.txt", "sub"}
	for i, w := range want {
		if entries[i].Name != w {
			t.Errorf("entries[%d].Name = %q, want %q", i, entries[i].Name, w)
		}
	}
	if entries[2].Type != "dir" {
		t.Errorf("sub.Type = %v, want dir", entries[2].Type)
	}
	if entries[0].Type != "file" {
		t.Errorf("a.txt.Type = %v, want file", entries[0].Type)
	}
	if entries[0].MTime == "" {
		t.Errorf("a.txt.MTime empty, want non-empty RFC3339")
	}
}
