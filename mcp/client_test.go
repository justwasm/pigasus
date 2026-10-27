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

	pigmcp "github.com/MichaelKinsy/PiG/mcp"
	"github.com/MichaelKinsy/PiG/mcp/mcptest"
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
	transport *mcptest.InMemoryTransport
	mu        sync.Mutex
	calls     []string
}

func newFakeServer(t *testing.T) (*fakeServer, *mcptest.InMemoryTransport) {
	t.Helper()
	client, server := mcptest.NewInMemoryTransportPair()
	fs := &fakeServer{transport: server}
	// The server end must be started before the client sends: the
	// in-memory transport refuses a send to a peer that is not started.
	if err := server.Start(); err != nil {
		t.Fatalf("start the fake server: %v", err)
	}
	server.OnMessage(func(msg pigmcp.JSONRPCMessage) {
		if msg.Method == "" || msg.ID == nil {
			return // a notification
		}
		result := fs.answer(msg.Method)
		_ = server.Send(context.Background(), pigmcp.JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result:  result,
		})
	})
	return fs, client
}

func (f *fakeServer) answer(method string) json.RawMessage {
	f.mu.Lock()
	f.calls = append(f.calls, method)
	f.mu.Unlock()
	switch method {
	case "initialize":
		return json.RawMessage(`{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"fake","version":"1"}}`)
	case "tools/call":
		return json.RawMessage(`{"content":[{"type":"text","text":"hello from the tool"}]}`)
	case "resources/list":
		return json.RawMessage(`{"resources":[{"uri":"file:///a.txt","name":"a.txt","mimeType":"text/plain"}]}`)
	case "resources/read":
		return json.RawMessage(`{"contents":[{"uri":"file:///a.txt","mimeType":"text/plain","text":"contents of a.txt"}]}`)
	default:
		return json.RawMessage(`{}`)
	}
}

// newTestPool returns a pool whose only server is driven by the fake server.
func newTestPool(t *testing.T) (*Pool, *fakeServer) {
	t.Helper()
	fake, transport := newFakeServer(t)
	pool := NewPool([]ServerSpec{{Name: "srv", Type: "stdio", Enabled: true}})
	pool.SetTransportFactory(func(ServerSpec) (pigmcp.Transport, error) {
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
	isErr := true
	cases := []struct {
		name string
		res  *pigmcp.CallToolResult
		want string
	}{
		{
			name: "text blocks are joined",
			res: &pigmcp.CallToolResult{Content: []pigmcp.ContentBlock{
				{Type: pigmcp.ContentText, Text: "one"},
				{Type: pigmcp.ContentText, Text: "two"},
			}},
			want: "one\ntwo",
		},
		{
			name: "structured content is the fallback",
			res:  &pigmcp.CallToolResult{StructuredContent: json.RawMessage(`{"a":1}`)},
			want: `{"a":1}`,
		},
		{
			name: "an error is announced",
			res: &pigmcp.CallToolResult{
				Content: []pigmcp.ContentBlock{{Type: pigmcp.ContentText, Text: "boom"}},
				IsError: &isErr,
			},
			want: "MCP tool srv/tool returned an error: boom",
		},
		{
			name: "an image is named, not inlined",
			res: &pigmcp.CallToolResult{Content: []pigmcp.ContentBlock{
				{Type: pigmcp.ContentImage, MimeType: "image/png"},
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
	blob := "AAA"
	res := &pigmcp.ReadResourceResult{Contents: []pigmcp.ResourceContents{
		{URI: "file:///a", MimeType: "text/plain", Text: ptr("the text")},
		{URI: "file:///b", MimeType: "application/octet-stream", Blob: &blob},
		{URI: "file:///c", MimeType: "image/png", Blob: &blob},
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

func ptr(s string) *string { return &s }
