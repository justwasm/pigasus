package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &ReadFile{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(`{"path":"hello.txt"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, _ := out.(string); !strings.Contains(got, "Showing lines 1-1 of 1\n1: hello world") {
		t.Errorf("got %q, want the reference's Showing/1: result", out)
	}
}

func TestCreateFile(t *testing.T) {
	dir := t.TempDir()
	c := &CreateFile{Workspace: dir}
	_, err := c.Run(context.Background(), json.RawMessage(`{"path":"sub/dir/file.txt","content":"new content"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sub/dir/file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new content" {
		t.Errorf("got %q, want %q", data, "new content")
	}
}

func TestWriteFile_Alias(t *testing.T) {
	// write_file is the 1.3.3 alias of create_file. It should still work
	// and produce identical output.
	dir := t.TempDir()
	w := &WriteFile{Workspace: dir}
	_, err := w.Run(context.Background(), json.RawMessage(`{"path":"alias.txt","content":"alias content"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "alias.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "alias content" {
		t.Errorf("got %q, want %q", data, "alias content")
	}
}

func TestEditFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	e := &EditFile{Workspace: dir}
	_, err := e.Run(context.Background(), json.RawMessage(`{"path":"f.txt","oldString":"world","newString":"earth"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello earth" {
		t.Errorf("got %q, want %q", data, "hello earth")
	}
}

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "sub/c.txt"} {
		full := filepath.Join(dir, n)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		_ = os.WriteFile(full, []byte("x"), 0644)
	}
	l := &ListDir{Workspace: dir}
	out, err := l.Run(context.Background(), json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	entries, ok := out.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", out)
	}
	if len(entries) < 2 {
		t.Errorf("got %d entries, want >= 2", len(entries))
	}
}
