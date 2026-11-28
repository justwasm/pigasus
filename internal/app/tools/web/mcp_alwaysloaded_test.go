// mcp_alwaysloaded_test.go — the always-loaded tool surface.
//
// The naming assertion is the one that matters most: the reference
// advertises a flagged tool as `mcp_<server>_<tool>` (single underscore on
// each side). Older notes in this tree claimed `mcp__<server>__<tool>`,
// which the 1.5.18 binary does not contain — it carries the format string
// `mcp_%s_%s` and real instances of it from its bundled chrome-devtools
// server (`mcp_chrome_devtools_list_pages`).
package web

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The provider double is mcp_test.go's fakeMCPProvider: it records the last
// call in calledServer/calledTool/calledArgs, which is exactly what these
// tests assert.

// TestAlwaysLoadedToolNameIsSingleUnderscore pins the reference's spelling.
//
// The double-underscore form is the one the older notes used; it is not
// what the binary emits, and a name the reference never sends is a name the
// model cannot call.
func TestAlwaysLoadedToolNameIsSingleUnderscore(t *testing.T) {
	got := AlwaysLoadedToolName("chrome_devtools", "list_pages")
	if want := "mcp_chrome_devtools_list_pages"; got != want {
		t.Errorf("AlwaysLoadedToolName = %q, want %q", got, want)
	}
	if strings.Contains(got, "__") {
		t.Errorf("AlwaysLoadedToolName = %q, want no double underscore", got)
	}
}

// TestAlwaysLoadedExecutorForwardsTheServersOwnSchema pins that the model is
// given the server's schema, not call_mcp_tool's: the arguments go straight
// through, so the property names must be the server's.
func TestAlwaysLoadedExecutorForwardsTheServersOwnSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"query_string":{"type":"string"}}}`)
	exec := &AlwaysLoadedToolExecutor{
		Server:     "srv",
		Tool:       "search",
		Advertised: "mcp_srv_search",
		Desc:       "search the docs",
		SchemaJSON: schema,
		Provider:   &fakeMCPProvider{},
	}
	if string(exec.Schema()) != string(schema) {
		t.Errorf("Schema = %s, want the server's own %s", exec.Schema(), schema)
	}
	if exec.Name() != "mcp_srv_search" {
		t.Errorf("Name = %q", exec.Name())
	}
	if exec.Description() != "search the docs" {
		t.Errorf("Description = %q", exec.Description())
	}
}

// TestAlwaysLoadedExecutorRunBindsServerAndTool pins that the model passes
// only the tool's arguments — the executor supplies the server and tool.
func TestAlwaysLoadedExecutorRunBindsServerAndTool(t *testing.T) {
	provider := &fakeMCPProvider{text: "ran search"}
	exec := &AlwaysLoadedToolExecutor{
		Server:     "srv",
		Tool:       "search",
		Advertised: "mcp_srv_search",
		Provider:   provider,
	}
	args := json.RawMessage(`{"query_string":"golang"}`)
	out, err := exec.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ran search" {
		t.Errorf("Run = %v, want the provider's text", out)
	}
	if provider.calledServer != "srv" || provider.calledTool != "search" {
		t.Errorf("provider called %s/%s, want srv/search", provider.calledServer, provider.calledTool)
	}
	if string(provider.calledArgs) != string(args) {
		t.Errorf("arguments = %s, want them verbatim %s", provider.calledArgs, args)
	}
}

// TestAlwaysLoadedExecutorNeedsAProvider pins the reference's error when no
// MCP server is configured.
func TestAlwaysLoadedExecutorNeedsAProvider(t *testing.T) {
	prev := SetMCPProvider(nil)
	t.Cleanup(func() { SetMCPProvider(prev) })
	exec := &AlwaysLoadedToolExecutor{Server: "srv", Tool: "search", Advertised: "mcp_srv_search"}
	_, err := exec.Run(context.Background(), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no MCP servers are configured") {
		t.Errorf("Run error = %v, want the reference's no-servers message", err)
	}
}

// TestAlwaysLoadedExecutorEmptyArgsBecomeAnObject pins that a call with no
// arguments still reaches the server with `{}`, not with nothing: a server
// expecting an object would otherwise get an empty payload.
func TestAlwaysLoadedExecutorEmptyArgsBecomeAnObject(t *testing.T) {
	provider := &fakeMCPProvider{}
	exec := &AlwaysLoadedToolExecutor{Server: "srv", Tool: "ping", Advertised: "mcp_srv_ping", Provider: provider}
	if _, err := exec.Run(context.Background(), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(provider.calledArgs) != "{}" {
		t.Errorf("arguments = %s, want {}", provider.calledArgs)
	}
}

// TestAlwaysLoadedExecutorWithoutSchemaStillDeclaresAnObject pins the
// fallback: a server that sent no schema must still produce a declaration
// the model can pass `{}` to.
func TestAlwaysLoadedExecutorWithoutSchemaStillDeclaresAnObject(t *testing.T) {
	exec := &AlwaysLoadedToolExecutor{Server: "srv", Tool: "ping", Advertised: "mcp_srv_ping"}
	var schema map[string]any
	if err := json.Unmarshal(exec.Schema(), &schema); err != nil {
		t.Fatalf("Schema is not JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("Schema type = %v, want object", schema["type"])
	}
}

// TestAlwaysLoadedExecutorsSkipNamelessEntries pins that a malformed listing
// does not produce an uncallable tool.
func TestAlwaysLoadedExecutorsSkipNamelessEntries(t *testing.T) {
	execs := AlwaysLoadedExecutors([]AlwaysLoadedTool{
		{Server: "", Name: "search"},
		{Server: "srv", Name: ""},
		{Server: "srv", Name: "search"},
	}, &fakeMCPProvider{})
	if len(execs) != 1 {
		t.Fatalf("AlwaysLoadedExecutors returned %d, want only the complete entry", len(execs))
	}
	if execs[0].Name() != "mcp_srv_search" {
		t.Errorf("Name = %q", execs[0].Name())
	}
}

// TestAlwaysLoadedDescriptionFallsBack pins that a server which sent no
// description still yields a described tool.
func TestAlwaysLoadedDescriptionFallsBack(t *testing.T) {
	execs := AlwaysLoadedExecutors([]AlwaysLoadedTool{{Server: "srv", Name: "search"}}, &fakeMCPProvider{})
	if len(execs) != 1 {
		t.Fatalf("AlwaysLoadedExecutors returned %d, want 1", len(execs))
	}
	desc, ok := execs[0].(interface{ Description() string })
	if !ok {
		t.Fatalf("%T does not describe itself", execs[0])
	}
	if desc.Description() == "" {
		t.Error("Description is empty, want a generated fallback")
	}
}

// TestAlwaysLoadedExecutorSurfacesTheProvidersError pins that a failed call
// is not swallowed.
func TestAlwaysLoadedExecutorSurfacesTheProvidersError(t *testing.T) {
	boom := errors.New("server exploded")
	exec := &AlwaysLoadedToolExecutor{
		Server: "srv", Tool: "search", Advertised: "mcp_srv_search",
		Provider: &fakeMCPProvider{err: boom},
	}
	if _, err := exec.Run(context.Background(), json.RawMessage(`{}`)); !errors.Is(err, boom) {
		t.Errorf("Run error = %v, want the provider's error", err)
	}
}
