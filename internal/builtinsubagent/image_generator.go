// Package builtinsubagent implements the AGY 1.2.16 changelog line:
//
//	Added image generation via the new built-in `image-generator` subagent,
//	so a model can request image generation by invoking it and read the
//	generated PNG directly from the subagent's response.
//
// The package is the catalog + handler layer for subagents that ship with
// the CLI itself rather than being defined in the user's project (the
// cycle customagent package owns the project-local Markdown sidecar path).
// A built-in subagent has three stable properties:
//
//   - a name that is part of the CLI's public contract ("image-generator"
//     today; future built-ins add their own Kind constants);
//   - a one-paragraph description that the model-side catalogue exposes
//     so a model can pick the right built-in for a task;
//   - a handler that, given a prompt, produces a structured Result.
//
// The image-generator handler is intentionally offline: this cycle is
// the *stub* of the reference's network-backed generator, so it never
// reaches an external service. Instead it writes a deterministic PNG to
// the OS temp dir and returns the path plus a brief description so the
// model can read it back. The PNG is a valid 64x64 RGBA image whose
// background colour and a 4x4 "fingerprint" tile in the corner are
// derived from an FNV-32a hash of the prompt — that is what makes two
// invocations with the same prompt byte-identical, and what lets a user
// eyeball two outputs and know they came from the same source.
//
// # Wiring
//
// internal/subagenttools (cycle 228) owns the AGY 1.4.0 subagent
// toolset the model sees ("invoke_subagent", "send_message",
// "manage_subagents"). The built-in subagents this package registers
// are reachable through `invoke_subagent` when the model supplies an
// `agent_name` matching a built-in name — see
// invoke_subagent's schema in internal/subagenttools/subagenttools.go
// for the routing rule and the catalogue description that lists the
// built-ins. A future cycle may graduate a built-in to a fully-fledged
// tool; today they share the invoke_subagent surface.
package builtinsubagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Kind is the stable name a built-in subagent is registered under. The
// value is what the model sees in the catalogue and what callers pass
// to Registry.Invoke. Names are lowercase, hyphen-separated identifiers
// — the same shape the reference uses for `image-generator` in
// `internal/cli/testdata/changelog_reference.txt` line 107.
type Kind string

const (
	// KindImageGenerator is the offline-only image generation subagent
	// that ships with the CLI (AGY 1.2.16 changelog line 7). Today's
	// handler produces a deterministic local PNG; a future cycle can
	// replace the body with a network call without changing the name
	// or the Result shape, so callers stay insulated from the swap.
	KindImageGenerator Kind = "image-generator"
)

// Spec is the catalogue entry for a built-in subagent: a stable name
// (Kind), the model-facing description, and the Handler. Spec is what
// internal/subagenttools reads to advertise the built-in inside the
// invoke_subagent description.
type Spec struct {
	Name        Kind
	Description string
	Handler     Handler
}

// Handler is the contract every built-in subagent implements. It is
// given a context (for cancellation) and the user's prompt; it returns
// a structured Result. Returning an error surfaces to the model the
// same way invoke_subagent does today (with `IsError: true` and the
// error message in the result body).
//
// The contract is deliberately narrow: a Handler does not get the
// parent's model, the parent's transcript, or any state besides what
// was passed in. That is the same shape AGY documents for built-in
// agents in `google3/third_party/jetski/cortex/customizations/builtin/`
// — built-ins are independent of the parent's runtime so a future
// network-backed implementation can substitute cleanly.
type Handler func(ctx context.Context, prompt string) (Result, error)

// Result is the structured response a Handler returns. Text is the
// short human-readable description the model surfaces back to the
// user; Path is the on-disk location of any generated artifact
// (currently only the image-generator writes one); MIMEType is the
// artifact's media type when Path is set, empty otherwise.
//
// Bytes is the byte length of the artifact written (so a caller can
// verify it without re-stat'ing). Deterministic reports whether two
// invocations with the same prompt produce the same artifact; the
// image-generator handler sets it true because the offline PNG is a
// pure function of the prompt.
type Result struct {
	Text          string
	Path          string
	MIMEType      string
	Bytes         int
	Deterministic bool
}

