package cli

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginValidate_ZipFile(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "test-plugin.zip")

	// Create valid zip archive containing plugin.json
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("plugin.json")
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := w.Write([]byte(`{"name":"zip-plugin","version":"1.0.0"}`)); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	_ = zw.Close()
	_ = f.Close()

	out, stderr, code := runValidateRef(t, zipPath)
	if code != 0 {
		t.Errorf("zip validate code = %d, want 0; err=%q out=%q", code, stderr, out)
	}
	if !strings.Contains(out, "Validating zip-plugin (plugin.json in ") {
		t.Errorf("out missing zip format header:\n%s", out)
	}
}

func TestPluginValidate_ZipNestedFolder(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "nested-plugin.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("my-folder/plugin.json")
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := w.Write([]byte(`{"name":"nested-plugin","version":"2.0.0"}`)); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	_ = zw.Close()
	_ = f.Close()

	out, stderr, code := runValidateRef(t, zipPath)
	if code != 0 {
		t.Errorf("nested zip validate code = %d, want 0; err=%q out=%q", code, stderr, out)
	}
	if !strings.Contains(out, "Validating nested-plugin (plugin.json in ") {
		t.Errorf("out missing zip format header:\n%s", out)
	}
}

func TestPluginValidate_CorruptZip(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "corrupt.zip")
	if err := os.WriteFile(zipPath, []byte("not a zip file"), 0600); err != nil {
		t.Fatal(err)
	}

	out, _, code := runValidateRef(t, zipPath)
	if code != 1 {
		t.Errorf("corrupt zip code = %d, want 1", code)
	}
	if !strings.Contains(out, "failed to read archive: zip: not a valid zip file") {
		t.Errorf("corrupt zip error missing:\n%s", out)
	}
}

func TestPluginValidate_RemoteZipURL(t *testing.T) {
	// Start local test server serving a valid plugin zip
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zw := zip.NewWriter(w)
		zf, err := zw.Create("plugin.json")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = zf.Write([]byte(`{"name":"remote-plugin","version":"1.0.0"}`))
		_ = zw.Close()
	}))
	defer ts.Close()

	remoteURL := ts.URL + "/my-remote-plugin.zip"
	out, stderr, code := runValidateRef(t, remoteURL)
	if code != 0 {
		t.Errorf("remote zip validate code = %d, want 0; err=%q out=%q", code, stderr, out)
	}
	if !strings.Contains(out, "Validating remote-plugin (plugin.json in ") {
		t.Errorf("out missing remote zip header:\n%s", out)
	}
}

func TestPluginValidate_Remote404URL(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	remoteURL := ts.URL + "/missing.zip"
	out, _, code := runValidateRef(t, remoteURL)
	if code != 1 {
		t.Errorf("remote 404 code = %d, want 1", code)
	}
	if !strings.Contains(out, `returned status 404`) {
		t.Errorf("remote 404 error missing:\n%s", out)
	}
}
