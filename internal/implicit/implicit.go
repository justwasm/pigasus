// Package implicit writes the reference's *implicit trajectory* store — the
// per-workspace record of the shell commands the user typed outside a model
// turn (the reference's `!` mode).
//
// 【一手】 agy 1.4.7, the store on this machine:
// `~/.gemini/antigravity-cli/implicit/3a75e51c-a757-4e3c-9b6c-3f17f7c9b3f8.pb`
// (23 863 B on disk → 23 835 B of plaintext). The facts, and where each came
// from:
//
//	store dir = <gemini-dir>/implicit/     setUpImplicitTrajectorySaver →
//	                                      fs.Path_Join(baseDir, "implicit")
//	file name = <uuid>.pb                 96 files; cascade_id == the file name
//	cipher = AES-256-GCM                  proto_saver.(*DiskSaver).loadUnlocked
//	key = safeCodeiumworldKeYsecretBalloon   a literal in the binary; it
//	                                      decrypts every one of the 96 files
//	layout = nonce(12) ‖ ciphertext ‖ tag   the decryption succeeds with
//	                                      exactly that split
//	retention = 100 files                 proto_saver.WithMaxSaveCount(100)
//	scope = per workspace                 TrajectoryScope{workspace_uri,
//	                                      git_root_uri, branch_name};
//	                                      records keyed by file://<workspace>
//
// The reference does **not** put the command in the conversation: the
// conversation store holds no USER_IMPLICIT record and the brain's
// transcript.jsonl only has MODEL / SYSTEM / USER_EXPLICIT sources. The
// command lives exclusively here, and the cumulative-prompt handler later
// converts the trajectory into chat messages for the next prompt.
//
// The clone writes the same format. It does not reproduce the reference's
// key→id map — the reference mints a random UUID per trajectory and remembers
// it in memory — so the clone derives the id from the workspace URI instead
// (UUIDv5). The id is opaque either way; what matters is that the file is
// loadable by the reference's own reader.
package implicit

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// Key is the reference's AES-256-GCM key for the store — a string
	// literal in the 1.4.7 binary, and the key that decrypts every file
	// the reference has written.
	Key = "safeCodeiumworldKeYsecretBalloon"

	// MaxFiles is the reference's retention: the store keeps at most a
	// hundred files and drops the oldest beyond that.
	MaxFiles = 100

	// DirName is the store's sub-directory under the gemini directory.
	DirName = "implicit"
)

// errNoTrajectory is returned when a file holds no trajectory at all.
var errNoTrajectory = errors.New("implicit: no trajectory in the file")

// Store is the implicit trajectory store under one gemini directory.
type Store struct {
	// dir is <gemini-dir>/implicit.
	dir string
}

// Open returns the store under the given gemini directory. The directory is
// created when a step is recorded, not when the store is opened.
func Open(geminiDir string) *Store {
	return &Store{dir: filepath.Join(geminiDir, DirName)}
}

// Dir returns the store's directory.
func (s *Store) Dir() string {
	return s.dir
}

// WorkspaceURI turns an absolute workspace path into the reference's scope
// key — "file://" followed by the path.
func WorkspaceURI(path string) string {
	if strings.HasPrefix(path, "file://") {
		return path
	}
	return "file://" + path
}

// Path returns the file the workspace's trajectory lives in.
func (s *Store) Path(workspaceURI string) string {
	return filepath.Join(s.dir, trajectoryID(workspaceURI)+".pb")
}

// Record appends a step to the workspace's trajectory and writes the file
// back, encrypted.
//
// The trajectory is read, the step appended and the file rewritten: the
// reference's trajectory is long-lived — it accumulates every `!` command of
// a workspace across turns and sessions.
func (s *Store) Record(workspaceURI string, step Step) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("implicit: mkdir: %w", err)
	}
	path := s.Path(workspaceURI)

	plaintext, err := s.read(path)
	switch {
	case err == nil:
		traj, err := decodeTrajectory(plaintext)
		if err != nil {
			return fmt.Errorf("implicit: decode %s: %w", path, err)
		}
		plaintext = encodeImplicitTrajectory(appendStep(traj.raw, step), workspaceURI)
	case errors.Is(err, os.ErrNotExist):
		traj := newTrajectory(workspaceURI)
		plaintext = encodeImplicitTrajectory(appendStep(traj, step), workspaceURI)
	default:
		return err
	}

	sealed, err := seal(plaintext)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		return fmt.Errorf("implicit: write %s: %w", path, err)
	}
	return s.prune()
}

