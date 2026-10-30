package agyupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/endpoints"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	t.Setenv(endpoints.ConfigPathEnvVar, path)
	return path
}

func TestFormatGlogError(t *testing.T) {
	now := time.Date(2026, time.October, 30, 11, 1, 31, 461180000, time.UTC)
	got := FormatGlogError(now, "auto_updater.go:491", "Failed to check for updates: test message")
	want := "ERROR: logging before google.Init: E1030 11:01:31.461180       1 auto_updater.go:491] Failed to check for updates: test message\n"
	if got != want {
		t.Errorf("FormatGlogError:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		remote  string
		current string
		want    bool
	}{
		{"1.4.11", "1.4.10", true},
		{"v1.4.11", "1.4.10", true},
		{"1.4.10", "1.4.10", false},
		{"1.4.9", "1.4.10", false},
		{"2.0.0", "1.4.10", true},
		{"1.5.0", "1.4.10", true},
	}
	for _, tc := range tests {
		if got := IsNewerVersion(tc.remote, tc.current); got != tc.want {
			t.Errorf("IsNewerVersion(%q, %q) = %v, want %v", tc.remote, tc.current, got, tc.want)
		}
	}
}

func TestRun_NetworkError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    "http://127.0.0.1:0/invalid-does-not-exist",
		CurrentVersion: "1.4.10",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	errStr := stderr.String()
	if !strings.Contains(errStr, "ERROR: logging before google.Init:") {
		t.Errorf("stderr missing glog prefix: %q", errStr)
	}
	if !strings.Contains(errStr, "auto_updater.go:491] Failed to check for updates: failed to fetch manifest:") {
		t.Errorf("stderr missing auto_updater log location: %q", errStr)
	}
	if !strings.Contains(errStr, "Error: failed to check for updates: failed to fetch manifest:") {
		t.Errorf("stderr missing CLI error summary: %q", errStr)
	}
}

func TestRun_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	errStr := stderr.String()
	if !strings.Contains(errStr, "status 404") {
		t.Errorf("stderr missing status 404: %q", errStr)
	}
}

func TestRun_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`))
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	errStr := stderr.String()
	if !strings.Contains(errStr, "failed to decode manifest:") {
		t.Errorf("stderr missing decode error: %q", errStr)
	}
}

func TestRun_AlreadyLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"version": "1.4.10"}`))
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, "✓ You are already on the latest version.") {
		t.Errorf("stdout missing up-to-date line: %q", outStr)
	}
	if stderr.Len() > 0 {
		t.Errorf("expected empty stderr, got: %q", stderr.String())
	}
}

func TestRun_NewVersionFound(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho updated-agy\n")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{
		Name: "agy",
		Mode: 0o755,
		Size: int64(len(binaryContent)),
	})
	_, _ = tw.Write(binaryContent)
	_ = tw.Close()
	_ = gw.Close()

	archiveBytes := buf.Bytes()
	h := sha256.Sum256(archiveBytes)
	sha256Hex := hex.EncodeToString(h[:])

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Manifest{
				Version: "1.4.11",
				URL:     ts.URL + "/archive.tar.gz",
				Sha256:  sha256Hex,
			})
		case "/archive.tar.gz":
			w.WriteHeader(http.StatusOK)
			w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, "✓ Found new version 1.4.11.") {
		t.Errorf("stdout missing new version notification: %q", outStr)
	}
	if !strings.Contains(outStr, "⟳ Downloading update...") {
		t.Errorf("stdout missing downloading line: %q", outStr)
	}
	if !strings.Contains(outStr, "✓ Update successful! Please restart agy.") {
		t.Errorf("stdout missing update successful line: %q", outStr)
	}
}

func TestRun_NewVersionMissingDownloadURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Manifest{
			Version: "1.4.11",
		})
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing download URL, got %d", code)
	}

	errStr := stderr.String()
	if !strings.Contains(errStr, "no download URL in manifest") {
		t.Errorf("stderr missing expected error: %q", errStr)
	}
}

