package tuilib

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRelPath_InsideWorkspace(t *testing.T) {
	ws := "/workspace/openagy"
	target := "/workspace/openagy/internal/tui/bridge.go"

	got := RelPath(ws, target)
	want := "internal/tui/bridge.go"
	if got != want {
		t.Errorf("RelPath(%q, %q) = %q, want %q", ws, target, got, want)
	}
}

func TestRelPath_ExactMatch(t *testing.T) {
	ws := "/workspace/openagy"
	got := RelPath(ws, ws)
	if got != "." {
		t.Errorf("RelPath(%q, %q) = %q, want '.'", ws, ws, got)
	}
}

func TestRelPath_FileURIScheme(t *testing.T) {
	ws := "file:///workspace/openagy"
	target := "file:///workspace/openagy/pkg/sub/mod.go"

	got := RelPath(ws, target)
	want := "pkg/sub/mod.go"
	if got != want {
		t.Errorf("RelPath(%q, %q) = %q, want %q", ws, target, got, want)
	}
}

func TestRelPath_OutsideWorkspaceCollapsesHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/home/testuser"
		t.Setenv("HOME", home)
	}

	ws := filepath.Join(home, "projects", "openagy")
	outside := filepath.Join(home, ".gemini", "config", "hooks.json")

	got := RelPath(ws, outside)
	want := "~/.gemini/config/hooks.json"
	if got != want {
		t.Errorf("RelPath(%q, %q) = %q, want %q", ws, outside, got, want)
	}
}

func TestRelPath_EmptyWorkspace(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/home/testuser"
		t.Setenv("HOME", home)
	}

	target := filepath.Join(home, "docs", "guide.md")
	got := RelPath("", target)
	want := "~/docs/guide.md"
	if got != want {
		t.Errorf("RelPath('', %q) = %q, want %q", target, got, want)
	}
}
