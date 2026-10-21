package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
)

// TestProbeWebM_ValidWithCluster — a header followed by a Cluster ID
// must pass: this is the canonical "agent attached a real recording"
// path. AGY 1.3.4's changelog gap is specifically the other side of
// this branch; this test pins the success side so a future regression
// does not regress valid attachments.
func TestProbeWebM_ValidWithCluster(t *testing.T) {
	// magic | vint(size=2) | 2 EBML body bytes | Cluster ID
	data := []byte{
		0x1A, 0x45, 0xDF, 0xA3,
		0x82, 0x01, 0x02,
		0x1F, 0x43, 0xB6, 0x75,
	}
	if err := probeWebM(data); err != nil {
		t.Errorf("probeWebM(valid-with-cluster): unexpected error: %v", err)
	}
}

// TestProbeWebM_ValidWithTracks — header + Tracks (no Cluster). A
// finished file almost always has a Cluster too, but a header-only
// file with Tracks metadata (e.g. an editor export that wrote the
// track table before the recording finished) is a plausible enough
// signature to let through; the rejection criterion is "neither Track
// nor Cluster present" rather than "Cluster specifically present".
func TestProbeWebM_ValidWithTracks(t *testing.T) {
	data := []byte{
		0x1A, 0x45, 0xDF, 0xA3,
		0x82, 0x01, 0x02,
		0x16, 0x54, 0xAE, 0x6B,
	}
	if err := probeWebM(data); err != nil {
		t.Errorf("probeWebM(valid-with-tracks): unexpected error: %v", err)
	}
}

// TestProbeWebM_Truncated — magic + EBML header but no Segment /
// Track / Cluster at all. This is the canonical "recording cut off
// before the first cluster landed" failure that bricked conversations
// before cycle truncated-webm-rejection: the agent sees the magic,
// thinks the file is well-formed, attaches it, and every later
// request dies when the backend tries to decode cluster data.
func TestProbeWebM_Truncated(t *testing.T) {
	data := []byte{
		0x1A, 0x45, 0xDF, 0xA3,
		0x82, 0x01, 0x02,
	}
	if err := probeWebM(data); !errors.Is(err, ErrTruncatedWebM) {
		t.Errorf("probeWebM(truncated): got %v, want ErrTruncatedWebM", err)
	}
}

// TestProbeWebM_HeaderOnly — same shape as Truncated but with extra
// EBML body bytes (DocType / DocTypeReadVersion / DocTypeExtension).
// Nothing after the header. AGY 1.3.4 calls this out by name in the
// changelog ("header-only").
func TestProbeWebM_HeaderOnly(t *testing.T) {
	data := []byte{
		0x1A, 0x45, 0xDF, 0xA3,
		0x85, 'w', 'e', 'b', 'm',
	}
	if err := probeWebM(data); !errors.Is(err, ErrTruncatedWebM) {
		t.Errorf("probeWebM(header-only): got %v, want ErrTruncatedWebM", err)
	}
}

// TestProbeWebM_Empty — zero-byte file. Should reject before reaching
// the magic check, which is what we want: a 0-byte .webm cannot be a
// real WebM.
func TestProbeWebM_Empty(t *testing.T) {
	if err := probeWebM(nil); !errors.Is(err, ErrTruncatedWebM) {
		t.Errorf("probeWebM(empty): got %v, want ErrTruncatedWebM", err)
	}
}

// TestProbeWebM_TooShort — 1–4 bytes: not enough to contain even the
// magic + size vint. Must reject.
func TestProbeWebM_TooShort(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4} {
		buf := make([]byte, n)
		// 5-byte array and below never matches the magic, but the
		// len check rejects before we get there.
		err := probeWebM(buf)
		if !errors.Is(err, ErrTruncatedWebM) {
			t.Errorf("probeWebM(%d bytes): got %v, want ErrTruncatedWebM", n, err)
		}
	}
}

// TestProbeWebM_BadMagic — looks WebM-shaped at first byte but the
// rest is wrong. Should reject.
func TestProbeWebM_BadMagic(t *testing.T) {
	data := []byte{0x1A, 0x45, 0xDE, 0xA3, 0x82, 0x01, 0x02, 0x1F, 0x43, 0xB6, 0x75}
	if err := probeWebM(data); !errors.Is(err, ErrTruncatedWebM) {
		t.Errorf("probeWebM(bad magic): got %v, want ErrTruncatedWebM", err)
	}
}

