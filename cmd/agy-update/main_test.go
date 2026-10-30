package main_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/endpoints"
)

func TestAgyUpdateHelp(t *testing.T) {
	for _, flag := range []string{"--help", "-help"} {
		cmd := exec.Command("go", "run", ".", flag)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil {
			t.Fatalf("expected exit code 2, got 0 for %s; stdout: %s, stderr: %s", flag, stdout.String(), stderr.String())
		}
		if stdout.Len() > 0 {
			t.Errorf("expected empty stdout on %s, got: %q", flag, stdout.String())
		}
		if !strings.Contains(stderr.String(), "Usage of update:\n") {
			t.Fatalf("expected Usage of update:\n in stderr for %s, got: %s", flag, stderr.String())
		}
	}
}

func TestAgyUpdateVersion(t *testing.T) {
	for _, flag := range []string{"--version", "-version"} {
		cmd := exec.Command("go", "run", ".", flag)
		cmd.Env = append(os.Environ(), "AGY_CURRENT_VERSION=1.4.10")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil {
			t.Fatalf("expected exit code 0, got err: %v for %s; stderr: %s", err, flag, stderr.String())
		}
		if strings.TrimSpace(stdout.String()) != "1.4.10" {
			t.Errorf("expected 1.4.10, got: %q", stdout.String())
		}
		if stderr.Len() > 0 {
			t.Errorf("expected empty stderr, got: %q", stderr.String())
		}
	}
}

func TestAgyUpdateUnknownFlag(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--unknown")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected exit code 1, got 0")
	}
	wantPrefix := "flags provided but not defined: -unknown\nUsage of update:\nError: flags provided but not defined: -unknown\n"
	if !strings.HasPrefix(stderr.String(), wantPrefix) {
		t.Errorf("expected unknown flag error in stderr to start with: %q, got: %q", wantPrefix, stderr.String())
	}
}


func TestAgyUpdateWithConfigEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"version": "1.4.10",
		})
	}))
	defer ts.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	cfgData := `{"endpoints":{"auto_updater":"` + ts.URL + `"}}`
	if err := os.WriteFile(configPath, []byte(cfgData), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Env = append(os.Environ(),
		endpoints.ConfigPathEnvVar+"="+configPath,
		"AGY_CURRENT_VERSION=1.4.10",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v, output: %s", err, string(out))
	}
	s := string(out)
	if !strings.Contains(s, "Checking for updates... (current version 1.4.10)") {
		t.Fatalf("expected checking for updates, got: %s", s)
	}
	if !strings.Contains(s, "You are already on the latest version.") {
		t.Fatalf("expected latest version message, got: %s", s)
	}
}

func TestAgyUpdateDownloadAndReplace(t *testing.T) {
	newBinary := []byte("#!/bin/sh\necho updated-agy\n")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{
		Name: "agy",
		Mode: 0o755,
		Size: int64(len(newBinary)),
	})
	_, _ = tw.Write(newBinary)
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
			_ = json.NewEncoder(w).Encode(map[string]string{
				"version": "1.4.11",
				"url":     ts.URL + "/archive.tar.gz",
				"sha256":  sha256Hex,
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
	if err := os.WriteFile(targetBinary, []byte("old-agy"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}

	configPath := filepath.Join(targetDir, "config.json")
	cfgData := `{"endpoints":{"auto_updater":"` + ts.URL + `/manifest.json"}}`
	if err := os.WriteFile(configPath, []byte(cfgData), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Env = append(os.Environ(),
		endpoints.ConfigPathEnvVar+"="+configPath,
		"AGY_CURRENT_VERSION=1.4.10",
		"AGY_TARGET_BINARY="+targetBinary,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v, output: %s", err, string(out))
	}
	s := string(out)
	if !strings.Contains(s, "Found new version 1.4.11") {
		t.Errorf("missing new version message, got: %s", s)
	}
	if !strings.Contains(s, "Update successful! Please restart agy.") {
		t.Errorf("missing update successful message, got: %s", s)
	}

	gotData, err := os.ReadFile(targetBinary)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(gotData, newBinary) {
		t.Fatalf("expected updated binary content %q, got %q", string(newBinary), string(gotData))
	}
}

