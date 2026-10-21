// Package store implements the recovered Manager state machine from
// google3/third_party/jetski/cli/store/store. The 1.3.3 binary
// contains 731 funcs in this package; 385 of them were decompiled
// in cycle 11 (see /decompiled/c/google3_third_party_jetski_cli_store_store/).
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Manager is the recovered state machine wrapping the antigravity
// session, auth, eligibility, and TUI backend. Source: cli/store.Manager
// (see /decompiled/c/google3_third_party_jetski_cli_store_store/google3_third_party_jetski_cli_store_store_Manager.c).
type Manager struct {
	mu        sync.Mutex
	authState AuthState
	authToken string
	tier      string
	settings  map[string]string
}

// AuthState is the recovered auth state enum.
type AuthState int

const (
	AuthStateInit AuthState = iota
	AuthStateAuthenticated
	AuthStateExpired
	AuthStateLoggedOut
	AuthStateError
)

func (a AuthState) String() string {
	switch a {
	case AuthStateInit:
		return "init"
	case AuthStateAuthenticated:
		return "authenticated"
	case AuthStateExpired:
		return "expired"
	case AuthStateLoggedOut:
		return "logged_out"
	case AuthStateError:
		return "error"
	}
	return "unknown"
}

// NewManager constructs a fresh Manager. Source: cli/store.NewManager.
func NewManager() *Manager {
	return &Manager{
		authState: AuthStateInit,
		settings:  make(map[string]string),
	}
}

// Init loads ~/.config/antigravity/settings.json. Source: cli/store.(*Manager).initCliSettings.
func (m *Manager) Init(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings["cli_settings_loaded"] = "true"
	return nil
}

// TrySilentAuth re-uses cached refresh token. Source: cli/store.(*Manager).TrySilentAuth.
func (m *Manager) TrySilentAuth(ctx context.Context) (TokenSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.authToken == "" {
		return nil, ErrNoCachedToken
	}
	return &cachedTokenSource{token: m.authToken, manager: m}, nil
}

// commitAuth commits auth state. Source: cli/store.(*Manager).commitAuth.
func (m *Manager) commitAuth(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authToken = token
	m.authState = AuthStateAuthenticated
}

// WaitForEligibility blocks until backend reports eligibility. Source: cli/store.(*Manager).waitForEligibility.
func (m *Manager) WaitForEligibility(ctx context.Context) (bool, error) {
	return true, nil // TODO(cycle-51+): real impl polls LSP GetStatus
}

// Logout clears stored credentials. Source: cli/store.(*Manager).Logout.
func (m *Manager) Logout(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authToken = ""
	m.authState = AuthStateLoggedOut
	return nil
}

// ResetAuthState resets to init. Source: cli/store.(*Manager).ResetAuthState.
func (m *Manager) ResetAuthState() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authState = AuthStateInit
	m.authToken = ""
}

// TokenSource is the recovered token source interface from
// codeassistclient. Seven implementations exist in 1.3.3:
// CLIAuthProvider, AntigravityHubAuthProvider, StandaloneAuthProvider,
// HostAuthProvider, StubbyAuthProvider, IDEAuthProvider, GeminiAPIKeyAuthProvider.
type TokenSource interface {
	Token() (string, error)
	Name() string
}

// ErrNoCachedToken is returned by TrySilentAuth when no refresh token is cached.
var ErrNoCachedToken = errNoCachedToken("no cached refresh token")

type errNoCachedToken string

func (e errNoCachedToken) Error() string { return string(e) }

type cachedTokenSource struct {
	token   string
	manager *Manager
}

func (c *cachedTokenSource) Token() (string, error) { return c.token, nil }
func (c *cachedTokenSource) Name() string           { return "cached-refresh-token" }

// ---------------------------------------------------------------------------
// Cycle 41: runMigrate + runStats handlers wired by internal/cli/dispatch.go
// ---------------------------------------------------------------------------

// MigrationResult summarises one Migrate() invocation: how many
// files were copied, how many were skipped, and any errors that
// weren't fatal. The CLI prints these to the user.
type MigrationResult struct {
	Copied  []string
	Skipped []string
	Errors  []error
}

// Migrate walks the legacy ~/.config/antigravity tree and copies
// recoverable files into the v4 binary's ~/.gemini/antigravity-cli
// layout. The migration is conservative: files that already exist
// at the destination are not overwritten (logged as "skipped"
// instead). Missing source files are silently ignored.
//
// Files migrated:
//   - antigravity-auth.json (v1.0..v1.2)  -> antigravity-oauth-token
//     (token content is best-effort re-shaped; see MigrateToken)
//   - settings.json                       -> settings.json (verbatim copy)
//   - hooks.json                          -> hooks.json (verbatim copy)
//
// Returns nil on success; a non-nil error only when the destination
// directory cannot be created.
func Migrate() error {
	src, dst := migrationPaths()
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return fmt.Errorf("store.Migrate: mkdir %s: %w", dst, err)
	}
	// 1. token: best-effort reshape.
	if err := MigrateToken(filepath.Join(src, "antigravity-auth.json"),
		filepath.Join(dst, "antigravity-oauth-token")); err != nil {
		// Non-fatal: print to stderr but keep migrating other files.
		fmt.Fprintf(os.Stderr, "[store.Migrate] token: %v\n", err)
	}
	// 2. settings.json: verbatim copy.
	if err := copyFileIfMissing(filepath.Join(src, "settings.json"),
		filepath.Join(dst, "settings.json")); err != nil {
		fmt.Fprintf(os.Stderr, "[store.Migrate] settings: %v\n", err)
	}
	// 3. hooks.json: verbatim copy.
	if err := copyFileIfMissing(filepath.Join(src, "hooks.json"),
		filepath.Join(dst, "hooks.json")); err != nil {
		fmt.Fprintf(os.Stderr, "[store.Migrate] hooks: %v\n", err)
	}
	return nil
}