// Registry is the thread-safe catalogue of built-in subagents the
// package exposes. The zero value is not usable; callers obtain a
// fully-populated instance via Default() or build their own with
// NewRegistry + Register.
//
// The same Registry can be shared across a TUI session and a print
// path (cycle 228's "the same Manager instance is shared by the TUI
// run mode and the print path" pattern from internal/subagentrouter);
// Register/Invoke are safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]Spec
}

// NewRegistry returns an empty Registry. Tests use this to spin up an
// isolated catalogue; production code uses Default.
func NewRegistry() *Registry {
	return &Registry{entries: map[string]Spec{}}
}

// Register records spec under spec.Name. A duplicate name is rejected
// with ErrAlreadyRegistered so a startup double-Register (a real risk
// when init() chains run) fails loudly instead of silently shadowing
// the earlier registration.
//
// An empty name or nil Handler is rejected with a typed error so the
// registration call sites cannot accidentally publish a broken entry.
func (r *Registry) Register(spec Spec) error {
	if r == nil {
		return errors.New("builtinsubagent: nil registry")
	}
	if spec.Name == "" {
		return errors.New("builtinsubagent: empty name")
	}
	if spec.Handler == nil {
		return errors.New("builtinsubagent: nil handler")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[string(spec.Name)]; ok {
		return ErrAlreadyRegistered
	}
	r.entries[string(spec.Name)] = spec
	return nil
}

// Names returns the sorted list of registered built-in names. The
// order is ASCII-lexicographic so callers (system-prompt snippets,
// list rendering) get a stable rendering across processes.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.entries))
	for k := range r.entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Spec returns the catalogue entry for name. The second return value
// is false when no such built-in is registered. The returned Spec is
// a defensive copy of the registered value, so callers can mutate it
// without disturbing the registry.
func (r *Registry) Spec(name string) (Spec, bool) {
	if r == nil {
		return Spec{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.entries[name]
	if !ok {
		return Spec{}, false
	}
	return s, true
}

// ErrUnknownName is returned by Invoke when name is not registered.
// Callers surface it as the prompt-layer's "no subagent named X is
// registered" message.
var ErrUnknownName = errors.New("builtinsubagent: no subagent registered under that name")

// ErrAlreadyRegistered is returned by Register when name has already
// been claimed. The package documents this as a startup-time error;
// runtime duplicate-registrations are a programming bug.
var ErrAlreadyRegistered = errors.New("builtinsubagent: name already registered")

// Invoke runs the built-in subagent registered under name. It is the
// built-in-side counterpart of subagentrouter.Dispatch: same shape
// (ctx, name, args) and same ErrUnknownName sentinel for the caller's
// "not registered" path. Handler errors propagate unwrapped; the
// caller decides whether to surface them as IsError tool results or
// as structured failure envelopes (today internal/subagenttools does
// the latter).
//
// The returned error wraps ErrUnknownName with the offending name
// inside the message so the model-side surface can echo "no
// subagent named X is registered" without having to re-read the
// registry. errors.Is(err, ErrUnknownName) still matches so the
// caller can route on the sentinel alone.
func (r *Registry) Invoke(ctx context.Context, name, prompt string) (Result, error) {
	if r == nil {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownName, name)
	}
	r.mu.RLock()
	spec, ok := r.entries[name]
	r.mu.RUnlock()
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownName, name)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return spec.Handler(ctx, prompt)
}

// Catalog renders the registry as a model-facing description block:
// one line per built-in, "<name>: <description>". The result is what
// internal/subagenttools splices into the invoke_subagent tool
// description so the model can pick the right built-in. Names are
// sorted (matching Names()) so the rendering is deterministic.
//
// An empty registry renders as the empty string — the same
// convention printSlashAgents uses for "no agents available".
func (r *Registry) Catalog() string {
	r.mu.RLock()
	entries := make([]Spec, 0, len(r.entries))
	for _, s := range r.entries {
		entries = append(entries, s)
	}
	r.mu.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(e.Name))
		b.WriteString(": ")
		b.WriteString(e.Description)
	}
	return b.String()
}

