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
// official Go SDK (【反编译】 the 1.4.7/1.4.11 binary carries
// google3/third_party/golang/github_com/modelcontextprotocol/go_sdk/v/v0/mcp
// with `(*ClientSession).ListResources` and `(*ClientSession).ReadResource`).
// The clone connects through the official Go SDK
// (github.com/modelcontextprotocol/go-sdk/mcp) directly, matching the reference's
// internal wire and session semantics.
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
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
	// CWD is the working directory of a stdio server process. Empty
	// means the clone's own working directory.
	CWD string
	// URL and Headers configure an http server.
	URL     string
	Headers map[string]string
	// Enabled mirrors the file's per-server flag. NewPool skips a
	// disabled spec.
	Enabled bool
	// EnabledTools is the documented allowlist: when it is set, every
	// other tool of the server is hidden from the agent.
	EnabledTools []string
	// DisabledTools is the documented denylist: the named tools are
	// withheld from the agent.
	DisabledTools []string
	// TimeoutSeconds is the documented per-server timeout: zero keeps
	// the defaults (180s per tool call, 300s to connect), a positive
	// value replaces both, a negative value turns both off.
	TimeoutSeconds int
	// AuthProviderType names the built-in authentication provider the
	// documentation describes ("google_credentials" attaches Google
	// Application Default Credentials).
	//
	// The spec carries it, but the clone runs no authentication flow:
	// the SDK's transport has an `OAuthHandler` slot, and filling it
	// needs a token source that neither this value nor the OAuth member
	// describes — the documentation names no scope and no token
	// endpoint. Inventing one would be a half-implemented auth flow.
	AuthProviderType string
	// OAuth carries the documented manual OAuth 2.0 client credentials
	// (`clientId` and an optional `clientSecret`). The spec carries
	// them; see AuthProviderType for why no flow runs.
	OAuth map[string]any
}

// The documented default timeouts: five minutes for a server to connect
// and list its tools, three minutes for each tool call.
const (
	// DefaultConnectTimeout is how long the clone waits for a server to
	// connect.
	DefaultConnectTimeout = 300 * time.Second
	// DefaultCallTimeout is how long one tool call may run.
	DefaultCallTimeout = 180 * time.Second
)

// ConnectTimeout returns how long the server has to connect.
//
// A positive TimeoutSeconds replaces the default, a negative one turns
// the limit off (zero time), and zero — or an absent member — keeps the
// documented default of five minutes.
func (s ServerSpec) ConnectTimeout() time.Duration {
	switch {
	case s.TimeoutSeconds < 0:
		return 0
	case s.TimeoutSeconds > 0:
		return time.Duration(s.TimeoutSeconds) * time.Second
	default:
		return DefaultConnectTimeout
	}
}

// CallTimeout returns how long one tool call may run.
//
// A positive TimeoutSeconds replaces the default, a negative one turns
// the limit off (zero time), and zero — or an absent member — keeps the
// documented default of three minutes.
func (s ServerSpec) CallTimeout() time.Duration {
	switch {
	case s.TimeoutSeconds < 0:
		return 0
	case s.TimeoutSeconds > 0:
		return time.Duration(s.TimeoutSeconds) * time.Second
	default:
		return DefaultCallTimeout
	}
}

