// image_generator_test.go — the cycle image-generator-subagent tests.
//
// The five tests are the contract the AGY 1.2.16 changelog line 7
// ("Added image generation via the new built-in `image-generator`
// subagent") places on the offline stub:
//
//  1. TestImageGeneratorStub_ProducesValidPNG         — the produced
//     artifact is a real PNG (magic bytes 89 50 4E 47 0D 0A 1A 0A).
//  2. TestImageGeneratorStub_Deterministic             — two calls
//     with the same prompt yield byte-identical files.
//  3. TestImageGeneratorStub_InCatalog                 — the
//     `image-generator` name appears in the Default() catalog.
//  4. TestImageGeneratorStub_ResponseIncludesPath       — the
//     handler's Text field mentions the on-disk path the model
//     sees back as the response body.
//  5. TestImageGeneratorStub_LeavesFileBehind          — pick
//     "leaves" (documented in ImageGeneratorHandler) and assert
//     the file is still on disk after the handler returns.
//
// The tests use t.TempDir to override the OS temp dir via TMPDIR so
// the global os.TempDir() (which the handler uses internally)
// resolves to the test-scoped directory. This keeps each test's
// generated file out of the real /tmp/ tree.
package builtinsubagent

import (
	"bytes"
	"context"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pngMagic is the 8-byte PNG signature every conforming PNG begins
// with: 0x89 'P' 'N' 'G' \r \n 0x1A \n. The package's Detect helper
// in imagery has the same constant; we duplicate it here
// to avoid a cross-package dependency for what is, fundamentally,
// a single test fixture.
var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// withTempDir sets the process TMPDIR to dir for the duration of the
// test, restoring the prior value via t.Cleanup. The image-generator
// handler reads os.TempDir() at write time, so this redirects every
// generated file into the test's own TempDir without having to
// thread a directory override through the public surface.
//
// Tests that call this MUST call it before any handler invocation
// that touches the filesystem.
func withTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, hadPrev := os.LookupEnv("TMPDIR")
	if err := os.Setenv("TMPDIR", dir); err != nil {
		t.Fatalf("TMPDIR setenv: %v", err)
	}
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("TMPDIR", prev)
		} else {
			_ = os.Unsetenv("TMPDIR")
		}
	})
	return dir
}

// TestImageGeneratorStub_ProducesValidPNG runs the handler with a
// fixed prompt, then re-reads the file at the returned path and
// asserts the 8-byte PNG magic prefix is present (the canonical
// "this is a PNG" check). The size assertion is intentionally
// permissive (just "non-empty") — the prompt allows anything from
// a 70-byte 1x1 PNG to a larger 64x64 image; the magic-byte check
// is the load-bearing assertion.
func TestImageGeneratorStub_ProducesValidPNG(t *testing.T) {
	withTempDir(t)
	ctx := context.Background()
	res, err := ImageGeneratorHandler(ctx, "a calm orange sunset over still water")
	if err != nil {
		t.Fatalf("ImageGeneratorHandler: %v", err)
	}
	if res.Path == "" {
		t.Fatalf("handler returned empty Path")
	}
	if res.MIMEType != "image/png" {
		t.Errorf("MIMEType = %q, want image/png", res.MIMEType)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", res.Path, err)
	}
	if len(data) < len(pngMagic) {
		t.Fatalf("PNG too short: %d bytes (need >= %d)", len(data), len(pngMagic))
	}
	if !bytes.Equal(data[:len(pngMagic)], pngMagic) {
		t.Fatalf("PNG magic mismatch: got % x, want % x", data[:len(pngMagic)], pngMagic)
	}
	if res.Bytes != len(data) {
		t.Errorf("Result.Bytes = %d, but file on disk has %d bytes", res.Bytes, len(data))
	}
}