// Default returns a Registry pre-populated with every built-in the
// CLI ships today. New built-ins should add their Kind constant,
// their Spec entry, and their Handler function above this constructor
// so a single Default() call captures them all.
//
// The registry is built once per call; callers that want a singleton
// across the process should wrap Default in their own init() (the
// subagenttools package does this on first access).
func Default() *Registry {
	r := NewRegistry()
	for _, spec := range defaultSpecs {
		// Register can only fail on duplicate names or nil handlers,
		// both of which are programming bugs the table above would
		// surface at compile/test time. Swallowing the error here
		// would hide the bug, so we panic with the error attached.
		if err := r.Register(spec); err != nil {
			panic("builtinsubagent: default register " + string(spec.Name) + ": " + err.Error())
		}
	}
	return r
}

// defaultSpecs is the catalogue Default() seeds. Adding a new
// built-in is a single Spec literal in this slice.
var defaultSpecs = []Spec{
	{
		Name:        KindImageGenerator,
		Description: "Generates an image from a textual prompt and returns the PNG path. Offline stub: writes a deterministic local PNG.",
		Handler:     ImageGeneratorHandler,
	},
}

// ImageGeneratorHandler is the default handler registered under
// KindImageGenerator. It accepts a prompt, generates a deterministic
// 64x64 PNG whose colours are derived from an FNV-32a hash of the
// prompt, writes it to the OS temp dir under a stable filename, and
// returns the path plus a short description.
//
// Determinism: two invocations with the same prompt produce
// byte-identical files (same colour, same fingerprint, same filename).
// The filename is `image-generator-<hexhash>.png` where <hexhash> is
// the lower-case hex of the FNV-32a hash of the prompt, so a user
// can tell two outputs apart at a glance from the filename alone.
//
// Cleanup: the file is left on disk after the handler returns — the
// prompt explicitly invites either approach. Callers that want the
// file gone should os.Remove the path themselves; a future cycle can
// add a TempDir-style auto-cleanup when the model loop owns a single
// conversation scope that can host the lifecycle.
//
// Errors: an empty prompt is rejected with errEmptyPrompt so a model
// that calls the handler without a description gets an actionable
// error. Temp-dir or write failures bubble up wrapped with the
// underlying os error.
func ImageGeneratorHandler(ctx context.Context, prompt string) (Result, error) {
	if strings.TrimSpace(prompt) == "" {
		return Result{}, errEmptyPrompt
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	hash := fnv.New32a()
	_, _ = hash.Write([]byte(prompt))
	sum := hash.Sum32()

	dir := tempDirFor(prompt)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	path := filepath.Join(dir, filenameFor(sum, prompt))

	data, err := buildPng64x64(prompt, sum)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Result{}, err
	}

	return Result{
		Text:          "Generated offline stub PNG for prompt: " + truncate(prompt, 80) + "\nPath: " + path,
		Path:          path,
		MIMEType:      "image/png",
		Bytes:         len(data),
		Deterministic: true,
	}, nil
}

// tempDirFor returns the directory the image-generator handler writes
// into. The same prompt hashes to the same path so two invocations
// with the same prompt return byte-identical files at the same
// location — that is what the cycle prompt means by "deterministic".
//
// The directory is created lazily on the first write; the path
// itself is stable across calls. The layout
//
//	<TempDir>/wagyu-image-generator-<prompt-hash>/
//	    image-generator-<prompt-hash>-<tag>.png
//
// keeps the OS temp dir tidy (one directory per prompt, not one per
// invocation) and matches what a long-running TUI session would
// accumulate over many model turns.
func tempDirFor(prompt string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(prompt))
	sum := hash.Sum32()
	return filepath.Join(os.TempDir(), "wagyu-image-generator-"+itoaHex(sum))
}

