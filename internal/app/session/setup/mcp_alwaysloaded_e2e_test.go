package setup

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/app/tools/web"
	"github.com/justwasm/pigasus/mcp"
)

// TestE2EAlwaysLoadedReachesTheAdvertisedList is the end-to-end proof: a
// server flags a tool, the install path runs, and the declaration list the
// model sees contains mcp_<server>_<tool>.
func TestE2EAlwaysLoadedReachesTheAdvertisedList(t *testing.T) {
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	server := sdk.NewServer(&sdk.Implementation{Name: "fake", Version: "1"}, nil)
	server.AddTool(&sdk.Tool{
		Meta:        sdk.Meta{mcp.AlwaysLoadKey: true},
		Name:        "search_docs",
		Description: "search the docs",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "found it"}}}, nil
	})
	session, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	pool := mcp.NewPool([]mcp.ServerSpec{{Name: "docs", Type: "stdio", Enabled: true}})
	pool.SetTransportFactory(func(mcp.ServerSpec) (sdk.Transport, error) { return clientTransport, nil })
	defer pool.Close()

	prev := web.SetMCPProvider(pool)
	defer web.SetMCPProvider(prev)
	installAlwaysLoadedTools(pool)

	decls := toolrunner.AdvertisedToolDecls("", surface.Catalog(t.TempDir()))
	found := false
	for _, d := range decls {
		if d.Name == "mcp_docs_search_docs" {
			found = true
			var schema map[string]any
			_ = json.Unmarshal(d.Parameters, &schema)
			if d.Description != "search the docs" {
				t.Errorf("description = %q", d.Description)
			}
		}
	}
	if !found {
		names := make([]string, 0, len(decls))
		for _, d := range decls {
			names = append(names, d.Name)
		}
		t.Fatalf("mcp_docs_search_docs missing from the advertised list; got %d names: %v", len(decls), names)
	}
}
