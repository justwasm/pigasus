# Cycle webp-from-clipboard-fix — AGY 1.2.13 WebP clipboard paste

## Goal

AGY 1.2.13:

> "Fixed pasted WebP images from the clipboard being rejected even when
> the user has WebP image support enabled; the image now passes through
> the same scaling pipeline as PNG and JPEG."

The fix lands the byte-level glue between a clipboard paste event and
the image-scaling pipeline that the previous
`feature/webp-image-scaling-fix` cycle parked in `internal/imagery`.

Before the fix, the CLI rejected pasted WebP bytes with "unsupported
image format" even when the user's terminal reported WebP support;
only PNG and JPEG were routed through `imagery.ScaleIfOversized`. After
the fix, the WebP branch is gated only on a `WebPEnabled` knob (default
true; the upstream CLI's default) and the bytes are handed to
`imagery.ScaleIfOversized` for the same decode → resize → re-encode
treatment PNG and JPEG have always received.

## Scope

1. **`internal/clipboard/paste.go`** — `internal/clipboard` package.
   The package is the byte-level glue; the terminal side (read
   pbpaste, parse OSC 52, react to kitty's image paste, etc.) is an
   `ImageSource` interface; the model side (build the attachment
   block, decide where the bytes land in the prompt) is the caller's
   job.

   - `PasteImage{Format, Data, Filename, Scaled}` is the result of
     inspecting a clipboard payload. `Format` is one of
     `imagery.{FormatPNG, FormatJPEG, FormatWebP, FormatUnknown}`;
     `Scaled` is true iff `imagery.ScaleIfOversized` actually shrank
     the image.
   - `Options{WebPEnabled, Counter}` configures ReadImage and
     AcceptPaste. `WebPEnabled` mirrors the upstream CLI's
     `imageSupport.webp` setting and defaults to `true`; the counter
     keys the suggested `Filename` (`pasted-image-1.png`,
     `pasted-image-2.webp`, …).
   - `ImageSource.Read() ([]byte, error)` is the read side. Platform
     implementations (`pbpaste`, `wl-paste -t image/*`, OSC 52,
     kitty graphics protocol "T" chunks) plug in here without the
     package taking an OS dependency. `BytesSource(data)` is the
     trivial in-memory implementation used by tests and by callers
     that already have the payload in memory.
   - `ReadImage(src, opts)` runs the source → bytes → scale pipeline
     end-to-end and returns the wrapped errors:
     - `ErrSourceUnavailable` for nil/failing sources
     - `ErrEmptyPayload` for zero-length payloads (the "fails
       gracefully" path)
     - `ErrWebPDisabled` for WebP with the gate closed
     - `imagery.ErrDecodeFailed` for recognised-format-but-malformed
       bodies
   - `AcceptPaste(data, opts)` is the pure, source-free half; same
     contract, no source indirection.
   - Sentinel errors are wrapped at the call site so `errors.Is`
     survives across the package boundary.

2. **Tests — `internal/clipboard/paste_test.go`** — 13 test
   functions (10 canonical + 3 brief-pattern aliases), 18 sub-cases
   total. The brief's named five cases are all covered:

   | Brief case | Test |
   |---|---|
   | WebP from clipboard accepted | `TestAcceptPaste_WebPFromClipboardAccepted` |
   | PNG still works | `TestAcceptPaste_PNGStillWorks` (small / oversized / JPEG regression) |
   | Oversized WebP scaled | `TestAcceptPaste_OversizedWebPScaledDown` |
   | Non-image rejected | `TestAcceptPaste_NonImageRejected` (4 sub-cases) |
   | No clipboard data fails gracefully | `TestAcceptPaste_EmptyPasteFailsGracefully` (5 sub-cases incl. nil source, failing source) |

   Plus `TestAcceptPaste_WebPDisabledRespected` (the gate works for
   PNG/JPEG even when WebP is closed), `TestAcceptPaste_WebPRejectedByDecoder`
   (decoder rejection bubbles up as `imagery.ErrDecodeFailed`),
   `TestAcceptPaste_FilenameSuggestion` (counter math + clamp),
   `TestReadImage_BytesSourceRoundTrip` (WebP/PNG through ReadImage),
   `TestDefaultOptions` (default sanity), and the three
   `TestPaste*`/`TestClipboard*`/`TestPasteImage*` aliases so the
   brief's targeted-run pattern (`TestWebP|TestPaste|TestClipboard|TestImage`)
   lands a match in the clipboard package.

   ```
   $ go test -vet=off -count=1 -v ./internal/clipboard/
   === RUN   TestAcceptPaste_WebPFromClipboardAccepted
   --- PASS: TestAcceptPaste_WebPFromClipboardAccepted (0.00s)
   === RUN   TestAcceptPaste_PNGStillWorks
   --- PASS: TestAcceptPaste_PNGStillWorks (0.37s)
       --- PASS: TestAcceptPaste_PNGStillWorks/small_PNG_passes_through_unchanged
       --- PASS: TestAcceptPaste_PNGStillWorks/oversized_PNG_is_scaled_down
       --- PASS: TestAcceptPaste_PNGStillWorks/JPEG_still_works_(regression_coverage)
   === RUN   TestAcceptPaste_OversizedWebPScaledDown
   --- PASS: TestAcceptPaste_OversizedWebPScaledDown (0.20s)
   === RUN   TestAcceptPaste_NonImageRejected
   --- PASS: TestAcceptPaste_NonImageRejected
       --- PASS: TestAcceptPaste_NonImageRejected/plain_text
       --- PASS: TestAcceptPaste_NonImageRejected/short_buffer_(3_bytes)
       --- PASS: TestAcceptPaste_NonImageRejected/RIFF_with_non-WEBP_form_type
       --- PASS: TestAcceptPaste_NonImageRejected/random_binary_blob
   === RUN   TestAcceptPaste_EmptyPasteFailsGracefully
   --- PASS: TestAcceptPaste_EmptyPasteFailsGracefully
       --- PASS: TestAcceptPaste_EmptyPasteFailsGracefully/AcceptPaste(nil)
       --- PASS: TestAcceptPaste_EmptyPasteFailsGracefully/AcceptPaste([]byte{})
       --- PASS: TestAcceptPaste_EmptyPasteFailsGracefully/ReadImage(BytesSource(nil))
       --- PASS: TestAcceptPaste_EmptyPasteFailsGracefully/ReadImage(nil_source)
       --- PASS: TestAcceptPaste_EmptyPasteFailsGracefully/ReadImage(failing_source)
   === RUN   TestAcceptPaste_WebPDisabledRespected
   --- PASS: TestAcceptPaste_WebPDisabledRespected
       --- PASS: TestAcceptPaste_WebPDisabledRespected/WebP_rejected_when_gate_is_closed
       --- PASS: TestAcceptPaste_WebPDisabledRespected/PNG_still_accepted_when_WebP_gate_is_closed
       --- PASS: TestAcceptPaste_WebPDisabledRespected/JPEG_still_accepted_when_WebP_gate_is_closed
   === RUN   TestAcceptPaste_WebPRejectedByDecoder
   --- PASS: TestAcceptPaste_WebPRejectedByDecoder (0.00s)
   === RUN   TestAcceptPaste_FilenameSuggestion
   --- PASS: TestAcceptPaste_FilenameSuggestion
       --- PASS: TestAcceptPaste_FilenameSuggestion/counter_increments_by_format
       --- PASS: TestAcceptPaste_FilenameSuggestion/counter_clamps_to_1_when_zero_or_negative
   === RUN   TestReadImage_BytesSourceRoundTrip
   --- PASS: TestReadImage_BytesSourceRoundTrip
       --- PASS: TestReadImage_BytesSourceRoundTrip/WebP_through_ReadImage
       --- PASS: TestReadImage_BytesSourceRoundTrip/PNG_through_ReadImage
   === RUN   TestDefaultOptions
   --- PASS: TestDefaultOptions (0.00s)
   === RUN   TestPasteWebPFromClipboardAccepted
   --- PASS: TestPasteWebPFromClipboardAccepted (0.00s)
   === RUN   TestPasteImagePNGStillWorks
   --- PASS: TestPasteImagePNGStillWorks (0.35s)
   === RUN   TestClipboardEmptyPasteFailsGracefully
   --- PASS: TestClipboardEmptyPasteFailsGracefully (0.00s)
   PASS
   ok  	github.com/chadgpt/openagy/internal/clipboard	1.463s
   ```

## What this cycle does NOT do

- It does not wire the package into `cmd/wagyu`'s TUI paste handler.
  The clone's `alt+v` / `ctrl+v` binding currently has no
  image-paste flow; the help-shortcut copy ("Paste image/video")
  predates any implementation. A future cycle is the place to
  add a `bubbletea` `tea.KeyMsg` arm that resolves the bytes and
  calls `clipboard.ReadImage`.
- It does not wire the `WebPEnabled` knob into the settings
  manager. `clipboard.DefaultOptions()` returns `true`; the
  setting's "user has explicitly turned WebP off" path is
  unreachable from the clone today. The flag's place in the API is
  documented and the gate is unit-tested.
- It does not add a real `ImageSource` implementation. The
  package exposes the interface; the platform side lives at
  `cmd/wagyu/` (or in a future `internal/clipboard/darwin.go`
  /`linux.go` if a future cycle chooses to put it here). `BytesSource`
  is the in-memory implementation for tests.

## Hard constraints respected

- **`internal/convstore/sqlite.go` untouched** — verified by `git
  diff main feature/webp-from-clipboard-fix -- internal/convstore/sqlite.go`
  returning no diff.
- **`gofmt -l internal/clipboard/` empty** — both new files are
  gofmt-clean.
- **Pre-existing not-clean files NOT reformatted wholesale** — the
  cycle touches only the two new files in `internal/clipboard/` plus
  `go.mod` (no change — `internal/imagery` is already a direct dep
  from the prior cycle). No other files modified.
- **`go build -o /tmp/wagyu-wfc ./cmd/wagyu` exits 0** — 73 MB
  Mach-O arm64 binary on this machine; matches the previous
  cycle's reported size.
- **Targeted test run passes** — `go test -vet=off -count=1 -run
  'TestWebP|TestPaste|TestClipboard|TestImage' ./cmd/wagyu/
  ./internal/.../` lands `ok internal/clipboard` plus
  "[no tests to run]" everywhere else.
- **No new third-party deps** — `internal/clipboard` imports only
  the standard library and the in-repo `internal/imagery`. The
  `github.com/atotto/clipboard` indirect dep is *not* promoted; the
  clone doesn't use it yet, and the cycle's `ImageSource` interface
  lets a future cycle plug it in without taking an OS dependency in
  this package.
- **Branch NOT pushed** — local commit on
  `feature/webp-from-clipboard-fix` only; no `git push` issued.

## Files

| Path | Lines | Status |
|---|---|---|
| `internal/clipboard/paste.go` | 227 | new |
| `internal/clipboard/paste_test.go` | 429 | new |
| `docs/cycles/webp-from-clipboard-fix/deliverable.md` | this file | new |

## 硬约束 / 存疑 / limitations

- **Branch base.** The brief says "Branch from HEAD" but the
  imagery package from the previous cycle only exists on
  `feature/webp-image-scaling-fix`. Branching from main would have
  meant re-deriving the imagery package's three files
  (`imagery.go`, `imagery_test.go`, `testdata/{small,large}.webp`),
  which is a much larger surface than this gap. The branch
  base is `feature/webp-image-scaling-fix` (the imagery-package
  tip) and the previous cycle's deliverable is acknowledged as the
  upstream of `internal/imagery`.
- **No `ImageSource` implementation.** The interface is exposed
  but no platform source is provided. The bubbletea TUI doesn't
  yet have an image-paste keybinding handler; adding one is a
  separate, larger cycle. `BytesSource` is the trivial
  implementation tests use.
- **Settings wiring deferred.** `WebPEnabled` defaults to `true`;
  the `/config` UI plumbing that lets a user toggle it is not
  in this cycle's scope. The setting's place in the API and its
  semantic ("only WebP is gated; PNG/JPEG always pass") is
  pinned in `TestAcceptPaste_WebPDisabledRespected`.
- **WebP decoder limit.** `internal/imagery` re-encodes WebP as
  PNG because `golang.org/x/image/webp` is decoder-only; this is
  the same trade-off the upstream CLI made and the prior
  `webp-image-scaling-fix` cycle documented. Format identity is
  not preserved end-to-end for WebP, but the byte stream is a
  valid raster under `imagery.MaxSide`, which is what the model's
  image limit checks for.
- **No end-to-end UI test.** The clone's TUI does not currently
  surface a paste-image UX (the help-shortcut copy is the only
  visible hint), so there is no UI-level flow to exercise. The
  `internal/clipboard` package is unit-tested at the package
  boundary; a future cycle can wire `bubbletea` paste events to
  `clipboard.ReadImage` and add integration coverage.
- **No `git push` issued.** Per the brief's "Branch must NOT be
  pushed" rule; the commit hash is local-only.
