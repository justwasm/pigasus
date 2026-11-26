package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/core/confhome"
)

const settingsSnapshotRefreshInterval = 250 * time.Millisecond

type settingsSnapshotCache struct {
	mu       sync.Mutex
	interval time.Duration
	now      func() time.Time
	stat     func(string) (os.FileInfo, error)
	read     func(string) ([]byte, error)

	path         string
	stamp        string
	checkedAt    time.Time
	readRetryAt  time.Time
	checked      bool
	snapshot     *SettingsSnapshot
	lastGoodPath string
	lastGoodBase map[string]any
}

func newSettingsSnapshotCache() *settingsSnapshotCache {
	return &settingsSnapshotCache{
		interval: settingsSnapshotRefreshInterval,
		now:      time.Now,
		stat:     os.Stat,
		read:     os.ReadFile,
	}
}

var cachedSettingsSnapshot = newSettingsSnapshotCache()

// SettingsSnapshot is an immutable, parsed view of settings.json. Its accessors
// return copies of compound values so callers cannot mutate the shared snapshot.
type SettingsSnapshot struct {
	fields map[string]any
	err    error
}

// CachedSnapshot returns a settings snapshot that bounds filesystem metadata
// checks to one per 250ms. InvalidateCache and SetSessionOverlay take effect on
// the next call immediately. The regular Field/String/Bool APIs remain
// immediate and continue to detect external edits on every call.
func CachedSnapshot() *SettingsSnapshot {
	return cachedSettingsSnapshot.get(Path(), func() map[string]any {
		sessionSettingsMu.RLock()
		defer sessionSettingsMu.RUnlock()
		return cloneSettingsMap(sessionSettingsOverlay)
	})
}

// Err reports a read or parse failure for the current refresh. Snapshot values
// continue to reflect the last valid file contents when possible.
func (s *SettingsSnapshot) Err() error {
	if s == nil {
		return nil
	}
	return s.err
}

// Has reports whether key exists in this snapshot.
func (s *SettingsSnapshot) Has(key string) bool {
	_, ok := s.lookup(key)
	return ok
}

// Field returns one value from the snapshot; dotted keys walk nested objects.
// Read/parse diagnostics are available separately through Err so last-good
// values remain usable while an external editor is writing an invalid file.
func (s *SettingsSnapshot) Field(key string) (any, error) {
	value, ok := s.lookup(key)
	if !ok {
		return nil, nil
	}
	return cloneSettingsValue(value), nil
}

// String returns a string value from the snapshot or "" when absent or mistyped.
func (s *SettingsSnapshot) String(key string) string {
	value, _ := s.lookup(key)
	text, _ := value.(string)
	return text
}

// Bool returns a boolean value from the snapshot, accepting legacy on/off strings.
func (s *SettingsSnapshot) Bool(key string) (value, ok bool) {
	v, exists := s.lookup(key)
	if !exists {
		return false, false
	}
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		switch b {
		case "on", "true":
			return true, true
		case "off", "false":
			return false, true
		}
	}
	return false, false
}

// ResolveModel returns the configured model, falling back when absent or blank.
func (s *SettingsSnapshot) ResolveModel(fallback string) string {
	if model := strings.TrimSpace(s.String("model")); model != "" {
		return model
	}
	return fallback
}

func (s *SettingsSnapshot) lookup(key string) (any, bool) {
	if s == nil || len(s.fields) == 0 {
		return nil, false
	}
	var current any = s.fields
	for {
		part, rest, more := strings.Cut(key, ".")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
		if !more {
			return current, true
		}
		key = rest
	}
}

func cloneSettingsMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneSettingsValue(value)
	}
	return out
}

func cloneSettingsValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneSettingsMap(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = cloneSettingsValue(v[i])
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return v
	}
}

func (c *settingsSnapshotCache) invalidate() {
	c.mu.Lock()
	c.checked = false
	c.stamp = ""
	c.mu.Unlock()
}

func (c *settingsSnapshotCache) get(path string, overlay func() map[string]any) *SettingsSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if c.checked && c.path == path {
		elapsed := now.Sub(c.checkedAt)
		if elapsed >= 0 && elapsed < c.interval {
			return c.snapshot
		}
	}

	stamp, statErr := c.fileStamp(path)
	if c.checked && c.path == path && c.stamp == stamp && c.snapshot != nil && (c.readRetryAt.IsZero() || now.Before(c.readRetryAt)) {
		c.checkedAt = now
		return c.snapshot
	}

	c.readRetryAt = time.Time{}
	base := map[string]any(nil)
	var refreshErr error
	if statErr != nil && !os.IsNotExist(statErr) {
		refreshErr = statErr
	} else if os.IsNotExist(statErr) {
		base = map[string]any{}
		c.lastGoodPath = path
		c.lastGoodBase = map[string]any{}
	} else {
		var raw []byte
		raw, refreshErr = c.read(path)
		if refreshErr != nil {
			// A transient read error can recover without changing mtime/size.
			// Retry at the refresh interval, never once per frame.
			c.readRetryAt = now.Add(settingsSnapshotRefreshInterval)
		}
		if refreshErr == nil {
			raw = confhome.StripBOM(raw)
			if err := json.Unmarshal(raw, &base); err != nil {
				refreshErr = err
			}
			if refreshErr == nil && base == nil {
				refreshErr = fmt.Errorf("settings: expected a JSON object")
			}
		}
		if refreshErr == nil {
			c.lastGoodPath = path
			c.lastGoodBase = cloneSettingsMap(base)
		}
	}
	if refreshErr != nil {
		if c.lastGoodPath == path && c.lastGoodBase != nil {
			base = cloneSettingsMap(c.lastGoodBase)
		} else {
			base = map[string]any{}
		}
	}
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range overlay() {
		base[key] = cloneSettingsValue(value)
	}

	c.path = path
	c.stamp = stamp
	c.checkedAt = now
	c.checked = true
	c.snapshot = &SettingsSnapshot{fields: base, err: refreshErr}
	return c.snapshot
}

func (c *settingsSnapshotCache) fileStamp(path string) (string, error) {
	info, err := c.stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing", err
		}
		return "stat-error:" + err.Error(), err
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano()), nil
}
