package remotecontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/justwasm/pigasus/internal/remotecontrol/files"
)

// This composition fixture proves the dispatcher uses the installed LS port,
// while dashboard aliases retain the separate scoped Provider contract.
type fileDomainFS struct {
	data   []byte
	denied bool
}

func (f *fileDomainFS) Stat(path string) (os.FileInfo, error) {
	return fs.Stat(fstest.MapFS{"main.go": {Data: f.data}}, "main.go")
}
func (f *fileDomainFS) Open(path string) (files.LSFile, error) {
	if f.denied {
		return nil, errors.New("installed port denied access")
	}
	info, err := f.Stat(path)
	if err != nil {
		return nil, err
	}
	return fileDomainFile{bytes.NewReader(f.data), info}, nil
}
func (*fileDomainFS) ReadDir(string) ([]os.DirEntry, error) {
	return nil, errors.New("fixture does not list directories")
}
func (*fileDomainFS) MkdirAll(string, os.FileMode) error { return nil }
func (f *fileDomainFS) WriteFile(_ string, data []byte, _ os.FileMode) error {
	f.data = append([]byte(nil), data...)
	return nil
}

type fileDomainFile struct {
	*bytes.Reader
	info os.FileInfo
}

func (f fileDomainFile) Stat() (os.FileInfo, error) { return f.info, nil }
func (fileDomainFile) Close() error                 { return nil }

func TestLSFileDomain_DispatcherComposition(t *testing.T) {
	installed := &fileDomainFS{data: []byte("LS")}
	dashboard := files.NewMemProvider(map[string][]files.MemEntry{
		"/work": {{Name: "main.go", IsDir: false, Data: []byte("dashboard")}},
	})
	d, err := NewDispatcher(Config{Session: NewSessionState("file-domain"), StatelessMode: true, FS: dashboard, LanguageServerFS: installed})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, method := range []string{"ReadFile", "readFile"} {
		answer, err := d.DispatchDirect(ctx, method, `{"uri":"file:///work/main.go"}`)
		if err != nil || answer != `{"content":"TFM=","language":"LANGUAGE_GO"}` {
			t.Fatalf("%s = %s, err %v", method, answer, err)
		}
	}
	for _, method := range []string{"ReadFile", "ReadRemoteAgentFile"} {
		answer, err := d.DispatchDirect(ctx, method, `{"path":"/work/main.go"}`)
		if err != nil || !strings.Contains(answer, `"content":"dashboard"`) || strings.Contains(answer, `"language"`) {
			t.Fatalf("dashboard %s = %s, err %v", method, answer, err)
		}
	}
	frame := d.Dispatch(ctx, []byte(`{"jsonrpc":"2.0","id":"r1","method":"readFile","params":{"uri":"file:///work/main.go"}}`))
	wire, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(wire), `{"id":"r1","jsonrpc":"2.0","result":{"content":"TFM=","language":"LANGUAGE_GO"}}`; got != want {
		t.Fatalf("wire = %s, want %s", got, want)
	}

	// A notification remains silent but performs the domain's write effect.
	if reply := d.Dispatch(ctx, []byte(`{"jsonrpc":"2.0","method":"writeFile","params":{"uri":"file:///work/main.go","content":"bmV3"}}`)); reply != nil {
		t.Fatalf("notification replied: %v", reply)
	}
	answer, err := d.DispatchDirect(ctx, "ReadFile", `{"uri":"file:///work/main.go"}`)
	if err != nil || answer != `{"content":"bmV3","language":"LANGUAGE_GO"}` {
		t.Fatalf("post-notification read = %s, err %v", answer, err)
	}

	// A port denial stays a failure; it must not retry via the dashboard or OS.
	installed.denied = true
	frame = d.Dispatch(ctx, []byte(`{"jsonrpc":"2.0","id":2,"method":"ReadFile","params":{"uri":"file:///work/main.go"}}`))
	wire, err = json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(wire), `{"error":{"code":-32004,"data":"installed port denied access","message":"Not found"},"id":2,"jsonrpc":"2.0"}`; got != want {
		t.Fatalf("denied wire = %s, want %s", got, want)
	}
	_, err = d.DispatchDirect(ctx, "UnknownFileMethod", `{}`)
	if err == nil || !strings.Contains(err.Error(), "-32601") {
		t.Fatalf("unknown error = %v", err)
	}
}
