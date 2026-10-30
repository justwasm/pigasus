package core

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// fakeToken builds a token whose id_token carries an email claim, the
// way Google's id_token does. The token is not signed — the clone only
// reads the claim, exactly like the reference's banner does.
func fakeToken(t *testing.T, email, access, refresh string) *auth.AntigravityToken {
	t.Helper()
	claims, err := json.Marshal(map[string]any{"email": email, "sub": "1"})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	id := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
	return &auth.AntigravityToken{
		Token: auth.AntigravityAccessToken{
			AccessToken:  access,
			TokenType:    "Bearer",
			RefreshToken: refresh,
			Expiry:       time.Now().Add(time.Hour).Format(time.RFC3339),
		},
		AuthMethod: "consumer",
		IDToken:    id,
	}
}

// isolate points the account registry and the shared credential paths
// at a temp directory, so a test never touches the real keychain or the
// real ~/.config.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(home, "accounts.json"))
	t.Setenv("OPENAGY_DISABLE_KEYCHAIN", "1")
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")
	return home
}

func TestPathHonoursTheEnvOverride(t *testing.T) {
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", "/tmp/override.json")
	if got := Path(); got != "/tmp/override.json" {
		t.Fatalf("Path() = %q, want the override", got)
	}
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := Path(); got != filepath.Join("/tmp/xdg", "openagy", "accounts.json") {
		t.Fatalf("Path() = %q, want the XDG path", got)
	}
}

func TestLoadMissingFileIsAnEmptyStore(t *testing.T) {
	isolate(t)
	st, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(st.Accounts) != 0 {
		t.Fatalf("accounts = %d, want 0", len(st.Accounts))
	}
	if st.Version != Version {
		t.Fatalf("version = %d, want %d", st.Version, Version)
	}
}

func TestSaveIsPrivateAndRoundTrips(t *testing.T) {
	home := isolate(t)
	st := &Store{}
	st.Put(Account{Email: "a@example.com", Token: fakeToken(t, "a@example.com", "at-a", "rt-a")})
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %v, want 0600", perm)
	}
	back, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	acc, ok := back.Find("a@example.com")
	if !ok {
		t.Fatalf("the account did not survive the round trip: %v", back.Emails())
	}
	if acc.Token == nil || acc.Token.Token.AccessToken != "at-a" {
		t.Fatalf("token = %+v, want the stored access token", acc.Token)
	}
	if _, err := os.Stat(filepath.Join(home, "accounts.json")); err != nil {
		t.Fatalf("the registry is not in the temp home: %v", err)
	}
}

func TestPutKeepsAddedAtAndMatchesCaseInsensitively(t *testing.T) {
	st := &Store{}
	first := time.Now().Add(-time.Hour).Truncate(time.Second)
	st.Put(Account{Email: "a@example.com", AddedAt: first})
	st.Put(Account{Email: "A@Example.com", AddedAt: time.Now()})
	if len(st.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(st.Accounts))
	}
	acc, ok := st.Find("a@EXAMPLE.com")
	if !ok {
		t.Fatal("Find did not match case-insensitively")
	}
	if !acc.AddedAt.Equal(first) {
		t.Fatalf("AddedAt = %v, want the original %v", acc.AddedAt, first)
	}
}

func TestRemoveDropsTheAccountAndClearsActive(t *testing.T) {
	st := &Store{Active: "a@example.com"}
	st.Put(Account{Email: "a@example.com"})
	st.Put(Account{Email: "b@example.com"})
	if !st.Remove("A@example.com") {
		t.Fatal("Remove reported the account was missing")
	}
	if len(st.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(st.Accounts))
	}
	if st.Active != "" {
		t.Fatalf("active = %q, want it cleared", st.Active)
	}
}

func TestRegisterAddsTheAccountAndMakesItActive(t *testing.T) {
	isolate(t)
	tok := fakeToken(t, "a@example.com", "at-a", "rt-a")
	acc, existed, err := Register(tok)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if existed {
		t.Fatal("Register reported the account already existed")
	}
	if acc.Email != "a@example.com" {
		t.Fatalf("email = %q", acc.Email)
	}
	st, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st.Active != "a@example.com" {
		t.Fatalf("active = %q, want the registered account", st.Active)
	}
	// A second registration of the same account updates it in place.
	if _, existed, err = Register(fakeToken(t, "a@example.com", "at-a2", "rt-a")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !existed {
		t.Fatal("Register did not report the account as known")
	}
	st, _ = Load()
	if len(st.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(st.Accounts))
	}
}

