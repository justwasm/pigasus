package projects

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uploadFixture builds a MemStore with one project and an FS upload
// store rooted in a temporary directory.
func uploadFixture(t *testing.T) (Store, UploadStore, Project) {
	t.Helper()
	store := NewMemStore(nil)
	row, err := store.Create(nil, Create{Name: "demo", Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store, NewFSUploadStore(t.TempDir()), row
}

func uploadParamsJSON(t *testing.T, m map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// pdfBytes is a >8 MiB binary payload with a PDF header and NUL bytes —
// the AGY 1.5.2 case (PDFs / binaries larger than 8 MiB).
func pdfBytes(t *testing.T) []byte {
	t.Helper()
	data := make([]byte, 9<<20)
	copy(data, "%PDF-1.7\n")
	for i := 9; i < len(data); i++ {
		data[i] = byte(i * 31)
	}
	data[len(data)/2] = 0
	return data
}

// TestUploadProjectFile_BinaryOver8MiB sends a 9 MiB binary PDF in one
// request and checks the stored bytes and the resource envelope.
func TestUploadProjectFile_BinaryOver8MiB(t *testing.T) {
	store, uploads, proj := uploadFixture(t)
	content := pdfBytes(t)

	raw := uploadParamsJSON(t, map[string]any{
		"project_id": proj.ID,
		"file_name":  "report.pdf",
		"mime_type":  "application/pdf",
		"size_bytes": len(content),
		"content":    base64.StdEncoding.EncodeToString(content),
	})
	res, errBody := HandleUpload(Seams{Store: store, Uploads: uploads}, raw)
	if errBody != nil {
		t.Fatalf("HandleUpload: %+v", errBody)
	}
	resource, _ := res.(map[string]any)["resource"].(map[string]any)
	if resource["type"] != "file" || resource["display_name"] != "report.pdf" {
		t.Fatalf("resource shape: %+v", resource)
	}
	if resource["mime_type"] != "application/pdf" {
		t.Errorf("mime_type = %v", resource["mime_type"])
	}
	if got, _ := resource["size_bytes"].(int64); got != int64(len(content)) {
		t.Errorf("size_bytes = %v, want %d", resource["size_bytes"], len(content))
	}
	mountPath, _ := resource["mount_path"].(string)
	if !strings.Contains(mountPath, filepath.Join(proj.ID, "uploads")) {
		t.Errorf("mount_path %q is not under the project's uploads directory", mountPath)
	}
	stored, err := os.ReadFile(mountPath)
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("stored bytes differ (len %d, want %d)", len(stored), len(content))
	}
}

// TestUploadProjectFile_Chunked sends three chunks at their byte
// offsets and verifies the assembled file.
func TestUploadProjectFile_Chunked(t *testing.T) {
	store, uploads, proj := uploadFixture(t)
	content := pdfBytes(t)
	chunk := 4 << 20
	seams := Seams{Store: store, Uploads: uploads}

	var last map[string]any
	for offset := 0; offset < len(content); offset += chunk {
		end := offset + chunk
		if end > len(content) {
			end = len(content)
		}
		raw := uploadParamsJSON(t, map[string]any{
			"project_id": proj.ID,
			"file_name":  "big.pdf",
			"offset":     offset,
			"size_bytes": len(content),
			"content":    base64.StdEncoding.EncodeToString(content[offset:end]),
		})
		res, errBody := HandleUpload(seams, raw)
		if errBody != nil {
			t.Fatalf("chunk at %d: %+v", offset, errBody)
		}
		last = res.(map[string]any)["resource"].(map[string]any)
	}
	if got, _ := last["size_bytes"].(int64); got != int64(len(content)) {
		t.Fatalf("assembled size = %v, want %d", last["size_bytes"], len(content))
	}
	stored, err := os.ReadFile(last["mount_path"].(string))
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("assembled bytes differ (len %d, want %d)", len(stored), len(content))
	}
}

// TestUploadProjectFile_RawTextAndCamelCase accepts a raw text
// attachment using the proto-JSON camelCase spelling.
func TestUploadProjectFile_RawTextAndCamelCase(t *testing.T) {
	store, uploads, proj := uploadFixture(t)
	raw := uploadParamsJSON(t, map[string]any{
		"projectId":       proj.ID,
		"fileName":        "notes.txt",
		"mimeType":        "text/plain",
		"contentEncoding": "raw",
		"content":         "hello remote agent",
	})
	res, errBody := HandleUpload(Seams{Store: store, Uploads: uploads}, raw)
	if errBody != nil {
		t.Fatalf("HandleUpload: %+v", errBody)
	}
	mountPath := res.(map[string]any)["resource"].(map[string]any)["mount_path"].(string)
	stored, err := os.ReadFile(mountPath)
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if string(stored) != "hello remote agent" {
		t.Errorf("stored = %q", stored)
	}
}

// TestUploadProjectFile_SanitizesTraversal stores ".." components as a
// plain base name inside the uploads directory.
func TestUploadProjectFile_SanitizesTraversal(t *testing.T) {
	store, uploads, proj := uploadFixture(t)
	raw := uploadParamsJSON(t, map[string]any{
		"project_id": proj.ID,
		"file_name":  "../../escape.txt",
		"content":    base64.StdEncoding.EncodeToString([]byte("x")),
	})
	res, errBody := HandleUpload(Seams{Store: store, Uploads: uploads}, raw)
	if errBody != nil {
		t.Fatalf("HandleUpload: %+v", errBody)
	}
	mountPath := res.(map[string]any)["resource"].(map[string]any)["mount_path"].(string)
	if filepath.Base(mountPath) != "escape.txt" {
		t.Errorf("base = %q, want escape.txt", filepath.Base(mountPath))
	}
	if !strings.Contains(mountPath, filepath.Join(proj.ID, "uploads", "escape.txt")) {
		t.Errorf("mount_path %q escaped the uploads directory", mountPath)
	}
}

// TestUploadProjectFile_Validation pins the error envelopes.
func TestUploadProjectFile_Validation(t *testing.T) {
	store, uploads, proj := uploadFixture(t)

	t.Run("unknown project", func(t *testing.T) {
		raw := uploadParamsJSON(t, map[string]any{
			"project_id": "0123456789abcdef0123456789abcdef",
			"file_name":  "a.txt",
			"content":    "",
		})
		_, errBody := HandleUpload(Seams{Store: store, Uploads: uploads}, raw)
		if errBody == nil || errBody.Code != -32004 {
			t.Fatalf("error = %+v, want -32004", errBody)
		}
	})

	t.Run("missing project id", func(t *testing.T) {
		raw := uploadParamsJSON(t, map[string]any{"file_name": "a.txt"})
		_, errBody := HandleUpload(Seams{Store: store, Uploads: uploads}, raw)
		if errBody == nil || errBody.Code != -32602 {
			t.Fatalf("error = %+v, want -32602", errBody)
		}
	})

	t.Run("oversize chunk", func(t *testing.T) {
		content := make([]byte, MaxUploadChunkSize+1)
		raw := uploadParamsJSON(t, map[string]any{
			"project_id": proj.ID,
			"file_name":  "a.bin",
			"content":    base64.StdEncoding.EncodeToString(content),
		})
		_, errBody := HandleUpload(Seams{Store: store, Uploads: uploads}, raw)
		if errBody == nil || errBody.Code != -32602 {
			t.Fatalf("error = %+v, want -32602", errBody)
		}
	})

	t.Run("no upload store", func(t *testing.T) {
		raw := uploadParamsJSON(t, map[string]any{
			"project_id": proj.ID,
			"file_name":  "a.txt",
		})
		_, errBody := HandleUpload(Seams{Store: store}, raw)
		if errBody == nil || errBody.Code != -32603 {
			t.Fatalf("error = %+v, want -32603", errBody)
		}
	})
}

// TestRoute_UploadProjectFile confirms the domain's table owns both
// spellings.
func TestRoute_UploadProjectFile(t *testing.T) {
	store, uploads, proj := uploadFixture(t)
	raw := uploadParamsJSON(t, map[string]any{
		"project_id": proj.ID,
		"file_name":  "r.txt",
		"content":    base64.StdEncoding.EncodeToString([]byte("ok")),
	})
	for _, method := range []string{MethodUploadProjectFile, LegacyMethodUploadProjectFile} {
		res, errBody, handled := Route(Seams{Store: store, Uploads: uploads}, method, raw)
		if !handled || errBody != nil {
			t.Fatalf("%s: handled=%v error=%+v", method, handled, errBody)
		}
		if res == nil {
			t.Fatalf("%s: nil result", method)
		}
	}
}

// TestFSUploadStore_RejectsTraversal directly exercises the store's
// path checks.
func TestFSUploadStore_RejectsTraversal(t *testing.T) {
	root := t.TempDir()
	u := NewFSUploadStore(root)
	if _, _, err := u.WriteChunk("../../escape", "a.txt", 0, []byte("x")); !errors.Is(err, ErrInvalidUploadPath) {
		t.Errorf("bad project id error = %v", err)
	}
	if _, _, err := u.WriteChunk("0123456789abcdef0123456789abcdef", "..", 0, []byte("x")); !errors.Is(err, ErrInvalidUploadPath) {
		t.Errorf("dot-dot name error = %v", err)
	}
}

// TestDecodeBytes covers the encodings a dashboard may use.
func TestDecodeBytes(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		encoding string
		want     []byte
		wantErr  bool
	}{
		{name: "std base64", raw: `"aGVsbG8="`, want: []byte("hello")},
		{name: "raw url base64", raw: `"aGVsbG8"`, want: []byte("hello")},
		{name: "byte array", raw: `[104,105]`, want: []byte("hi")},
		{name: "raw text", raw: `"hello"`, encoding: "raw", want: []byte("hello")},
		{name: "explicit bad base64", raw: `"!!!!"`, encoding: "base64", wantErr: true},
		{name: "fallback text", raw: `"!!!!"`, want: []byte("!!!!")},
		{name: "invalid type", raw: `{}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeBytes(json.RawMessage(tc.raw), tc.encoding)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("decodeBytes(%s) = %q, want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeBytes: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("decodeBytes = %q, want %q", got, tc.want)
			}
		})
	}
}
