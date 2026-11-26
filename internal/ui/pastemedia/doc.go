// Package pastemedia owns the TUI's paste-and-attach half: what a paste
// means (text, a dropped image path, or an image on the clipboard), the
// clipboard probe that shells out to the platform reader, and the
// conversion of pending attachments into the transport's image parts.
//
// # Boundary
//
// The package is a vertical slice carved out of internal/tui's
// tui_attach_media.go. It knows nothing about the RootModel, the
// transcript, or the turn engine. Everything it needs from the host
// arrives through three narrow ports:
//
//   - InputEditor: write text into the composer (the host's paste fold).
//   - AttachmentSink: read, append, and clear the pending attachments,
//     and show the "no image in the clipboard" notice.
//   - Clipboard: read the platform clipboard's image payload.
//
// The clipboard's text/file probing (PastedMediaPaths, MediaFromPath,
// MediaFromFile) is pure and stays in internal/features/tools/clipboard;
// this package only orchestrates it against the ports.
//
// # Model
//
// A Model bundles the three ports and exposes the paste entry point
// (HandlePasteContent), the probe command (ProbeClipboardMedia), the
// probe's answer (UpdatePasteMedia), and the image-part projection
// (ImageParts). The RootModel keeps thin method wrappers that build a
// Model from its own state and delegate, so the host's method signatures
// are unchanged.
package pastemedia