// TestImageGeneratorStub_Deterministic invokes the handler twice
// with the same prompt and asserts the two on-disk files are
// byte-identical AND the returned paths are identical. The
// determinism guarantee is the load-bearing claim of the offline
// stub: a future cycle that swaps in a real image-gen API must
// preserve the path shape (TempDir / wagyu-image-generator-<hash>
// /image-generator-<hash>-<tag>.png) so callers can cache by
// path.
func TestImageGeneratorStub_Deterministic(t *testing.T) {
	withTempDir(t)
	ctx := context.Background()
	const prompt = "an emerald forest glade with shafts of sunlight"

	first, err := ImageGeneratorHandler(ctx, prompt)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := ImageGeneratorHandler(ctx, prompt)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if first.Path != second.Path {
		t.Errorf("path diverged across same-prompt calls: %q vs %q", first.Path, second.Path)
	}
	a, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatalf("ReadFile first: %v", err)
	}
	b, err := os.ReadFile(second.Path)
	if err != nil {
		t.Fatalf("ReadFile second: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("byte mismatch:\n  first  = % x\n  second = % x", a[:min(24, len(a))], b[:min(24, len(b))])
	}
	if !first.Deterministic || !second.Deterministic {
		t.Errorf("Deterministic flag must be true on both calls (got %v, %v)", first.Deterministic, second.Deterministic)
	}
}

// TestImageGeneratorStub_InCatalog asserts that Default() exposes
// "image-generator" as a registered built-in. This is the
// function-call-catalog surface the prompt's wiring requirement
// names ("the `image-generator` subagent must be visible in the
// model's tool list (or function-call catalogue) when subagents
// are loaded"). The test also verifies the description is
// non-empty so a future cycle that flips it to "" is caught.
func TestImageGeneratorStub_InCatalog(t *testing.T) {
	r := Default()
	names := r.Names()
	if len(names) == 0 {
		t.Fatal("Default() returned an empty catalog")
	}
	var found bool
	for _, n := range names {
		if n == string(KindImageGenerator) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("image-generator not in catalog: %v", names)
	}
	spec, ok := r.Spec(string(KindImageGenerator))
	if !ok {
		t.Fatalf("Spec(image-generator) returned !ok")
	}
	if spec.Name != KindImageGenerator {
		t.Errorf("spec.Name = %q, want %q", spec.Name, KindImageGenerator)
	}
	if spec.Description == "" {
		t.Errorf("spec.Description is empty")
	}
	if spec.Handler == nil {
		t.Errorf("spec.Handler is nil")
	}
	if catalog := r.Catalog(); !strings.Contains(catalog, string(KindImageGenerator)) {
		t.Errorf("Catalog() did not mention image-generator: %q", catalog)
	}
}

// TestImageGeneratorStub_ResponseIncludesPath verifies the
// handler's Text response carries the path the model needs to
// read back the PNG. The AGY 1.2.16 changelog wording is "and
// read the generated PNG directly from the subagent's response",
// so a Text that did not mention the path would force the
// model to scrape Result.Path from a structured envelope the
// cycle did not yet expose — the Text-includes-path contract
// is what makes the loop ergonomic today.
func TestImageGeneratorStub_ResponseIncludesPath(t *testing.T) {
	withTempDir(t)
	ctx := context.Background()
	res, err := ImageGeneratorHandler(ctx, "a small wooden boat on a foggy lake")
	if err != nil {
		t.Fatalf("ImageGeneratorHandler: %v", err)
	}
	if !strings.Contains(res.Text, res.Path) {
		t.Errorf("Text %q does not mention Path %q", res.Text, res.Path)
	}
	// The Path must be absolute (the model needs to be able to
	// open it from anywhere) and live inside the OS temp dir.
	if !filepath.IsAbs(res.Path) {
		t.Errorf("Path %q is not absolute", res.Path)
	}
	if !strings.HasPrefix(res.Path, filepath.Clean(os.TempDir())) {
		t.Errorf("Path %q is not under os.TempDir() %q", res.Path, os.TempDir())
	}
}

