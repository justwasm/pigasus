package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if got := SettingsString("foo"); got != "" {
		t.Errorf("SettingsString on missing file: got %q, want empty", got)
	}
	if val, ok := SettingsBool("foo"); ok || val {
		t.Errorf("SettingsBool on missing file: got (%v, %v), want (false, false)", val, ok)
	}
	if raw, err := SettingsField("foo.bar"); err == nil || raw != nil {
		t.Errorf("SettingsField on missing file: got (%v, %v)", raw, err)
	}
}

func TestSettingsBasicSetGet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if _, err := WriteCompatDefaults(); err != nil {
		t.Fatal(err)
	}
	if err := SetSettingsString("myString", "hello"); err != nil {
		t.Fatalf("SetSettingsString: %v", err)
	}
	if got := SettingsString("myString"); got != "hello" {
		t.Errorf("SettingsString: got %q, want hello", got)
	}
	if err := SetSettingsBool("statusLine", true); err != nil {
		t.Fatalf("SetSettingsBool: %v", err)
	}
	if val, ok := SettingsBool("statusLine"); !ok || !val {
		t.Errorf("SettingsBool: got (%v, %v), want (true, true)", val, ok)
	}
	if err := SetSettingsBool("statusLine", false); err != nil {
		t.Fatalf("SetSettingsBool false: %v", err)
	}
	if val, ok := SettingsBool("statusLine"); !ok || val {
		t.Errorf("SettingsBool: got (%v, %v), want (false, true)", val, ok)
	}
}

func TestSettingsBoolLegacyStrings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if _, err := WriteCompatDefaults(); err != nil {
		t.Fatal(err)
	}
	if err := SetSettingsString("legacyOn", "on"); err != nil {
		t.Fatal(err)
	}
	if val, ok := SettingsBool("legacyOn"); !ok || !val {
		t.Errorf("SettingsBool legacy 'on': got (%v, %v), want (true, true)", val, ok)
	}
	if err := SetSettingsString("legacyOff", "off"); err != nil {
		t.Fatal(err)
	}
	if val, ok := SettingsBool("legacyOff"); !ok || val {
		t.Errorf("SettingsBool legacy 'off': got (%v, %v), want (false, true)", val, ok)
	}
}

func TestSettingsNestedFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if _, err := WriteCompatDefaults(); err != nil {
		t.Fatal(err)
	}
	if err := SetSettingsField("statusLine.nested.enabled", true); err != nil {
		t.Fatalf("SetSettingsField: %v", err)
	}
	val, err := SettingsField("statusLine.nested.enabled")
	if err != nil {
		t.Fatalf("SettingsField: %v", err)
	}
	if b, ok := val.(bool); !ok || !b {
		t.Errorf("SettingsField nested: got %v (%T), want true (bool)", val, val)
	}
	missing, err := SettingsField("statusLine.nested.absent")
	if err != nil || missing != nil {
		t.Errorf("SettingsField absent: got (%v, %v), want (nil, nil)", missing, err)
	}
}

func TestSettingsDeleteNestedFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if _, err := WriteCompatDefaults(); err != nil {
		t.Fatal(err)
	}
	if err := SetSettingsField("statusLine.nested.enabled", true); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSettingsField("statusLine.nested.enabled"); err != nil {
		t.Fatalf("DeleteSettingsField: %v", err)
	}
	afterDel, err := SettingsField("statusLine.nested.enabled")
	if err != nil || afterDel != nil {
		t.Errorf("SettingsField after delete: got (%v, %v), want (nil, nil)", afterDel, err)
	}
	parent, err := SettingsField("statusLine.nested")
	if err != nil {
		t.Fatalf("SettingsField parent: %v", err)
	}
	if _, ok := parent.(map[string]any); !ok {
		t.Errorf("Parent shell was not preserved: %v (%T)", parent, parent)
	}
}

func TestSettingsDeleteCreatesIfMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if err := DeleteSettingsField("nonexistent.key"); err != nil {
		t.Fatalf("DeleteSettingsField on absent file: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatalf("ReadFile after DeleteSettingsField: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal after DeleteSettingsField: %v", err)
	}
}

func TestSettingsFormattingAndKeyOrdering(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if _, err := WriteCompatDefaults(); err != nil {
		t.Fatal(err)
	}
	if err := SetSettingsString("zKey", "zVal"); err != nil {
		t.Fatal(err)
	}
	if err := SetSettingsString("aKey", "aVal"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	aIdx := strings.Index(s, `"aKey"`)
	zIdx := strings.Index(s, `"zKey"`)
	if aIdx == -1 || zIdx == -1 || aIdx > zIdx {
		t.Errorf("keys not sorted alphabetically:\n%s", s)
	}
	if !strings.Contains(s, "\n  \"aKey\": \"aVal\"") {
		t.Errorf("missing 2-space indentation:\n%s", s)
	}
}

func TestSettingsCorruptFileHandling(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(home, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{corrupt json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SettingsField("foo"); err == nil {
		t.Errorf("SettingsField on corrupt file: expected error, got nil")
	}
	if err := SetSettingsField("foo", "bar"); err == nil {
		t.Errorf("SetSettingsField on corrupt file: expected error, got nil")
	}
	if err := DeleteSettingsField("foo"); err != nil {
		t.Fatalf("DeleteSettingsField on corrupt file: %v", err)
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("file remained corrupt after DeleteSettingsField: %v", err)
	}
}
