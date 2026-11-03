package agyproxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCaptureFile drops a capture-shaped file into dir with a known size
// and a known mtime, so the rotation's ordering is deterministic.
func writeCaptureFile(t *testing.T, dir, name string, size int, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	ts := time.Now().Add(-age)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

// TestCapturePruneRemovesOldestFirst pins the rotation: the directory is
// trimmed to its ceiling and the *oldest* files go, because the newest
// evidence is the evidence you want.
func TestCapturePruneRemovesOldestFirst(t *testing.T) {
	dir := t.TempDir()
	oldest := writeCaptureFile(t, dir, "1000-001-streamGenerateContent.json", 100, 3*time.Hour)
	middle := writeCaptureFile(t, dir, "1001-002-streamGenerateContent.json", 100, 2*time.Hour)
	newest := writeCaptureFile(t, dir, "1002-003-streamGenerateContent.json", 100, time.Hour)

	c := newCapture(dir, t.Logf, 250)
	defer c.close()
	c.prune()

	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Errorf("the oldest capture survived the prune: %v", err)
	}
	for _, keep := range []string{middle, newest} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s was pruned but should have survived: %v", keep, err)
		}
	}
}

// TestCapturePruneUnderCeilingKeepsEverything pins the other half: a
// directory below its ceiling loses nothing.
func TestCapturePruneUnderCeilingKeepsEverything(t *testing.T) {
	dir := t.TempDir()
	name := writeCaptureFile(t, dir, "1000-001-streamGenerateContent.json", 100, time.Hour)

	c := newCapture(dir, t.Logf, 1<<20)
	defer c.close()
	c.prune()

	if _, err := os.Stat(name); err != nil {
		t.Fatalf("a capture under the ceiling was pruned: %v", err)
	}
}

// TestCapturePruneLeavesForeignFiles pins the blast radius: a capture
// directory that holds anything else keeps it. The rotation only ever
// removes the files this proxy wrote.
func TestCapturePruneLeavesForeignFiles(t *testing.T) {
	dir := t.TempDir()
	foreign := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(foreign, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write foreign: %v", err)
	}
	half := filepath.Join(dir, "123-abc.json")
	if err := os.WriteFile(half, []byte("keep me too"), 0o600); err != nil {
		t.Fatalf("write foreign: %v", err)
	}
	writeCaptureFile(t, dir, "1000-001-streamGenerateContent.json", 4000, time.Hour)

	c := newCapture(dir, t.Logf, 1)
	defer c.close()
	c.prune()

	for _, keep := range []string{foreign, half} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s was pruned but is not a capture: %v", keep, err)
		}
	}
}

// TestCaptureNoCeilingWritesEverything pins that a ceiling of zero — the
// explicit "unlimited" — keeps the old unbounded behaviour.
func TestCaptureNoCeilingWritesEverything(t *testing.T) {
	dir := t.TempDir()
	c := newCapture(dir, t.Logf, 0)
	c.write(captureRecord{Kind: "request", Method: "loadCodeAssist"})
	c.write(captureRecord{Kind: "response", Method: "loadCodeAssist"})
	c.close()

	files, total := scanCaptures(dir)
	if len(files) != 2 {
		t.Fatalf("captures written = %d, want 2", len(files))
	}
	if total <= 0 {
		t.Fatalf("capture total size = %d", total)
	}
}

