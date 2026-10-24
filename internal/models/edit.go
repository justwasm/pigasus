// edit.go — the file-scoped writer for the provider-centric configuration.
//
// # One writer, two front-ends
//
// The provider configuration is written by two programs: the clone's CLI
// (`agy provider add`, `agy model add`) and the TUI's `/provider` panel.
// The wrapper (`agy wrapper providers add`) is a third front-end on the
// same file. All of them go through this Editor, so there is one
// implementation of "what a write does to the file" and one place where
// the file's shape is decided.
//
// # Targeted writes
//
// A write touches the field it changes and nothing else. Mutations use
// sjson and gjson to replace fields in-place:
//
//   - a field the clone does not model (an OAuth block, a key a newer
//     version of the file carries) survives the write;
//   - the key order survives the write, so a diff shows the one line that
//     changed.
//
// # Locking
//
// The lock is a sibling file created with O_EXCL. It is portable (no
// per-platform syscall) and it keeps a CLI write and a TUI write from
// clobbering each other.
package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"
)

// lockTimeout bounds how long a writer waits for another writer's lock.
const lockTimeout = 5 * time.Second

// lockStaleAfter is how old a lock file may be before it is treated as
// abandoned by a writer that died.
const lockStaleAfter = 30 * time.Second

// Editor writes the provider section of one configuration file.
type Editor struct {
	path string
}

// NewEditor returns an editor for the configuration file at path.
func NewEditor(path string) *Editor { return &Editor{path: path} }

// ProviderEditor returns an editor for the conventional provider
// configuration file (see ProvidersPath).
func ProviderEditor() *Editor { return NewEditor(ProvidersPath()) }

// Path returns the file this editor writes.
func (e *Editor) Path() string {
	if e == nil {
		return ""
	}
	return e.path
}

// List returns the provider section of the file.
func (e *Editor) List() (map[string]ProviderConfig, error) {
	if e == nil {
		return nil, fmt.Errorf("models: nil editor")
	}
	raw, err := os.ReadFile(e.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]ProviderConfig{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", e.path, err)
	}
	return parseProvidersJSON(raw)
}

func parseProvidersJSON(raw []byte) (map[string]ProviderConfig, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return map[string]ProviderConfig{}, nil
	}
	if !gjson.ValidBytes(trimmed) {
		return nil, fmt.Errorf("models: invalid JSON")
	}
	res := gjson.GetBytes(trimmed, "providers")
	if !res.Exists() || !res.IsObject() {
		return map[string]ProviderConfig{}, nil
	}
	out := make(map[string]ProviderConfig)
	var parseErr error
	res.ForEach(func(key, val gjson.Result) bool {
		var p ProviderConfig
		if err := json.Unmarshal([]byte(val.Raw), &p); err != nil {
			parseErr = fmt.Errorf("models: parse provider %q: %w", key.String(), err)
			return false
		}
		if strings.TrimSpace(p.ID) == "" {
			p.ID = key.String()
		}
		out[key.String()] = p
		return true
	})
	if parseErr != nil {
		return nil, parseErr
	}
	return out, nil
}

// PutProvider creates or replaces a provider entry.
//
// The provider's own fields are written; its models are not touched.
// Use PutModel to change one model without touching the others.
func (e *Editor) PutProvider(id string, p ProviderConfig) error {
	if e == nil {
		return fmt.Errorf("models: nil editor")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("models: provider id is required")
	}
	if strings.TrimSpace(p.ID) == "" {
		p.ID = id
	}
	return e.edit(func(raw []byte) ([]byte, error) {
		return applyProvider(raw, id, p)
	})
}

