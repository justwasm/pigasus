// Package endpoints — config.go
//
// The configuration file the overrides come from.
package endpoints

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tailscale/hujson"
	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"

	"github.com/justwasm/pigasus/atomicfile"
	"github.com/justwasm/pigasus/configdir"
)

// ConfigPathEnvVar is the environment variable that overrides the
// configuration file's path. It is the sibling of
// $OPENAGY_PROVIDERS_CONFIG, which names the provider-centric file.
const ConfigPathEnvVar = "OPENAGY_CONFIG"

// Config is the configuration document.
//
// The document is a map of sections rather than a bare map of endpoints so
// that the file can grow a second section without a format change. There are
// two today:
//
//   - "endpoints" — the endpoint overrides.
//   - "wrapper" — the proxy's own persisted state. Its shape belongs to
//     internal/agyproxy, so this package carries it as an opaque section:
//     the file has one reader, and the section's owner still owns the
//     shape.
//
// The account pool's policy is **not** here: it lives in `accounts.json`,
// beside the accounts it is about. See
// `internal/features/accounts/core.CurrentAccountPool`.
type Config struct {
	// Endpoints maps an endpoint name (see the name constants in
	// endpoints.go) to the URL to use instead of the compiled-in
	// default.
	Endpoints map[string]string `json:"endpoints,omitempty"`

	// GeminiAPIKey is an optional API key for Gemini models and real-time voice (Gemini Live).
	GeminiAPIKey string `json:"gemini_api_key,omitempty"`

	// GeminiLiveModel is an optional override for the real-time voice model
	// (e.g. "models/gemini-3.8-live" or "models/gemini-3.8-live-extended-thinking").
	GeminiLiveModel string `json:"gemini_live_model,omitempty"`

	// VoiceNoteInterval is an optional override for the minimum delay between
	// voice progress notes during background tool execution (e.g. "5s", "3000ms", or 5).
	VoiceNoteInterval json.RawMessage `json:"voice_note_interval,omitempty"`

	// VoiceNoteIntervalSeconds is an optional numeric override (in seconds) for
	// the minimum delay between voice progress notes.
	VoiceNoteIntervalSeconds *float64 `json:"voice_note_interval_seconds,omitempty"`

	// Wrapper is the proxy's persisted state, carried verbatim. It is
	// nil when the file carries no such section.
	Wrapper json.RawMessage `json:"wrapper,omitempty"`
}

// Path returns the configuration file's path:
//
//	$OPENAGY_CONFIG, or
//	<the configuration home>/config.json — $HOME/.openagy/config.json
//	unless $OPENAGY_CONFIG_HOME says otherwise.
//
// The configuration home is the clone's single home (the `configdir`
// package); the retired $HOME/.config/openagy is migrated into it.
func Path() string {
	if p := strings.TrimSpace(os.Getenv(ConfigPathEnvVar)); p != "" {
		return p
	}
	return configdir.Path("config.json")
}

// Load reads the configuration document at path.
//
// A missing file is an empty configuration, not an error: the defaults
// apply until the user writes one.
func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("endpoints: read %s: %w", path, err)
	}
	if std, herr := hujson.Standardize(raw); herr == nil {
		raw = std
	}
	var doc Config
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Config{}, fmt.Errorf("endpoints: parse %s: %w", path, err)
	}
	return doc, nil
}

// overrides is one resolved snapshot of the file's endpoint overrides.
type overrides struct {
	stamp  string
	values map[string]string
}

// overridesCache holds the last snapshot, with the stamp of the file it
// was built from.
var overridesCache atomic.Pointer[overrides]

// currentOverrides returns the endpoint overrides, re-reading the file only
// when it changed on disk.
//
// The stamp is the file's size and modification time — the same identity
// the provider registry uses for its own cache. A file that does not exist
// has the stamp "-", which is stable: the resolver does not stat it again
// until it appears.
func currentOverrides() map[string]string {
	path := Path()
	stamp := fileStamp(path)

	if cached := overridesCache.Load(); cached != nil && cached.stamp == stamp {
		return cached.values
	}

	values := map[string]string{}
	if cfg, err := Load(path); err == nil {
		for name, url := range cfg.Endpoints {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			values[name] = strings.TrimSpace(url)
		}
	}

	snapshot := &overrides{stamp: stamp, values: values}
	overridesCache.Store(snapshot)
	return snapshot.values
}

