package bin

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandbox removes every ambient way Path could find a real agy: the well-known
// install paths, $AGY_REAL_BIN and $PATH.
func sandbox(t *testing.T) {
	t.Helper()
	oldPaths := DefaultPaths
	DefaultPaths = nil
	t.Cleanup(func() { DefaultPaths = oldPaths })
	t.Setenv(EnvRealBin, "")
	t.Setenv("PATH", t.TempDir())
	SetDisableAutoDownload(true)
	t.Cleanup(func() { SetDisableAutoDownload(false) })
}

// fakeAgy writes an executable stub that behaves like agy --version.
func fakeAgy(t *testing.T, dir, version string) string {
	t.Helper()
	p := filepath.Join(dir, "agy")
	script := "#!/bin/sh\necho '" + version + "'\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPathRefusesSelf(t *testing.T) {
	sandbox(t)
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable: %v", err)
	}
	got, err := Path(self)
	if err == nil {
		t.Fatalf("Path(self) = %q, want an error", got)
	}
	if !strings.Contains(err.Error(), "refusing to exec self") {
		t.Fatalf("error should name the self guard, got %v", err)
	}
	// The same binary through a symlink must also be refused: the guard
	// resolves links before comparing.
	link := filepath.Join(t.TempDir(), "agy")
	if err := os.Symlink(self, link); err == nil {
		if got, err := Path(link); err == nil {
			t.Fatalf("Path(symlink to self) = %q, want an error", got)
		}
	}
}

func TestPathExplicitWins(t *testing.T) {
	sandbox(t)
	want := fakeAgy(t, t.TempDir(), "9.9.9")
	got, err := Path(want)
	if err != nil {
		t.Fatal(err)
	}
	if same(t, got, want) == false {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

// same compares two paths after symlink resolution: on macOS t.TempDir()
// hands back /var/... while EvalSymlinks answers /private/var/....
func same(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		t.Fatal(err)
	}
	return ra == rb
}

func TestPathResolvesSymlink(t *testing.T) {
	sandbox(t)
	dir := t.TempDir()
	target := fakeAgy(t, dir, "1.4.2")
	link := filepath.Join(dir, "agy-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	got, err := Path(link)
	if err != nil {
		t.Fatal(err)
	}
	// EvalSymlinks on macOS returns /private/var/... for /var/...; compare
	// resolved forms rather than raw strings.
	wantResolved, _ := filepath.EvalSymlinks(target)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Fatalf("Path = %q, want %q", gotResolved, wantResolved)
	}
}

func TestPathRejectsNonExecutableAndDirectory(t *testing.T) {
	sandbox(t)
	dir := t.TempDir()
	plain := filepath.Join(dir, "agy")
	if err := os.WriteFile(plain, []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Path(plain); err == nil {
		t.Fatalf("Path(non-executable) = %q, want an error", got)
	}
	if got, err := Path(dir); err == nil {
		t.Fatalf("Path(directory) = %q, want an error", got)
	}
}

func TestPathUsesEnvWhenNoExplicit(t *testing.T) {
	sandbox(t)
	want := fakeAgy(t, t.TempDir(), "1.4.2")
	t.Setenv(EnvRealBin, want)
	got, err := Path("")
	if err != nil {
		t.Fatal(err)
	}
	if !same(t, got, want) {
		t.Fatalf("Path = %q, want %q (from %s)", got, want, EnvRealBin)
	}
}

func TestVersion(t *testing.T) {
	bin := fakeAgy(t, t.TempDir(), "1.4.2")
	got, err := Version(bin)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.4.2" {
		t.Fatalf("Version = %q, want 1.4.2", got)
	}
	if _, err := Version(""); err == nil {
		t.Fatal("Version(\"\") should fail")
	}
}

func createTarGz(t *testing.T, filename, content string, mode int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name:     filename,
		Mode:     mode,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAutoDownload(t *testing.T) {
	script := "#!/bin/sh\necho '1.4.10'\n"
	tarBytes := createTarGz(t, "antigravity", script, 0o755)

	h := sha512.New()
	h.Write(tarBytes)
	shaHex := hex.EncodeToString(h.Sum(nil))

	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"version":"1.4.10","url":"%s/cli.tar.gz","sha512":"%s"}`, serverURL, shaHex)
		case "/cli.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(tarBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL

	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("AGY_AUTO_UPDATER_URL", srv.URL+"/manifest.json")

	downloaded, err := AutoDownload(context.Background())
	if err != nil {
		t.Fatalf("AutoDownload failed: %v", err)
	}

	info, err := os.Stat(downloaded)
	if err != nil {
		t.Fatalf("Stat downloaded failed: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("Downloaded binary is not executable: mode %v", info.Mode())
	}

	ver, err := Version(downloaded)
	if err != nil {
		t.Fatalf("Version check failed: %v", err)
	}
	if ver != "1.4.10" {
		t.Fatalf("Version = %q, want 1.4.10", ver)
	}
}

func TestAutoDownloadChecksumMismatch(t *testing.T) {
	script := "#!/bin/sh\necho '1.4.10'\n"
	tarBytes := createTarGz(t, "antigravity", script, 0o755)

	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"version":"1.4.10","url":"%s/cli.tar.gz","sha512":"badchecksum"}`, serverURL)
		case "/cli.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(tarBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL

	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("AGY_AUTO_UPDATER_URL", srv.URL+"/manifest.json")

	_, err := AutoDownload(context.Background())
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected 'checksum mismatch' error, got %v", err)
	}
}

