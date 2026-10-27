// client.go — the clone's own MCP client, used by the three MCP tools.
//
// What this is for
// ----------------
// The reference exposes three MCP tools to the model:
//
//	call_mcp_tool   call a tool on a server the model discovered lazily
//	list_resources  list a server's resources
//	read_resource   read one resource
//
// Their declarations are recovered in internal/tools/mcp.go; this file is
// the transport underneath them: a pool that opens one MCP connection per
// configured server, on demand, and speaks `tools/call`, `resources/list`
// and `resources/read`.
//
// Provenance
// ----------
// The protocol is the MCP spec, and the reference's own client is the
// official Go SDK (【反编译】 the 1.4.7 binary carries
// google3/third_party/golang/github_com/modelcontextprotocol/go_sdk/v/v0/mcp
// with `(*ClientSession).ListResources` and `(*ClientSession).ReadResource`).
// The clone speaks the same wire protocol through PiG's `mcp` package
// (github.com/MichaelKinsy/PiG/mcp) rather than the SDK, so the clone does
// not carry a second MCP stack.
//
// The reference's error strings this file reproduces:
//
//	MCP server %q not found          (the capital-M spelling)
//	mcp server %q not found          (the lower-case spelling)
//	mcp server does not support resources
//	MCP client session is not initialized
//	tool %s is not enabled for server %s
//
// Result shape
// ------------
// The *text the model sees* is not recoverable from the binary without
// decompiling the reference's result renderer (it lives in a package the
// pclntab does not name for the MCP tools — the seeds carry
// `cortex/core/tools/tools.newListResourcesTool` and `newReadResourceTool`
// but no Execute method for them, 【反编译】). What the binary *does* pin is
// the step payload the reference records:
//
//	CortexStepListResources{server_name, cursor, resources, next_cursor}
//	CortexStepReadResource{server_name, uri, contents,
//	                       skipped_non_image_binary_content}
//
// (out/local-macos/protos/cortex_1.4.6.proto). The rendering below is
// therefore 【推测】: the resource list is the MCP `resources` array in the
// server's own JSON shape, a resource read is the contents' text, and a
// tool call is the tool's text content. The one behaviour the proto does
// pin is `skipped_non_image_binary_content`: binary content that is not an
// image is *skipped* — the model never sees it — so this file skips it
// too.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	pigmcp "github.com/MichaelKinsy/PiG/mcp"
)

// ErrPoolClosed is returned when a call arrives after the pool was closed.
var ErrPoolClosed = errors.New("mcp: the client pool is closed")

// ServerSpec is one MCP server's connection settings.
type ServerSpec struct {
	// Name is the server's name as the model sees it — the `ServerName`
	// argument of the three MCP tools.
	Name string
	// Type is "stdio" or "http" (see ServerFile.NormaliseType).
	Type string
	// Command, Args and Env configure a stdio server.
	Command string
	Args    []string
	Env     map[string]string
	// URL and Headers configure an http server.
	URL     string
	Headers map[string]string
	// Enabled mirrors the file's per-server flag. NewPool skips a
	// disabled spec.
	Enabled bool
}

// Pool holds at most one live MCP connection per server and opens them
// lazily: a server is connected on the first call that names it, and the
// connection is reused afterwards.
//
// The pool is safe for concurrent use.
type Pool struct {
	mu      sync.Mutex
	specs   map[string]ServerSpec
	entries map[string]*poolEntry
	factory TransportFactory
	closed  bool
}

// TransportFactory builds the transport a server spec asks for.
//
// It exists so a test can drive the pool over an in-process transport
// instead of spawning a child process — the same seam the PiG runtime has
// (pigrt.Options.CreateTransport). Nil means the pool builds the transport
// itself: a stdio transport for a stdio server, a streamable-HTTP one for
// an http server.
type TransportFactory func(spec ServerSpec) (pigmcp.Transport, error)

// poolEntry is one server's slot in the pool. The mutex serialises the
// connect so two concurrent calls do not spawn two server processes.
type poolEntry struct {
	mu     sync.Mutex
	spec   ServerSpec
	client *pigmcp.Client
}

