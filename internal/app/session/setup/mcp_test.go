// mcp_test.go — the runtime's MCP configuration.
//
// The runtime reads the merged set of the files the `mcp` package
// resolves: the documented global file, the reference binary's own file
// and the workspace file. The workspace's file wins for a server both
// files name.
package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/app/tools/web"
)

// writeFile writes body to path, creating the directory.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// namedServers is the optional half of the provider seam that can list
// the servers it knows.
type namedServers interface {
	Servers() []string
}

// installedServers returns the names of the servers the installed
// provider knows.
func installedServers(t *testing.T) []string {
	t.Helper()
	provider := web.DefaultMCPProvider()
	if provider == nil {
		t.Fatal("no MCP provider is installed")
	}
	named, ok := provider.(namedServers)
	if !ok {
		t.Fatalf("the installed provider %T cannot list its servers", provider)
	}
	return named.Servers()
}

// TestInstallMCPProviderReadsTheMergedSet: the global file, the
// reference's own file and the workspace file all contribute.
func TestInstallMCPProviderReadsTheMergedSet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMINI_CONFIG_HOME", "")
	agentDir := filepath.Join(home, ".gemini", "antigravity-cli")
	workspace := t.TempDir()

	writeFile(t, filepath.Join(home, ".gemini", "config", "mcp_config.json"),
		`{"mcpServers":{"global":{"command":"node"}}}`)
	writeFile(t, filepath.Join(agentDir, "mcp.json"),
		`{"servers":{"reference":{"type":"stdio","command":"node","enabled":true}}}`)
	writeFile(t, filepath.Join(workspace, ".agents", "mcp_config.json"),
		`{"mcpServers":{"workspace":{"command":"node"}}}`)

	InstallMCPProvider(agentDir, workspace)
	got := installedServers(t)
	if len(got) != 3 {
		t.Fatalf("servers = %v, want the three files' servers", got)
	}
}

// TestInstallMCPProviderWorkspaceOverridesGlobal: the workspace file wins
// for a server both files name.
func TestInstallMCPProviderWorkspaceOverridesGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMINI_CONFIG_HOME", "")
	agentDir := filepath.Join(home, ".gemini", "antigravity-cli")
	workspace := t.TempDir()

	writeFile(t, filepath.Join(home, ".gemini", "config", "mcp_config.json"),
		`{"mcpServers":{"shared":{"command":"global"}}}`)
	writeFile(t, filepath.Join(workspace, ".agents", "mcp_config.json"),
		`{"mcpServers":{"shared":{"command":"workspace"}}}`)

	InstallMCPProvider(agentDir, workspace)
	got := installedServers(t)
	if len(got) != 1 || got[0] != "shared" {
		t.Fatalf("servers = %v, want [shared]", got)
	}
}

// TestInstallMCPProviderWithoutWorkspace: a host that cannot supply a
// workspace reads the two global files only.
func TestInstallMCPProviderWithoutWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMINI_CONFIG_HOME", "")
	agentDir := filepath.Join(home, ".gemini", "antigravity-cli")

	writeFile(t, filepath.Join(home, ".gemini", "config", "mcp_config.json"),
		`{"mcpServers":{"global":{"command":"node"}}}`)
	writeFile(t, filepath.Join(agentDir, "mcp.json"),
		`{"servers":{"reference":{"type":"stdio","command":"node","enabled":true}}}`)

	InstallMCPProvider(agentDir, "")
	got := installedServers(t)
	if len(got) != 2 {
		t.Fatalf("servers = %v, want the two global files' servers", got)
	}
}