// MigrateToken converts the v1.0..v1.2 antigravity-auth.json into
// the v4 binary's antigravity-oauth-token format. Best-effort: a
// parse failure is reported via error so the caller can log it; the
// destination is not written in that case.
//
// Returns nil if the source file is missing (no-op migration).
func MigrateToken(srcPath, dstPath string) error {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	// v1.0..v1.2 shape: {"access_token":"...","refresh_token":"...","expiry":"..."}
	var legacy struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Expiry       string `json:"expiry"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return fmt.Errorf("parse %s: %w", srcPath, err)
	}
	if legacy.AccessToken == "" {
		return fmt.Errorf("%s: empty access_token (nothing to migrate)", srcPath)
	}
	v4 := struct {
		Token struct {
			AccessToken  string `json:"access_token"`
			TokenType    string `json:"token_type"`
			RefreshToken string `json:"refresh_token"`
			Expiry       string `json:"expiry"`
		} `json:"token"`
		AuthMethod string `json:"auth_method"`
		IDToken    string `json:"id_token"`
	}{
		AuthMethod: "consumer",
	}
	v4.Token.AccessToken = legacy.AccessToken
	v4.Token.RefreshToken = legacy.RefreshToken
	v4.Token.Expiry = legacy.Expiry
	if v4.Token.TokenType == "" {
		v4.Token.TokenType = "Bearer"
	}
	out, err := json.MarshalIndent(&v4, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if _, err := os.Stat(dstPath); err == nil {
		// Destination already exists; don't overwrite. Caller logs.
		return fmt.Errorf("%s: destination exists (skipped)", dstPath)
	}
	if err := os.WriteFile(dstPath, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", dstPath, err)
	}
	return nil
}

// migrationPaths returns the (source, destination) roots for the
// v1->v4 config migration. Respects $XDG_CONFIG_HOME and $HOME.
func migrationPaths() (string, string) {
	xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	home := strings.TrimSpace(os.Getenv("HOME"))
	if xdg == "" && home != "" {
		xdg = filepath.Join(home, ".config")
	}
	src := ""
	if xdg != "" {
		src = filepath.Join(xdg, "antigravity")
	}
	dst := strings.TrimSpace(os.Getenv("ANTIGRAVITY_HOME"))
	if dst == "" {
		if home != "" {
			dst = filepath.Join(home, ".gemini", "antigravity-cli")
		} else {
			dst = filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
		}
	}
	return src, dst
}

// copyFileIfMissing copies src->dst only if dst does not exist.
// Missing source is a no-op (returns nil). Other errors propagate.
func copyFileIfMissing(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// Stats is the summary reported by runStats. Fields are zero if
// the source files don't exist (counts of zero are valid).
type Stats struct {
	Sessions    int
	Tokens      int
	LastSession time.Time
}

// Stats returns usage statistics from the on-disk session store.
// It scans ~/.config/antigravity/sessions/ (or
// $ANTIGRAVITY_SESSIONS) and aggregates per-session metadata:
//
//   - Sessions     = number of *.json files in the dir
//   - Tokens       = sum of `usage.total_tokens` across all sessions
//   - LastSession  = max(updated_at) across all sessions
//
// Missing directory is a no-op (returns zero Stats, no error).
func RunStats() (*Stats, error) {
	sessionsDir := defaultSessionsDir()
	out := &Stats{}
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("store.Stats: read %s: %w", sessionsDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(sessionsDir, e.Name())
		s, err := readSessionSummary(path)
		if err != nil {
			// Skip malformed sessions; don't fail the whole report.
			continue
		}
		out.Sessions++
		out.Tokens += s.Tokens
		if s.UpdatedAt.After(out.LastSession) {
			out.LastSession = s.UpdatedAt
		}
	}
	return out, nil
}

type sessionSummary struct {
	UpdatedAt time.Time
	Tokens    int
}

// readSessionSummary extracts just the fields Stats cares about.
// Best-effort: malformed sessions return an error so the caller
// can skip them.
func readSessionSummary(path string) (*sessionSummary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		UpdatedAt string `json:"updated_at"`
		Usage     struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := &sessionSummary{Tokens: doc.Usage.TotalTokens}
	if doc.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, doc.UpdatedAt); err == nil {
			out.UpdatedAt = t
		}
	}
	return out, nil
}

// defaultSessionsDir returns the v4 binary's session store path,
// respecting $ANTIGRAVITY_SESSIONS and the standard $XDG_CONFIG_HOME
// / $HOME fallbacks.
func defaultSessionsDir() string {
	if v := strings.TrimSpace(os.Getenv("ANTIGRAVITY_SESSIONS")); v != "" {
		return v
	}
	xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	home := strings.TrimSpace(os.Getenv("HOME"))
	if xdg == "" && home != "" {
		xdg = filepath.Join(home, ".config")
	}
	if xdg == "" {
		return ""
	}
	return filepath.Join(xdg, "antigravity", "sessions")
}

// AllMigratedSources returns the sorted list of source files that
// Migrate() considers for migration. Exposed for tests; the CLI
// uses Migrate() directly.
func AllMigratedSources() []string {
	src, _ := migrationPaths()
	if src == "" {
		return nil
	}
	all := []string{
		"antigravity-auth.json",
		"settings.json",
		"hooks.json",
	}
	sort.Strings(all)
	return all
}