func applyProvider(raw []byte, id string, p ProviderConfig) ([]byte, error) {
	pfx := "providers." + escapePath(id) + "."
	var err error
	if raw, err = setOrDelete(raw, pfx+"id", p.ID); err != nil {
		return nil, err
	}
	if raw, err = setOrDelete(raw, pfx+"name", p.Name); err != nil {
		return nil, err
	}
	if raw, err = setOrDelete(raw, pfx+"type", p.Type); err != nil {
		return nil, err
	}
	if raw, err = setOrDelete(raw, pfx+"base_url", p.BaseURL); err != nil {
		return nil, err
	}
	if raw, err = setOrDelete(raw, pfx+"api_url", p.APIURL); err != nil {
		return nil, err
	}
	if raw, err = setOrDelete(raw, pfx+"api_key", p.APIKey); err != nil {
		return nil, err
	}
	if raw, err = setOrDeleteInt(raw, pfx+"timeout_seconds", p.TimeoutSeconds); err != nil {
		return nil, err
	}
	if raw, err = setOrDeleteInt(raw, pfx+"max_retries", p.MaxRetries); err != nil {
		return nil, err
	}
	if len(p.ExtraHeaders) > 0 {
		return sjson.SetBytes(raw, pfx+"extra_headers", p.ExtraHeaders)
	}
	return sjson.DeleteBytes(raw, pfx+"extra_headers")
}

// DeleteProvider removes a provider entry and every model listed under it.
func (e *Editor) DeleteProvider(id string) error {
	if e == nil {
		return fmt.Errorf("models: nil editor")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("models: provider id is required")
	}
	return e.edit(func(raw []byte) ([]byte, error) {
		path := "providers." + escapePath(id)
		if !gjson.GetBytes(raw, path).Exists() {
			return nil, fmt.Errorf("models: no provider named %q", id)
		}
		return sjson.DeleteBytes(raw, path)
	})
}

// PutModel creates or replaces a model under a provider.
func (e *Editor) PutModel(providerID string, m ProviderModel) error {
	if e == nil {
		return fmt.Errorf("models: nil editor")
	}
	providerID = strings.TrimSpace(providerID)
	m.ID = strings.TrimSpace(m.ID)
	if providerID == "" {
		return fmt.Errorf("models: provider id is required")
	}
	if m.ID == "" {
		return fmt.Errorf("models: model id is required")
	}
	return e.edit(func(raw []byte) ([]byte, error) {
		return applyModel(raw, providerID, m)
	})
}

func applyModel(raw []byte, providerID string, m ProviderModel) ([]byte, error) {
	pPath := "providers." + escapePath(providerID)
	if !gjson.GetBytes(raw, pPath).Exists() {
		return nil, fmt.Errorf("models: unknown provider %q", providerID)
	}
	modelsPath := pPath + ".models"
	idx := findModelIndex(raw, modelsPath, m.ID)
	if idx >= 0 {
		return updateModelFields(raw, fmt.Sprintf("%s.%d", modelsPath, idx), m)
	}
	return sjson.SetBytes(raw, modelsPath+".-1", modelToMap(m))
}

func findModelIndex(raw []byte, modelsPath, id string) int {
	res := gjson.GetBytes(raw, modelsPath)
	if !res.IsArray() {
		return -1
	}
	for i, item := range res.Array() {
		if item.Get("id").String() == id {
			return i
		}
	}
	return -1
}

func updateModelFields(raw []byte, target string, m ProviderModel) ([]byte, error) {
	var err error
	if raw, err = setOrDelete(raw, target+".id", m.ID); err != nil {
		return nil, err
	}
	if raw, err = setOrDelete(raw, target+".name", m.Name); err != nil {
		return nil, err
	}
	if raw, err = setOrDelete(raw, target+".model", m.Model); err != nil {
		return nil, err
	}
	if raw, err = setOrDeleteInt(raw, target+".context_window", m.ContextWindow); err != nil {
		return nil, err
	}
	if raw, err = setOrDeleteInt(raw, target+".default_max_tokens", m.DefaultMaxTokens); err != nil {
		return nil, err
	}
	return setOrDelete(raw, target+".reasoning_effort", m.ReasoningEffort)
}

