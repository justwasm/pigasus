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
// The read half (the platform clipboard) lives in internal/clipboard.
package cliapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/clipboard"
)

// clipboardReadMedia is the seam the tests replace: it reads the
// platform clipboard's image payload.
var clipboardReadMedia = clipboard.ReadMedia

// pasteMediaMsg carries the outcome of a clipboard media probe.
type pasteMediaMsg struct {
	media clipboard.Media
	err   error
}

// handlePasteContent is the single paste entry point.
//
// text is what the paste delivered: the clipboard's text for the ctrl+v
// key, the bracketed paste's payload for the terminal's own shortcut.
//
// A path to an image file becomes an attachment — that is what "dragging
// image files into the prompt" produces, because a terminal cannot hand
// a file to a program and inserts its path instead. Ordinary text is
// pasted as text. Only a paste that delivered nothing asks the platform
// for an image, which is the "Cmd+V with only an image copied" case.
func (m *runMode) handlePasteContent(text string) tea.Cmd {
	if m.helpMode || m.panel.kind != panelNone {
		// A panel owns the keyboard (the /rename field pastes its own
		// text); the composer's paste does not reach it.
		return m.insertPastedText(text)
	}
	if media, ok := clipboard.MediaFromPath(text); ok {
		m.attachMedia(media)
		return nil
	}
	if strings.TrimSpace(text) != "" {
		return m.insertPastedText(text)
	}
	return m.probeClipboardMedia()
}

// probeClipboardMedia asks the platform clipboard for an image. The read
// shells out (osascript, wl-paste, xclip), so it runs off the update
// goroutine.
func (m *runMode) probeClipboardMedia() tea.Cmd {
	return func() tea.Msg {
		media, err := clipboardReadMedia(clipboard.DefaultOptions())
		return pasteMediaMsg{media: media, err: err}
	}
}

// updatePasteMedia handles the probe's answer: an image is attached, and
// a clipboard with nothing to attach says so.
func (m *runMode) updatePasteMedia(msg pasteMediaMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil && len(msg.media.Data) > 0 {
		m.attachMedia(msg.media)
		return m, nil
	}
	m.inputNotice = clipboardMediaNotice(msg.err)
	return m, nil
}

// clipboardMediaNotice renders the sentence the composer shows when the
// clipboard holds no image.
//
// The reference's own wording is "No image or video found in the
// clipboard"; a missing helper names the tool to install ("install
// wl-clipboard on Wayland"), and an SSH session without X11 forwarding
// gets the reference's ssh advice.
func clipboardMediaNotice(err error) string {
	if err == nil {
		return clipboard.ErrNoMedia.Error()
	}
	return err.Error()
}

// attachMedia adds an image to the attachments of the next turn.
func (m *runMode) attachMedia(media clipboard.Media) {
	if len(media.Data) == 0 {
		return
	}
	m.pendingMedia = append(m.pendingMedia, media)
	m.inputNotice = ""
}

// mediaRows renders the attachment indicator above the composer.
func (m *runMode) mediaRows() []string {
	if len(m.pendingMedia) == 0 {
		return nil
	}
	rows := make([]string, 0, len(m.pendingMedia))
	for _, media := range m.pendingMedia {
		rows = append(rows, cDimGrayOpen+"  [Attached: "+media.Filename+"]"+cDimGrayClose)
	}
	return rows
}

// mediaImageParts turns the attachments into the transport's image
// parts. The mime type comes from the detected format, so the model
// receives the same inlineData block the reference sends.
func (m *runMode) mediaImageParts() []backend.ImagePart {
	if len(m.pendingMedia) == 0 {
		return nil
	}
	parts := make([]backend.ImagePart, 0, len(m.pendingMedia))
	for _, media := range m.pendingMedia {
		mime := media.Format.MIMEType()
		if mime == "" {
			continue
		}
		parts = append(parts, backend.ImagePart{MIMEType: mime, Data: media.Data})
	}
	return parts
}