// Steps returns the run-command steps recorded for the workspace.
func (s *Store) Steps(workspaceURI string) ([]Step, error) {
	plaintext, err := s.read(s.Path(workspaceURI))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	traj, err := decodeTrajectory(plaintext)
	if err != nil {
		return nil, fmt.Errorf("implicit: decode: %w", err)
	}
	return traj.steps, nil
}

// read reads and decrypts one trajectory file.
func (s *Store) read(path string) ([]byte, error) {
	sealed, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return open(sealed)
}

// prune drops the oldest files beyond the reference's retention.
func (s *Store) prune() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("implicit: read dir: %w", err)
	}
	type aged struct {
		path string
		mod  time.Time
	}
	var files []aged
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pb") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, aged{path: filepath.Join(s.dir, entry.Name()), mod: info.ModTime()})
	}
	if len(files) <= MaxFiles {
		return nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, file := range files[:len(files)-MaxFiles] {
		_ = os.Remove(file.path)
	}
	return nil
}

// newTrajectory builds a fresh Trajectory message for a workspace.
func newTrajectory(workspaceURI string) []byte {
	id := trajectoryID(workspaceURI)

	traj := appendStringField(nil, fieldTrajectoryID, newUUIDv4())
	traj = appendVarintField(traj, fieldTrajectoryType, uint64(TrajectoryTypeUserMainline))
	traj = appendStringField(traj, fieldCascadeID, id)

	scope := appendStringField(nil, fieldScopeWorkspace, workspaceURI)
	scope = appendBytesField(scope, fieldScopeBranch, nil)

	meta := appendMessageField(nil, fieldMetaScope, scope)
	meta = appendMessageField(meta, fieldMetaTimestamps, encodeTimestamp(time.Now()))
	meta = appendStringField(meta, fieldMetaUUID, newUUIDv4())
	traj = appendMessageField(traj, fieldTrajectoryMeta, meta)
	return traj
}

// encodeImplicitTrajectory wraps a Trajectory in an ImplicitTrajectory.
func encodeImplicitTrajectory(traj []byte, workspaceURI string) []byte {
	out := appendMessageField(nil, fieldTrajectory, traj)
	// The outer scope carries the workspace URI and nothing else; the
	// reference writes exactly one field there.
	scope := appendStringField(nil, fieldScopeWorkspace, workspaceURI)
	return appendMessageField(out, fieldTrajectoryScope, scope)
}

// seal encrypts a trajectory with the reference's key and layout.
func seal(plaintext []byte) ([]byte, error) {
	gcm, err := newGCM()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("implicit: nonce: %w", err)
	}
	// Seal appends the tag to the ciphertext; the reference's layout is
	// nonce ‖ ciphertext ‖ tag.
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	return append(nonce, sealed...), nil
}

// open decrypts a trajectory file.
func open(sealed []byte) ([]byte, error) {
	gcm, err := newGCM()
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("implicit: the file is too short to be a trajectory")
	}
	nonce := sealed[:gcm.NonceSize()]
	ciphertext := sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("implicit: decrypt: %w", err)
	}
	return plaintext, nil
}

// newGCM builds the reference's AES-256-GCM cipher.
func newGCM() (cipher.AEAD, error) {
	block, err := aes.NewCipher([]byte(Key))
	if err != nil {
		return nil, fmt.Errorf("implicit: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("implicit: gcm: %w", err)
	}
	return gcm, nil
}

// namespaceUUID is the namespace the clone derives trajectory ids in. It is
// an arbitrary fixed value — the reference's ids are random, so there is
// nothing to match.
var namespaceUUID = uuid.UUID{
	0x9e, 0x6b, 0x1c, 0x44, 0x0f, 0x2a, 0x58, 0x3d,
	0xb1, 0x87, 0x64, 0xea, 0x35, 0x0c, 0x92, 0x7f,
}

// trajectoryID derives a workspace's trajectory id.
//
// The reference mints a random UUID per trajectory and keeps a key→id map in
// memory; the clone derives the id from the workspace URI instead, so the
// file name is stable across sessions without a sidecar map.
func trajectoryID(workspaceURI string) string {
	return uuid.NewSHA1(namespaceUUID, []byte(workspaceURI)).String()
}

// newUUIDv4 returns a random RFC 4122 version-4 UUID.
func newUUIDv4() string {
	u, err := uuid.NewRandom()
	if err != nil {
		// A random source failure leaves the id empty rather than
		// aborting the write; the id is opaque.
		return ""
	}
	return u.String()
}