// TestCaptureRotationEndToEnd drives the proxy's own HTTP surface: the
// captures the server writes are held to the ceiling, and the oldest file
// goes.
func TestCaptureRotationEndToEnd(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	oldest := writeCaptureFile(t, dir, "1000-001-loadCodeAssist.json", 4000, 2*time.Hour)
	newer := writeCaptureFile(t, dir, "1001-002-loadCodeAssist.json", 4000, time.Hour)

	max := int64(5000)
	s, err := New(Config{
		Addr:            "127.0.0.1:0",
		Upstream:        upstream.URL,
		CaptureDir:      dir,
		CaptureMaxBytes: &max,
		Logf:            t.Logf,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if _, err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	resp := post(t, s, "/v1internal:loadCodeAssist", `{"probe":true}`)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	// Close drains the capture queue, so the files are on disk by now.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Errorf("the oldest capture survived the rotation: %v", err)
	}
	if _, err := os.Stat(newer); err != nil {
		t.Errorf("the newer capture was pruned but should have survived: %v", err)
	}
	files, total := scanCaptures(dir)
	if len(files) < 2 {
		t.Errorf("captures on disk = %d, want the surviving file plus the new pair", len(files))
	}
	if total > max {
		t.Errorf("capture directory = %d bytes, over the %d ceiling", total, max)
	}
}

// TestPurgeCaptures pins the purge: it removes the capture files and
// reports what went, and it leaves everything else alone.
func TestPurgeCaptures(t *testing.T) {
	dir := t.TempDir()
	writeCaptureFile(t, dir, "1000-001-streamGenerateContent.json", 100, time.Hour)
	writeCaptureFile(t, dir, "1001-002-v1internal-cascadeNuxes.json", 200, time.Hour)
	foreign := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(foreign, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write foreign: %v", err)
	}

	files, freed, err := purgeCaptures(dir)
	if err != nil {
		t.Fatalf("purgeCaptures: %v", err)
	}
	if files != 2 {
		t.Errorf("purged %d files, want 2", files)
	}
	if freed != 300 {
		t.Errorf("freed %d bytes, want 300", freed)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("the purge removed a foreign file: %v", err)
	}
	if left, _ := scanCaptures(dir); len(left) != 0 {
		t.Errorf("%d capture files survived the purge", len(left))
	}
}

// TestIsCaptureName pins the name rule the rotation and the purge share:
// "<epochmillis>-<seq>-<method>.json". The method may carry dashes of its
// own, so only the first two fields are split off.
func TestIsCaptureName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"1792429582609-026-streamGenerateContent.json", true},
		{"1792760999745-5057-v1internal-cascadeNuxes.json", true},
		{"1000-001-.json", false},
		{"1000-001.json", false},
		{"abc-001-streamGenerateContent.json", false},
		{"1000-abc-streamGenerateContent.json", false},
		{"notes.txt", false},
		{"1000-001-streamGenerateContent.txt", false},
	}
	for _, c := range cases {
		if got := isCaptureName(c.name); got != c.want {
			t.Errorf("isCaptureName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestParseBytes pins the size syntax the admin commands and the
// environment variable accept.
func TestParseBytes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"1024", 1024},
		{"1KB", 1000},
		{"1MB", 1000 * 1000},
		{"1GB", 1000 * 1000 * 1000},
		{"1K", 1 << 10},
		{"1M", 1 << 20},
		{"1G", 1 << 30},
		{"1KiB", 1 << 10},
		{"1MiB", 1 << 20},
		{"1GiB", 1 << 30},
		{"512MB", 512 * 1000 * 1000},
		{"  2MB  ", 2 * 1000 * 1000},
	}
	for _, c := range cases {
		got, err := parseBytes(c.in)
		if err != nil {
			t.Errorf("parseBytes(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseBytes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	for _, in := range []string{"", "abc", "-5", "1XB"} {
		if _, err := parseBytes(in); err == nil {
			t.Errorf("parseBytes(%q) accepted a bad value", in)
		}
	}
}

// TestResolveCaptureMaxPrecedence pins the ceiling's resolution order:
// environment beats file, file beats the default, and 0 means "no ceiling".
func TestResolveCaptureMaxPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAGY_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("OPENAGY_CONFIG_HOME", dir)
	t.Setenv(EnvCaptureMax, "")

	// Default.
	if got := resolveSettings(fileConfig{}).CaptureMaxBytes; got != DefaultCaptureMaxBytes {
		t.Fatalf("default capture max = %d, want %d", got, DefaultCaptureMaxBytes)
	}

	// File beats default.
	fileMax := int64(500 * 1000 * 1000)
	if got := resolveSettings(fileConfig{CaptureMaxBytes: &fileMax}).CaptureMaxBytes; got != fileMax {
		t.Fatalf("file capture max = %d, want %d", got, fileMax)
	}

	// Environment beats file.
	t.Setenv(EnvCaptureMax, "2MB")
	if got := resolveSettings(fileConfig{CaptureMaxBytes: &fileMax}).CaptureMaxBytes; got != 2*1000*1000 {
		t.Fatalf("env capture max = %d, want %d", got, 2*1000*1000)
	}

	// 0 is "unlimited", from either source.
	t.Setenv(EnvCaptureMax, "0")
	if got := resolveSettings(fileConfig{}).CaptureMaxBytes; got != 0 {
		t.Fatalf("env 0 capture max = %d, want 0 (unlimited)", got)
	}
	zero := int64(0)
	if got := resolveSettings(fileConfig{CaptureMaxBytes: &zero}).CaptureMaxBytes; got != 0 {
		t.Fatalf("file 0 capture max = %d, want 0 (unlimited)", got)
	}
}

// TestCaptureMaxRoundTrip pins the admin command: `capture max` persists
// the ceiling and `capture purge` empties the directory.
func TestCaptureMaxRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAGY_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("OPENAGY_CONFIG_HOME", dir)
	t.Setenv(EnvCaptureDir, filepath.Join(dir, "captures"))
	t.Setenv(EnvCaptureMax, "")
	t.Setenv("HOME", dir)

	if code := Admin(DefaultOptions(), []string{"capture", "max", "250MB"}); code != 0 {
		t.Fatalf("capture max = %d", code)
	}
	got := loadConfig().CaptureMaxBytes
	if got == nil || *got != 250*1000*1000 {
		t.Fatalf("persisted capture max = %v, want 250000000", got)
	}

	// The environment overrides the persisted value.
	t.Setenv(EnvCaptureMax, "10MB")
	if got := resolveSettings(loadConfig()).CaptureMaxBytes; got != 10*1000*1000 {
		t.Fatalf("env capture max = %d, want 10000000", got)
	}
	t.Setenv(EnvCaptureMax, "")

	captureDir := filepath.Join(dir, "captures")
	if err := os.MkdirAll(captureDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeCaptureFile(t, captureDir, "1000-001-streamGenerateContent.json", 100, time.Hour)

	if code := Admin(DefaultOptions(), []string{"capture", "purge"}); code != 0 {
		t.Fatalf("capture purge = %d", code)
	}
	if left, _ := scanCaptures(captureDir); len(left) != 0 {
		t.Fatalf("%d capture files survived the purge", len(left))
	}
}