func TestPathAutoDownloadsWhenNotFound(t *testing.T) {
	script := "#!/bin/sh\necho '1.4.10'\n"
	tarBytes := createTarGz(t, "antigravity", script, 0o755)

	h := sha512.New()
	h.Write(tarBytes)
	shaHex := hex.EncodeToString(h.Sum(nil))

	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"version":"1.4.10","url":"%s/cli.tar.gz","sha512":"%s"}`, serverURL, shaHex)
		case "/cli.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(tarBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL

	// Sandbox without disabling auto-download
	oldPaths := DefaultPaths
	DefaultPaths = nil
	t.Cleanup(func() { DefaultPaths = oldPaths })
	t.Setenv(EnvRealBin, "")
	t.Setenv("PATH", t.TempDir())
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("AGY_AUTO_UPDATER_URL", srv.URL+"/manifest.json")

	got, err := Path("")
	if err != nil {
		t.Fatalf("Path(\"\") failed: %v", err)
	}

	ver, err := Version(got)
	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}
	if ver != "1.4.10" {
		t.Fatalf("Version = %q, want 1.4.10", ver)
	}
}

func createMultiFileTarGz(t *testing.T, files []struct{ name, content string }) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, f := range files {
		mode := int64(0o644)
		if strings.Contains(f.name, "antigravity") || strings.Contains(f.name, "agy") {
			mode = 0o755
		}
		hdr := &tar.Header{
			Name:     f.name,
			Mode:     mode,
			Size:     int64(len(f.content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func createZip(t *testing.T, filename, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAutoDownload_MultiFileTar(t *testing.T) {
	script := "#!/bin/sh\necho '1.4.11'\n"
	tarBytes := createMultiFileTarGz(t, []struct{ name, content string }{
		{name: "README.md", content: "# Antigravity CLI\nDocumentation first"},
		{name: "antigravity", content: script},
		{name: "LICENSE", content: "Apache-2.0"},
	})

	h := sha512.New()
	h.Write(tarBytes)
	shaHex := hex.EncodeToString(h.Sum(nil))

	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"version":"1.4.11","url":"%s/cli_mac.tar.gz","sha512":"%s"}`, serverURL, shaHex)
		case "/cli_mac.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(tarBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL

	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("AGY_AUTO_UPDATER_URL", srv.URL+"/manifest.json")

	downloaded, err := AutoDownload(context.Background())
	if err != nil {
		t.Fatalf("AutoDownload multi-file tar failed: %v", err)
	}

	info, err := os.Stat(downloaded)
	if err != nil {
		t.Fatalf("Stat downloaded failed: %v", err)
	}
	if !isExecutable(info) {
		t.Fatalf("Downloaded binary is not executable: mode %v", info.Mode())
	}

	ver, err := Version(downloaded)
	if err != nil {
		t.Fatalf("Version check failed: %v", err)
	}
	if ver != "1.4.11" {
		t.Fatalf("Version = %q, want 1.4.11", ver)
	}
}

