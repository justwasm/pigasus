package wasm_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"


	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/modular/toolsets/wasm"
	"github.com/justwasm/pigasus/internal/tools"
)

func TestWASMProviderDeclarations(t *testing.T) {
	tmp := t.TempDir()
	prov := wasm.NewProvider(tmp)

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

func TestWASMProviderRunCommandStub(t *testing.T) {
	tmp := t.TempDir()
	prov := wasm.NewProvider(tmp)

	res, err := prov.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: []byte(`{"CommandLine":"ls -la"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res, "local process execution is not supported") {
		t.Fatalf("expected sandbox warning message, got: %s", res)
	}
	if !tools.IsFailureText(res) {
		t.Fatalf("expected failure marker recognised by tools.IsFailureText, got: %s", res)
	}
}

func TestWASMProviderWireAndCatalogFilesystemTools(t *testing.T) {
	tmp := t.TempDir()
	prov := wasm.NewProvider(tmp)

	testFile := filepath.Join(tmp, "hello.txt")

	// 1. write_file (wire name)
	_, err := prov.Run(context.Background(), backend.ToolCall{
		Name:      "write_file",
		Arguments: []byte(`{"Path":"` + testFile + `","Content":"hello from wasm write_file\n"}`),
	})
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}

	// 2. read_file (wire name)
	res, err := prov.Run(context.Background(), backend.ToolCall{
		Name:      "read_file",
		Arguments: []byte(`{"Path":"` + testFile + `"}`),
	})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	if !strings.Contains(res, "hello from wasm write_file") {
		t.Fatalf("expected file contents, got: %s", res)
	}

	// 3. view_file (catalog name)
	res, err = prov.Run(context.Background(), backend.ToolCall{
		Name:      "view_file",
		Arguments: []byte(`{"AbsolutePath":"` + testFile + `"}`),
	})
	if err != nil {
		t.Fatalf("view_file failed: %v", err)
	}
	if !strings.Contains(res, "hello from wasm write_file") {
		t.Fatalf("expected file contents from view_file, got: %s", res)
	}
}

func TestWASMProviderUnknownTool(t *testing.T) {
	tmp := t.TempDir()
	prov := wasm.NewProvider(tmp)

	_, err := prov.Run(context.Background(), backend.ToolCall{
		Name:      "non_existent_tool",
		Arguments: []byte(`{}`),
	})
	if err == nil {
		t.Fatalf("expected error for unknown tool, got nil")
	}
}