// CurrentGeminiAPIKey returns the Gemini API key from GEMINI_API_KEY, OPENAGY_API_KEY,
// or the configuration file's gemini_api_key field.
func CurrentGeminiAPIKey() string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("OPENAGY_API_KEY")); v != "" {
		return v
	}
	if v := cachedGeminiAPIKey(); v != "" {
		return v
	}
	return ""
}

// apiKeyCache holds the last key read from the config file, with the
// stamp of the file it came from.
//
// This is the same shape as overridesCache, and for the same reason: the
// status row asks for the key while rendering, and the status row is
// rendered whenever the terminal's usable height is computed — which the
// /logs panel does on every repaint. Without the cache, drawing a frame
// read and parsed config.json; on a scrolling panel that was the single
// largest cost in the frame, and it was invisible from the panel's own
// code.
var apiKeyCache atomic.Pointer[apiKeySnapshot]

type apiKeySnapshot struct {
	stamp string
	key   string
}

// cachedGeminiAPIKey returns the config file's key, re-reading the file
// only when it changed on disk.
func cachedGeminiAPIKey() string {
	path := Path()
	stamp := fileStamp(path)
	if cached := apiKeyCache.Load(); cached != nil && cached.stamp == stamp {
		return cached.key
	}
	key := ""
	if cfg, err := Load(path); err == nil {
		key = strings.TrimSpace(cfg.GeminiAPIKey)
	}
	apiKeyCache.Store(&apiKeySnapshot{stamp: stamp, key: key})
	return key
}

// CurrentGeminiLiveModel returns the live voice model override from GEMINI_LIVE_MODEL,
// OPENAGY_LIVE_MODEL, or the configuration file's gemini_live_model field.
func CurrentGeminiLiveModel() string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_LIVE_MODEL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("OPENAGY_LIVE_MODEL")); v != "" {
		return v
	}
	if cfg, err := Load(Path()); err == nil && strings.TrimSpace(cfg.GeminiLiveModel) != "" {
		return strings.TrimSpace(cfg.GeminiLiveModel)
	}
	return ""
}

// DefaultVoiceNoteInterval is the default minimum delay between non-critical voice progress notes.
const DefaultVoiceNoteInterval = 5 * time.Second

func parseVoiceInterval(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d, true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		return time.Duration(f * float64(time.Second)), true
	}
	return 0, false
}

func parseVoiceIntervalRaw(raw json.RawMessage) (time.Duration, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return parseVoiceInterval(str)
	}
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil && num > 0 {
		return time.Duration(num * float64(time.Second)), true
	}
	return 0, false
}

// CurrentVoiceNoteInterval returns the voice note throttle interval from
// OPENAGY_VOICE_NOTE_INTERVAL, GEMINI_VOICE_NOTE_INTERVAL, or the
// configuration file's voice_note_interval / voice_note_interval_seconds fields.
// If none is configured or invalid, it returns DefaultVoiceNoteInterval (5 seconds).
func CurrentVoiceNoteInterval() time.Duration {
	if v := os.Getenv("OPENAGY_VOICE_NOTE_INTERVAL"); v != "" {
		if d, ok := parseVoiceInterval(v); ok {
			return d
		}
	}
	if v := os.Getenv("GEMINI_VOICE_NOTE_INTERVAL"); v != "" {
		if d, ok := parseVoiceInterval(v); ok {
			return d
		}
	}
	if cfg, err := Load(Path()); err == nil {
		if d, ok := parseVoiceIntervalRaw(cfg.VoiceNoteInterval); ok {
			return d
		}
		if cfg.VoiceNoteIntervalSeconds != nil && *cfg.VoiceNoteIntervalSeconds > 0 {
			return time.Duration(*cfg.VoiceNoteIntervalSeconds * float64(time.Second))
		}
	}
	return DefaultVoiceNoteInterval
}

