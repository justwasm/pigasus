package pastemedia

import (
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
)

// InputEditor is the host's composer: the paste fold writes text into it.
type InputEditor interface {
	// InsertPastedText writes pasted text into the composer without
	// submitting it. It returns the command the insertion produced.
	InsertPastedText(text string) tea.Cmd
	// HasActiveModal reports whether a modal overlay owns the keyboard,
	// in which case the composer's paste must not reach it.
	HasActiveModal() bool
}

// AttachmentSink is the host's pending-attachment store plus the
// composer's transient notice line.
type AttachmentSink interface {
	// PendingMedia returns the attachments queued for the next turn.
	PendingMedia() []clipboard.Media
	// AddMedia appends one attachment to the next turn.
	AddMedia(media clipboard.Media)
	// ClearNotice clears the composer's transient notice line.
	ClearNotice()
	// SetNotice shows a transient notice in the composer's input slot.
	SetNotice(text string)
}

// Clipboard reads the platform clipboard's image payload. The real
// implementation shells out (osascript, wl-paste, xclip); tests replace
// it with a fixture.
type Clipboard interface {
	ReadMedia(opts clipboard.Options) (clipboard.Media, error)
}

// Model bundles the ports the paste/media logic needs. All state lives
// behind the ports; the Model itself is stateless between calls.
type Model struct {
	Input     InputEditor
	Attach    AttachmentSink
	Clipboard Clipboard
	// GOOS is the operating system the path-splitting rules target. It
	// defaults to runtime.GOOS when empty.
	GOOS string
}

// goos returns the configured GOOS or the host's.
func (md *Model) goos() string {
	if md.GOOS != "" {
		return md.GOOS
	}
	return runtime.GOOS
}

// MediaMsg carries the outcome of a clipboard media probe. It is the
// message the probe command emits and UpdatePasteMedia consumes.
type MediaMsg struct {
	Media clipboard.Media
	Err   error
}

// HandlePasteContent is the single paste entry point.
//
// text is what the paste delivered: the clipboard's text for the ctrl+v
// key, the bracketed paste's payload for the terminal's own shortcut.
//
// A path to an image file becomes an attachment — that is what "dragging
// image files into the prompt" produces, because a terminal cannot hand
// a file to a program and inserts its path instead. Ordinary text is
// pasted as text. Only a paste that delivered nothing asks the platform
// for an image, which is the "Cmd+V with only an image copied" case.
func (md *Model) HandlePasteContent(text string) tea.Cmd {
	if md.Input.HasActiveModal() {
		// A panel owns the keyboard (the /rename field pastes its own
		// text); the composer's paste does not reach it.
		return md.Input.InsertPastedText(text)
	}
	if paths := clipboard.PastedMediaPaths(text, md.goos()); len(paths) > 0 {
		for _, p := range paths {
			if media, ok := clipboard.MediaFromFile(p); ok {
				md.Attach.AddMedia(media)
			}
		}
		return nil
	}
	if media, ok := clipboard.MediaFromPath(text); ok {
		md.Attach.AddMedia(media)
		return nil
	}
	if strings.TrimSpace(text) != "" {
		return md.Input.InsertPastedText(text)
	}
	return md.ProbeClipboardMedia()
}

// ProbeClipboardMedia asks the platform clipboard for an image. The read
// shells out (osascript, wl-paste, xclip), so it runs off the update
// goroutine.
func (md *Model) ProbeClipboardMedia() tea.Cmd {
	return func() tea.Msg {
		media, err := md.Clipboard.ReadMedia(clipboard.DefaultOptions())
		return MediaMsg{Media: media, Err: err}
	}
}

// UpdatePasteMedia handles the probe's answer: an image is attached, and
// a clipboard with nothing to attach says so. It reports whether the
// media was attached.
func (md *Model) UpdatePasteMedia(msg MediaMsg) bool {
	if msg.Err == nil && len(msg.Media.Data) > 0 {
		md.Attach.AddMedia(msg.Media)
		return true
	}
	md.Attach.SetNotice(Notice(msg.Err))
	return false
}

// Notice renders the sentence the composer shows when the clipboard
// holds no image.
//
// The reference's own wording is "No image or video found in the
// clipboard"; a missing helper names the tool to install ("install
// wl-clipboard on Wayland"), and an SSH session without X11 forwarding
// gets the reference's ssh advice.
func Notice(err error) string {
	if err == nil {
		return clipboard.ErrNoMedia.Error()
	}
	return err.Error()
}

// ImageParts turns the pending attachments into the transport's image
// parts. The mime type comes from the detected format, so the model
// receives the same inlineData block the reference sends.
func (md *Model) ImageParts() []backend.ImagePart {
	pending := md.Attach.PendingMedia()
	if len(pending) == 0 {
		return nil
	}
	parts := make([]backend.ImagePart, 0, len(pending))
	for _, media := range pending {
		mime := media.Format.MIMEType()
		if mime == "" {
			continue
		}
		parts = append(parts, backend.ImagePart{MIMEType: mime, Data: media.Data})
	}
	return parts
}