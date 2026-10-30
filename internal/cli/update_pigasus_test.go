package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/justwasm/pigasus/internal/branding"
)

type mockRelease struct {
	version string
	isLess  bool
}

func (m *mockRelease) Version() string {
	return m.version
}

func (m *mockRelease) LessOrEqual(other string) bool {
	return m.isLess
}

type mockUpdater struct {
	release    Release
	found      bool
	detectErr  error
	updateErr  error
	updatedExe string
	calledRel  Release
}

func (m *mockUpdater) DetectLatest(ctx context.Context, repoSlug string) (Release, bool, error) {
	if m.detectErr != nil {
		return nil, false, m.detectErr
	}
	return m.release, m.found, nil
}

func (m *mockUpdater) UpdateTo(ctx context.Context, rel Release, cmdPath string) error {
	m.calledRel = rel
	m.updatedExe = cmdPath
	return m.updateErr
}

func TestUpdate_Pigasus_AlreadyLatest(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandPigasus)

	mock := &mockUpdater{
		release: &mockRelease{version: ReferenceVersion, isLess: true},
		found:   true,
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err != nil {
		t.Fatalf("RunUpdate failed: %v", err)
	}

	got := buf.String()
	wantHeader := "\u27f3 Checking for updates... (current version " + ReferenceVersion + ")\n"
	wantLatest := "\u2713 You are already on the latest version.\n"

	if !strings.HasPrefix(got, wantHeader) {
		t.Errorf("output missing header: got %q", got)
	}
	if !strings.Contains(got, wantLatest) {
		t.Errorf("output missing latest line: got %q", got)
	}
}

func TestUpdate_Pigasus_FoundNewVersion(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandPigasus)

	mock := &mockUpdater{
		release: &mockRelease{version: "1.5.0", isLess: false},
		found:   true,
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err != nil {
		t.Fatalf("RunUpdate failed: %v", err)
	}

	got := buf.String()
	wantFound := "\u2705 Found new version 1.5.0.\n\u27f3 Downloading update...\n\u2713 Successfully updated to version 1.5.0.\n"

	if !strings.Contains(got, wantFound) {
		t.Errorf("output missing update success flow: got %q, want %q", got, wantFound)
	}
}

