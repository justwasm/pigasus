// Package clipboard reads and writes the system clipboard.
//
// The reference binary vendors github.com/atotto/clipboard — the
// recovered symbols are
// google3/third_party/golang/github_com/atotto/clipboard/v/v0/clipboard.readAll
// and …clipboard.getPasteCommand, …clipboard.writeAll and
// …clipboard.getCopyCommand. 【反编译】 That library shells out to
// pbcopy/pbpaste on darwin and to xclip/xsel/wl-copy on Linux, so the
// utilities this package calls are the reference's own.
package clipboard

import (
	"os"

	atotto "github.com/atotto/clipboard"
)

// Reader is the seam tests can override to mock clipboard reads.
var Reader = defaultRead

// Writer is the seam tests can override to mock clipboard writes.
var Writer = defaultWrite

// Read reads text from the system clipboard.
func Read() string {
	return Reader()
}

// Write writes text to the system clipboard.
//
// The write goes through atotto/clipboard — the library the reference
// binary vendors (see the package doc for the recovered symbols).
func Write(text string) error {
	return Writer(text)
}

// defaultRead reads the system clipboard through atotto/clipboard —
// the library the reference binary vendors (see the package doc for
// the recovered symbols).
//
// A clipboard that cannot be read is an empty clipboard: the read
// half of this API has no error to return, so a missing or failing
// utility means an empty string — as before.
func defaultRead() string {
	out, err := atotto.ReadAll()
	if err != nil {
		return ""
	}
	return out
}

// defaultWrite writes to the system clipboard through atotto/clipboard
// — the library the reference binary vendors (see the package doc for
// the recovered symbols).
//
// The OSC 52 escape goes first: a terminal that understands it can
// carry the text across an SSH hop, where no local clipboard utility
// exists at all. Only when the terminal cannot take it does the write
// fall through to the system clipboard.
func defaultWrite(text string) error {
	if SupportsOSC52() && WriteOSC52(os.Stderr, text) {
		return nil
	}
	return atotto.WriteAll(text)
}
