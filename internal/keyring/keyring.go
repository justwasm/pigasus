// Package keyring implements the recovered keyring abstraction from
// google3/third_party/jetski/cli/backend/auth/auth.
//
// Upstream uses github.com/zalando/go-keyring to interact with the OS keyring:
//   - Linux: D-Bus Secret Service (libsecret) / fallback
//   - macOS: Keychain Services
//   - Windows: Credential Manager
//
// When the OS keyring is unavailable (e.g. headless CI, container sandbox),
// the manager falls back to a file-based store (FileStore).
package keyring

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/renameio/v2"
	"github.com/zalando/go-keyring"
)

// Service name used in the OS keyring.
const Service = "antigravity-cli"

// DefaultUser is the key/user identity stored in the OS keyring.
const DefaultUser = "auth"

// Backend is the recovered keyring backend enum.
type Backend int

const (
	BackendAuto Backend = iota
	BackendSecretService // Linux D-Bus
	BackendKeychain      // macOS
	BackendCredManager   // Windows
	BackendFile          // fallback
)

func (b Backend) String() string {
	switch b {
	case BackendSecretService:
		return "SecretService"
	case BackendKeychain:
		return "Keychain"
	case BackendCredManager:
		return "CredentialManager"
	case BackendFile:
		return "file"
	}
	return "auto"
}

// Auth is the recovered auth blob stored in the keyring.
type Auth struct {
	RefreshToken string `json:"refresh_token"`
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	ClientID     string `json:"client_id"`
	Account      string `json:"account"`
	ExpiresAt    int64  `json:"expires_at"`
	Tier         string `json:"tier"`
}

var ErrNotFound = errors.New("keyring: not found")

// OSStore provides methods backed by github.com/zalando/go-keyring.
type OSStore struct {
	service string
}

// NewOSStore creates an OSStore with the default or specified service name.
func NewOSStore(service ...string) *OSStore {
	svc := Service
	if len(service) > 0 && service[0] != "" {
		svc = service[0]
	}
	return &OSStore{service: svc}
}

// Set stores the auth blob under the given user key in the OS keyring.
func (s *OSStore) Set(user string, a *Auth) error {
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return keyring.Set(s.service, user, string(data))
}

// Get retrieves the auth blob for the given user key from the OS keyring.
func (s *OSStore) Get(user string) (*Auth, error) {
	val, err := keyring.Get(s.service, user)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var a Auth
	if err := json.Unmarshal([]byte(val), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// Delete removes the auth blob for the given user key from the OS keyring.
func (s *OSStore) Delete(user string) error {
	err := keyring.Delete(s.service, user)
	if err != nil && errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// Store holds the hybrid keyring manager that tries OS keyring first,
// and transparently falls back to FileStore if the OS keyring is unavailable.
type Store struct {
	osStore   *OSStore
	fileStore *FileStore
}

// NewStore returns a unified Store that prefers OS keyring and falls back to dir.
func NewStore(dir string) *Store {
	return &Store{
		osStore:   NewOSStore(),
		fileStore: NewFileStore(dir),
	}
}

// Set stores the auth blob into the OS keyring, falling back to disk if unavailable.
func (s *Store) Set(user string, a *Auth) error {
	if err := s.osStore.Set(user, a); err == nil {
		return nil
	}
	return s.fileStore.Store(a)
}

// Get attempts to read from OS keyring, falling back to disk if not found or unavailable.
func (s *Store) Get(user string) (*Auth, error) {
	if a, err := s.osStore.Get(user); err == nil {
		return a, nil
	}
	return s.fileStore.Get()
}

// Delete removes the auth blob from both the OS keyring and fallback store.
func (s *Store) Delete(user string) error {
	osErr := s.osStore.Delete(user)
	fileErr := s.fileStore.Delete()
	if osErr == nil || fileErr == nil {
		return nil
	}
	if errors.Is(osErr, ErrNotFound) && errors.Is(fileErr, ErrNotFound) {
		return ErrNotFound
	}
	return osErr
}

// FileStore is the recovered file-based fallback used when the
// OS keyring is unavailable (e.g. inside a container/sandbox).
type FileStore struct {
	mu   sync.Mutex
	path string
}

// NewFileStore returns a FileStore rooted at the given directory.
func NewFileStore(dir string) *FileStore {
	return &FileStore{path: filepath.Join(dir, "antigravity-auth.json")}
}

// Store persists the auth blob to disk with 0600 perms.
func (f *FileStore) Store(a *Auth) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(f.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return renameio.WriteFile(f.path, data, 0600)
}

// Get returns the persisted auth blob or ErrNotFound.
func (f *FileStore) Get() (*Auth, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var a Auth
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// Delete removes the auth blob from disk.
func (f *FileStore) Delete() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := os.Remove(f.path)
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}

// RandomID generates a random 16-byte hex ID, used for auth state IDs.
func RandomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