func TestUpdate_Pigasus_DetectError(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandPigasus)

	mock := &mockUpdater{
		detectErr: errors.New("network timeout"),
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "network timeout") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdate_Pigasus_NotFound(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandPigasus)

	mock := &mockUpdater{
		found: false,
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "could not be found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdate_Pigasus_UpdateError(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandPigasus)

	mock := &mockUpdater{
		release:   &mockRelease{version: "1.5.0", isLess: false},
		found:     true,
		updateErr: errors.New("permission denied"),
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdate_Antigravity_AlwaysOffline(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandAntigravity)

	// Even if updater would report an error, Antigravity brand short-circuits.
	mock := &mockUpdater{
		detectErr: errors.New("should not be called"),
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err != nil {
		t.Fatalf("RunUpdate failed: %v", err)
	}

	got := buf.String()
	want := "\u27f3 Checking for updates... (current version " + ReferenceVersion + ")\n" +
		"\u2713 You are already on the latest version.\n"

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func createTarGz(filename string, content []byte) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{
		Name: filename,
		Mode: 0o755,
		Size: int64(len(content)),
	})
	_, _ = tw.Write(content)
	_ = tw.Close()
	_ = gw.Close()
	return buf.Bytes()
}

func createZip(filename string, content []byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(filename)
	_, _ = w.Write(content)
	_ = zw.Close()
	return buf.Bytes()
}

func TestCommandAssetFilter(t *testing.T) {
	filter := commandAssetFilter("pigga", "darwin", "arm64")
	re := regexp.MustCompile(filter)

	if !re.MatchString("pigga_v1.5.0_Darwin_arm64.tar.gz") {
		t.Errorf("filter %q should match pigga_v1.5.0_Darwin_arm64.tar.gz", filter)
	}
	if re.MatchString("pigga_v1.5.0_Linux_x86_64.tar.gz") {
		t.Errorf("filter %q should NOT match pigga_v1.5.0_Linux_x86_64.tar.gz", filter)
	}
	if re.MatchString("pigasus_v1.5.0_Darwin_arm64.tar.gz") {
		t.Errorf("filter %q should NOT match pigasus_v1.5.0_Darwin_arm64.tar.gz", filter)
	}
}

func TestDefaultSelfUpdater_UpdateTo_Success(t *testing.T) {
	binaryName := "pigga"
	newContent := []byte("#!/bin/sh\necho updated\n")
	archiveData := createTarGz(binaryName, newContent)
	archiveHash := fmt.Sprintf("%x", sha256.Sum256(archiveData))
	assetName := "pigga_v1.5.0_darwin_arm64.tar.gz"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/asset":
			w.WriteHeader(http.StatusOK)
			w.Write(archiveData)
		case "/checksums.txt":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%s  %s\n", archiveHash, assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	targetDir := t.TempDir()
	targetExe := filepath.Join(targetDir, binaryName)
	if err := os.WriteFile(targetExe, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("failed to create old binary: %v", err)
	}

	updater := &defaultSelfUpdater{}
	rel := &selfupdate.Release{
		AssetName:          assetName,
		AssetURL:           ts.URL + "/asset",
		ValidationAssetURL: ts.URL + "/checksums.txt",
	}

	if err := updater.UpdateTo(context.Background(), rel, targetExe); err != nil {
		t.Fatalf("UpdateTo failed: %v", err)
	}

	gotData, err := os.ReadFile(targetExe)
	if err != nil {
		t.Fatalf("failed to read updated binary: %v", err)
	}
	if !bytes.Equal(gotData, newContent) {
		t.Errorf("binary content mismatch: got %q, want %q", string(gotData), string(newContent))
	}
}

func TestDefaultSelfUpdater_UpdateTo_ZipArchive(t *testing.T) {
	binaryName := "pigga"
	newContent := []byte("#!/bin/sh\necho updated from zip\n")
	archiveData := createZip(binaryName, newContent)
	archiveHash := fmt.Sprintf("%x", sha256.Sum256(archiveData))
	assetName := "pigga_v1.5.0_windows_amd64.zip"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/asset":
			w.WriteHeader(http.StatusOK)
			w.Write(archiveData)
		case "/checksums.txt":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%s  %s\n", archiveHash, assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	targetDir := t.TempDir()
	targetExe := filepath.Join(targetDir, binaryName)
	if err := os.WriteFile(targetExe, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("failed to create old binary: %v", err)
	}

	updater := &defaultSelfUpdater{}
	rel := &selfupdate.Release{
		AssetName:          assetName,
		AssetURL:           ts.URL + "/asset",
		ValidationAssetURL: ts.URL + "/checksums.txt",
	}

	if err := updater.UpdateTo(context.Background(), rel, targetExe); err != nil {
		t.Fatalf("UpdateTo with zip failed: %v", err)
	}

	gotData, err := os.ReadFile(targetExe)
	if err != nil {
		t.Fatalf("failed to read updated binary: %v", err)
	}
	if !bytes.Equal(gotData, newContent) {
		t.Errorf("binary content mismatch: got %q, want %q", string(gotData), string(newContent))
	}
}

func TestDefaultSelfUpdater_UpdateTo_FallbackCommandCandidate(t *testing.T) {
	// If executable is named "custom-bin", it should fallback to extracting "pigga"
	newContent := []byte("#!/bin/sh\necho fallback binary\n")
	archiveData := createTarGz("pigga", newContent)
	archiveHash := fmt.Sprintf("%x", sha256.Sum256(archiveData))
	assetName := "pigga_v1.5.0_darwin_arm64.tar.gz"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/asset":
			w.WriteHeader(http.StatusOK)
			w.Write(archiveData)
		case "/checksums.txt":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%s  %s\n", archiveHash, assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	targetDir := t.TempDir()
	targetExe := filepath.Join(targetDir, "custom-bin")
	if err := os.WriteFile(targetExe, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("failed to create old binary: %v", err)
	}

	updater := &defaultSelfUpdater{}
	rel := &selfupdate.Release{
		AssetName:          assetName,
		AssetURL:           ts.URL + "/asset",
		ValidationAssetURL: ts.URL + "/checksums.txt",
	}

	if err := updater.UpdateTo(context.Background(), rel, targetExe); err != nil {
		t.Fatalf("UpdateTo failed with candidate fallback: %v", err)
	}

	gotData, err := os.ReadFile(targetExe)
	if err != nil {
		t.Fatalf("failed to read updated binary: %v", err)
	}
	if !bytes.Equal(gotData, newContent) {
		t.Errorf("binary content mismatch: got %q, want %q", string(gotData), string(newContent))
	}
}

func TestDefaultSelfUpdater_UpdateTo_ChecksumMismatch(t *testing.T) {
	binaryName := "pigga"
	newContent := []byte("#!/bin/sh\necho updated\n")
	archiveData := createTarGz(binaryName, newContent)
	assetName := "pigga_v1.5.0_darwin_arm64.tar.gz"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/asset":
			w.WriteHeader(http.StatusOK)
			w.Write(archiveData)
		case "/checksums.txt":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "0000000000000000000000000000000000000000000000000000000000000000  %s\n", assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	targetDir := t.TempDir()
	targetExe := filepath.Join(targetDir, binaryName)
	if err := os.WriteFile(targetExe, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("failed to create old binary: %v", err)
	}

	updater := &defaultSelfUpdater{}
	rel := &selfupdate.Release{
		AssetName:          assetName,
		AssetURL:           ts.URL + "/asset",
		ValidationAssetURL: ts.URL + "/checksums.txt",
	}

	err := updater.UpdateTo(context.Background(), rel, targetExe)
	if err == nil {
		t.Fatalf("expected error on checksum mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "checksum validation failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveCommandName(t *testing.T) {
	if got := resolveCommandName("/usr/local/bin/pigga"); got != "pigga" {
		t.Errorf("got %q, want pigga", got)
	}
	if got := resolveCommandName("/usr/local/bin/pigasus"); got != "pigasus" {
		t.Errorf("got %q, want pigasus", got)
	}
	if got := resolveCommandName("/tools/wagyu"); got != "wagyu" {
		t.Errorf("got %q, want wagyu", got)
	}
	if got := resolveCommandName("/usr/bin/unknown-custom"); got != "pigasus" {
		t.Errorf("got %q, want pigasus fallback", got)
	}
}

func TestDefaultSelfUpdater_UpdateTo_EmptyBinary(t *testing.T) {
	binaryName := "pigga"
	archiveData := createTarGz(binaryName, []byte{}) // 0-byte binary
	assetName := "pigga_v1.5.0_darwin_arm64.tar.gz"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(archiveData)
	}))
	defer ts.Close()

	targetDir := t.TempDir()
	targetExe := filepath.Join(targetDir, binaryName)
	_ = os.WriteFile(targetExe, []byte("preserve-me"), 0o755)

	updater := &defaultSelfUpdater{}
	rel := &selfupdate.Release{
		AssetName: assetName,
		AssetURL:  ts.URL + "/asset",
	}

	err := updater.UpdateTo(context.Background(), rel, targetExe)
	if err == nil {
		t.Fatalf("expected error on empty extracted binary, got nil")
	}

	// Verify target file was not corrupted
	data, _ := os.ReadFile(targetExe)
	if string(data) != "preserve-me" {
		t.Errorf("target file corrupted: %q", string(data))
	}
}

func TestDefaultSelfUpdater_UpdateTo_NestedInSubdirectory(t *testing.T) {
	binaryName := "pigga"
	nestedName := "pigga_v1.5.0_Darwin_arm64/pigga"
	newContent := []byte("#!/bin/sh\necho updated-nested\n")
	archiveData := createTarGz(nestedName, newContent)
	assetName := "pigga_v1.5.0_darwin_arm64.tar.gz"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(archiveData)
	}))
	defer ts.Close()

	targetDir := t.TempDir()
	targetExe := filepath.Join(targetDir, binaryName)
	_ = os.WriteFile(targetExe, []byte("old-content"), 0o755)

	updater := &defaultSelfUpdater{}
	rel := &selfupdate.Release{
		AssetName: assetName,
		AssetURL:  ts.URL + "/asset",
	}

	if err := updater.UpdateTo(context.Background(), rel, targetExe); err != nil {
		t.Fatalf("UpdateTo failed for nested binary: %v", err)
	}

	gotData, err := os.ReadFile(targetExe)
	if err != nil {
		t.Fatalf("read updated binary: %v", err)
	}
	if !bytes.Equal(gotData, newContent) {
		t.Errorf("binary content mismatch: got %q, want %q", string(gotData), string(newContent))
	}
}

