package files

import (
	"io"
	"os"
)

// LSSeams supplies the language-server file domain's only effect: filesystem
// access. It is separate from Seams.Provider because the dashboard's scoped,
// read-only contract and the language server's read/write contract differ.
// A nil FS fails closed; the host, not this package's router, chooses an adapter.
type LSSeams struct {
	FS LanguageServerFS
}

// LanguageServerFS is the filesystem port used by the five language-server file
// methods. Implementations retain responsibility for any access policy. The
// domain never falls back to the process filesystem when an operation fails.
type LanguageServerFS interface {
	Stat(path string) (os.FileInfo, error)
	ReadDir(path string) ([]os.DirEntry, error)
	Open(path string) (LSFile, error)
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(path string, data []byte, perm os.FileMode) error
}

// LSFile preserves the original open/stat/seek/read/close lifecycle, including
// checking the opened file's size rather than statting a path before opening it.
type LSFile interface {
	io.Reader
	io.Seeker
	io.Closer
	Stat() (os.FileInfo, error)
}

// OSLanguageServerFS preserves the language server's existing process-filesystem
// behavior. It deliberately does not replace or relax the dashboard Provider's
// scope policy. Hosts can instead supply a policy-enforcing implementation.
type OSLanguageServerFS struct{}

func (OSLanguageServerFS) Stat(path string) (os.FileInfo, error)      { return os.Stat(path) }
func (OSLanguageServerFS) ReadDir(path string) ([]os.DirEntry, error) { return os.ReadDir(path) }
func (OSLanguageServerFS) Open(path string) (LSFile, error)           { return os.Open(path) }
func (OSLanguageServerFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
func (OSLanguageServerFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