// TestIsWebMPath — case-insensitive suffix match. ".WEBM", ".WebM",
// trailing path components should all match; ".webp" (an entirely
// different format) should not.
func TestIsWebMPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"clip.webm", true},
		{"clip.WEBM", true},
		{"clip.WebM", true},
		{"/a/b/c/rec.webm", true},
		{"clip.webp", false}, // different format — must NOT be probed
		{"clip.txt", false},
		{"", false},
		{"webm", false}, // just the extension name, no dot
	}
	for _, c := range cases {
		if got := IsWebMPath(c.path); got != c.want {
			t.Errorf("IsWebMPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestSkipEBMLSize — direct unit test for the VINT advance. Mirrors
// RFC 9559 §4.2 "Variable-Size Integer" examples; if a future
// regression breaks VINT reading, every WebM-shaped file flips to
// "truncated", which this test catches immediately rather than via
// downstream symptoms.
func TestSkipEBMLSize(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		id   int
		want int
		ok   bool
	}{
		{"1-byte vint value=0", []byte{0x80}, 0, 1, true},
		{"1-byte vint value=2", []byte{0x82}, 0, 1, true},
		{"2-byte vint value=64", []byte{0x40, 0x40}, 0, 2, true},
		{"zero first byte", []byte{0x00, 0x00}, 0, 0, false},
		{"idLen past end", []byte{0x80}, 2, 0, false},
		{"vint would overflow", []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01}, 4, 0, false},
	}
	for _, c := range cases {
		end, ok := skipEBMLSize(c.data, c.id)
		if ok != c.ok || end != c.want {
			t.Errorf("%s: skipEBMLSize(%x, %d) = (%d, %v), want (%d, %v)",
				c.name, c.data, c.id, end, ok, c.want, c.ok)
		}
	}
}

// TestReadFile_WebM_Truncated — the integration check: a
// header-only .webm file written to the workspace should make
// ReadFile return ErrTruncatedWebM (wrapped as a plain error by the
// tool layer). The error must surface at read-time so the model gets
// a clear signal before attaching, instead of after.
func TestReadFile_WebM_Truncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.webm")
	data := []byte{
		0x1A, 0x45, 0xDF, 0xA3,
		0x82, 0x01, 0x02,
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &ReadFile{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(`{"path":"clip.webm"}`))
	if err == nil {
		t.Fatalf("ReadFile(WEBm truncated): expected error, got output %v", out)
	}
	if !errors.Is(err, ErrTruncatedWebM) {
		t.Errorf("ReadFile: err = %v, want ErrTruncatedWebM", err)
	}
}

// TestReadFile_WebM_Valid — integration happy path: a WebM with the
// magic and a Cluster ID must read through to the (otherwise
// undecoded) bytes. The probe is intentionally tolerant here; the
// full decode is somebody else's problem.
func TestReadFile_WebM_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.webm")
	data := []byte{
		0x1A, 0x45, 0xDF, 0xA3,
		0x82, 0x01, 0x02,
		0x1F, 0x43, 0xB6, 0x75,
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &ReadFile{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(`{"path":"clip.webm"}`))
	if err != nil {
		t.Fatalf("ReadFile(valid WebM): %v", err)
	}
	got, ok := out.(string)
	if !ok {
		t.Fatalf("ReadFile: out type = %T, want string", out)
	}
	// Cycle 293: the tool result now carries the reference's timestamp header
	// and "N: " line prefix; the golden file holds the file's bytes.
	if i := strings.Index(got, "\n1: "); i >= 0 {
		got = got[i+4:]
	}
	golden.RequireEqual(t, []byte(got))
}

// TestReadFile_NonWebM_NoRejection — the extension gate must NOT fire
// for non-.webm paths even when the bytes happen to start with the
// WebM magic. The probe is only consulted when IsWebMPath returns
// true; this test pins that contract so a future refactor of
// ReadFile does not accidentally reject plain text or JSON files.
func TestReadFile_NonWebM_NoRejection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	content := "Hello, world.\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &ReadFile{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(`{"path":"notes.txt"}`))
	if err != nil {
		t.Fatalf("ReadFile(notes.txt): %v", err)
	}
	if got, ok := out.(string); !ok || !strings.Contains(got, "1: Hello, world.") {
		t.Errorf("ReadFile: got %v (%T), want the numbered line result", out, out)
	}
}

// TestReadFile_NonWebMExtension_WebMMagic_NoRejection — a file with
// the .webp extension whose bytes happen to start with the WebM
// magic. The probe must not fire because IsWebMPath is the gate; the
// file is returned as-is.
func TestReadFile_NonWebMExtension_WebMMagic_NoRejection(t *testing.T) {
	dir := t.TempDir()
	// Use ".webp" (a different format) to prove the suffix check,
	// not the magic check, drives the gate.
	path := filepath.Join(dir, "clip.webp")
	data := []byte{
		0x1A, 0x45, 0xDF, 0xA3,
		0x82, 0x01, 0x02,
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &ReadFile{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(`{"path":"clip.webp"}`))
	if err != nil {
		t.Fatalf("ReadFile(clip.webp with WebM magic): %v", err)
	}
	got, ok := out.(string)
	if !ok {
		t.Fatalf("ReadFile: out type = %T, want string", out)
	}
	// Cycle 293: the tool result now carries the reference's timestamp header
	// and "N: " line prefix; the golden file holds the file's bytes.
	if i := strings.Index(got, "\n1: "); i >= 0 {
		got = got[i+4:]
	}
	golden.RequireEqual(t, []byte(got))
}
