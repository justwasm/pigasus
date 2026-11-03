package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Cycle 133: GrepSearchTool accepts capitalised Pattern/Path
// and matches against a file (the recovered wire format case).
func TestGrepSearchTool_CapitalisedArgs(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "secret.txt"), []byte("hello world\nhello again\n"), 0644); err != nil {
		t.Fatal(err)
	}
	g := &GrepSearchTool{Workspace: tmp}
	// Capitalised "Pattern" + "Path" — would have failed before cycle 133.
	raw := []byte(`{"Pattern":"hello","Path":"secret.txt"}`)
	out, err := g.Run(context.Background(), raw)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("expected string output, got %T", out)
	}
	if !strings.Contains(s, "hello") {
		t.Errorf("expected matches in output, got %q", s)
	}
}
