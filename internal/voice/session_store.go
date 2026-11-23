package voice

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/configdir"
)

var (
	sessionDirOverride string
	sessionDirMu       sync.RWMutex
	sessionFileMu      sync.Mutex
)

// SetLiveSessionDirOverride sets a temporary directory override for session storage (useful in tests).
func SetLiveSessionDirOverride(dir string) {
	sessionDirMu.Lock()
	defer sessionDirMu.Unlock()
	sessionDirOverride = dir
}

// LiveSessionDir returns the directory where LiveChat sessions are saved.
func LiveSessionDir() string {
	sessionDirMu.RLock()
	override := sessionDirOverride
	sessionDirMu.RUnlock()
	if override != "" {
		return override
	}

	if env := os.Getenv("OPENAGY_LIVECHAT_SESSION_DIR"); env != "" {
		return env
	}

	base := configdir.Dir()
	if base == "" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			base = filepath.Join(home, ".openagy")
		} else {
			base = filepath.Join(os.TempDir(), "openagy")
		}
	}
	return filepath.Join(base, "livechat", "sessions")
}

// LiveSessionData represents a fully persisted LiveChat conversation session.
type LiveSessionData struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	Workspace string           `json:"workspace"`
	Model     string           `json:"model"`
	Voice     string           `json:"voice"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Turns     []LiveTurnRecord `json:"turns"`
	Logs      []string         `json:"logs"`
}

// LiveSessionSummary is a lightweight descriptor used for listing saved sessions.
type LiveSessionSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Workspace string    `json:"workspace"`
	Model     string    `json:"model"`
	Voice     string    `json:"voice"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	TurnCount int       `json:"turn_count"`
	Path      string    `json:"path"`
}

// GenerateLiveSessionID produces a unique chronological session ID (e.g. "20261117-173000-a1b2").
func GenerateLiveSessionID() string {
	ts := time.Now().Format("20060102-150405")
	buf := make([]byte, 2)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%s-%s", ts, hex.EncodeToString(buf))
}

// DeriveSessionTitle generates a concise human-readable title from the first meaningful utterance or workspace.
func DeriveSessionTitle(turns []LiveTurnRecord, ws string) string {
	for _, t := range turns {
		if strings.EqualFold(t.Role, "User") {
			content := strings.TrimSpace(t.Content)
			if content != "" && !strings.HasPrefix(content, "[") {
				// Truncate to first 50 chars
				runes := []rune(content)
				if len(runes) > 50 {
					return string(runes[:47]) + "..."
				}
				return content
			}
		}
	}
	if ws != "" {
		return "LiveChat in " + filepath.Base(ws)
	}
	return "LiveChat Session"
}

// SaveLiveSession atomically saves session data to disk as JSON.
func SaveLiveSession(data *LiveSessionData) error {
	if data == nil {
		return errors.New("cannot save nil session data")
	}
	data.ID = strings.TrimSpace(data.ID)
	if data.ID == "" || data.ID == "." || data.ID == ".." {
		data.ID = GenerateLiveSessionID()
	} else {
		data.ID = filepath.Base(data.ID)
	}
	if data.CreatedAt.IsZero() {
		data.CreatedAt = time.Now()
	}
	data.UpdatedAt = time.Now()
	if data.Title == "" {
		data.Title = DeriveSessionTitle(data.Turns, data.Workspace)
	}

	dir := LiveSessionDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory %s: %w", dir, err)
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session json: %w", err)
	}

	sessionFileMu.Lock()
	defer sessionFileMu.Unlock()

	targetPath := filepath.Join(dir, data.ID+".json")
	tmpFile, err := os.CreateTemp(dir, data.ID+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp session file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write session file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp session file: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("failed to commit session file: %w", err)
	}
	cleanupTmp = false
	return nil
}

