// Package projects — upload_fs.go: the on-disk UploadStore.
//
// Uploaded files live beside the project records they belong to:
//
//	$ANTIGRAVITY_HOME/projects/<project-id>/uploads/<file-name>
//
// Each file is written with 0600 permissions and its directory with
// 0700, so a project upload is private to the OS user — the same
// posture as the saved MCP sign-ins and the reference's own project
// data. The record files sit one level up (`<project-id>.json`), so
// the uploads directory never collides with a project's JSON file.
//
// Path safety is enforced twice: project ids are restricted to the
// store's minted id shape (lower-case hex, no separators), and file
// names are reduced to their base component before the final path is
// checked against the uploads directory. An upload therefore cannot
// escape the project's own directory even if a dashboard sends
// "../../something".
//
// Writes are serialized by one mutex. Chunks of the same file arrive
// as separate RPCs, and the store is shared by every connection; a
// single lock keeps the read-modify-write of each chunk atomic.
//
// Stdlib-only. No third-party deps.
package projects

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FSUploadStore is the production UploadStore. RootDir is the projects
// root (the same directory FSStore writes `<id>.json` files into).
type FSUploadStore struct {
	RootDir string

	mu sync.Mutex
}

// NewFSUploadStore returns an upload store rooted at rootDir.
func NewFSUploadStore(rootDir string) *FSUploadStore {
	return &FSUploadStore{RootDir: rootDir}
}

// WriteChunk writes content at offset into the project's uploads
// directory and returns the file's absolute path and total size.
//
// offset == 0 truncates the file, so the first chunk of a new upload
// replaces any previous file of the same name; later chunks carry the
// byte position they start at. A zero-length chunk still creates (or
// truncates) the file, which lets a dashboard reserve the name.
func (u *FSUploadStore) WriteChunk(projectID, fileName string, offset int64, content []byte) (string, int64, error) {
	path, err := u.resolve(projectID, fileName)
	if err != nil {
		return "", 0, err
	}
	if offset < 0 {
		return "", 0, fmt.Errorf("%w: offset must be >= 0", ErrInvalidUploadPath)
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", 0, err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return "", 0, err
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return "", 0, err
		}
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return "", 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return "", 0, err
	}
	if err := f.Close(); err != nil {
		return "", 0, err
	}
	return path, info.Size(), nil
}

// resolve turns (projectID, fileName) into an absolute path inside the
// store and refuses anything that would leave it.
func (u *FSUploadStore) resolve(projectID, fileName string) (string, error) {
	if strings.TrimSpace(u.RootDir) == "" {
		return "", fmt.Errorf("%w: empty upload root", ErrInvalidUploadPath)
	}
	if !validProjectID(projectID) {
		return "", fmt.Errorf("%w: invalid project id", ErrInvalidUploadPath)
	}
	name, err := safeUploadName(fileName)
	if err != nil {
		return "", err
	}

	dir, err := filepath.Abs(filepath.Join(u.RootDir, projectID, "uploads"))
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: file name escapes the uploads directory", ErrInvalidUploadPath)
	}
	return path, nil
}

// validProjectID accepts the store's minted ids (lower-case hex). It is
// deliberately narrower than "no separators": a project id is never
// user text, so an id with any other character is a bad request rather
// than a path to sanitize.
func validProjectID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

// safeUploadName reduces an upload's name to a single path component.
// Both separators are treated as separators (a Windows dashboard may
// send a backslash), and "." / ".." / empty names are rejected.
func safeUploadName(name string) (string, error) {
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	base := filepath.Base(name)
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return "", fmt.Errorf("%w: invalid file name", ErrInvalidUploadPath)
	}
	if strings.ContainsRune(base, 0) {
		return "", fmt.Errorf("%w: file name contains NUL", ErrInvalidUploadPath)
	}
	return base, nil
}