func TestRun_NewVersionDownloadHTTPError(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Manifest{
				Version: "1.4.11",
				URL:     ts.URL + "/missing.tar.gz",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 1 {
		t.Fatalf("expected exit code 1 for 404 download, got %d", code)
	}

	errStr := stderr.String()
	if !strings.Contains(errStr, "status 404") {
		t.Errorf("stderr missing 404 status: %q", errStr)
	}
}

func TestRun_NewVersionChecksumMismatch(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Manifest{
				Version: "1.4.11",
				URL:     ts.URL + "/archive.tar.gz",
				Sha256:  "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			})
		case "/archive.tar.gz":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("not matching hash"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 1 {
		t.Fatalf("expected exit code 1 on checksum mismatch, got %d", code)
	}

	errStr := stderr.String()
	if !strings.Contains(errStr, "checksum mismatch") {
		t.Errorf("stderr missing checksum mismatch error: %q", errStr)
	}
}

func TestRun_ZipExtraction(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho updated-from-zip\n")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("agy.exe")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := f.Write(binaryContent); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	archiveBytes := buf.Bytes()
	h := sha256.Sum256(archiveBytes)
	sha256Hex := hex.EncodeToString(h[:])

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Manifest{
				Version: "1.4.11",
				URL:     ts.URL + "/archive.zip",
				Sha256:  sha256Hex,
			})
		case "/archive.zip":
			w.WriteHeader(http.StatusOK)
			w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	targetDir := t.TempDir()
	targetBinary := filepath.Join(targetDir, "agy.exe")
	_ = os.WriteFile(targetBinary, []byte("old-binary"), 0o755)

	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
		TargetBinary:   targetBinary,
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("expected code 0, got %d; stderr: %s", code, stderr.String())
	}

	gotData, err := os.ReadFile(targetBinary)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(gotData, binaryContent) {
		t.Fatalf("expected %q, got %q", string(binaryContent), string(gotData))
	}
}

func TestRun_BinaryNotFoundInArchive(t *testing.T) {
	binaryContent := []byte("some text\n")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{
		Name: "unrelated.txt",
		Mode: 0o644,
		Size: int64(len(binaryContent)),
	})
	_, _ = tw.Write(binaryContent)
	_ = tw.Close()
	_ = gw.Close()

	archiveBytes := buf.Bytes()
	h := sha256.Sum256(archiveBytes)
	sha256Hex := hex.EncodeToString(h[:])

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Manifest{
				Version: "1.4.11",
				URL:     ts.URL + "/archive.tar.gz",
				Sha256:  sha256Hex,
			})
		case "/archive.tar.gz":
			w.WriteHeader(http.StatusOK)
			w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
		TargetBinary:   "/path/to/custom-binary",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 1 {
		t.Fatalf("expected code 1 when binary not found in archive, got %d", code)
	}

	errStr := stderr.String()
	if !strings.Contains(errStr, "failed to extract binary:") {
		t.Errorf("stderr missing extract binary error: %q", errStr)
	}
}

func TestRun_NewVersionWithTargetBinary(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho updated-agy\n")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{
		Name: "agy",
		Mode: 0o755,
		Size: int64(len(binaryContent)),
	})
	_, _ = tw.Write(binaryContent)
	_ = tw.Close()
	_ = gw.Close()

	archiveBytes := buf.Bytes()
	h := sha256.Sum256(archiveBytes)
	sha256Hex := hex.EncodeToString(h[:])

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Manifest{
				Version: "1.4.11",
				URL:     ts.URL + "/archive.tar.gz",
				Sha256:  sha256Hex,
			})
		case "/archive.tar.gz":
			w.WriteHeader(http.StatusOK)
			w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	targetDir := t.TempDir()
	targetBinary := filepath.Join(targetDir, "agy")
	_ = os.WriteFile(targetBinary, []byte("old-agy"), 0o755)

	var stdout, stderr bytes.Buffer
	opts := Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
		TargetBinary:   targetBinary,
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("expected code 0, got %d; stderr: %s", code, stderr.String())
	}

	gotData, err := os.ReadFile(targetBinary)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(gotData, binaryContent) {
		t.Fatalf("expected %q, got %q", string(binaryContent), string(gotData))
	}
	if !strings.Contains(stdout.String(), "✓ Update successful! Please restart agy.") {
		t.Errorf("missing success message in stdout: %q", stdout.String())
	}
}


func TestRun_RespectsEndpointsConfig(t *testing.T) {
	requestedPath := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"version": "1.4.10"}`))
	}))
	defer ts.Close()

	writeConfig(t, `{"endpoints": {"auto_updater": "`+ts.URL+`"}}`)

	var stdout, stderr bytes.Buffer
	opts := Options{
		CurrentVersion: "1.4.10",
		GOOS:           "darwin",
		GOARCH:         "arm64",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	if requestedPath != "/manifests/darwin_arm64.json" {
		t.Errorf("expected request to /manifests/darwin_arm64.json, got %q", requestedPath)
	}
}

func TestRun_RespectsEnvOverride(t *testing.T) {
	requestedPath := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"version": "1.4.10"}`))
	}))
	defer ts.Close()

	t.Setenv("OPENAGY_AUTO_UPDATER_URL", ts.URL+"/custom")

	var stdout, stderr bytes.Buffer
	opts := Options{
		CurrentVersion: "1.4.10",
		GOOS:           "linux",
		GOARCH:         "amd64",
	}

	code := Run(context.Background(), &stdout, &stderr, opts)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	if requestedPath != "/custom/manifests/linux_amd64.json" {
		t.Errorf("expected request to /custom/manifests/linux_amd64.json, got %q", requestedPath)
	}
}
