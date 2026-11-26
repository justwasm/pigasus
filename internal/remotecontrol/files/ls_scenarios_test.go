package files

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// Each scenario uses explicit fixture bytes, a fixed timestamp and an in-memory
// filesystem. No Dispatcher, process filesystem, clock, transport or shell runs.
var lsScenarioTime = time.Date(2026, 10, 26, 13, 47, 0, 907136440, time.UTC)

type lsScenarioFS struct {
	files fstest.MapFS
	trace []string
	fail  string
	sizes map[string]int64
}

func newLSScenarioFS() *lsScenarioFS {
	return &lsScenarioFS{files: fstest.MapFS{
		"work":         {Mode: os.ModeDir | 0o755, ModTime: lsScenarioTime},
		"work/main.go": {Data: []byte("hello"), Mode: 0o644, ModTime: lsScenarioTime},
	}}
}

func (m *lsScenarioFS) record(op, path string) error {
	m.trace = append(m.trace, op+":"+path)
	if m.fail == op {
		return errors.New("fixture denies " + op)
	}
	return nil
}

func (m *lsScenarioFS) info(path string) (os.FileInfo, error) {
	info, err := fs.Stat(m.files, strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, err
	}
	if size, ok := m.sizes[path]; ok {
		return lsScenarioInfo{info, size}, nil
	}
	return info, nil
}

func (m *lsScenarioFS) Stat(path string) (os.FileInfo, error) {
	if err := m.record("stat", path); err != nil {
		return nil, err
	}
	return m.info(path)
}
func (m *lsScenarioFS) ReadDir(path string) ([]os.DirEntry, error) {
	if err := m.record("readdir", path); err != nil {
		return nil, err
	}
	return fs.ReadDir(m.files, strings.TrimPrefix(path, "/"))
}
func (m *lsScenarioFS) Open(path string) (LSFile, error) {
	if err := m.record("open", path); err != nil {
		return nil, err
	}
	info, err := m.info(path)
	if err != nil {
		return nil, err
	}
	data := m.files[strings.TrimPrefix(path, "/")].Data
	return &lsScenarioFile{Reader: bytes.NewReader(data), fs: m, path: path, info: info}, nil
}
func (m *lsScenarioFS) MkdirAll(path string, perm os.FileMode) error {
	if err := m.record("mkdir", path); err != nil {
		return err
	}
	m.files[strings.TrimPrefix(path, "/")] = &fstest.MapFile{Mode: os.ModeDir | perm, ModTime: lsScenarioTime}
	return nil
}
func (m *lsScenarioFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := m.record("write", path); err != nil {
		return err
	}
	m.files[strings.TrimPrefix(path, "/")] = &fstest.MapFile{Data: append([]byte(nil), data...), Mode: perm, ModTime: lsScenarioTime}
	return nil
}

type lsScenarioInfo struct {
	os.FileInfo
	size int64
}

func (i lsScenarioInfo) Size() int64 { return i.size }

type lsScenarioFile struct {
	*bytes.Reader
	fs   *lsScenarioFS
	path string
	info os.FileInfo
}

func (f *lsScenarioFile) Stat() (os.FileInfo, error) {
	if err := f.fs.record("fstat", f.path); err != nil {
		return nil, err
	}
	return f.info, nil
}
func (f *lsScenarioFile) Seek(offset int64, whence int) (int64, error) {
	if err := f.fs.record("seek", f.path); err != nil {
		return 0, err
	}
	return f.Reader.Seek(offset, whence)
}
func (f *lsScenarioFile) Read(p []byte) (int, error) {
	if f.fs.fail == "read" {
		return 0, errors.New("fixture denies read")
	}
	return f.Reader.Read(p)
}
func (f *lsScenarioFile) Close() error { return f.fs.record("close", f.path) }