// wrapperKey is the section name the proxy's persisted state lives under in
// the configuration document.
const wrapperKey = "wrapper"

// wrapperSnapshot is one resolved snapshot of the document's "wrapper"
// section.
type wrapperSnapshot struct {
	stamp string
	value json.RawMessage
}

// wrapperCache holds the last snapshot, with the stamp of the file it was
// built from.
var wrapperCache atomic.Pointer[wrapperSnapshot]

// CurrentWrapper returns the document's "wrapper" section, or nil when the
// document carries none.
//
// The section is carried verbatim: its shape belongs to the proxy, not to
// this package. The file is re-read only when it changed on disk — the
// same stamp the other sections use.
func CurrentWrapper() json.RawMessage {
	path := Path()
	stamp := fileStamp(path)

	if cached := wrapperCache.Load(); cached != nil && cached.stamp == stamp {
		return cached.value
	}

	var value json.RawMessage
	if cfg, err := Load(path); err == nil && len(cfg.Wrapper) > 0 {
		value = cfg.Wrapper
	}
	wrapperCache.Store(&wrapperSnapshot{stamp: stamp, value: value})
	return value
}

// SaveWrapper writes raw into the document's "wrapper" section.
//
// It leaves the rest of the document — the endpoint overrides, the
// account-routing policy, and any section a newer version wrote — as it
// was: the edit is made on the raw document, so the key order survives.
// An empty value removes the section.
//
// The write is atomic, and the cached snapshots are dropped, so the state
// the proxy reads is the one that was just written.
func SaveWrapper(raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return updateDocument(wrapperKey, nil, true)
	}
	return updateDocument(wrapperKey, raw, false)
}

// documentMu serialises the read-modify-write cycles on the configuration
// document: two sections written at the same time must not lose each other.
var documentMu sync.Mutex

// updateDocument rewrites one section of the configuration document.
//
// Everything else in the document survives — including its key order — and
// the write is atomic. The cached snapshots are dropped, so the next read
// picks the new document up.
func updateDocument(key string, value []byte, remove bool) error {
	documentMu.Lock()
	defer documentMu.Unlock()

	path := Path()
	if strings.TrimSpace(path) == "" {
		return errors.New("endpoints: no configuration file path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("endpoints: read %s: %w", path, err)
		}
		raw = []byte("{}")
	}
	document := bytes.TrimSpace(raw)
	if len(document) == 0 {
		document = []byte("{}")
	}
	if !gjson.ValidBytes(document) {
		return fmt.Errorf("endpoints: %s is not valid JSON", path)
	}

	var updated []byte
	if remove {
		updated, err = sjson.DeleteBytes(document, key)
		if err != nil {
			return fmt.Errorf("endpoints: remove %s: %w", key, err)
		}
	} else {
		updated, err = sjson.SetRawBytes(document, key, value)
		if err != nil {
			return fmt.Errorf("endpoints: write %s: %w", key, err)
		}
	}

	formatted := append(pretty.Pretty(updated), '\n')
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("endpoints: create %s: %w", dir, err)
		}
	}
	if err := atomicfile.WriteFile(path, formatted, 0o600); err != nil {
		return fmt.Errorf("endpoints: write %s: %w", path, err)
	}
	// The cached snapshots describe the file before the write: drop them,
	// so the next read picks the new one up.
	wrapperCache.Store(nil)
	return nil
}

// fileStamp renders the identity of a configuration file: its path, and
// its size and modification time when it exists.
func fileStamp(path string) string {
	if path == "" {
		return "-"
	}
	var b strings.Builder
	b.WriteString(path)
	b.WriteByte(0)
	if st, err := os.Stat(path); err == nil {
		fmt.Fprintf(&b, "%d:%d", st.Size(), st.ModTime().UnixNano())
	} else {
		b.WriteString("-")
	}
	return b.String()
}