func TestRegisterRejectsATokenWithoutAnEmail(t *testing.T) {
	isolate(t)
	if _, _, err := Register(&auth.AntigravityToken{}); err == nil {
		t.Fatal("Register accepted a token without an email")
	}
}

// TestRegisterWithEmail covers the credential the reference refreshed:
// its id_token is gone, so the email comes from somewhere else.
func TestRegisterWithEmail(t *testing.T) {
	isolate(t)
	acc, existed, err := RegisterWithEmail("a@example.com", &auth.AntigravityToken{
		Token: auth.AntigravityAccessToken{AccessToken: "at-a", RefreshToken: "rt-a"},
	})
	if err != nil {
		t.Fatalf("RegisterWithEmail: %v", err)
	}
	if existed {
		t.Fatal("RegisterWithEmail reported the account already existed")
	}
	if acc.Email != "a@example.com" {
		t.Fatalf("email = %q", acc.Email)
	}
}

// TestCurrentFallsBackToTheRegisteredAccount covers a credential whose
// id_token is gone: the refresh token survives a refresh, so it names
// the account the registry holds.
func TestCurrentFallsBackToTheRegisteredAccount(t *testing.T) {
	isolate(t)
	if _, _, err := Register(fakeToken(t, "a@example.com", "at-a", "rt-a")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// The shared credential lost its id_token, the way a refreshed
	// token does.
	if err := auth.SaveTokenUnified(&auth.AntigravityToken{
		Token: auth.AntigravityAccessToken{
			AccessToken:  "at-a2",
			RefreshToken: "rt-a",
			Expiry:       time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}); err != nil {
		t.Fatalf("SaveTokenUnified: %v", err)
	}
	_, email := Current()
	if email != "a@example.com" {
		t.Fatalf("Current() = %q, want a@example.com", email)
	}
}

// TestActivateWritesTheSharedCredential is the core of the feature: the
// switch has to land in the places the reference reads, or `agy` would
// keep using the old account.
func TestActivateWritesTheSharedCredential(t *testing.T) {
	home := isolate(t)
	if _, _, err := Register(fakeToken(t, "a@example.com", "at-a", "rt-a")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, _, err := Register(fakeToken(t, "b@example.com", "at-b", "rt-b")); err != nil {
		t.Fatalf("Register: %v", err)
	}

	acc, err := Activate(t.Context(), "b@example.com")
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if acc.Email != "b@example.com" {
		t.Fatalf("email = %q, want b@example.com", acc.Email)
	}

	// The shared on-disk path the reference reads must now hold b.
	raw, err := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token"))
	if err != nil {
		t.Fatalf("read the shared token: %v", err)
	}
	if !strings.Contains(string(raw), "at-b") {
		t.Fatalf("the shared token does not hold the activated account: %s", raw)
	}

	// And the clone's own cascade must read it back as b.
	tok, err := auth.LoadTokenUnified()
	if err != nil {
		t.Fatalf("LoadTokenUnified: %v", err)
	}
	if got := tok.UserEmail(); got != "b@example.com" {
		t.Fatalf("the shared credential reads back %q, want b@example.com", got)
	}
}

func TestActivateRejectsAnUnknownAccount(t *testing.T) {
	isolate(t)
	if _, err := Activate(t.Context(), "nobody@example.com"); err == nil {
		t.Fatal("Activate accepted an unknown account")
	}
}

// TestRemoveClearsTheSharedCredential covers the sign-out path: the
// account leaves the registry and the shared credential is cleared, so
// neither the clone nor the reference keeps using it.
func TestRemoveClearsTheSharedCredential(t *testing.T) {
	home := isolate(t)
	if _, _, err := Register(fakeToken(t, "a@example.com", "at-a", "rt-a")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	acc, active, err := Remove("a@example.com")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !active {
		t.Fatal("Remove did not report the account as active")
	}
	if acc.Email != "a@example.com" {
		t.Fatalf("email = %q", acc.Email)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")); !os.IsNotExist(err) {
		t.Fatalf("the shared token survived the removal (stat err = %v)", err)
	}
	st, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(st.Accounts) != 0 {
		t.Fatalf("accounts = %d, want 0", len(st.Accounts))
	}
}