func TestAutoDownload_RawExecutable(t *testing.T) {
	script := "#!/bin/sh\necho '1.4.11'\n"
	rawBytes := []byte(script)

	h := sha512.New()
	h.Write(rawBytes)
	shaHex := hex.EncodeToString(h.Sum(nil))

	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"version":"1.4.11","url":"%s/cli_windows_x64.exe","sha512":"%s"}`, serverURL, shaHex)
		case "/cli_windows_x64.exe":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(rawBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL

	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("AGY_AUTO_UPDATER_URL", srv.URL+"/manifest.json")

	downloaded, err := AutoDownload(context.Background())
	if err != nil {
		t.Fatalf("AutoDownload raw executable failed: %v", err)
	}

	info, err := os.Stat(downloaded)
	if err != nil {
		t.Fatalf("Stat downloaded failed: %v", err)
	}
	if !isExecutable(info) {
		t.Fatalf("Downloaded binary is not executable: mode %v", info.Mode())
	}

	ver, err := Version(downloaded)
	if err != nil {
		t.Fatalf("Version check failed: %v", err)
	}
	if ver != "1.4.11" {
		t.Fatalf("Version = %q, want 1.4.11", ver)
	}
}

func TestAutoDownload_ZipArchive(t *testing.T) {
	script := "#!/bin/sh\necho '1.4.11'\n"
	zipBytes := createZip(t, "antigravity", script)

	h := sha512.New()
	h.Write(zipBytes)
	shaHex := hex.EncodeToString(h.Sum(nil))

	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"version":"1.4.11","url":"%s/cli.zip","sha512":"%s"}`, serverURL, shaHex)
		case "/cli.zip":
			w.Header().Set("Content-Type", "application/zip")
			w.Write(zipBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL

	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("AGY_AUTO_UPDATER_URL", srv.URL+"/manifest.json")

	downloaded, err := AutoDownload(context.Background())
	if err != nil {
		t.Fatalf("AutoDownload zip failed: %v", err)
	}

	info, err := os.Stat(downloaded)
	if err != nil {
		t.Fatalf("Stat downloaded failed: %v", err)
	}
	if !isExecutable(info) {
		t.Fatalf("Downloaded binary is not executable: mode %v", info.Mode())
	}

	ver, err := Version(downloaded)
	if err != nil {
		t.Fatalf("Version check failed: %v", err)
	}
	if ver != "1.4.11" {
		t.Fatalf("Version = %q, want 1.4.11", ver)
	}
}

func TestAutoDownload_MissingChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":"1.4.11","url":"http://example.com/cli.tar.gz"}`)
	}))
	defer srv.Close()

	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("OPENAGY_AUTO_UPDATER_URL", srv.URL+"/manifest.json")

	_, err := AutoDownload(context.Background())
	if err == nil {
		t.Fatal("expected error for missing checksum, got nil")
	}
	if !strings.Contains(err.Error(), "manifest missing checksum") {
		t.Fatalf("expected 'manifest missing checksum' error, got %v", err)
	}
}

func TestAutoDownload_OverwriteExistingTarget(t *testing.T) {
	script := "#!/bin/sh\necho '1.4.12'\n"
	tarBytes := createTarGz(t, "antigravity", script, 0o755)

	h := sha512.New()
	h.Write(tarBytes)
	shaHex := hex.EncodeToString(h.Sum(nil))

	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"version":"1.4.12","url":"%s/cli.tar.gz","sha_512":"%s"}`, serverURL, shaHex)
		case "/cli.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(tarBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL

	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("OPENAGY_AUTO_UPDATER_URL", srv.URL+"/manifest.json")

	// Pre-create existing target file to verify overwrite logic
	targetDir := filepath.Join(cacheDir, "openagy", "bin")
	_ = os.MkdirAll(targetDir, 0o755)
	existingTarget := filepath.Join(targetDir, CacheBinaryName())
	_ = os.WriteFile(existingTarget, []byte("#!/bin/sh\necho 'old'\n"), 0o755)

	downloaded, err := AutoDownload(context.Background())
	if err != nil {
		t.Fatalf("AutoDownload overwrite failed: %v", err)
	}

	ver, err := Version(downloaded)
	if err != nil {
		t.Fatalf("Version check failed: %v", err)
	}
	if ver != "1.4.12" {
		t.Fatalf("Version = %q, want 1.4.12", ver)
	}
}


