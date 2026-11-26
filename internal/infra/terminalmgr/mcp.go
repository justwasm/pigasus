package terminalmgr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type mcpResourceDef struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ServeMCP runs a JSON-RPC 2.0 stdio MCP server exposing terminal sessions,
// screen capture (including "self" TUI inspection), input injection, and tab control.
func ServeMCP(ctx context.Context, r io.Reader, w io.Writer, backend Backend, selfID string) error {
	if backend == nil {
		return fmt.Errorf("terminal mcp: nil backend")
	}
	if selfID == "" {
		selfID = SelfSessionIDFromEnv()
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req mcpRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(mcpResponse{
				JSONRPC: "2.0",
				Error:   &mcpRPCError{Code: -32700, Message: "parse error: " + err.Error()},
			})
			continue
		}
		// Notifications have no id and expect no response.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue
		}
		resp := handleMCPRequest(ctx, req, backend, selfID)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handleMCPRequest(ctx context.Context, req mcpRequest, backend Backend, selfID string) mcpResponse {
	resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "pigasus-terminal",
				"version": "1.0.0",
			},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": mcpToolDefs()}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &mcpRPCError{Code: -32602, Message: "invalid params: " + err.Error()}
			return resp
		}
		text, isErr := callMCPTool(ctx, backend, selfID, params.Name, params.Arguments)
		resp.Result = map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": isErr,
		}
	case "resources/list":
		resp.Result = map[string]any{"resources": listMCPResources(backend, selfID)}
	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &mcpRPCError{Code: -32602, Message: "invalid params: " + err.Error()}
			return resp
		}
		contents, err := readMCPResource(ctx, backend, selfID, params.URI)
		if err != nil {
			resp.Error = &mcpRPCError{Code: -32602, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]any{"contents": contents}
	default:
		resp.Error = &mcpRPCError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

func mcpToolDefs() []mcpToolDef {
	return []mcpToolDef{
		{
			Name:        "terminal_list",
			Description: "List all open terminal tabs/sessions in pigasus terminal (including active and self flags).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "terminal_new",
			Description: "Open a new terminal tab/session in pigasus terminal.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"cwd":{"type":"string"},"command":{"type":"string"},"focus":{"type":"boolean"},"wait_ms":{"type":"integer"}}}`),
		},
		{
			Name:        "terminal_capture",
			Description: "Capture the live screen and optional scrollback of a terminal tab. Pass id=\"self\" to read the caller's own TUI screen.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"scrollback":{"type":"integer"},"raw":{"type":"boolean"}}}`),
		},
		{
			Name:        "terminal_send",
			Description: "Send text, bracketed paste, or special keys (Enter, Esc, Ctrl+C, Up, etc.) to a terminal tab.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"text":{"type":"string"},"keys":{"type":"array","items":{"type":"string"}},"paste":{"type":"boolean"},"wait_ms":{"type":"integer"}}}`),
		},
		{
			Name:        "terminal_switch",
			Description: "Switch the active tab in pigasus terminal.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
		},
		{
			Name:        "terminal_rename",
			Description: "Rename a terminal tab in pigasus terminal.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"}},"required":["title"]}`),
		},
		{
			Name:        "terminal_close",
			Description: "Close a terminal tab/session.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
		},
	}
}

func resolveMCPTarget(id, selfID string) string {
	trimmed := strings.TrimSpace(id)
	if strings.EqualFold(trimmed, "self") && selfID != "" {
		return selfID
	}
	return trimmed
}

func callMCPTool(ctx context.Context, backend Backend, selfID, name string, rawArgs json.RawMessage) (string, bool) {
	switch name {
	case "terminal_list":
		sessions := backend.List()
		for i := range sessions {
			if selfID != "" && sessions[i].ID == selfID {
				sessions[i].Self = true
			}
		}
		b, _ := json.MarshalIndent(sessions, "", "  ")
		return string(b), false

	case "terminal_new":
		var opts NewOptions
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &opts); err != nil {
				return err.Error(), true
			}
		}
		res, err := backend.New(ctx, opts)
		if err != nil {
			return err.Error(), true
		}
		return formatMCPCapture(res), false

	case "terminal_capture":
		var opts CaptureOptions
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &opts); err != nil {
				return err.Error(), true
			}
		}
		opts.ID = resolveMCPTarget(opts.ID, selfID)
		res, err := backend.Capture(ctx, opts)
		if err != nil {
			return err.Error(), true
		}
		return formatMCPCapture(res), false

	case "terminal_send":
		var opts SendOptions
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &opts); err != nil {
				return err.Error(), true
			}
		}
		opts.ID = resolveMCPTarget(opts.ID, selfID)
		res, err := backend.Send(ctx, opts)
		if err != nil {
			return err.Error(), true
		}
		return formatMCPCapture(res), false

	case "terminal_switch":
		var args struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return err.Error(), true
		}
		info, err := backend.Switch(resolveMCPTarget(args.ID, selfID))
		if err != nil {
			return err.Error(), true
		}
		b, _ := json.MarshalIndent(info, "", "  ")
		return string(b), false

	case "terminal_rename":
		var args struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return err.Error(), true
		}
		info, err := backend.Rename(resolveMCPTarget(args.ID, selfID), args.Title)
		if err != nil {
			return err.Error(), true
		}
		b, _ := json.MarshalIndent(info, "", "  ")
		return string(b), false

	case "terminal_close":
		var args struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return err.Error(), true
		}
		info, err := backend.Kill(resolveMCPTarget(args.ID, selfID))
		if err != nil {
			return err.Error(), true
		}
		b, _ := json.MarshalIndent(info, "", "  ")
		return string(b), false

	default:
		return "unknown tool: " + name, true
	}
}

func listMCPResources(backend Backend, selfID string) []mcpResourceDef {
	out := []mcpResourceDef{{
		URI:         "terminal://sessions",
		Name:        "Terminal Sessions",
		Description: "JSON list of all open terminal tabs/sessions",
		MimeType:    "application/json",
	}}
	if selfID != "" {
		out = append(out, mcpResourceDef{
			URI:         "terminal://self/screen",
			Name:        "Self Terminal Screen",
			Description: "Live screen capture of the caller's own terminal tab",
			MimeType:    "text/plain",
		})
	}
	for _, s := range backend.List() {
		label := s.Title
		if label == "" {
			label = s.Name
		}
		if label == "" {
			label = s.ID
		}
		out = append(out, mcpResourceDef{
			URI:         "terminal://session/" + s.ID + "/screen",
			Name:        "Screen: " + label,
			Description: fmt.Sprintf("Live screen of terminal %s (%dx%d)", s.ID, s.Cols, s.Rows),
			MimeType:    "text/plain",
		})
	}
	return out
}

func readMCPResource(ctx context.Context, backend Backend, selfID, uri string) ([]map[string]any, error) {
	if uri == "terminal://sessions" {
		sessions := backend.List()
		for i := range sessions {
			if selfID != "" && sessions[i].ID == selfID {
				sessions[i].Self = true
			}
		}
		b, _ := json.MarshalIndent(sessions, "", "  ")
		return []map[string]any{{
			"uri":      uri,
			"mimeType": "application/json",
			"text":     string(b),
		}}, nil
	}
	var targetID string
	switch {
	case uri == "terminal://self/screen":
		if selfID == "" {
			return nil, fmt.Errorf("self terminal ID is not set")
		}
		targetID = selfID
	case strings.HasPrefix(uri, "terminal://session/") && strings.HasSuffix(uri, "/screen"):
		targetID = strings.TrimSuffix(strings.TrimPrefix(uri, "terminal://session/"), "/screen")
	default:
		return nil, fmt.Errorf("unknown resource URI: %s", uri)
	}
	res, err := backend.Capture(ctx, CaptureOptions{ID: targetID})
	if err != nil {
		return nil, err
	}
	return []map[string]any{{
		"uri":      uri,
		"mimeType": "text/plain",
		"text":     formatMCPCapture(res),
	}}, nil
}

func formatMCPCapture(res CaptureResult) string {
	s := res.Session
	var b strings.Builder
	fmt.Fprintf(&b, "id=%s", s.ID)
	if s.Name != "" {
		fmt.Fprintf(&b, " name=%s", s.Name)
	}
	if s.Title != "" {
		fmt.Fprintf(&b, " title=%q", s.Title)
	}
	if s.Active {
		b.WriteString(" active=true")
	}
	if s.Self {
		b.WriteString(" self=true")
	}
	fmt.Fprintf(&b, " status=%s size=%dx%d cursor=(%d,%d)", s.Status, s.Rows, s.Cols, s.CursorX, s.CursorY)
	if s.Cwd != "" {
		fmt.Fprintf(&b, " cwd=%s", s.Cwd)
	}
	b.WriteByte('\n')
	if len(res.Scrollback) > 0 {
		fmt.Fprintf(&b, "--- scrollback (%d lines) ---\n", len(res.Scrollback))
		for _, line := range res.Scrollback {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	b.WriteString("--- screen ---\n")
	for _, line := range res.Screen {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
