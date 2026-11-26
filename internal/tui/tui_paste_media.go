package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/pastemedia"
)

// Package cliapp — tui_paste_media.go: the paste key's image half.
//
// agy 1.4.9 (upstream release notes):
//
//	"Added image pasting through your terminal's own paste shortcut, such
//	 as Cmd+V or Ctrl+Shift+V with only an image copied, and by dragging
//	 image files into the prompt, which attaches them instead of
//	 inserting their paths; image paste now also works over SSH with X11
//	 or Wayland forwarding, and the CLI's paste key now says
//	 `No image or video found in the clipboard` when there is nothing to
//	 attach and names the clipboard tool to install, such as
//	 `wl-clipboard` on Wayland, when one is missing."
//
// What this file does:
//
//   - the paste key (ctrl+v) and the terminal's own bracketed paste both
//     land in handlePasteContent, which decides what the paste means:
//     an image on the clipboard becomes an attachment, a path to an image
//     file becomes an attachment, everything else stays text;
//   - the attachment lives in pendingMedia until the next turn is sent,
//     and then rides along with the user message as an image part (the
//     transports already carry images: internal/backend/client.go's
//     Message.Images);
//   - when the clipboard holds nothing to attach, the composer says the
//     reference's own sentence — "No image or video found in the
//     clipboard" — or the missing-tool sentence the platform reader
//     produced.
//
// The read half (the platform clipboard) lives in internal/features/tools/clipboard.
// The indicator above the composer is the ui tier's own attachment rows
// (internal/ui/rows.AttachmentRows); this file owns the attachment, not
// the shape of the row that shows it.

// pasteMediaMsg carries the outcome of a clipboard media probe. It is an
// alias of the pastemedia module's message so the host's Update switch
// keeps matching on the same type.
type pasteMediaMsg = pastemedia.MediaMsg

// pasteMediaModel builds a pastemedia.Model bound to the host's state.
// Every adapter reads the model at call time, so a Model built before the
// composer exists still writes to the live one.
func (m *RootModel) pasteMediaModel() *pastemedia.Model {
	return &pastemedia.Model{
		Input:     pasteMediaInputPort{m: m},
		Attach:    pasteMediaAttachPort{m: m},
		Clipboard: clipboardReadMediaPort{},
	}
}

// clipboardReadMedia is the seam the tests replace: it reads the
// platform clipboard's image payload.
var clipboardReadMedia = clipboard.ReadMedia

// clipboardReadMediaPort adapts the package-level seam to the
// pastemedia.Clipboard port.
type clipboardReadMediaPort struct{}

// ReadMedia reads the platform clipboard's image payload.
func (clipboardReadMediaPort) ReadMedia(opts clipboard.Options) (clipboard.Media, error) {
	return clipboardReadMedia(opts)
}

// pasteMediaInputPort presents the host composer to the pastemedia module.
type pasteMediaInputPort struct{ m *RootModel }

// InsertPastedText writes pasted text into the composer.
func (p pasteMediaInputPort) InsertPastedText(text string) tea.Cmd {
	return p.m.insertPastedText(text)
}

// HasActiveModal reports whether a modal overlay owns the keyboard.
func (p pasteMediaInputPort) HasActiveModal() bool { return p.m.hasActiveModal() }

// pasteMediaAttachPort presents the host's pending attachments and notice
// line to the pastemedia module.
type pasteMediaAttachPort struct{ m *RootModel }

// PendingMedia returns the attachments queued for the next turn.
func (p pasteMediaAttachPort) PendingMedia() []clipboard.Media {
	return p.m.prompt.pendingMedia
}

// AddMedia appends one attachment to the next turn and clears the notice.
func (p pasteMediaAttachPort) AddMedia(media clipboard.Media) {
	if len(media.Data) == 0 {
		return
	}
	p.m.prompt.pendingMedia = append(p.m.prompt.pendingMedia, media)
	p.m.viewport.inputNotice = ""
}

// ClearNotice clears the composer's transient notice line.
func (p pasteMediaAttachPort) ClearNotice() { p.m.viewport.inputNotice = "" }

// SetNotice shows a transient notice in the composer's input slot.
func (p pasteMediaAttachPort) SetNotice(text string) { p.m.viewport.inputNotice = text }

// handlePasteContent is the single paste entry point. It delegates to the
// pastemedia module.
func (m *RootModel) handlePasteContent(text string) tea.Cmd {
	return m.pasteMediaModel().HandlePasteContent(text)
}

// updatePasteMedia handles the probe's answer: an image is attached, and
// a clipboard with nothing to attach says so.
func (m *RootModel) updatePasteMedia(msg pasteMediaMsg) (tea.Model, tea.Cmd) {
	m.pasteMediaModel().UpdatePasteMedia(msg)
	return m, nil
}

// mediaImageParts turns the attachments into the transport's image parts.
func (m *RootModel) mediaImageParts() []backend.ImagePart {
	return m.pasteMediaModel().ImageParts()
}