func TestUpdate_Pigga_VersionHonored(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()
	defer SetProgramVersion("")

	branding.SetCurrent(branding.BrandPigasus)
	SetProgramVersion("2.1.0-wasm")

	mock := &mockUpdater{
		release: &mockRelease{version: "2.1.0", isLess: true},
		found:   true,
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err != nil {
		t.Fatalf("RunUpdate failed: %v", err)
	}

	got := buf.String()
	wantHeader := "Checking for updates... (current version 2.1.0-wasm)"
	if !strings.Contains(got, wantHeader) {
		t.Errorf("output missing custom version header: got %q", got)
	}
	if !strings.Contains(got, "You are already on the latest version.") {
		t.Errorf("expected already on latest version, got %q", got)
	}
}

func TestUpdate_Pigga_CustomRepoSlug(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandPigasus)
	t.Setenv("PIGGA_UPDATE_REPO", "custom-org/custom-pigga")

	var queriedSlug string
	customUpdater := &customSlugUpdater{
		onDetect: func(repoSlug string) {
			queriedSlug = repoSlug
		},
	}
	SetDefaultUpdater(customUpdater)

	var buf bytes.Buffer
	// Simulate running as pigga
	origArgs := os.Args
	os.Args = []string{"pigga", "update"}
	defer func() { os.Args = origArgs }()

	_ = RunUpdate(&buf, nil)

	if queriedSlug != "custom-org/custom-pigga" {
		t.Errorf("expected queriedSlug custom-org/custom-pigga, got %q", queriedSlug)
	}
}

type customSlugUpdater struct {
	onDetect func(slug string)
}

func (c *customSlugUpdater) DetectLatest(ctx context.Context, repoSlug string) (Release, bool, error) {
	if c.onDetect != nil {
		c.onDetect(repoSlug)
	}
	return &mockRelease{version: "9.9.9", isLess: true}, true, nil
}

func (c *customSlugUpdater) UpdateTo(ctx context.Context, rel Release, cmdPath string) error {
	return nil
}


