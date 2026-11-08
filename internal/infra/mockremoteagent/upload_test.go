package mockremoteagent

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestUploadProjectFileWritesTheResource pins the server half: the inline
// bytes land on disk and the response describes the resource.
func TestUploadProjectFileWritesTheResource(t *testing.T) {
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"projectId": "p1",
		"fileName":  "a.txt",
		"content":   []byte("abc"),
		"mimeType":  "text/plain",
	})
	rec := httptest.NewRecorder()
	srv.service.handleUploadProjectFile(rec, body)

	var got UploadProjectFileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	r := got.Resource
	if r.ResourceID != "a.txt" || r.ProjectID != "p1" || r.MIMEType != "text/plain" || r.SizeBytes != 3 {
		t.Fatalf("resource = %+v", r)
	}
	if !strings.HasPrefix(r.URL, "file://") || r.MountPath == "" {
		t.Errorf("resource = %+v", r)
	}
	data, err := os.ReadFile(r.MountPath)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(data) != "abc" {
		t.Errorf("content = %q", data)
	}
	_ = os.Remove(r.MountPath)
}

func TestUploadProjectFileRejectsTraversalNames(t *testing.T) {
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	body, _ := json.Marshal(map[string]any{"fileName": "../../escape.txt", "content": []byte("x")})
	rec := httptest.NewRecorder()
	srv.service.handleUploadProjectFile(rec, body)
	var got UploadProjectFileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if strings.Contains(got.Resource.MountPath, "..") {
		t.Errorf("mount path escaped: %q", got.Resource.MountPath)
	}
	_ = os.Remove(got.Resource.MountPath)
}