func modelToMap(m ProviderModel) map[string]any {
	out := map[string]any{"id": m.ID}
	if m.Name != "" {
		out["name"] = m.Name
	}
	if m.Model != "" {
		out["model"] = m.Model
	}
	if m.ContextWindow != 0 {
		out["context_window"] = m.ContextWindow
	}
	if m.DefaultMaxTokens != 0 {
		out["default_max_tokens"] = m.DefaultMaxTokens
	}
	if m.ReasoningEffort != "" {
		out["reasoning_effort"] = m.ReasoningEffort
	}
	return out
}

// DeleteModel removes a model by catalogue id from whichever provider
// lists it. It returns the provider it was removed from and whether
// anything was removed.
func (e *Editor) DeleteModel(id string) (string, bool, error) {
	if e == nil {
		return "", false, fmt.Errorf("models: nil editor")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false, fmt.Errorf("models: model id is required")
	}
	found := ""
	err := e.edit(func(raw []byte) ([]byte, error) {
		pid, idx, count := findModelLocation(raw, id)
		if idx < 0 {
			return raw, nil
		}
		found = pid
		pfx := "providers." + escapePath(pid) + ".models"
		if count <= 1 {
			return sjson.DeleteBytes(raw, pfx)
		}
		return sjson.DeleteBytes(raw, fmt.Sprintf("%s.%d", pfx, idx))
	})
	if err != nil {
		return "", false, err
	}
	return found, found != "", nil
}

func findModelLocation(raw []byte, id string) (string, int, int) {
	res := gjson.GetBytes(raw, "providers")
	if !res.IsObject() {
		return "", -1, 0
	}
	foundPid := ""
	foundIdx := -1
	foundCount := 0
	res.ForEach(func(pKey, pVal gjson.Result) bool {
		models := pVal.Get("models")
		if !models.IsArray() {
			return true
		}
		arr := models.Array()
		for i, m := range arr {
			if m.Get("id").String() == id {
				foundPid = pKey.String()
				foundIdx = i
				foundCount = len(arr)
				return false
			}
		}
		return true
	})
	return foundPid, foundIdx, foundCount
}

// edit reads the document, applies fn to it and writes it back under the
// file lock.
func (e *Editor) edit(fn func(raw []byte) ([]byte, error)) error {
	if strings.TrimSpace(e.path) == "" {
		return fmt.Errorf("models: no provider config path")
	}
	return withFileLock(e.path, func() error {
		raw, err := os.ReadFile(e.path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				raw = []byte("{}")
			} else {
				return fmt.Errorf("read %s: %w", e.path, err)
			}
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			raw = []byte("{}")
		} else if !gjson.ValidBytes(trimmed) {
			return fmt.Errorf("models: invalid JSON")
		}
		updated, err := fn(raw)
		if err != nil {
			return err
		}
		return writeFormattedJSON(e.path, updated)
	})
}

func writeFormattedJSON(path string, raw []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("models: mkdir %s: %w", dir, err)
		}
	}
	formatted := pretty.Pretty(raw)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, formatted, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func setOrDelete(raw []byte, path, val string) ([]byte, error) {
	if val != "" {
		return sjson.SetBytes(raw, path, val)
	}
	return sjson.DeleteBytes(raw, path)
}

func setOrDeleteInt(raw []byte, path string, val int) ([]byte, error) {
	if val != 0 {
		return sjson.SetBytes(raw, path, val)
	}
	return sjson.DeleteBytes(raw, path)
}

func escapePath(key string) string {
	key = strings.ReplaceAll(key, `\`, `\\`)
	key = strings.ReplaceAll(key, `.`, `\.`)
	key = strings.ReplaceAll(key, `:`, `\:`)
	return key
}

// withFileLock runs fn while holding an exclusive lock on path.
func withFileLock(path string, fn func() error) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("models: mkdir %s: %w", dir, err)
		}
	}
	lockPath := path + ".lock"
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			defer func() { _ = os.Remove(lockPath) }()
			return fn()
		}
		if !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("models: lock %s: %w", lockPath, err)
		}
		if st, serr := os.Stat(lockPath); serr == nil && time.Since(st.ModTime()) > lockStaleAfter {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("models: %s is locked by another writer", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
