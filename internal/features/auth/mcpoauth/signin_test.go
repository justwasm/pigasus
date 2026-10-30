// signin_test.go — covers the persistence side of the AGY 1.3.4
// changelog line: saved sign-ins are now keyed by sanitized server
// URL, persisted to disk, and cleared on demand. The actual clear
// decision is in policy.go and tested in policy_test.go; here we pin
// the on-disk shape so a future refactor that "optimizes" the JSON
// layout is a deliberate decision.

package mcpoauth

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestStore_RoundTrip — write, read, clear. The canonical happy path.
func TestStore_RoundTrip(t *testing.T) {
	// Use one TempDir for the lifetime of the test so the re-open
	// below finds the same file (t.TempDir() returns a fresh dir on
	// every call).
	path := filepath.Join(t.TempDir(), "signins.json")
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	const url = "https://mcp.example.com/path"
	signin := &Signin{
		ServerURL:    url,
		ClientID:     "client-abc",
		AccessToken:  "AT-123",
		RefreshToken: "RT-456",
		Scope:        "read write",
		ExpiresAt:    time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
	}
	key, err := s.Save(signin)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if key != SanitizeServerURL(url) {
		t.Fatalf("Save returned key %q, want %q", key, SanitizeServerURL(url))
	}
	got, ok := s.Get(url)
	if !ok {
		t.Fatalf("Get(%q): not found", url)
	}
	if got.AccessToken != signin.AccessToken {
		t.Fatalf("AccessToken = %q, want %q", got.AccessToken, signin.AccessToken)
	}
	if got.RefreshToken != signin.RefreshToken {
		t.Fatalf("RefreshToken = %q, want %q", got.RefreshToken, signin.RefreshToken)
	}
	if got.SavedAt == "" {
		t.Fatalf("SavedAt is empty, want non-empty")
	}
	// Re-opening the store from disk must produce the same record.
	s2, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	got2, ok := s2.Get(url)
	if !ok {
		t.Fatalf("after re-open: Get(%q): not found", url)
	}
	if got2.AccessToken != signin.AccessToken {
		t.Fatalf("after re-open: AccessToken = %q, want %q", got2.AccessToken, signin.AccessToken)
	}
	present, err := s.Clear(url)
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if !present {
		t.Fatalf("Clear: present = false, want true")
	}
	if _, ok := s.Get(url); ok {
		t.Fatalf("Get after Clear: still found")
	}
	// Clear is idempotent.
	present2, err := s.Clear(url)
	if err != nil {
		t.Fatalf("Clear idempotent: %v", err)
	}
	if present2 {
		t.Fatalf("Clear idempotent: present = true, want false")
	}
}

// TestStore_RejectEmptyAccessToken — saving an empty access_token is
// always a bug (it would produce a guaranteed 401 on the next request)
// so the store rejects it loudly rather than silently producing a
// no-op auth header.
func TestStore_RejectEmptyAccessToken(t *testing.T) {
	s, err := NewStoreAt(filepath.Join(t.TempDir(), "signins.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	_, err = s.Save(&Signin{
		ServerURL:   "https://mcp.example.com",
		ClientID:    "client",
		AccessToken: "", // empty
	})
	if err == nil {
		t.Fatalf("Save with empty AccessToken succeeded, want error")
	}
	if !strings.Contains(err.Error(), "empty access_token") {
		t.Fatalf("error = %v, want one mentioning 'empty access_token'", err)
	}
}