// ToolAllowed reports whether the agent may call the named tool on the
// server.
//
// The documented `enabledTools` list is an allowlist: when it is set,
// every other tool of the server is hidden. The documented
// `disabledTools` list is a denylist: the named tools are withheld. The
// allowlist wins when a tool is named by both.
func (s ServerSpec) ToolAllowed(tool string) bool {
	if len(s.EnabledTools) > 0 {
		for _, name := range s.EnabledTools {
			if name == tool {
				return true
			}
		}
		return false
	}
	for _, name := range s.DisabledTools {
		if name == tool {
			return false
		}
	}
	return true
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
type TransportFactory func(spec ServerSpec) (sdk.Transport, error)

// poolEntry is one server's slot in the pool. The mutex serialises the
// connect so two concurrent calls do not spawn two server processes.
type poolEntry struct {
	mu      sync.Mutex
	spec    ServerSpec
	session *sdk.ClientSession
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

// NewPoolFromFile returns a pool over the servers in one MCP
// configuration file.
//
// Both file shapes are read (see ConfigFile.AllServers), so the file may
// be the reference's own `mcp.json` or the documented `mcp_config.json`.
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
		session := entry.session
		entry.session = nil
		entry.mu.Unlock()
		if session == nil {
			continue
		}
		if err := session.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// CallTool calls a tool on a server and returns the text the model sees.
//
// args is the tool's argument object, verbatim: the server owns the
// argument names, so the clone must not rewrite them.
//
// The server's documented allow/deny tool lists are enforced here: the
// clone's own client never lists a server's tools (the three MCP tools
// call a named tool directly), so the call is the only place the tool
// name is known.
func (p *Pool) CallTool(ctx context.Context, server, tool string, args json.RawMessage) (string, error) {
	spec, ok := p.specFor(server)
	if !ok {
		return "", fmt.Errorf("MCP server %q not found", server)
	}
	if tool == "" {
		return "", errors.New("call_mcp_tool: ToolName is a required parameter. Follow the function call schema exactly.")
	}
	if !spec.ToolAllowed(tool) {
		return "", fmt.Errorf("tool %s is not enabled for server %s", tool, server)
	}
	session, err := p.client(ctx, server)
	if err != nil {
		return "", err
	}
	var payload any
	if len(args) == 0 {
		payload = map[string]any{}
	} else {
		if err := json.Unmarshal(args, &payload); err != nil {
			return "", fmt.Errorf("call_mcp_tool: Arguments is not valid JSON: %w", err)
		}
		if payload == nil {
			payload = map[string]any{}
		}
	}
	params := &sdk.CallToolParams{
		Name:      tool,
		Arguments: payload,
	}
	callCtx, cancel := withTimeout(ctx, spec.CallTimeout())
	defer cancel()
	res, err := session.CallTool(callCtx, params)
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("MCP tool call to server %q timed out after %s", server, spec.CallTimeout())
		}
		return "", fmt.Errorf("mcp server %q: tool %q: %w", server, tool, err)
	}
	return renderCallTool(server, tool, res), nil
}

// withTimeout derives a context with the given timeout. A zero timeout
// means the limit is off, and the caller's context is used as it is.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// ListResources lists a server's resources and returns the JSON the model
// sees — the MCP `resources` array in the server's own shape.
func (p *Pool) ListResources(ctx context.Context, server string) (string, error) {
	session, err := p.client(ctx, server)
	if err != nil {
		return "", err
	}
	res, err := session.ListResources(ctx, &sdk.ListResourcesParams{})
	if err != nil {
		return "", fmt.Errorf("mcp server %q: %w", server, err)
	}
	if res == nil || len(res.Resources) == 0 {
		return "[]", nil
	}
	out, err := json.Marshal(res.Resources)
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
	session, err := p.client(ctx, server)
	if err != nil {
		return "", err
	}
	if uri == "" {
		return "", errors.New("read_resource: Uri is a required parameter. Follow the function call schema exactly.")
	}
	params := &sdk.ReadResourceParams{
		URI: uri,
	}
	res, err := session.ReadResource(ctx, params)
	if err != nil {
		return "", fmt.Errorf("mcp server %q: %w", server, err)
	}
	return renderReadResource(res), nil
}

