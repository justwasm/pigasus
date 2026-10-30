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
func Write(text string) error {
	return Writer(text)
}

// defaultRead reads the system clipboard through atotto/clipboard —
// the library the reference binary vendors. 【反编译】 the reference's
// own recovered symbols are
// google3/third_party/golang/github_com/atotto/clipboard/v/v0/clipboard.readAll
// and …clipboard.getPasteCommand, i.e. the reference shells out to
// the same utilities this now calls.
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

// defaultWrite writes to the system clipboard.
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