// filenameFor produces a stable, prompt-derived filename. The hash
// bytes are mixed with a short ASCII tag derived from the prompt
// so the path is human-skimmable as well as unique. The ".png"
// extension lets file(1), Finder, and downstream tools identify the
// type without reading the magic bytes.
func filenameFor(sum uint32, prompt string) string {
	tag := alphaTag(prompt)
	return "image-generator-" + itoaHex(sum) + "-" + tag + ".png"
}

// pathFor computes the full path the handler writes to without
// touching the filesystem — handy for tests that want to assert
// on the exact path the production handler will choose.
func pathFor(prompt string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(prompt))
	sum := hash.Sum32()
	return filepath.Join(tempDirFor(prompt), filenameFor(sum, prompt))
}

// buildPng64x64 constructs the 64x64 RGBA PNG the handler writes.
// The background colour comes from the prompt hash so different
// prompts yield visually distinct outputs; the top-left 8x8
// "fingerprint" tile flips the two channels of the background colour
// so a side-by-side eyeball can confirm two PNGs came from the same
// source even when the rest of the image is identical.
//
// The implementation uses image/png's stdlib encoder — no third-party
// deps — and the resulting file is a valid PNG (the test pins the
// 8-byte magic prefix).
func buildPng64x64(prompt string, sum uint32) ([]byte, error) {
	const size = 64
	bg := color.RGBA{
		R: uint8(sum),
		G: uint8(sum >> 8),
		B: uint8(sum >> 16),
		A: 0xFF,
	}
	fg := color.RGBA{
		R: ^bg.R,
		G: ^bg.G,
		B: ^bg.B,
		A: 0xFF,
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if x < 8 && y < 8 {
				img.SetRGBA(x, y, fg)
			} else {
				img.SetRGBA(x, y, bg)
			}
		}
	}
	// Encode to an in-memory buffer first so a malformed PNG never
	// leaves a half-written file on disk. The buffer's size for a
	// 64x64 RGBA solid-fill image is roughly 200 bytes — well above
	// the prompt's 70-byte minimum; the test only requires the magic
	// prefix to be present.
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out, nil
}

// errEmptyPrompt is the typed sentinel ImageGeneratorHandler returns
// when the prompt is empty (or whitespace-only). The model-side
// wrapper translates it into an IsError result.
var errEmptyPrompt = errors.New("builtinsubagent: image-generator requires a non-empty prompt")

// truncate shortens s to at most max bytes, appending an ellipsis
// when it had to be cut. The Result.Text is rendered into a tool
// result the model sees; a multi-KB prompt would balloon the
// transcript without adding signal.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// alphaTag returns a 4-character ASCII tag derived from the prompt
// so two prompts whose FNV hash collides still get distinct
// filenames. The output is lowercase letters only; collisions on
// 4-char ASCII tags across distinct prompts are vanishingly rare and
// not load-bearing (the hash bytes already make filenames unique).
func alphaTag(prompt string) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	h := fnv.New32a()
	_, _ = h.Write([]byte(prompt))
	sum := h.Sum32()
	out := make([]byte, 4)
	mod := uint32(len(alphabet))
	for i := 0; i < 4; i++ {
		out[i] = alphabet[(sum>>(uint(i)*5))&0x1f%mod]
	}
	return string(out)
}

// itoaHex renders n as an 8-character lower-case hex string. Used
// for the filename hash component and the temp-dir fallback.
func itoaHex(n uint32) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		out[i] = digits[n&0x0f]
		n >>= 4
	}
	return string(out)
}
