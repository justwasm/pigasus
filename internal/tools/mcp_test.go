// mcp_test.go — the three MCP tools.
//
// The declarations are the reference's own (see internal/tools/mcp.go's
// provenance note); the tests below pin them so a drift in the schemas or
// the descriptions fails here rather than on the wire.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeMCPProvider records what the tools asked for.
type fakeMCPProvider struct {
	servers []string

	calledServer string
	calledTool   string
	calledArgs   json.RawMessage

	listedServer string
	readServer   string
	readURI      string

	text string
	err  error
}

func (f *fakeMCPProvider) CallTool(_ context.Context, server, tool string, args json.RawMessage) (string, error) {
	f.calledServer, f.calledTool, f.calledArgs = server, tool, args
	return f.text, f.err
}

func (f *fakeMCPProvider) ListResources(_ context.Context, server string) (string, error) {
	f.listedServer = server
	return f.text, f.err
}

func (f *fakeMCPProvider) ReadResource(_ context.Context, server, uri string) (string, error) {
	f.readServer, f.readURI = server, uri
	return f.text, f.err
}

func (f *fakeMCPProvider) Servers() []string { return f.servers }

// TestMCPToolDescriptions pins the reference's own description text for the
// three tools (【strings】, read out of the 1.4.8 binary and committed at
// testdata/tool_descriptions_1.4.7.json).
func TestMCPToolDescriptions(t *testing.T) {
	cases := []struct {
		tool Tool
		name string
		desc string
	}{
		{&CallMcpTool{}, "call_mcp_tool", "Call a lazy-loaded MCP tool. Read the tool's schema file to understand the tool's arguments and usage."},
		{&ListResources{}, "list_resources", "Lists the available resources from an MCP server."},
		{&ReadResource{}, "read_resource", "Retrieves a specified resource's contents."},
	}
	for _, tc := range cases {
		if got := tc.tool.Name(); got != tc.name {
			t.Errorf("%s: name = %q, want %q", tc.name, got, tc.name)
		}
		if got := Describe(tc.tool); got != tc.desc {
			t.Errorf("%s: description = %q, want %q", tc.name, got, tc.desc)
		}
	}
}

// TestMCPToolSchemas pins the reference's reflected parameter schemas
// (【反编译】, the 1.4.8 binary's Go type metadata).
func TestMCPToolSchemas(t *testing.T) {
	cases := []struct {
		tool     Tool
		props    []string
		required []string
	}{
		{&CallMcpTool{}, []string{"ServerName", "ToolName", "Arguments"}, []string{"ServerName", "ToolName", "Arguments"}},
		{&ListResources{}, []string{"ServerName"}, []string{"ServerName"}},
		{&ReadResource{}, []string{"ServerName", "Uri"}, nil},
	}
	for _, tc := range cases {
		var schema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(tc.tool.Schema(), &schema); err != nil {
			t.Fatalf("%s: schema is not JSON: %v", tc.tool.Name(), err)
		}
		if schema.Type != "object" {
			t.Errorf("%s: schema type = %q, want object", tc.tool.Name(), schema.Type)
		}
		for _, name := range tc.props {
			if _, ok := schema.Properties[name]; !ok {
				t.Errorf("%s: schema is missing the %q property", tc.tool.Name(), name)
			}
		}
		if len(schema.Properties) != len(tc.props) {
			t.Errorf("%s: schema has %d properties, want %d", tc.tool.Name(), len(schema.Properties), len(tc.props))
		}
		if tc.required == nil {
			if len(schema.Required) != 0 {
				t.Errorf("%s: schema requires %v, want none", tc.tool.Name(), schema.Required)
			}
			continue
		}
		if len(schema.Required) != len(tc.required) {
			t.Errorf("%s: schema requires %v, want %v", tc.tool.Name(), schema.Required, tc.required)
			continue
		}
		for i, name := range tc.required {
			if schema.Required[i] != name {
				t.Errorf("%s: required[%d] = %q, want %q", tc.tool.Name(), i, schema.Required[i], name)
			}
		}
	}
}

// TestMCPToolSchemaDescriptions pins the reference's own parameter
// descriptions — the jsonschema_description tags of the reflected structs.
func TestMCPToolSchemaDescriptions(t *testing.T) {
	cases := []struct {
		tool Tool
		prop string
		desc string
	}{
		{&CallMcpTool{}, "ServerName", "Name of the MCP server."},
		{&CallMcpTool{}, "ToolName", "Name of the tool to call."},
		{&CallMcpTool{}, "Arguments", "Arguments to pass to the tool."},
		{&ListResources{}, "ServerName", "Name of the server to list available resources from."},
		{&ReadResource{}, "ServerName", "Name of the server to read the resource from."},
		{&ReadResource{}, "Uri", "Unique identifier for the resource."},
	}
	for _, tc := range cases {
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(tc.tool.Schema(), &schema); err != nil {
			t.Fatalf("%s: schema is not JSON: %v", tc.tool.Name(), err)
		}
		got, ok := schema.Properties[tc.prop]
		if !ok {
			t.Errorf("%s: schema is missing the %q property", tc.tool.Name(), tc.prop)
			continue
		}
		if got.Description != tc.desc {
			t.Errorf("%s.%s: description = %q, want %q", tc.tool.Name(), tc.prop, got.Description, tc.desc)
		}
	}
}

