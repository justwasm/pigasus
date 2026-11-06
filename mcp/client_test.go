// client_test.go — the MCP client pool.
//
// The pool is exercised end to end over an in-process transport: a fake MCP
// server answers the handshake and the three calls the three tools make.
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- the configuration file ------------------------------------------------

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	body := `{"servers":{"fs":{"type":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","/tmp"],
		"env":{"TOKEN":"x"},"enabled":true,"alwaysLoad":["read_file"]},
		"web":{"type":"http","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer $TOKEN"},"enabled":true},
		"off":{"type":"stdio","command":"x","enabled":false}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if len(cfg.Servers) != 3 {
		t.Fatalf("servers = %d, want 3", len(cfg.Servers))
	}
	fs := cfg.Servers["fs"]
	if !fs.Enabled {
		t.Error("fs is not enabled")
	}
	if len(fs.AlwaysLoad) != 1 || fs.AlwaysLoad[0] != "read_file" {
		t.Errorf("fs.alwaysLoad = %v, want [read_file]", fs.AlwaysLoad)
	}
	if got := cfg.Servers["web"].Headers["Authorization"]; got != "Bearer $TOKEN" {
		t.Errorf("web header = %q, want the raw value", got)
	}

	specs := cfg.Specs()
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2 (the disabled server is skipped)", len(specs))
	}
	if specs[0].Name != "fs" || specs[1].Name != "web" {
		t.Errorf("spec names = %q, %q, want fs, web (sorted)", specs[0].Name, specs[1].Name)
	}
	if specs[0].Type != "stdio" || specs[1].Type != "http" {
		t.Errorf("spec types = %q, %q, want stdio, http", specs[0].Type, specs[1].Type)
	}
}

func TestLoadConfigFileMissingIsEmpty(t *testing.T) {
	cfg, err := LoadConfigFile(filepath.Join(t.TempDir(), "mcp.json"))
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if len(cfg.Servers) != 0 {
		t.Errorf("servers = %d, want 0", len(cfg.Servers))
	}
}

func TestLoadConfigFileStripsBOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"servers":{}}`)...)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfigFile(path); err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
}

func TestLoadConfigFileMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfigFile(path); err == nil {
		t.Fatal("LoadConfigFile: want the parse error")
	}
}

func TestConfigFileDisableAllMCP(t *testing.T) {
	cfg := ConfigFile{
		Servers:       map[string]ServerFile{"a": {Type: "stdio", Command: "x", Enabled: true}},
		DisableAllMCP: true,
	}
	if specs := cfg.Specs(); len(specs) != 0 {
		t.Errorf("specs = %d, want 0 when MCP is disabled", len(specs))
	}
}

// ---- the pool --------------------------------------------------------------

// fakeServer is an MCP server that answers the handshake and one tool call,
// one resource list and one resource read.
type fakeServer struct {
	session *sdk.ServerSession
	mu      sync.Mutex
	calls   []string
}

func newFakeServer(t *testing.T) (*fakeServer, *sdk.InMemoryTransport) {
	t.Helper()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	server := sdk.NewServer(&sdk.Implementation{Name: "fake", Version: "1"}, nil)
	fs := &fakeServer{}

	server.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			fs.mu.Lock()
			fs.calls = append(fs.calls, method)
			fs.mu.Unlock()
			return next(ctx, method, req)
		}
	})

	server.AddTool(&sdk.Tool{
		Name:        "search",
		Description: "search tool",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{
			Content: []sdk.Content{
				&sdk.TextContent{Text: "hello from the tool"},
			},
		}, nil
	})

	server.AddResource(&sdk.Resource{
		URI:      "file:///a.txt",
		Name:     "a.txt",
		MIMEType: "text/plain",
	}, func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return &sdk.ReadResourceResult{
			Contents: []*sdk.ResourceContents{
				{
					URI:      "file:///a.txt",
					MIMEType: "text/plain",
					Text:     "contents of a.txt",
				},
			},
		}, nil
	})

	session, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the fake server: %v", err)
	}
	fs.session = session
	t.Cleanup(func() { _ = session.Close() })
	return fs, clientTransport
}

// newTestPool returns a pool whose only server is driven by the fake server.
func newTestPool(t *testing.T) (*Pool, *fakeServer) {
	t.Helper()
	fake, transport := newFakeServer(t)
	pool := NewPool([]ServerSpec{{Name: "srv", Type: "stdio", Enabled: true}})
	pool.SetTransportFactory(func(ServerSpec) (sdk.Transport, error) {
		return transport, nil
	})
	t.Cleanup(func() { _ = pool.Close() })
	return pool, fake
}