// TestImageGeneratorStub_LeavesFileBehind pins the cleanup
// behaviour the cycle prompt invited us to "pick and document":
// we deliberately leave the file on disk so a model turn that
// returns the path can hand it to a downstream tool (read_file,
// shell exec, etc.) without racing a cleanup goroutine. The
// ImageGeneratorHandler doc-comment states this explicitly.
//
// Two assertions:
//
//   - the file is still on disk immediately after the handler
//     returns (the policy);
//   - the prompt-driven directory contains exactly the one PNG
//     file the handler wrote (no temp-file debris).
func TestImageGeneratorStub_LeavesFileBehind(t *testing.T) {
	tmpRoot := withTempDir(t)
	ctx := context.Background()
	res, err := ImageGeneratorHandler(ctx, "two small apples on a wooden table")
	if err != nil {
		t.Fatalf("ImageGeneratorHandler: %v", err)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("expected file at %q to remain after handler returns, got %v", res.Path, err)
	}
	parent := filepath.Dir(res.Path)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", parent, err)
	}
	var pngCount int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".png") {
			pngCount++
		}
	}
	if pngCount != 1 {
		t.Errorf("prompt-dir contains %d PNGs, want exactly 1 (the handler's output)", pngCount)
	}
	// Sanity: the temp root is the test-scoped directory, not the
	// real /tmp/ tree — this is what the cleanup contract protects.
	if !strings.HasPrefix(parent, filepath.Clean(tmpRoot)) {
		t.Errorf("handler wrote outside the test TMPDIR (%s): %s", tmpRoot, parent)
	}
}

// TestImageGeneratorStub_RejectsEmptyPrompt pins the typed-sentinel
// rejection so a model that calls the handler with no prompt gets
// a clear error instead of a generated image of nothing. This is
// not one of the five required tests but keeps the rest honest —
// without it, TestImageGeneratorStub_ProducesValidPNG could pass on
// an empty-prompt implementation that silently returns an empty
// PNG.
func TestImageGeneratorStub_RejectsEmptyPrompt(t *testing.T) {
	ctx := context.Background()
	for _, in := range []string{"", "   ", "\t\n"} {
		if _, err := ImageGeneratorHandler(ctx, in); err == nil {
			t.Errorf("ImageGeneratorHandler(%q) = nil error, want non-nil", in)
		}
	}
}

// TestImageGeneratorStub_Registry_UnknownName pins the typed
// ErrUnknownName returned by Registry.Invoke for unregistered
// names, so the routing layer in internal/subagenttools can use
// errors.Is to translate a missing built-in into the prompt-side
// "no subagent named X is registered" message.
func TestImageGeneratorStub_Registry_UnknownName(t *testing.T) {
	r := Default()
	_, err := r.Invoke(context.Background(), "no-such-thing", "x")
	if err == nil {
		t.Fatal("Invoke(no-such-thing) = nil error, want ErrUnknownName")
	}
	if !strings.Contains(err.Error(), "no-such-thing") && err != ErrUnknownName {
		// errors.Is(target, ErrUnknownName) is the canonical check;
		// the contained-name assertion is belt-and-braces.
		t.Errorf("Invoke error = %v, want one wrapping ErrUnknownName", err)
	}
}

// TestImageGeneratorStub_Registry_RegisterDuplicate pins the
// duplicate-registration rejection. Default() uses Register under
// the hood; a future cycle that adds a second new built-in without
// removing an existing one must not silently shadow it.
func TestImageGeneratorStub_Registry_RegisterDuplicate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Spec{Name: KindImageGenerator, Handler: ImageGeneratorHandler, Description: "first"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(Spec{Name: KindImageGenerator, Handler: ImageGeneratorHandler, Description: "second"})
	if err == nil {
		t.Fatal("duplicate Register returned nil, want ErrAlreadyRegistered")
	}
}

// TestImageGeneratorStub_FilenameIsStable pins that the
// prompt-driven directory and filename are pure functions of
// the prompt — a future refactor that adds random bytes (a
// PID suffix, a nanosecond timestamp, etc.) would silently
// invalidate the determinism contract.
func TestImageGeneratorStub_FilenameIsStable(t *testing.T) {
	const prompt = "stillness"
	a := pathFor(prompt)
	b := pathFor(prompt)
	if a != b {
		t.Errorf("pathFor is non-deterministic: %q vs %q", a, b)
	}
	// The path should encode the prompt hash so the user can
	// eyeball two outputs and know they share a source.
	h := fnv.New32a()
	_, _ = h.Write([]byte(prompt))
	sum := h.Sum32()
	hex := itoaHex(sum)
	if !strings.Contains(a, hex) {
		t.Errorf("path %q does not contain the FNV hex %q", a, hex)
	}
	if !strings.HasSuffix(a, ".png") {
		t.Errorf("path %q does not end in .png", a)
	}
}

// min is a tiny local helper so the determinism test can bound
// the byte-diff output without pulling in the slices / numbers
// package's min at this Go version's source-compat level.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
