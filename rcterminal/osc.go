// osc.go: the OSC sequences a shell reports.
//
// A shell tells its terminal about its title and its working
// directory with OSC sequences:
//
//	ESC ] 0 ; <title> BEL        — the window title
//	ESC ] 2 ; <title> BEL        — the window title (the other name)
//	ESC ] 7 ; file://host/path ESC\  — the working directory
//
// The reference reports both as their own stream frames (the
// StreamTerminalOutputResponse oneof has `title` and `cwd`
// members), so the scanner looks for them in the pty's byte
// stream. The bytes themselves are still forwarded: the terminal
// in the browser interprets the sequences itself, and dropping them
// would lose the shell's own rendering.
package rcterminal

import (
	"bytes"
	"net/url"
	"strings"
)

// oscScanner finds the OSC sequences in a pty byte stream. It keeps
// the tail of a chunk that may hold a half-written sequence, so a
// sequence split across two reads is still found.
type oscScanner struct {
	pending []byte
}

// scan reports the title and the working directory a chunk carries.
func (s *oscScanner) scan(chunk []byte) (title, cwd string) {
	buf := append(s.pending, chunk...)
	s.pending = nil
	for {
		start := bytes.Index(buf, []byte("\x1b]"))
		if start < 0 {
			s.keepTail(buf)
			return title, cwd
		}
		body, rest, ok := cutOSC(buf[start+2:])
		if !ok {
			// The sequence is not complete yet.
			s.pending = append([]byte(nil), buf[start:]...)
			return title, cwd
		}
		buf = rest
		switch {
		case strings.HasPrefix(body, "0;"):
			title = body[2:]
		case strings.HasPrefix(body, "2;"):
			title = body[2:]
		case strings.HasPrefix(body, "7;"):
			cwd = pathFromFileURL(body[2:])
		}
	}
}

// keepTail remembers the bytes that may begin a sequence.
func (s *oscScanner) keepTail(buf []byte) {
	if len(buf) == 0 {
		return
	}
	if buf[len(buf)-1] == 0x1b {
		s.pending = append([]byte(nil), buf[len(buf)-1:]...)
		return
	}
	if len(buf) >= 2 && buf[len(buf)-2] == 0x1b && buf[len(buf)-1] == ']' {
		s.pending = append([]byte(nil), buf[len(buf)-2:]...)
	}
}

// cutOSC cuts the body of an OSC sequence off the front of buf. The
// sequence ends with BEL or with ST (ESC \). ok is false when the
// sequence is not complete.
func cutOSC(buf []byte) (body string, rest []byte, ok bool) {
	bel := bytes.IndexByte(buf, 0x07)
	st := bytes.Index(buf, []byte("\x1b\\"))
	switch {
	case bel >= 0 && (st < 0 || bel < st):
		return string(buf[:bel]), buf[bel+1:], true
	case st >= 0:
		return string(buf[:st]), buf[st+2:], true
	default:
		return "", buf, false
	}
}

// pathFromFileURL turns the OSC 7 payload into a path.
func pathFromFileURL(payload string) string {
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "file://") {
		return trimmed
	}
	path := strings.TrimPrefix(trimmed, "file://")
	if idx := strings.IndexByte(path, '/'); idx > 0 {
		// Drop the host part.
		path = path[idx:]
	}
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	return path
}