// Serialize through the real RPC envelope, so assertions describe what a caller
// can observe rather than only inspecting an internal result map.
func lsScenarioAnswer(t *testing.T, seams LSSeams, method, params string) string {
	t.Helper()
	result, rpcErr, handled := LSRoute(seams, method, json.RawMessage(params))
	if !handled {
		if result != nil || rpcErr != nil {
			t.Fatal("unhandled route returned a result or error")
		}
		return "unhandled"
	}
	var answer any = rpc.Result(json.RawMessage(`17`), result)
	if rpcErr != nil {
		answer = rpc.Error(json.RawMessage(`17`), rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestLSFileScenario_CreateWriteReadStat(t *testing.T) {
	m := newLSScenarioFS()
	steps := []struct{ method, params, want string }{
		{"createDirectory", `{"uri":"file:///work/new"}`, `{"id":17,"jsonrpc":"2.0","result":{}}`},
		{"writeFile", `{"uri":"file:///work/new/hello%20world.go","content":"aGVsbG8="}`, `{"id":17,"jsonrpc":"2.0","result":{}}`},
		{"StatUri", `{"uri":"file:///work/new/hello%20world.go"}`, `{"id":17,"jsonrpc":"2.0","result":{"fileType":"FILE_TYPE_FILE","modTime":"2026-10-26T13:47:00.907136440Z","normalizedUri":"file:///work/new/hello world.go","size":"5"}}`},
		{"readFile", `{"uri":"file:///work/new/hello%20world.go","offset":"2"}`, `{"id":17,"jsonrpc":"2.0","result":{"content":"bGxv","language":"LANGUAGE_GO"}}`},
		{"ReadDir", `{"uri":"file:///work/new"}`, `{"id":17,"jsonrpc":"2.0","result":{"entries":[{"fileType":"FILE_TYPE_FILE","uri":"file:///work/new/hello world.go"}]}}`},
	}
	for _, step := range steps {
		if got := lsScenarioAnswer(t, LSSeams{FS: m}, step.method, step.params); got != step.want {
			t.Fatalf("%s answer = %s, want %s", step.method, got, step.want)
		}
	}
	wantTrace := []string{"mkdir:/work/new", "mkdir:/work/new", "write:/work/new/hello world.go", "stat:/work/new/hello world.go", "open:/work/new/hello world.go", "fstat:/work/new/hello world.go", "seek:/work/new/hello world.go", "close:/work/new/hello world.go", "readdir:/work/new"}
	if !reflect.DeepEqual(m.trace, wantTrace) {
		t.Fatalf("effects = %v, want %v", m.trace, wantTrace)
	}
	if got := m.files["work/new/hello world.go"].Mode; got != 0o644 {
		t.Fatalf("written mode = %o", got)
	}
}

func TestLSFileScenario_AliasesAndDashboardFallthrough(t *testing.T) {
	for _, tc := range []struct{ method, alias, params string }{
		{"StatUri", "statUri", `{"uri":"file:///work/main.go"}`},
		{"ReadDir", "readDir", `{"uri":"file:///work"}`},
		{"ReadFile", "readFile", `{"uri":"file:///work/main.go"}`},
		{"WriteFile", "writeFile", `{"uri":"file:///work/main.go","content":"eA=="}`},
		{"CreateDirectory", "createDirectory", `{"uri":"file:///work/empty"}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			canonical := lsScenarioAnswer(t, LSSeams{FS: newLSScenarioFS()}, tc.method, tc.params)
			if alias := lsScenarioAnswer(t, LSSeams{FS: newLSScenarioFS()}, tc.alias, tc.params); alias != canonical {
				t.Fatalf("alias = %s, canonical = %s", alias, canonical)
			}
		})
	}
	for _, method := range []string{"ReadFile", "readFile", "ReadDir", "readDir", "ReadRemoteAgentFile", "ReadRemoteAgentDir", "UnknownFileMethod"} {
		t.Run("fallthrough_"+method, func(t *testing.T) {
			m := newLSScenarioFS()
			if got := lsScenarioAnswer(t, LSSeams{FS: m}, method, `{"path":"/work/main.go"}`); got != "unhandled" {
				t.Fatalf("answer = %s", got)
			}
			if len(m.trace) != 0 {
				t.Fatalf("unhandled route accessed filesystem: %v", m.trace)
			}
		})
	}
	m := newLSScenarioFS()
	if got := lsScenarioAnswer(t, LSSeams{FS: m}, "ReadFile", `{"uri":"file:///work/main.go","offset":2}`); got != `{"id":17,"jsonrpc":"2.0","result":{"content":"bGxv","language":"LANGUAGE_GO"}}` {
		t.Fatal(got)
	}
	m.files["work/empty"] = &fstest.MapFile{Mode: os.ModeDir | 0o755}
	if got := lsScenarioAnswer(t, LSSeams{FS: m}, "ReadDir", `{"uri":"file:///work/empty"}`); got != `{"id":17,"jsonrpc":"2.0","result":{}}` {
		t.Fatal(got)
	}
}

func TestLSFileScenario_ValidationAndDeniedEffects(t *testing.T) {
	for _, tc := range []struct {
		name, method, params, fail string
		code                       int
		message, detail            string
		trace                      []string
	}{
		{"malformed", "StatUri", `{`, "", -32602, "uri is required", "", nil},
		{"missing_uri", "WriteFile", `{}`, "", -32602, "uri is required", "", nil},
		{"invalid_base64", "WriteFile", `{"uri":"file:///work/main.go","content":"raw text"}`, "", -32602, "content is not base64", "illegal base64 data at input byte 3", nil},
		{"negative_offset", "ReadFile", `{"uri":"file:///work/main.go","offset":"-1"}`, "", -32602, "offset must be >= 0", "", nil},
		{"invalid_offset", "ReadFile", `{"uri":"file:///work/main.go","offset":"nope"}`, "", -32602, "uri is required", "", nil},
		{"offset_past_end", "ReadFile", `{"uri":"file:///work/main.go","offset":6}`, "", -32602, "offset beyond file size", "", []string{"open:/work/main.go", "fstat:/work/main.go", "close:/work/main.go"}},
		{"deny_stat", "StatUri", `{"uri":"file:///work/main.go"}`, "stat", -32004, "Not found", "fixture denies stat", []string{"stat:/work/main.go"}},
		{"deny_list", "ReadDir", `{"uri":"file:///work"}`, "readdir", -32004, "Not found", "fixture denies readdir", []string{"readdir:/work"}},
		{"deny_open", "ReadFile", `{"uri":"file:///work/main.go"}`, "open", -32004, "Not found", "fixture denies open", []string{"open:/work/main.go"}},
		{"deny_opened_stat", "ReadFile", `{"uri":"file:///work/main.go"}`, "fstat", -32004, "Not found", "fixture denies fstat", []string{"open:/work/main.go", "fstat:/work/main.go", "close:/work/main.go"}},
		{"deny_seek", "ReadFile", `{"uri":"file:///work/main.go","offset":1}`, "seek", -32603, "Internal error", "fixture denies seek", []string{"open:/work/main.go", "fstat:/work/main.go", "seek:/work/main.go", "close:/work/main.go"}},
		{"deny_read", "ReadFile", `{"uri":"file:///work/main.go"}`, "read", -32603, "Internal error", "fixture denies read", []string{"open:/work/main.go", "fstat:/work/main.go", "close:/work/main.go"}},
		{"deny_parent_mkdir", "WriteFile", `{"uri":"file:///work/main.go","content":"eA=="}`, "mkdir", -32603, "MkdirAll failed", "fixture denies mkdir", []string{"mkdir:/work"}},
		{"deny_write", "WriteFile", `{"uri":"file:///work/main.go","content":"eA=="}`, "write", -32603, "WriteFile failed", "fixture denies write", []string{"mkdir:/work", "write:/work/main.go"}},
		{"deny_directory", "CreateDirectory", `{"uri":"file:///work/new"}`, "mkdir", -32603, "MkdirAll failed", "fixture denies mkdir", []string{"mkdir:/work/new"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newLSScenarioFS()
			m.fail = tc.fail
			var detail any
			if tc.detail != "" {
				detail = tc.detail
			}
			want, err := json.Marshal(rpc.Error(json.RawMessage(`17`), tc.code, tc.message, detail))
			if err != nil {
				t.Fatal(err)
			}
			if got := lsScenarioAnswer(t, LSSeams{FS: m}, tc.method, tc.params); got != string(want) {
				t.Fatalf("answer = %s, want %s", got, want)
			}
			if !reflect.DeepEqual(m.trace, tc.trace) {
				t.Fatalf("effects = %v, want %v", m.trace, tc.trace)
			}
			if got := string(m.files["work/main.go"].Data); got != "hello" {
				t.Fatalf("failed operation changed file to %q", got)
			}
		})
	}
	t.Run("nil_port_never_uses_OS", func(t *testing.T) {
		want := `{"error":{"code":-32603,"data":"no language-server FS installed","message":"Internal error"},"id":17,"jsonrpc":"2.0"}`
		if got := lsScenarioAnswer(t, LSSeams{}, "StatUri", `{"uri":"file:///work/main.go"}`); got != want {
			t.Fatalf("answer = %s", got)
		}
	})
}

func TestLSFileScenario_MediaPagingAndDisplayLimits(t *testing.T) {
	for _, tc := range []struct {
		path  string
		size  int64
		limit string
	}{
		{"/work/large.txt", (20 << 20) + 1, "20MB"},
		{"/work/large.mp4", (200 << 20) + 1, "200MB"},
	} {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			m := newLSScenarioFS()
			m.files[strings.TrimPrefix(tc.path, "/")] = &fstest.MapFile{Data: []byte("x")}
			m.sizes = map[string]int64{tc.path: tc.size}
			params, _ := json.Marshal(map[string]string{"uri": NormalizedURI(tc.path)})
			got := lsScenarioAnswer(t, LSSeams{FS: m}, "ReadFile", string(params))
			if !strings.Contains(got, `"code":-32603`) || !strings.Contains(got, tc.limit+" display limit") {
				t.Fatalf("answer = %s", got)
			}
			wantTrace := []string{"open:" + tc.path, "fstat:" + tc.path, "close:" + tc.path}
			if !reflect.DeepEqual(m.trace, wantTrace) {
				t.Fatalf("effects = %v, want %v", m.trace, wantTrace)
			}
		})
	}
	m := newLSScenarioFS()
	data := bytes.Repeat([]byte("x"), int(lsReadChunkBytes)+3)
	m.files["work/clip.MP4"] = &fstest.MapFile{Data: data}
	for _, tc := range []struct {
		offset    string
		size      int
		truncated bool
	}{
		{"0", int(lsReadChunkBytes), true},
		{"20971520", 3, false},
		{"20971523", 0, false},
	} {
		params := `{"uri":"file:///work/clip.MP4","offset":"` + tc.offset + `"}`
		answer := lsScenarioAnswer(t, LSSeams{FS: m}, "ReadFile", params)
		var wire struct {
			Result struct {
				Content   string
				Truncated bool
				Language  string
			}
		}
		if err := json.Unmarshal([]byte(answer), &wire); err != nil {
			t.Fatal(err)
		}
		decoded, err := base64.StdEncoding.DecodeString(wire.Result.Content)
		if err != nil || len(decoded) != tc.size || !bytes.Equal(decoded, data[:tc.size]) {
			t.Fatalf("offset %s: decoded size %d, err %v", tc.offset, len(decoded), err)
		}
		if wire.Result.Truncated != tc.truncated || wire.Result.Language != "" {
			t.Fatalf("offset %s: metadata = %+v", tc.offset, wire.Result)
		}
	}
}
