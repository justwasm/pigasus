package keyring

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	keyringlib "github.com/zalando/go-keyring"
)

func TestFileStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileStore(dir)
	a := &Auth{
		RefreshToken: "rt-123",
		AccessToken:  "at-456",
		IDToken:      "id-789",
		ClientID:     "test-client",
		Account:      "test@example.com",
		ExpiresAt:    1234567890,
		Tier:         "free",
	}
	if err := fs.Store(a); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := fs.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RefreshToken != a.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", got.RefreshToken, a.RefreshToken)
	}
}

func TestFileStore_Get_NotFound(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileStore(dir)
	_, err := fs.Get()
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFileStore_Delete(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileStore(dir)
	a := &Auth{RefreshToken: "rt-1"}
	if err := fs.Store(a); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := fs.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Second delete should return ErrNotFound.
	if err := fs.Delete(); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestFileStore_Perms(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileStore(dir)
	if err := fs.Store(&Auth{RefreshToken: "rt-1"}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	path := filepath.Join(dir, "antigravity-auth.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// Recovered: 0600 perms (owner R/W only).
	if info.Mode().Perm() != 0600 {
		t.Errorf("perms = %o, want 0600", info.Mode().Perm())
	}
}

func TestRandomID(t *testing.T) {
	a := RandomID()
	b := RandomID()
	if a == "" {
		t.Error("RandomID returned empty string")
	}
	if a == b {
		t.Error("RandomID returned same value twice")
	}
	if len(a) != 32 { // 16 bytes -> 32 hex chars
		t.Errorf("len = %d, want 32", len(a))
	}
}

func TestBackend_String(t *testing.T) {
	if BackendAuto.String() != "auto" {
		t.Errorf("BackendAuto.String() = %q, want %q", BackendAuto.String(), "auto")
	}
	if BackendFile.String() != "file" {
		t.Errorf("BackendFile.String() = %q, want %q", BackendFile.String(), "file")
	}
}

func TestOSStore_MockedKeyring(t *testing.T) {
	// Use in-memory mock provider from go-keyring
	keyringlib.MockInit()

	store := NewOSStore("test-service")
	authData := &Auth{
		RefreshToken: "os-token-123",
		Account:      "dev@example.com",
	}

	// 1. Not found initially
	_, err := store.Get("user1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// 2. Set and Get
	if err := store.Set("user1", authData); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	got, err := store.Get("user1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.RefreshToken != authData.RefreshToken {
		t.Errorf("got %q, want %q", got.RefreshToken, authData.RefreshToken)
	}

	// 3. Delete
	if err := store.Delete("user1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = store.Get("user1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestHybridStore_Fallback(t *testing.T) {
	keyringlib.MockInit()
	dir := t.TempDir()
	store := NewStore(dir)

	authData := &Auth{
		RefreshToken: "hybrid-token-xyz",
	}

	if err := store.Set(DefaultUser, authData); err != nil {
		t.Fatalf("store.Set failed: %v", err)
	}

	got, err := store.Get(DefaultUser)
	if err != nil {
		t.Fatalf("store.Get failed: %v", err)
	}
	if got.RefreshToken != authData.RefreshToken {
		t.Errorf("got %q, want %q", got.RefreshToken, authData.RefreshToken)
	}

	if err := store.Delete(DefaultUser); err != nil {
		t.Fatalf("store.Delete failed: %v", err)
	}
	_, err = store.Get(DefaultUser)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
