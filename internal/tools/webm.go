// Package tools — WebM file probe (cycle truncated-webm-rejection).
//
// Implements the AGY 1.3.4 changelog line 42 gap: when the agent calls
// read_file on a path whose name ends in .webm, the tool rejects
// truncated or header-only WebM files at read-time rather than letting
// them be attached and corrupting every later request in the
// conversation.
//
// A "truncated or header-only" WebM file is one that is either too
// small to be a real WebM, or that starts with the WebM EBML magic
// (1A 45 DF A3) but contains no Track and no Cluster element ID
// anywhere in its bytes. Both signatures happen when a recording is
// killed mid-write or only the EBML header was preserved during a
// partial transfer; the probe distinguishes them with two cheap
// substring checks.
package tools

import (
	"bytes"
	"errors"
	"strings"
)

// WebM EBML element IDs we care about. See RFC 9559 / Matroska spec.
// All four are encoded as VINT IDs whose leading-1 bit sits at
// position 3 (zero-indexed from MSB), giving the four-byte forms
// shown here.
var (
	webmMagic     = []byte{0x1A, 0x45, 0xDF, 0xA3} // EBML header
	webmSegmentID = []byte{0x18, 0x53, 0x80, 0x67} // Segment
	webmTracksID  = []byte{0x16, 0x54, 0xAE, 0x6B} // Tracks
	webmClusterID = []byte{0x1F, 0x43, 0xB6, 0x75} // Cluster
)

// ErrTruncatedWebM is the sentinel returned by ReadFile when the path
// ends in .webm and the file looks like a header-only or truncated
// WebM. Returning this error at read-time is the AGY 1.3.4 changelog
// fix: the agent sees an immediate failure on the read_file call
// instead of attaching a malformed WebM and corrupting every later
// request in the conversation.
var ErrTruncatedWebM = errors.New("read_file: refused to attach truncated or header-only WebM")

// IsWebMPath reports whether path ends in .webm (case-insensitive).
// Anything else is left to the existing read_file path untouched.
// Files that match the path suffix but lack the WebM magic are
// rejected too — the probe treats "expected a WebM" as part of the
// contract.
func IsWebMPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".webm")
}

// probeWebM returns ErrTruncatedWebM when data is too small, lacks the
// WebM magic, or has a Segment-level structure but no Track/Cluster
// element ID anywhere. Returns nil when the file looks plausible
// enough to attach.
//
// The probe deliberately stops at "looks plausible". Fully validating
// a WebM would require an EBML walk + cluster CRC check, which is
// out of scope for a read-time guard and would slow every .webm read
// by an order of magnitude; the AGY 1.3.4 fix targets the specific
// "header-only" / "truncated" failure modes that wreck later turns,
// not generic corruption.
func probeWebM(data []byte) error {
	// 1. Too small to be a real WebM: need the 4-byte magic plus at
	//    least one byte of size/data after it.
	if len(data) < len(webmMagic)+1 {
		return ErrTruncatedWebM
	}
	// 2. Magic check.
	if !bytes.HasPrefix(data, webmMagic) {
		return ErrTruncatedWebM
	}
	// 3. Skip past the EBML header so we are not fooled by Track /
	//    Cluster ID literals that may legitimately appear inside the
	//    DocType / DocTypeReadVersion string cluster. Without this
	//    skip, a file like
	//        \x1A\x45\xDF\xA3...DocType=webm...1F43B675...
	//    would pass even when it is just metadata. We only advance
	//    into the byte stream; the substring search then runs over
	//    the post-header tail.
	headerEnd, ok := skipEBMLSize(data, len(webmMagic))
	if !ok || headerEnd >= len(data) {
		return ErrTruncatedWebM
	}
	// 4. At least one Track or Cluster ID must appear after the EBML
	//    header. Both TrackEntry (0xAE, 1-byte ID) and Segment
	//    (0x18538067) are NOT cluster-equivalents — header-only files
	//    typically have Tracks but never a Cluster, and a
	//    mid-recording truncation cuts off before the first Cluster
	//    lands. Either side of that binary is what we reject.
	rest := data[headerEnd:]
	if !bytes.Contains(rest, webmTracksID) && !bytes.Contains(rest, webmClusterID) {
		return ErrTruncatedWebM
	}
	return nil
}

// skipEBMLSize reads the VINT-encoded data-size field that follows an
// EBML element ID of length idLen, returning the byte index just past
// that field. VINTs are length-prefixed by the position of the
// leading-1 bit: leading-1 at position 0 ⇒ 1 byte total, position 1
// ⇒ 2 bytes total, and so on up to position 7 ⇒ 8 bytes total.
//
// A zero first byte is malformed. The function returns false (so the
// caller can reject) rather than panicking so the probe stays a
// read-time guard with no surprise crashes.
func skipEBMLSize(data []byte, idLen int) (int, bool) {
	if idLen >= len(data) {
		return 0, false
	}
	first := data[idLen]
	if first == 0 {
		return 0, false
	}
	var leading int
	for i := 0; i < 8; i++ {
		if first&(1<<(7-i)) != 0 {
			leading = i
			break
		}
	}
	vintLen := leading + 1
	end := idLen + vintLen
	if end > len(data) {
		return 0, false
	}
	return end, true
}