// NewPool returns a pool over the given server specs.
//
// A disabled spec is skipped, so a caller can hand the pool every server in
// the configuration file without filtering first. A spec with no name is
// skipped too: the model's `ServerName` argument addresses a server by
// name, so a nameless server is unreachable.
func NewPool(specs []ServerSpec) *Pool {
	p := &Pool{
		specs:   map[string]ServerSpec{},
		entries: map[string]*poolEntry{},
	}
	for _, spec := range specs {
		if spec.Name == "" || !spec.Enabled {
			continue
		}
		if _, dup := p.specs[spec.Name]; dup {
			continue
		}
		p.specs[spec.Name] = spec
		p.entries[spec.Name] = &poolEntry{spec: spec}
	}
	return p
}

// NewPoolFromFile returns a pool over the servers in an agy `mcp.json`.
//
// A missing file yields an empty pool (the reference's "no servers
// configured" state) and a nil error. A malformed file yields an empty pool
// and the parse error: the caller decides whether to surface it, because a
// broken configuration file must not take the whole session down.
func NewPoolFromFile(path string) (*Pool, error) {
	cfg, err := LoadConfigFile(path)
	if err != nil {
		return NewPool(nil), err
	}
	return NewPool(cfg.Specs()), nil
}

// SetTransportFactory installs the factory that builds a server's
// transport. Nil restores the default (stdio / streamable-HTTP).
func (p *Pool) SetTransportFactory(f TransportFactory) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.factory = f
}

// Servers returns the names of the pool's servers, sorted.
func (p *Pool) Servers() []string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	names := make([]string, 0, len(p.specs))
	for name := range p.specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Has reports whether the pool knows a server by that name.
func (p *Pool) Has(name string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.specs[name]
	return ok
}

// Close closes every open connection. It is safe to call twice.
func (p *Pool) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	entries := make([]*poolEntry, 0, len(p.entries))
	for _, entry := range p.entries {
		entries = append(entries, entry)
	}
	p.mu.Unlock()

	var firstErr error
	for _, entry := range entries {
		entry.mu.Lock()
		client := entry.client
		entry.client = nil
		entry.mu.Unlock()
		if client == nil {
			continue
		}
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// CallTool calls a tool on a server and returns the text the model sees.
//
// args is the tool's argument object, verbatim: the server owns the
// argument names, so the clone must not rewrite them.
func (p *Pool) CallTool(ctx context.Context, server, tool string, args json.RawMessage) (string, error) {
	client, err := p.client(ctx, server)
	if err != nil {
		return "", err
	}
	if tool == "" {
		return "", errors.New("call_mcp_tool: ToolName is a required parameter. Follow the function call schema exactly.")
	}
	var payload any
	if len(args) == 0 {
		payload = map[string]any{}
	} else {
		if err := json.Unmarshal(args, &payload); err != nil {
			return "", fmt.Errorf("call_mcp_tool: Arguments is not valid JSON: %w", err)
		}
	}
	res, err := client.CallTool(ctx, tool, payload, pigmcp.RequestOptions{})
	if err != nil {
		return "", fmt.Errorf("mcp server %q: tool %q: %w", server, tool, err)
	}
	return renderCallTool(server, tool, res), nil
}

// ListResources lists a server's resources and returns the JSON the model
// sees — the MCP `resources` array in the server's own shape.
func (p *Pool) ListResources(ctx context.Context, server string) (string, error) {
	client, err := p.client(ctx, server)
	if err != nil {
		return "", err
	}
	resources, err := client.ListResources(ctx, pigmcp.RequestOptions{})
	if err != nil {
		return "", fmt.Errorf("mcp server %q: %w", server, err)
	}
	if len(resources) == 0 {
		return "[]", nil
	}
	out, err := json.Marshal(resources)
	if err != nil {
		return "", fmt.Errorf("mcp server %q: %w", server, err)
	}
	return string(out), nil
}

// ReadResource reads one resource from a server and returns the text the
// model sees.
//
// Binary content that is not an image is skipped, matching the reference's
// `skipped_non_image_binary_content` step field.
func (p *Pool) ReadResource(ctx context.Context, server, uri string) (string, error) {
	client, err := p.client(ctx, server)
	if err != nil {
		return "", err
	}
	if uri == "" {
		return "", errors.New("read_resource: Uri is a required parameter. Follow the function call schema exactly.")
	}
	res, err := client.ReadResource(ctx, uri, pigmcp.RequestOptions{})
	if err != nil {
		return "", fmt.Errorf("mcp server %q: %w", server, err)
	}
	return renderReadResource(res), nil
}

// client returns the pool's connection to a server, opening it if needed.
func (p *Pool) client(ctx context.Context, name string) (*pigmcp.Client, error) {
	if p == nil {
		return nil, errors.New("MCP server %q not found")
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrPoolClosed
	}
	entry, ok := p.entries[name]
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("MCP server %q not found", name)
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.client != nil {
		return entry.client, nil
	}
	client, err := p.connect(ctx, entry.spec)
	if err != nil {
		return nil, err
	}
	entry.client = client
	return client, nil
}

// connect opens one MCP connection to a server.
func (p *Pool) connect(ctx context.Context, spec ServerSpec) (*pigmcp.Client, error) {
	p.mu.Lock()
	factory := p.factory
	p.mu.Unlock()

	var transport pigmcp.Transport
	var err error
	if factory != nil {
		transport, err = factory(spec)
	} else {
		transport, err = transportFor(spec)
	}
	if err != nil {
		return nil, err
	}
	client := pigmcp.NewClient(pigmcp.ClientOptions{})
	if _, err := client.Connect(ctx, transport); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("mcp server %q: %w", spec.Name, err)
	}
	return client, nil
}