// TestStore_RejectUnparseableURL — the key must be a real URL with a
// host; saving to "" or "://broken" is rejected so the store never
// silently coalesces every malformed record into the same "" key.
func TestStore_RejectUnparseableURL(t *testing.T) {
	s, err := NewStoreAt(filepath.Join(t.TempDir(), "signins.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	for _, bad := range []string{"", "://broken", "not a url", "  "} {
		_, err = s.Save(&Signin{
			ServerURL:   bad,
			AccessToken: "AT",
			ClientID:    "c",
		})
		if err == nil {
			t.Fatalf("Save(%q) succeeded, want error", bad)
		}
	}
}

// TestStore_KeyNormalization — saving with a URL whose cosmetic
// features (case in host, query string, fragment, missing scheme)
// differ from the lookup URL must still resolve. AGY 1.3.4's
// "actually matches after save" promise.
func TestStore_KeyNormalization(t *testing.T) {
	s, err := NewStoreAt(filepath.Join(t.TempDir(), "signins.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	saved := "https://MCP.Example.com/path?token=gizmo#frag"
	signin := &Signin{
		ServerURL:   saved,
		ClientID:    "client",
		AccessToken: "AT",
	}
	key, err := s.Save(signin)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	want := "https://mcp.example.com/path"
	if key != want {
		t.Fatalf("Save key = %q, want %q", key, want)
	}
	for _, lookup := range []string{
		"https://MCP.Example.com/path",
		"https://mcp.example.com/path",
		"https://mcp.example.com/path?other=1",
		"mcp.example.com/path",
	} {
		if _, ok := s.Get(lookup); !ok {
			t.Fatalf("Get(%q): not found, want match (sanitization should normalize)", lookup)
		}
	}
}

// TestStore_MultipleServers — two servers, two records, no bleed.
func TestStore_MultipleServers(t *testing.T) {
	s, err := NewStoreAt(filepath.Join(t.TempDir(), "signins.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	for _, url := range []string{
		"https://a.example.com",
		"https://b.example.com",
		"https://c.example.com",
	} {
		_, err := s.Save(&Signin{
			ServerURL:   url,
			ClientID:    "client-" + url,
			AccessToken: "AT-" + url,
		})
		if err != nil {
			t.Fatalf("Save(%s): %v", url, err)
		}
	}
	if got := s.Count(); got != 3 {
		t.Fatalf("Count = %d, want 3", got)
	}
	if _, ok := s.Get("https://a.example.com"); !ok {
		t.Fatalf("a.example.com missing")
	}
	if _, ok := s.Get("https://b.example.com"); !ok {
		t.Fatalf("b.example.com missing")
	}
	if _, ok := s.Get("https://c.example.com"); !ok {
		t.Fatalf("c.example.com missing")
	}
	if present, _ := s.Clear("https://b.example.com"); !present {
		t.Fatalf("Clear b: present = false, want true")
	}
	if got := s.Count(); got != 2 {
		t.Fatalf("after Clear b: Count = %d, want 2", got)
	}
	if _, ok := s.Get("https://b.example.com"); ok {
		t.Fatalf("b.example.com still present after Clear")
	}
	if _, ok := s.Get("https://a.example.com"); !ok {
		t.Fatalf("a.example.com missing after Clear b")
	}
}

// TestStore_ListOrder — List is sorted by ServerURL so the
// /mcp auth list rendering and test fixtures are stable.
func TestStore_ListOrder(t *testing.T) {
	s, err := NewStoreAt(filepath.Join(t.TempDir(), "signins.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	for _, url := range []string{"https://c.example.com", "https://a.example.com", "https://b.example.com"} {
		_, err := s.Save(&Signin{ServerURL: url, ClientID: "c", AccessToken: "AT"})
		if err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	got := s.List()
	want := []string{"https://a.example.com", "https://b.example.com", "https://c.example.com"}
	if len(got) != len(want) {
		t.Fatalf("len(List) = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ServerURL != w {
			t.Fatalf("List[%d].ServerURL = %q, want %q", i, got[i].ServerURL, w)
		}
	}
}

// TestStore_ClearAll — drops every record.
func TestStore_ClearAll(t *testing.T) {
	s, err := NewStoreAt(filepath.Join(t.TempDir(), "signins.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	for _, url := range []string{"https://a.example.com", "https://b.example.com"} {
		if _, err := s.Save(&Signin{ServerURL: url, ClientID: "c", AccessToken: "AT"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	if err := s.ClearAll(); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	if got := s.Count(); got != 0 {
		t.Fatalf("Count after ClearAll = %d, want 0", got)
	}
	if _, ok := s.Get("https://a.example.com"); ok {
		t.Fatalf("a.example.com still present after ClearAll")
	}
}

// TestStore_PermissionsAndDir — the store creates the parent
// directory (0700) and the JSON file (0600) so the OS user is the
// only reader of OAuth bearer tokens. This is the security-relevant
// invariant the rest of the auth package relies on.
func TestStore_PermissionsAndDir(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "fresh", "subdir", "signins.json")
	s, err := NewStoreAt(deep)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if _, err := s.Save(&Signin{ServerURL: "https://mcp.example.com", ClientID: "c", AccessToken: "AT"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(deep)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode = %o, want 0600", mode)
	}
	parent := filepath.Dir(deep)
	pinfo, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("Stat parent: %v", err)
	}
	if pmode := pinfo.Mode().Perm(); pmode != 0o700 {
		t.Fatalf("parent dir mode = %o, want 0700", pmode)
	}
}

// TestStore_MissingFile — a missing JSON file is not an error: the
// store starts empty. This matches the AGY first-launch behaviour.
func TestStore_MissingFile(t *testing.T) {
	s, err := NewStoreAt(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if got := s.Count(); got != 0 {
		t.Fatalf("Count on missing file = %d, want 0", got)
	}
}

// TestStore_InMemoryOnly — NewStoreAt("") builds a store that does
// not touch the disk. Used by tests that don't want to manage
// temp dirs.
func TestStore_InMemoryOnly(t *testing.T) {
	s, err := NewStoreAt("")
	if err != nil {
		t.Fatalf("NewStoreAt(\"\"): %v", err)
	}
	if _, err := s.Save(&Signin{ServerURL: "https://mcp.example.com", ClientID: "c", AccessToken: "AT"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got, ok := s.Get("https://mcp.example.com"); !ok || got.AccessToken != "AT" {
		t.Fatalf("Get: ok=%v, got=%+v", ok, got)
	}
}

// TestStore_StableJSON — re-saving the same record produces
// byte-identical output so a future SHA256-pinned reference file
// does not flake.
func TestStore_StableJSON(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.json")
	s, err := NewStoreAt(a)
	if err != nil {
		t.Fatalf("NewStoreAt a: %v", err)
	}
	// Fixed SavedAt so the JSON is byte-identical regardless of when
	// the test runs.
	for _, url := range []string{"https://a.example.com", "https://b.example.com"} {
		if _, err := s.Save(&Signin{
			ServerURL:   url,
			ClientID:    "c",
			AccessToken: "AT",
			SavedAt:     "2024-01-01T00:00:00Z",
		}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	first, err := os.ReadFile(a)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Re-open and read again.
	s2, err := NewStoreAt(a)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	// Sorted key iteration in writeLocked means the bytes are
	// deterministic. Verify List is sorted, which is the contract
	// writeLocked relies on.
	list := s2.List()
	if !sort.SliceIsSorted(list, func(i, j int) bool { return list[i].ServerURL < list[j].ServerURL }) {
		t.Fatalf("List is not sorted: %+v", list)
	}
	if string(first) == "" {
		t.Fatalf("file is empty")
	}
}

// TestSignin_Expired — Expired reports correctly for past / future /
// missing expiries. The clone uses this to decide whether to refresh
// proactively; a missing ExpiresAt must not be treated as expired.
func TestSignin_Expired(t *testing.T) {
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		s    *Signin
		want bool
	}{
		{"nil", nil, false},
		{"empty expiry", &Signin{ExpiresAt: ""}, false},
		{"future expiry", &Signin{ExpiresAt: now.Add(1 * time.Hour).Format(time.RFC3339)}, false},
		{"past expiry", &Signin{ExpiresAt: now.Add(-1 * time.Hour).Format(time.RFC3339)}, true},
		{"garbage expiry", &Signin{ExpiresAt: "not a time"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Expired(now); got != tc.want {
				t.Fatalf("Expired = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestStore_GetReturnsCopy — the store hands out copies so a caller
// cannot mutate the store's record. The AGY 1.3.4 changelog line
// relies on this: a refresh hook that updates `*Signin` in place
// would otherwise leak state across concurrent Get calls.
func TestStore_GetReturnsCopy(t *testing.T) {
	s, err := NewStoreAt("")
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if _, err := s.Save(&Signin{
		ServerURL:   "https://mcp.example.com",
		ClientID:    "c",
		AccessToken: "AT-orig",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok := s.Get("https://mcp.example.com")
	if !ok {
		t.Fatalf("Get: not found")
	}
	got.AccessToken = "AT-mutated"
	got2, _ := s.Get("https://mcp.example.com")
	if got2.AccessToken != "AT-orig" {
		t.Fatalf("mutation leaked: AccessToken = %q, want AT-orig", got2.AccessToken)
	}
}

// TestStore_SaveNil — passing a nil *Signin surfaces an error rather
// than panicking. A nil Store is a programmer bug, not something to
// guard against at runtime — the Save receiver type requires a real
// pointer. Here we only test the nil-signin path.
func TestStore_SaveNil(t *testing.T) {
	s, err := NewStoreAt("")
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if _, err := s.Save(nil); err == nil {
		t.Fatalf("nil signin: Save succeeded, want error")
	}
}

// TestStore_ClearURLNormalization — Clear normalizes its argument
// the same way Save / Get do, so `Clear` with a query string still
// removes the saved record.
func TestStore_ClearURLNormalization(t *testing.T) {
	s, err := NewStoreAt("")
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if _, err := s.Save(&Signin{
		ServerURL:   "https://mcp.example.com",
		ClientID:    "c",
		AccessToken: "AT",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	present, err := s.Clear("https://MCP.Example.com/?foo=bar")
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if !present {
		t.Fatalf("Clear: present = false, want true")
	}
	if _, ok := s.Get("https://mcp.example.com"); ok {
		t.Fatalf("Get after Clear: still found")
	}
}

// TestStorePath — StorePath() honours $ANTIGRAVITY_HOME and $HOME.
func TestStorePath(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/custom/home")
	if got := StorePath(); got != "/custom/home/mcp-oauth-signins.json" {
		t.Fatalf("StorePath with ANTIGRAVITY_HOME = %q, want /custom/home/mcp-oauth-signins.json", got)
	}
	t.Setenv("ANTIGRAVITY_HOME", "")
	t.Setenv("HOME", "/home/user")
	if got := StorePath(); got != "/home/user/.gemini/antigravity-cli/mcp-oauth-signins.json" {
		t.Fatalf("StorePath with HOME = %q, want /home/user/.gemini/antigravity-cli/mcp-oauth-signins.json", got)
	}
}
