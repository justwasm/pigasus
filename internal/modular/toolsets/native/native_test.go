//go:build !js

package native_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/modular"
	"github.com/justwasm/pigasus/internal/modular/toolsets/native"
)

func TestNativeProviderDeclarations(t *testing.T) {
	tmp := t.TempDir()
	prov := native.NewProvider(tmp)

	decls := prov.Declarations()
	if len(decls) == 0 {
		t.Fatalf("expected non-empty tool declarations")
	}

	names := make(map[string]bool)
	for _, d := range decls {
		names[d.Name] = true
	}

	required := []string{"read_file", "write_file", "edit_file", "search_web", "ask_question", "run_command"}
	for _, r := range required {
		if !names[r] {
			t.Errorf("missing expected tool declaration %q", r)
		}
	}
}

func TestNativeProviderRunCommand(t *testing.T) {
	tmp := t.TempDir()
	prov := native.NewProvider(tmp)

	res, err := prov.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: []byte(`{"CommandLine":"printf 'hello from native shell'","toolSummary":"test echo","WaitMsBeforeAsync":0}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res, "hello from native shell") {
		t.Fatalf("expected command execution output, got: %s", res)
	}
}

func TestNativeProviderFilesystemTools(t *testing.T) {
	tmp := t.TempDir()
	prov := native.NewProvider(tmp)

	testFile := filepath.Join(tmp, "native_hello.txt")

	// write_file
	_, err := prov.Run(context.Background(), backend.ToolCall{
		Name:      "write_file",
		Arguments: []byte(`{"Path":"` + testFile + `","Content":"native file content\n"}`),
	})
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}

	// read_file
	res, err := prov.Run(context.Background(), backend.ToolCall{
		Name:      "read_file",
		Arguments: []byte(`{"Path":"` + testFile + `"}`),
	})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	if !strings.Contains(res, "native file content") {
		t.Fatalf("expected file contents, got: %s", res)
	}
}

func TestNativeProviderUnknownTool(t *testing.T) {
	tmp := t.TempDir()
	prov := native.NewProvider(tmp)

	_, err := prov.Run(context.Background(), backend.ToolCall{
		Name:      "non_existent_tool",
		Arguments: []byte(`{}`),
	})
	if err == nil {
		t.Fatalf("expected error for unknown tool, got nil")
	}
}

type testCustomTool struct{}

func (c *testCustomTool) Name() string                     { return "custom_echo" }
func (c *testCustomTool) Decl() backend.ToolDecl           { return backend.ToolDecl{Name: "custom_echo"} }
func (c *testCustomTool) Run(ctx context.Context, args []byte) (string, error) {
	return "custom echo result", nil
}

func TestNativeProviderDispatchFallbackToCustomTool(t *testing.T) {
	modular.RegisterTool(&testCustomTool{})
	res, err := modular.DispatchToolCall(context.Background(), backend.ToolCall{
		Name:      "custom_echo",
		Arguments: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("expected custom tool to be dispatched successfully, got: %v", err)
	}
	if res != "custom echo result" {
		t.Fatalf("expected custom echo result, got: %s", res)
	}
}