// LoadLiveSession reads a session from a file path or session ID.
func LoadLiveSession(idOrPath string) (*LiveSessionData, error) {
	idOrPath = strings.TrimSpace(idOrPath)
	if idOrPath == "" {
		return nil, errors.New("empty session id or path")
	}

	path := idOrPath
	if !filepath.IsAbs(path) && !strings.HasSuffix(path, ".json") {
		path = filepath.Join(LiveSessionDir(), idOrPath+".json")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	var data LiveSessionData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("failed to parse session data: %w", err)
	}
	return &data, nil
}

// ListLiveSessions returns all saved sessions sorted by UpdatedAt descending (newest first).
func ListLiveSessions(workspaceFilter ...string) ([]LiveSessionSummary, error) {
	dir := LiveSessionDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var wsFilter string
	if len(workspaceFilter) > 0 {
		wsFilter = strings.TrimSpace(workspaceFilter[0])
	}

	var summaries []LiveSessionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		var data LiveSessionData
		if err := json.Unmarshal(raw, &data); err != nil {
			continue
		}

		if wsFilter != "" && data.Workspace != "" {
			if !strings.EqualFold(filepath.Clean(data.Workspace), filepath.Clean(wsFilter)) {
				continue
			}
		}

		summaries = append(summaries, LiveSessionSummary{
			ID:        data.ID,
			Title:     data.Title,
			Workspace: data.Workspace,
			Model:     data.Model,
			Voice:     data.Voice,
			CreatedAt: data.CreatedAt,
			UpdatedAt: data.UpdatedAt,
			TurnCount: len(data.Turns),
			Path:      fullPath,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	return summaries, nil
}

// FindLiveSession locates a session by exact ID, 1-based index (e.g. "1"), or prefix match.
func FindLiveSession(target string, workspaceFilter ...string) (*LiveSessionData, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, errors.New("empty target")
	}

	// 1. Direct Load by Exact ID / Path
	if data, err := LoadLiveSession(target); err == nil {
		return data, nil
	}

	// 2. Query list for prefix match or 1-based index
	summaries, err := ListLiveSessions(workspaceFilter...)
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 && len(workspaceFilter) > 0 {
		// Fallback to global list if workspace filter yielded no matches
		summaries, _ = ListLiveSessions()
	}

	if len(summaries) == 0 {
		return nil, fmt.Errorf("no saved sessions found")
	}

	// Index match (e.g. "1" or "#1")
	if strings.HasPrefix(target, "#") {
		if idx, err := strconv.Atoi(strings.TrimPrefix(target, "#")); err == nil {
			if idx >= 1 && idx <= len(summaries) {
				return LoadLiveSession(summaries[idx-1].Path)
			}
			return nil, fmt.Errorf("session index #%d out of range (1-%d)", idx, len(summaries))
		}
	} else if idx, err := strconv.Atoi(target); err == nil && idx >= 1 && idx <= len(summaries) {
		return LoadLiveSession(summaries[idx-1].Path)
	}

	// Keyword "latest" / "last"
	if strings.EqualFold(target, "latest") || strings.EqualFold(target, "last") {
		return LoadLiveSession(summaries[0].Path)
	}

	// Prefix match
	var matches []LiveSessionSummary
	lowerTarget := strings.ToLower(target)
	for _, s := range summaries {
		if strings.HasPrefix(strings.ToLower(s.ID), lowerTarget) {
			matches = append(matches, s)
		}
	}

	if len(matches) == 1 {
		return LoadLiveSession(matches[0].Path)
	}
	if len(matches) > 1 {
		var ids []string
		for _, m := range matches {
			ids = append(ids, m.ID)
		}
		return nil, fmt.Errorf("ambiguous session prefix %q matches multiple sessions: %s", target, strings.Join(ids, ", "))
	}

	return nil, fmt.Errorf("session %q not found", target)
}

// DeleteLiveSession removes a saved session by ID or path.
func DeleteLiveSession(idOrPath string) error {
	idOrPath = strings.TrimSpace(idOrPath)
	path := idOrPath
	if !filepath.IsAbs(path) && !strings.HasSuffix(path, ".json") {
		path = filepath.Join(LiveSessionDir(), idOrPath+".json")
	}
	return os.Remove(path)
}
