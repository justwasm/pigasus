package pigrt_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/mcpext"
	"github.com/MichaelKinsy/PiG/mcp"
	"github.com/MichaelKinsy/PiG/mcp/mcptest"

	"github.com/justwasm/pigasus/internal/pigrt"
)

func waitForSessionTool(rt *pigrt.Runtime, toolName string, timeout time.Duration) piagent.AgentTool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, candidate := range rt.Session().Tools() {
			if candidate.Name() == toolName {
				return candidate
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func setupDocsMCP(t *testing.T, dir string) string {
	t.Helper()
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpDoc := `{"servers":{"docs":{"type":"stdio","command":"/bin/true","enabled":true}}}`
	if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(mcpDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	return agentDir
}

// TestRuntimeConnectsMCPFromAgyConfig is the proof that MCP comes along with the runtime.
func TestRuntimeConnectsMCPFromAgyConfig(t *testing.T) {
	dir := t.TempDir()
	agentDir := setupDocsMCP(t, dir)

	clientTransport, serverTransport := mcptest.NewInMemoryTransportPair()
	go serveDocsMCP(serverTransport)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:           dir,
		AgentDir:      agentDir,
		Model:         fauxModel(),
		NoSession:     true,
		MCPConfigPath: filepath.Join(agentDir, "mcp.json"),
		CreateTransport: func(mcpext.McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return clientTransport, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	tool := waitForSessionTool(rt, "mcp__docs__echo", 10*time.Second)
	if tool == nil {
		t.Fatalf("session tools never included %q", "mcp__docs__echo")
	}

	res, err := tool.Execute(ctx, "call-1", json.RawMessage(`{"text":"hi"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	payload, err := json.Marshal(res.Content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "echo: hi") {
		t.Fatalf("tool result %s does not contain the server's reply", payload)
	}
}

// serveDocsMCP answers the MCP handshake and exposes one `echo` tool. It is the
// server half of the in-process pair; the client half is handed to the coding
// extension as its transport.
func serveDocsMCP(transport *mcptest.InMemoryTransport) {
	transport.OnMessage(func(msg mcp.JSONRPCMessage) {
		if !msg.IsRequest() {
			// notifications/initialized and friends: nothing to answer.
			return
		}
		var result any
		switch msg.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": mcp.LatestProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "docs", "version": "1.0.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name":        "echo",
				"description": "Echo the text back.",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"text": map[string]any{"type": "string"}},
				},
			}}}
		case "resources/list":
			result = map[string]any{"resources": []any{}}
		case "tools/call":
			var params struct {
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			text, _ := params.Arguments["text"].(string)
			result = map[string]any{"content": []any{map[string]any{
				"type": "text",
				"text": "echo: " + text,
			}}}
		default:
			return
		}
		payload, _ := json.Marshal(result)
		_ = transport.Send(context.Background(), mcp.NewResult(*msg.ID, payload))
	})
	_ = transport.Start()
}

// guard against an unused import if the faux model helper moves.
var _ = strings.TrimSpace
var _ ai.Usage

// orderedStringsToMap unpacks PiG's *extension.OrderedStrings back
// into a plain map for test assertions. The helper iterates Keys()
// and reads each value through Get(); a nil pointer yields nil so
// callers can branch on absence.
func orderedStringsToMap(o *extension.OrderedStrings) map[string]string {
	if o == nil {
		return nil
	}
	out := make(map[string]string, len(o.Keys()))
	for _, k := range o.Keys() {
		if v, ok := o.Get(k); ok {
			out[k] = v
		}
	}
	return out
}

func verifyAlwaysLoadResults(t *testing.T, rt *pigrt.Runtime) {
	loaded := rt.AlwaysLoadedTools()
	want := []string{
		"mcp__browser__screenshot",
		"mcp__docs__echo",
		"mcp__docs__pinned",
		"mcp__docs__search",
	}
	if len(loaded) != len(want) {
		t.Fatalf("AlwaysLoadedTools: got %d entries, want %d (got=%v)", len(loaded), len(want), loaded)
	}
	for i, w := range want {
		if loaded[i] != w {
			t.Errorf("AlwaysLoadedTools[%d] = %q, want %q", i, loaded[i], w)
		}
	}
	byServer := rt.AlwaysLoadByServer()
	if got := byServer["docs"]; !reflect.DeepEqual(got, []string{"mcp__docs__echo", "mcp__docs__pinned", "mcp__docs__search"}) {
		t.Errorf("byServer[docs] = %v", got)
	}
	if got := byServer["browser"]; !reflect.DeepEqual(got, []string{"mcp__browser__screenshot"}) {
		t.Errorf("byServer[browser] = %v", got)
	}
	hints := rt.AlwaysLoadHints()
	wantSources := map[string]string{
		"mcp__docs__echo":          "static",
		"mcp__docs__search":        "static",
		"mcp__docs__pinned":        "dynamic",
		"mcp__browser__screenshot": "dynamic",
	}
	if len(hints) != 4 {
		t.Fatalf("AlwaysLoadHints: got %d, want 4 (%+v)", len(hints), hints)
	}
	for _, hint := range hints {
		key := pigrt.MCPName(hint.Server, hint.Tool)
		if wantSource, ok := wantSources[key]; !ok || hint.Source != wantSource {
			t.Errorf("hint %s: Source=%q, want %q", key, hint.Source, wantSources[key])
		}
	}
}

// TestRuntimeSurfacesAlwaysLoadHints exercises static and dynamic hints.
func TestRuntimeSurfacesAlwaysLoadHints(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpDoc := `{"servers":{"docs":{"type":"stdio","command":"/bin/true","enabled":true,"alwaysLoad":["echo","search"]}}}`
	if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(mcpDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	clientTransport, serverTransport := mcptest.NewInMemoryTransportPair()
	go serveDocsMCP(serverTransport)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:           dir,
		AgentDir:      agentDir,
		Model:         fauxModel(),
		NoSession:     true,
		MCPConfigPath: filepath.Join(agentDir, "mcp.json"),
		CreateTransport: func(mcpext.McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return clientTransport, nil
		},
		AlwaysLoadToolsByServer: map[string][]string{
			"docs":    {"pinned"},
			"browser": {"screenshot"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	verifyAlwaysLoadResults(t, rt)
}

// TestRuntimeNoConfigHasNoAlwaysLoad verifies the negative case: a
// runtime built without a config path AND without the dynamic
// channel has no always-load hints. AlwaysLoadedTools and the other
// accessors return nil. This is the test that fails if a future
// refactor accidentally synthesises empty hint slices and changes
// nil vs empty semantics elsewhere.
func TestRuntimeNoConfigHasNoAlwaysLoad(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:       dir,
		AgentDir:  filepath.Join(dir, "agent"),
		Model:     fauxModel(),
		NoSession: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	if got := rt.AlwaysLoadedTools(); got != nil {
		t.Errorf("AlwaysLoadedTools: got %v, want nil", got)
	}
	if got := rt.AlwaysLoadByServer(); got != nil {
		t.Errorf("AlwaysLoadByServer: got %v, want nil", got)
	}
	if got := rt.AlwaysLoadHints(); got != nil {
		t.Errorf("AlwaysLoadHints: got %v, want nil", got)
	}
}

// TestLoadAgyMCP_SubstitutesHeaderVars is the cycle 247 wire-up:
// the AGY 1.4.0 fix that substitution-flavored MCP headers
// (`"Authorization": "Bearer ${TOKEN}"`) reach PiG's McpServerConfig
// with the literal placeholder resolved. Three cases pin the
// behaviour:
//
//  1. `${VAR}` against a host-env variable is substituted.
//  2. Server-env wins over host-env (server-env is the override
//     layer for the same substitution namespace).
//  3. `${VAR:-default}` against an unset variable yields the
//     default, not the literal placeholder.
//
// The test reads a temp mcp.json through the public LoadAgyMCP
// path and asserts on PiG's McpServerConfig.Headers (an
// OrderedStrings). The OrderedStrings value is unpacked by
// iterating through PiG's accessor to keep the test robust against
// upstream reordering of internal methods.
func TestLoadAgyMCP_SubstitutesHeaderVars(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOST_TOKEN", "host-token-value")
	t.Setenv("HOST_SHARED", "from-host")
	t.Setenv("SERVER_ONLY", "from-host-server-only")
	// mcp.json ships a single http server whose headers reference
	// a mix of host-env, server-env (overriding), and an unset var
	// with a default clause.
	mcpDoc := `{
		"servers":{
			"api":{
				"type":"http",
				"url":"https://example.com/mcp",
				"enabled":true,
				"env":{
					"HOST_SHARED":"from-server-wins",
					"SERVER_ONLY":"from-server"
				},
				"headers":{
					"Authorization":"Bearer ${HOST_TOKEN}",
					"X-Tenant":"${HOST_SHARED}",
					"X-Region":"${SERVER_ONLY}",
					"X-Env":"${MISSING:-fallback}",
					"X-Static":"no-vars"
				}
			}
		}
	}`
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(mcpDoc), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded := pigrt.LoadAgyMCP(path)
	if len(loaded.Errors) != 0 {
		t.Fatalf("LoadAgyMCP errors: %v", loaded.Errors)
	}
	if len(loaded.Servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(loaded.Servers))
	}
	cfg := loaded.Servers[0].Config
	if cfg.Type != "http" {
		t.Fatalf("type = %q, want http", cfg.Type)
	}
	if cfg.URL != "https://example.com/mcp" {
		t.Fatalf("url = %q", cfg.URL)
	}
	if cfg.Headers == nil {
		t.Fatal("cfg.Headers is nil; want substituted headers")
	}
	// The OrderedStrings exposes a JSON-like map; read each value
	// back as a string and compare against the expected, fully
	// substituted values.
	want := map[string]string{
		"Authorization": "Bearer host-token-value",
		"X-Tenant":      "from-server-wins", // server-env wins over host
		"X-Region":      "from-server",      // server-only env
		"X-Env":         "fallback",         // ${MISSING:-fallback}
		"X-Static":      "no-vars",
	}
	got := orderedStringsToMap(cfg.Headers)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Headers[%q] = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("header count = %d, want %d (got=%+v)", len(got), len(want), got)
	}
	// Sanity: the literal `${…}` placeholder must not appear in any
	// emitted value — that's the AGY 1.4.0 bug we're closing.
	for k, v := range got {
		if strings.Contains(v, "${") {
			t.Errorf("header %q still carries a placeholder: %q", k, v)
		}
	}
}