// transportFor builds the transport a server spec asks for.
func transportFor(spec ServerSpec) (pigmcp.Transport, error) {
	switch spec.Type {
	case "http":
		if spec.URL == "" {
			return nil, fmt.Errorf("mcp server %q: no url", spec.Name)
		}
		headers := spec.Headers
		if len(headers) > 0 {
			headers = SubstituteHeaderVars(headers, ServerFile{Env: spec.Env}.MergedEnv())
		}
		return pigmcp.NewStreamableHTTPTransport(pigmcp.StreamableHTTPTransportOptions{
			URL:     spec.URL,
			Headers: headers,
		})
	default:
		if spec.Command == "" {
			return nil, fmt.Errorf("mcp server %q: no command", spec.Name)
		}
		return pigmcp.NewStdioTransport(pigmcp.StdioTransportOptions{
			Command: spec.Command,
			Args:    spec.Args,
			Env:     spec.Env,
		}), nil
	}
}

// renderCallTool renders a tools/call result as the text the model sees.
//
// Text blocks are joined in the order the server sent them. A result with
// no text block falls back to the structured content, which is the MCP
// spec's own machine-readable answer. Binary blocks are named rather than
// inlined: the model cannot read an image out of a text result, and the
// reference records them as separate `images` / `media` step fields.
func renderCallTool(server, tool string, res *pigmcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, block := range res.Content {
		switch block.Type {
		case pigmcp.ContentText:
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case pigmcp.ContentResource:
			if block.Resource != nil && block.Resource.Text != nil && *block.Resource.Text != "" {
				parts = append(parts, *block.Resource.Text)
			}
		case pigmcp.ContentResourceLink:
			if block.URI != "" {
				parts = append(parts, block.URI)
			}
		case pigmcp.ContentImage, pigmcp.ContentAudio:
			mime := block.MimeType
			if mime == "" {
				mime = string(block.Type)
			}
			parts = append(parts, fmt.Sprintf("[%s content: %s]", block.Type, mime))
		}
	}
	text := strings.Join(parts, "\n")
	if text == "" && len(res.StructuredContent) > 0 {
		text = string(res.StructuredContent)
	}
	if res.IsError != nil && *res.IsError {
		if text == "" {
			return fmt.Sprintf("MCP tool %s/%s returned an error", server, tool)
		}
		return fmt.Sprintf("MCP tool %s/%s returned an error: %s", server, tool, text)
	}
	return text
}

// renderReadResource renders a resources/read result as the text the model
// sees.
//
// Text contents are joined in order. Binary contents that are not an image
// are skipped — the reference's step carries
// `skipped_non_image_binary_content` for exactly this case. An image is
// named rather than inlined.
func renderReadResource(res *pigmcp.ReadResourceResult) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, content := range res.Contents {
		switch {
		case content.Text != nil && *content.Text != "":
			parts = append(parts, *content.Text)
		case content.Blob != nil && *content.Blob != "":
			if strings.HasPrefix(content.MimeType, "image/") {
				parts = append(parts, fmt.Sprintf("[image content: %s]", content.MimeType))
			}
			// A non-image binary content is skipped, matching the
			// reference's skipped_non_image_binary_content.
		}
	}
	return strings.Join(parts, "\n")
}