func TestPoolCallTool(t *testing.T) {
	pool, fake := newTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := pool.CallTool(ctx, "srv", "search", json.RawMessage(`{"q":"x"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out != "hello from the tool" {
		t.Errorf("CallTool = %q, want the tool's text", out)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) == 0 || fake.calls[len(fake.calls)-1] != "tools/call" {
		t.Errorf("server calls = %v, want a tools/call", fake.calls)
	}
}

func TestPoolListResources(t *testing.T) {
	pool, _ := newTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := pool.ListResources(ctx, "srv")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if !strings.Contains(out, "file:///a.txt") {
		t.Errorf("ListResources = %q, want the resource list", out)
	}
}

func TestPoolReadResource(t *testing.T) {
	pool, _ := newTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := pool.ReadResource(ctx, "srv", "file:///a.txt")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if out != "contents of a.txt" {
		t.Errorf("ReadResource = %q, want the resource's text", out)
	}
}

func TestPoolUnknownServer(t *testing.T) {
	pool, _ := newTestPool(t)
	_, err := pool.CallTool(context.Background(), "nope", "search", nil)
	if err == nil {
		t.Fatal("CallTool: want an error for the unknown server")
	}
	if !strings.Contains(err.Error(), `MCP server "nope" not found`) {
		t.Errorf("error = %q, want the reference's message", err)
	}
}

func TestPoolStdioCommandNotFound(t *testing.T) {
	pool := NewPool([]ServerSpec{
		{Name: "missing", Type: "stdio", Command: "nosuchtool12345", Enabled: true},
	})
	_, err := pool.CallTool(context.Background(), "missing", "search", nil)
	if err == nil {
		t.Fatal("expected error for missing stdio command, got nil")
	}
	want := `"nosuchtool12345" is not installed or not on PATH, install it or set "command" to its full path`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

func TestPoolServers(t *testing.T) {
	pool := NewPool([]ServerSpec{
		{Name: "b", Type: "stdio", Enabled: true},
		{Name: "a", Type: "stdio", Enabled: true},
		{Name: "off", Type: "stdio", Enabled: false},
	})
	got := pool.Servers()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Servers = %v, want [a b] (the disabled one is skipped)", got)
	}
}

// ---- the rendering ---------------------------------------------------------

func TestRenderCallTool(t *testing.T) {
	cases := []struct {
		name string
		res  *sdk.CallToolResult
		want string
	}{
		{
			name: "text blocks are joined",
			res: &sdk.CallToolResult{Content: []sdk.Content{
				&sdk.TextContent{Text: "one"},
				&sdk.TextContent{Text: "two"},
			}},
			want: "one\ntwo",
		},
		{
			name: "structured content is the fallback",
			res:  &sdk.CallToolResult{StructuredContent: json.RawMessage(`{"a":1}`)},
			want: `{"a":1}`,
		},
		{
			name: "an error is announced",
			res: &sdk.CallToolResult{
				Content: []sdk.Content{&sdk.TextContent{Text: "boom"}},
				IsError: true,
			},
			want: "MCP tool srv/tool returned an error: boom",
		},
		{
			name: "an image is named, not inlined",
			res: &sdk.CallToolResult{Content: []sdk.Content{
				&sdk.ImageContent{MIMEType: "image/png"},
			}},
			want: "[image content: image/png]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderCallTool("srv", "tool", tc.res); got != tc.want {
				t.Errorf("renderCallTool = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderReadResourceSkipsBinary(t *testing.T) {
	blob := []byte("AAA")
	res := &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{
		{URI: "file:///a", MIMEType: "text/plain", Text: "the text"},
		{URI: "file:///b", MIMEType: "application/octet-stream", Blob: blob},
		{URI: "file:///c", MIMEType: "image/png", Blob: blob},
	}}
	got := renderReadResource(res)
	if !strings.Contains(got, "the text") {
		t.Errorf("renderReadResource = %q, want the text content", got)
	}
	if strings.Contains(got, "file:///b") {
		t.Errorf("renderReadResource = %q, want the non-image binary skipped", got)
	}
	if !strings.Contains(got, "image/png") {
		t.Errorf("renderReadResource = %q, want the image named", got)
	}
}
