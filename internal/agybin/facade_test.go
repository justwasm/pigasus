package agybin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/agybin"
)

func TestAgybinFacade(t *testing.T) {
	if agybin.EnvRealBin != "AGY_REAL_BIN" {
		t.Fatalf("unexpected EnvRealBin: %s", agybin.EnvRealBin)
	}
	if agybin.EnvNoAutoDownload != "AGY_NO_AUTO_DOWNLOAD" {
		t.Fatalf("unexpected EnvNoAutoDownload: %s", agybin.EnvNoAutoDownload)
	}
	if len(agybin.DefaultPaths) == 0 {
		t.Fatal("expected non-empty DefaultPaths")
	}
	if agybin.CacheBinaryName() != "agy" && agybin.CacheBinaryName() != "agy.exe" {
		t.Fatalf("unexpected CacheBinaryName: %s", agybin.CacheBinaryName())
	}
	if agybin.CacheDir() == "" {
		t.Fatal("expected non-empty CacheDir")
	}
	if agybin.CacheBinaryPath() == "" {
		t.Fatal("expected non-empty CacheBinaryPath")
	}

	agybin.SetDisableAutoDownload(true)
	defer agybin.SetDisableAutoDownload(false)

	tmpBin := filepath.Join(t.TempDir(), "dummy-agy")
	if err := os.WriteFile(tmpBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := agybin.Path(tmpBin)
	if err != nil {
		t.Fatalf("unexpected error from agybin.Path: %v", err)
	}
	want, err := filepath.EvalSymlinks(tmpBin)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
