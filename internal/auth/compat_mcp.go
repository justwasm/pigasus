package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// MCPServer is the v1.3.5 binary 1:1 schema for one entry in
// ~/.gemini/antigravity-cli/mcp.json servers{}. The Type field
// is either "http" (url) or "stdio" (command + args + env).
type MCPServer struct {
	Type    string            `json:"type"`    // "http" or "stdio"
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
}

// MCPConfig is the v1.3.5 binary 1:1 schema for
// ~/.gemini/antigravity-cli/mcp.json.
type MCPConfig struct {
	Servers map[string]MCPServer `json:"servers"`
}

// LoadMCPConfig reads ~/.gemini/antigravity-cli/mcp.json. A missing file is not
// an error: it means no servers are configured, which is the reference's
// default (WriteCompatDefaults writes an empty servers map).
func LoadMCPConfig() (*MCPConfig, error) {
	data, err := os.ReadFile(filepath.Join(AntigravityConfigHome(), "mcp.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &MCPConfig{}, nil
		}
		return nil, err
	}
	data = stripBOM(data)
	var cfg MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// disableAllMCPField is the top-level boolean in
// ~/.gemini/antigravity-cli/mcp.json that gates every MCP server,
// independent of each server's per-row enabled flag. Same rationale as
// disableAllPluginsField.
func disableAllMCPField() bool {
	data, err := os.ReadFile(filepath.Join(AntigravityConfigHome(), "mcp.json"))
	if err != nil {
		return false
	}
	data = stripBOM(data)
	var doc struct {
		DisableAllMCP bool `json:"disableAllMCP"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	return doc.DisableAllMCP
}

// MCPsBlocked is the MCP-server equivalent of PluginsBlocked: env var
// AGY_CLI_DISABLE_MCP wins, then mcp.json's disableAllMCP boolean.
func MCPsBlocked() bool {
	if strings.TrimSpace(os.Getenv("AGY_CLI_DISABLE_MCP")) != "" {
		return true
	}
	return disableAllMCPField()
}
