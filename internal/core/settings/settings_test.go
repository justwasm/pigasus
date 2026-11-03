package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDefaults materialises the settings.json the reference's compat
// layer writes on first launch, so the store has a file to read and
// rewrite.
func writeDefaults(t *testing.T, home string) {
	t.Helper()
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"colorScheme":"dark"}`), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if got := String("foo"); got != "" {
		t.Errorf("String on missing file: got %q, want empty", got)
	}
	if val, ok := Bool("foo"); ok || val {
		t.Errorf("Bool on missing file: got (%v, %v), want (false, false)", val, ok)
	}
	if raw, err := Field("foo.bar"); err == nil || raw != nil {
		t.Errorf("Field on missing file: got (%v, %v)", raw, err)
	}
}

func TestSettingsBasicSetGet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	writeDefaults(t, home)
	if err := SetString("myString", "hello"); err != nil {
		t.Fatalf("SetString: %v", err)
	}
	if got := String("myString"); got != "hello" {
		t.Errorf("String: got %q, want hello", got)
	}
	if err := SetBool("statusLine", true); err != nil {
		t.Fatalf("SetBool: %v", err)
	}
	if val, ok := Bool("statusLine"); !ok || !val {
		t.Errorf("Bool: got (%v, %v), want (true, true)", val, ok)
	}
	if err := SetBool("statusLine", false); err != nil {
		t.Fatalf("SetBool false: %v", err)
	}
	if val, ok := Bool("statusLine"); !ok || val {
		t.Errorf("Bool: got (%v, %v), want (false, true)", val, ok)
	}
}

func TestSettingsBoolLegacyStrings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	writeDefaults(t, home)
	if err := SetString("legacyOn", "on"); err != nil {
		t.Fatal(err)
	}
	if val, ok := Bool("legacyOn"); !ok || !val {
		t.Errorf("Bool legacy 'on': got (%v, %v), want (true, true)", val, ok)
	}
	if err := SetString("legacyOff", "off"); err != nil {
		t.Fatal(err)
	}
	if val, ok := Bool("legacyOff"); !ok || val {
		t.Errorf("Bool legacy 'off': got (%v, %v), want (false, true)", val, ok)
	}
}

func TestSettingsNestedFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	writeDefaults(t, home)
	if err := SetField("statusLine.nested.enabled", true); err != nil {
		t.Fatalf("SetField: %v", err)
	}
	val, err := Field("statusLine.nested.enabled")
	if err != nil {
		t.Fatalf("Field: %v", err)
	}
	if b, ok := val.(bool); !ok || !b {
		t.Errorf("Field nested: got %v (%T), want true (bool)", val, val)
	}
	missing, err := Field("statusLine.nested.absent")
	if err != nil || missing != nil {
		t.Errorf("Field absent: got (%v, %v), want (nil, nil)", missing, err)
	}
}

func TestSettingsDeleteNestedFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	writeDefaults(t, home)
	if err := SetField("statusLine.nested.enabled", true); err != nil {
		t.Fatal(err)
	}
	if err := DeleteField("statusLine.nested.enabled"); err != nil {
		t.Fatalf("DeleteField: %v", err)
	}
	afterDel, err := Field("statusLine.nested.enabled")
	if err != nil || afterDel != nil {
		t.Errorf("Field after delete: got (%v, %v), want (nil, nil)", afterDel, err)
	}
	parent, err := Field("statusLine.nested")
	if err != nil {
		t.Fatalf("Field parent: %v", err)
	}
	if _, ok := parent.(map[string]any); !ok {
		t.Errorf("Parent shell was not preserved: %v (%T)", parent, parent)
	}
}

func TestSettingsDeleteCreatesIfMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if err := DeleteField("nonexistent.key"); err != nil {
		t.Fatalf("DeleteField on absent file: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatalf("ReadFile after DeleteField: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal after DeleteField: %v", err)
	}
}

func TestSettingsFormattingAndKeyOrdering(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	writeDefaults(t, home)
	if err := SetString("zKey", "zVal"); err != nil {
		t.Fatal(err)
	}
	if err := SetString("aKey", "aVal"); err != nil {
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
	if _, err := Field("foo"); err == nil {
		t.Errorf("Field on corrupt file: expected error, got nil")
	}
	if err := SetField("foo", "bar"); err == nil {
		t.Errorf("SetField on corrupt file: expected error, got nil")
	}
	if err := DeleteField("foo"); err != nil {
		t.Fatalf("DeleteField on corrupt file: %v", err)
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("file remained corrupt after DeleteField: %v", err)
	}
}