// TestCallMcpToolPassesArgumentsVerbatim is the load-bearing test of this
// file: the MCP server owns the argument names, so the clone must not
// rewrite them. The canonicaliser would turn `queryString` into
// `query_string`, and the server would reject the call.
func TestCallMcpToolPassesArgumentsVerbatim(t *testing.T) {
	provider := &fakeMCPProvider{servers: []string{"srv"}, text: "ok"}
	tool := &CallMcpTool{Provider: provider}
	args := json.RawMessage(`{"ServerName":"srv","ToolName":"search","Arguments":{"queryString":"hi","maxResults":3}}`)
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if provider.calledServer != "srv" || provider.calledTool != "search" {
		t.Fatalf("called %q/%q, want srv/search", provider.calledServer, provider.calledTool)
	}
	var got map[string]any
	if err := json.Unmarshal(provider.calledArgs, &got); err != nil {
		t.Fatalf("arguments are not JSON: %v", err)
	}
	inner, ok := got["queryString"].(string)
	if !ok {
		t.Fatalf("arguments = %s, want the queryString key verbatim", provider.calledArgs)
	}
	if inner != "hi" {
		t.Errorf("arguments.queryString = %q, want %q", inner, "hi")
	}
	if _, ok := got["query_string"]; ok {
		t.Errorf("arguments = %s, want no canonicalised key", provider.calledArgs)
	}
}

// TestCallMcpToolAcceptsReferenceNames checks the reference's PascalCase
// parameter names are the ones that reach the client.
func TestCallMcpToolAcceptsReferenceNames(t *testing.T) {
	provider := &fakeMCPProvider{servers: []string{"srv"}, text: "ok"}
	tool := &CallMcpTool{Provider: provider}
	args := json.RawMessage(`{"ServerName":"srv","ToolName":"search","Arguments":{}}`)
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if provider.calledServer != "srv" {
		t.Errorf("server = %q, want srv", provider.calledServer)
	}
	if provider.calledTool != "search" {
		t.Errorf("tool = %q, want search", provider.calledTool)
	}
}

// TestCallMcpToolMissingArguments checks the reference's own message for a
// missing required parameter.
func TestCallMcpToolMissingArguments(t *testing.T) {
	provider := &fakeMCPProvider{servers: []string{"srv"}}
	tool := &CallMcpTool{Provider: provider}
	_, err := tool.Run(context.Background(), json.RawMessage(`{"ToolName":"search"}`))
	if err == nil {
		t.Fatal("Run: want an error for the missing ServerName")
	}
	want := "ServerName is a required parameter. Follow the function call schema exactly."
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
}

// TestMCPToolsUseTheProcessWideProvider checks the default-provider seam.
func TestMCPToolsUseTheProcessWideProvider(t *testing.T) {
	provider := &fakeMCPProvider{servers: []string{"srv"}, text: "ok"}
	prev := SetMCPProvider(provider)
	defer SetMCPProvider(prev)

	if _, err := (&ListResources{}).Run(context.Background(), json.RawMessage(`{"ServerName":"srv"}`)); err != nil {
		t.Fatalf("list_resources: %v", err)
	}
	if provider.listedServer != "srv" {
		t.Errorf("listed %q, want srv", provider.listedServer)
	}
	if _, err := (&ReadResource{}).Run(context.Background(), json.RawMessage(`{"ServerName":"srv","Uri":"file:///a"}`)); err != nil {
		t.Fatalf("read_resource: %v", err)
	}
	if provider.readServer != "srv" || provider.readURI != "file:///a" {
		t.Errorf("read %q/%q, want srv/file:///a", provider.readServer, provider.readURI)
	}
}

// TestMCPToolsRejectUnknownServer checks the error the tools report when the
// model names a server that is not configured.
func TestMCPToolsRejectUnknownServer(t *testing.T) {
	provider := &fakeMCPProvider{servers: []string{"other"}}
	tool := &ListResources{Provider: provider}
	_, err := tool.Run(context.Background(), json.RawMessage(`{"ServerName":"srv"}`))
	if err == nil {
		t.Fatal("Run: want an error for the unknown server")
	}
	if !strings.Contains(err.Error(), `MCP server "srv" not found`) {
		t.Errorf("error = %q, want it to name the unknown server", err)
	}
	if !strings.Contains(err.Error(), "other") {
		t.Errorf("error = %q, want it to list the known servers", err)
	}
}

// TestMCPToolsWithoutProvider checks the tools fail loudly when no MCP
// server is configured.
func TestMCPToolsWithoutProvider(t *testing.T) {
	prev := SetMCPProvider(nil)
	defer SetMCPProvider(prev)

	_, err := (&CallMcpTool{}).Run(context.Background(), json.RawMessage(`{"ServerName":"srv","ToolName":"t","Arguments":{}}`))
	if err == nil {
		t.Fatal("Run: want an error with no provider")
	}
	if !strings.Contains(err.Error(), "no MCP servers are configured") {
		t.Errorf("error = %q, want the no-servers message", err)
	}
}

// TestMCPToolsAreNotPlaceholders checks the catalogue wires real executors:
// the reference's three MCP tools are no longer advertised-but-unimplemented.
func TestMCPToolsAreNotPlaceholders(t *testing.T) {
	reg := Catalog(t.TempDir())
	for _, name := range []string{"call_mcp_tool", "list_resources", "read_resource"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Fatalf("%s is not in the catalogue", name)
		}
		_, err := tool.Run(context.Background(), json.RawMessage(`{}`))
		if err == nil {
			t.Errorf("%s: Run returned no error", name)
			continue
		}
		if errors.Is(err, ErrNotImplemented) {
			t.Errorf("%s: still a placeholder: %v", name, err)
		}
	}
}