// specFor returns the pool's settings for a server, without opening a
// connection.
func (p *Pool) specFor(name string) (ServerSpec, bool) {
	if p == nil {
		return ServerSpec{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	spec, ok := p.specs[name]
	return spec, ok
}

// client returns the pool's connection to a server, opening it if needed.
func (p *Pool) client(ctx context.Context, name string) (*sdk.ClientSession, error) {
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
	if entry.session != nil {
		return entry.session, nil
	}
	// The connect limit is the documented one: the server has to
	// connect and list its tools within it.
	connectCtx, cancel := withTimeout(ctx, entry.spec.ConnectTimeout())
	defer cancel()
	session, err := p.connect(connectCtx, entry.spec)
	if err != nil {
		if errors.Is(connectCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("mcp server %q: connect timed out after %s", name, entry.spec.ConnectTimeout())
		}
		return nil, err
	}
	entry.session = session
	return session, nil
}

// connect opens one MCP connection to a server.
func (p *Pool) connect(ctx context.Context, spec ServerSpec) (*sdk.ClientSession, error) {
	p.mu.Lock()
	factory := p.factory
	p.mu.Unlock()

	var transport sdk.Transport
	var err error
	if factory != nil {
		transport, err = factory(spec)
	} else {
		transport, err = transportFor(spec)
	}
	if err != nil {
		return nil, err
	}
	client := sdk.NewClient(&sdk.Implementation{
		Name:    "pigasus",
		Version: "1.5.0",
	}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found in $PATH") {
			return nil, fmt.Errorf("%q is not installed or not on PATH, install it or set \"command\" to its full path", spec.Command)
		}
		return nil, fmt.Errorf("mcp server %q: %w", spec.Name, err)
	}
	return session, nil
}

// headerTransport adds configured HTTP headers to outbound requests.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range t.headers {
		clone.Header.Set(k, v)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

// transportFor builds the transport a server spec asks for.
func transportFor(spec ServerSpec) (sdk.Transport, error) {
	switch spec.Type {
	case "http":
		if spec.URL == "" {
			return nil, fmt.Errorf("mcp server %q: no url", spec.Name)
		}
		headers := spec.Headers
		if len(headers) > 0 {
			headers = SubstituteHeaderVars(headers, ServerFile{Env: spec.Env}.MergedEnv())
		}
		httpClient := &http.Client{
			Transport: &headerTransport{
				headers: headers,
			},
		}
		return &sdk.StreamableClientTransport{
			Endpoint:   spec.URL,
			HTTPClient: httpClient,
		}, nil
	default:
		if spec.Command == "" {
			return nil, fmt.Errorf("mcp server %q: no command", spec.Name)
		}
		if _, err := exec.LookPath(spec.Command); err != nil {
			if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found in $PATH") {
				return nil, fmt.Errorf("%q is not installed or not on PATH, install it or set \"command\" to its full path", spec.Command)
			}
		}
		cmd := exec.Command(spec.Command, spec.Args...)
		// The documented `cwd` is the working directory of the spawned
		// server process.
		cmd.Dir = spec.CWD
		if len(spec.Env) > 0 {
			merged := ServerFile{Env: spec.Env}.MergedEnv()
			envList := make([]string, 0, len(merged))
			for k, v := range merged {
				envList = append(envList, fmt.Sprintf("%s=%s", k, v))
			}
			sort.Strings(envList)
			cmd.Env = envList
		}
		return &sdk.CommandTransport{
			Command: cmd,
		}, nil
	}
}

// renderCallTool renders a tools/call result as the text the model sees.
//
// Text blocks are joined in the order the server sent them. A result with
// no text block falls back to the structured content, which is the MCP
// spec's own machine-readable answer. Binary blocks are named rather than
// inlined: the model cannot read an image out of a text result, and the
// reference records them as separate `images` / `media` step fields.
func renderCallTool(server, tool string, res *sdk.CallToolResult) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, block := range res.Content {
		switch b := block.(type) {
		case *sdk.TextContent:
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case *sdk.EmbeddedResource:
			if b.Resource != nil && b.Resource.Text != "" {
				parts = append(parts, b.Resource.Text)
			}
		case *sdk.ResourceLink:
			if b.URI != "" {
				parts = append(parts, b.URI)
			}
		case *sdk.ImageContent:
			mime := b.MIMEType
			if mime == "" {
				mime = "image"
			}
			parts = append(parts, fmt.Sprintf("[image content: %s]", mime))
		case *sdk.AudioContent:
			mime := b.MIMEType
			if mime == "" {
				mime = "audio"
			}
			parts = append(parts, fmt.Sprintf("[audio content: %s]", mime))
		}
	}
	text := strings.Join(parts, "\n")
	if text == "" && res.StructuredContent != nil {
		if raw, ok := res.StructuredContent.(json.RawMessage); ok && len(raw) > 0 {
			text = string(raw)
		} else if s, ok := res.StructuredContent.(string); ok && s != "" {
			text = s
		} else {
			if b, err := json.Marshal(res.StructuredContent); err == nil && len(b) > 0 {
				text = string(b)
			}
		}
	}
	if res.IsError {
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
func renderReadResource(res *sdk.ReadResourceResult) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, content := range res.Contents {
		if content == nil {
			continue
		}
		switch {
		case content.Text != "":
			parts = append(parts, content.Text)
		case len(content.Blob) > 0:
			if strings.HasPrefix(content.MIMEType, "image/") {
				parts = append(parts, fmt.Sprintf("[image content: %s]", content.MIMEType))
			}
			// A non-image binary content is skipped, matching the
			// reference's skipped_non_image_binary_content.
		}
	}
	return strings.Join(parts, "\n")
}
