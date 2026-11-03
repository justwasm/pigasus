package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

func TestMCPConfigPath_ProgrammaticServers(t *testing.T) {
	dir := t.TempDir()

	servers := []*pb.McpServerConfig{
		func() *pb.McpServerConfig {
			s := &pb.McpServerConfig{}
			s.SetName("my-stdio")
			stdio := &pb.McpStdioTransport{}
			stdio.SetCommand("python")
			stdio.SetArgs([]string{"server.py"})
			stdio.SetEnv(map[string]string{"DEBUG": "1"})
			s.SetStdio(stdio)
			return s
		}(),
		func() *pb.McpServerConfig {
			s := &pb.McpServerConfig{}
			s.SetName("my-http")
			http := &pb.McpHttpTransport{}
			http.SetUrl("https://mcp.example.com")
			http.SetHeaders(map[string]string{"Authorization": "Bearer test"})
			s.SetHttp(http)
			return s
		}(),
	}

	path := mcpConfigPath(dir, servers)
	if path != filepath.Join(dir, "mcp.json") {
		t.Fatalf("unexpected path: %q", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read generated mcp.json: %v", err)
	}

	var doc struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Enabled bool              `json:"enabled"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("failed to parse mcp.json: %v", err)
	}

	stdio, ok := doc.Servers["my-stdio"]
	if !ok || stdio.Type != "stdio" || stdio.Command != "python" || len(stdio.Args) != 1 || stdio.Env["DEBUG"] != "1" || !stdio.Enabled {
		t.Errorf("stdio server mismatch: %+v", stdio)
	}

	http, ok := doc.Servers["my-http"]
	if !ok || http.Type != "http" || http.URL != "https://mcp.example.com" || http.Headers["Authorization"] != "Bearer test" || !http.Enabled {
		t.Errorf("http server mismatch: %+v", http)
	}
}
