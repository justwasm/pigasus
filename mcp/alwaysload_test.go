// alwaysload_test.go — the `_meta: {"antigravity/alwaysLoad": true}` hint.
//
// The changelog pins the contract (AGY 1.3.3, line 42): a server marks a
// tool by setting that key on it in its `tools/list` response, "so the
// agent gets that tool directly instead of discovering it on demand". Only
// a JSON `true` opts in; the changelog spells out that false, the string
// "true", the number 1, null and a missing key all mean "not
// always-loaded", and these tests pin each of them.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// errAlwaysLoadUnreachable stands in for a server whose transport cannot be
// built (a missing binary, a refused connection).
var errAlwaysLoadUnreachable = errors.New("alwaysload test: the server is unreachable")

// alwaysLoadServer starts an in-memory MCP server whose `tools/list`
// carries the given tools verbatim, and returns a pool connected to it.
func alwaysLoadServer(t *testing.T, tools ...*sdk.Tool) *Pool {
	t.Helper()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	server := sdk.NewServer(&sdk.Implementation{Name: "fake", Version: "1"}, nil)
	for _, tool := range tools {
		tool := tool
		server.AddTool(tool, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{
				Content: []sdk.Content{&sdk.TextContent{Text: "ran " + tool.Name}},
			}, nil
		})
	}
	session, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the fake server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	pool := NewPool([]ServerSpec{{Name: "srv", Type: "stdio", Enabled: true}})
	pool.SetTransportFactory(func(ServerSpec) (sdk.Transport, error) {
		return clientTransport, nil
	})
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

func metaWith(value any) sdk.Meta {
	return sdk.Meta{AlwaysLoadKey: value}
}

// TestAlwaysLoadedToolsReadsTheHint is the positive case: a tool carrying a
// JSON true is reported, with its name, description and schema.
func TestAlwaysLoadedToolsReadsTheHint(t *testing.T) {
	pool := alwaysLoadServer(t, &sdk.Tool{
		Meta:        metaWith(true),
		Name:        "search_docs",
		Description: "search the docs",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"q": map[string]any{"type": "string"}},
		},
	})

	got := pool.AlwaysLoadedTools(context.Background())
	if len(got) != 1 {
		t.Fatalf("AlwaysLoadedTools returned %d tools, want 1: %+v", len(got), got)
	}
	tool := got[0]
	if tool.Server != "srv" || tool.Name != "search_docs" {
		t.Errorf("tool = %s/%s, want srv/search_docs", tool.Server, tool.Name)
	}
	if tool.Description != "search the docs" {
		t.Errorf("Description = %q, want the server's own", tool.Description)
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema is not JSON: %v (%s)", err, tool.InputSchema)
	}
	if schema["type"] != "object" {
		t.Errorf("InputSchema type = %v, want object", schema["type"])
	}
}

// TestAlwaysLoadedToolsIgnoresEveryNonTrueValue pins the changelog's
// "anything else defaults to not always-loaded" sentence, one shape at a
// time. Each case is a real JSON value a server could send.
func TestAlwaysLoadedToolsIgnoresEveryNonTrueValue(t *testing.T) {
	cases := []struct {
		name string
		meta sdk.Meta
	}{
		{"missing key", sdk.Meta{}},
		{"nil meta", nil},
		{"false", metaWith(false)},
		{"the string true", metaWith("true")},
		{"the number one", metaWith(float64(1))},
		{"null", metaWith(nil)},
		{"an object", metaWith(map[string]any{"v": true})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := alwaysLoadServer(t, &sdk.Tool{
				Meta:        tc.meta,
				Name:        "search_docs",
				Description: "search the docs",
				InputSchema: map[string]any{"type": "object"},
			})
			if got := pool.AlwaysLoadedTools(context.Background()); len(got) != 0 {
				t.Errorf("AlwaysLoadedTools = %+v, want none: only a JSON true opts in", got)
			}
		})
	}
}

// TestAlwaysLoadedToolsHonoursTheToolAllowlist pins that the documented
// allow/deny lists still gate the always-loaded set: an operator who
// withheld a tool must not get it back because the server flagged it.
func TestAlwaysLoadedToolsHonoursTheToolAllowlist(t *testing.T) {
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	server := sdk.NewServer(&sdk.Implementation{Name: "fake", Version: "1"}, nil)
	for _, name := range []string{"allowed", "withheld"} {
		name := name
		server.AddTool(&sdk.Tool{
			Meta:        metaWith(true),
			Name:        name,
			InputSchema: map[string]any{"type": "object"},
		}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{
				Content: []sdk.Content{&sdk.TextContent{Text: "ran " + name}},
			}, nil
		})
	}
	session, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the fake server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	pool := NewPool([]ServerSpec{{
		Name:         "srv",
		Type:         "stdio",
		Enabled:      true,
		EnabledTools: []string{"allowed"},
	}})
	pool.SetTransportFactory(func(ServerSpec) (sdk.Transport, error) {
		return clientTransport, nil
	})
	t.Cleanup(func() { _ = pool.Close() })

	got := pool.AlwaysLoadedTools(context.Background())
	if len(got) != 1 || got[0].Name != "allowed" {
		t.Fatalf("AlwaysLoadedTools = %+v, want only the allowlisted tool", got)
	}
}

// TestAlwaysLoadedToolsIsSorted pins the order the executors are
// registered in, so the advertised list is stable across runs.
func TestAlwaysLoadedToolsIsSorted(t *testing.T) {
	pool := alwaysLoadServer(t,
		&sdk.Tool{Meta: metaWith(true), Name: "zeta", InputSchema: map[string]any{"type": "object"}},
		&sdk.Tool{Meta: metaWith(true), Name: "alpha", InputSchema: map[string]any{"type": "object"}},
		&sdk.Tool{Meta: metaWith(false), Name: "beta", InputSchema: map[string]any{"type": "object"}},
	)
	got := pool.AlwaysLoadedTools(context.Background())
	names := make([]string, 0, len(got))
	for _, t2 := range got {
		names = append(names, t2.Name)
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("names = %v, want sorted", names)
	}
	if len(names) != 2 {
		t.Errorf("names = %v, want the two flagged tools", names)
	}
}

// TestAlwaysLoadedToolsSurvivesAnUnreachableServer pins that one broken
// server does not cost the model the tools of the healthy ones.
func TestAlwaysLoadedToolsSurvivesAnUnreachableServer(t *testing.T) {
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	server := sdk.NewServer(&sdk.Implementation{Name: "fake", Version: "1"}, nil)
	server.AddTool(&sdk.Tool{
		Meta:        metaWith(true),
		Name:        "search_docs",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "ok"}}}, nil
	})
	session, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the fake server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	pool := NewPool([]ServerSpec{
		{Name: "broken", Type: "stdio", Enabled: true},
		{Name: "healthy", Type: "stdio", Enabled: true},
	})
	pool.SetTransportFactory(func(spec ServerSpec) (sdk.Transport, error) {
		if spec.Name == "broken" {
			return nil, errAlwaysLoadUnreachable
		}
		return clientTransport, nil
	})
	t.Cleanup(func() { _ = pool.Close() })

	got := pool.AlwaysLoadedTools(context.Background())
	if len(got) != 1 || got[0].Server != "healthy" {
		t.Fatalf("AlwaysLoadedTools = %+v, want the healthy server's tool only", got)
	}
}
