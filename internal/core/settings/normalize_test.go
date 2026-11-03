// normalize_test.go — the start-up normalisation's own tests.
//
// Every test points $ANTIGRAVITY_HOME at a scratch directory: the
// normalisation resolves its file through `confhome.Antigravity()`, so a
// test without the override would rewrite the developer's own
// settings.json.
package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scratchSettings points $ANTIGRAVITY_HOME at a fresh directory and
// returns the settings.json path inside it.
func scratchSettings(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	return filepath.Join(home, "settings.json")
}

// writeSettings writes the settings file with the given content and mode.
func writeSettings(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

// readSettings reads the settings file back.
func readSettings(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestDefaultsCoverEveryValidKey pins the defaults table against the
// reference's key catalogue: a key the store knows but the table does not
// has no default, and the normalisation would keep its value forever.
func TestDefaultsCoverEveryValidKey(t *testing.T) {
	for key := range ValidKeys {
		if _, ok := defaults[key]; !ok {
			t.Errorf("key %q has no default", key)
		}
	}
	for key := range defaults {
		if !ValidKeys[key] {
			t.Errorf("default for unknown key %q", key)
		}
	}
	if got := len(defaults); got != len(ValidKeys) {
		t.Errorf("defaults has %d keys, ValidKeys has %d", got, len(ValidKeys))
	}
}

// TestDefaultString pins the string accessor: a key whose default is not a
// string yields the empty string.
func TestDefaultString(t *testing.T) {
	if got := DefaultString("toolPermission"); got != "request-review" {
		t.Errorf("DefaultString(toolPermission) = %q, want request-review", got)
	}
	if got := DefaultString("historySize"); got != "" {
		t.Errorf("DefaultString(historySize) = %q, want empty", got)
	}
	if got := DefaultString("nope"); got != "" {
		t.Errorf("DefaultString(nope) = %q, want empty", got)
	}
}

// TestNormalizeDropsDefaultValues pins the reference's own rule: a key
// whose value equals the compiled default is removed, a key whose value
// differs is kept.
func TestNormalizeDropsDefaultValues(t *testing.T) {
	path := scratchSettings(t)
	writeSettings(t, path, `{"altScreenMode":"default","verbosity":"high","historySize":2000}`, 0600)

	if err := NormalizeOnStartup(); err != nil {
		t.Fatalf("NormalizeOnStartup: %v", err)
	}

	got := readSettings(t, path)
	if strings.Contains(got, "altScreenMode") {
		t.Errorf("default altScreenMode survived: %s", got)
	}
	if strings.Contains(got, "historySize") {
		t.Errorf("default historySize survived: %s", got)
	}
	if !strings.Contains(got, `"verbosity": "high"`) {
		t.Errorf("non-default verbosity dropped: %s", got)
	}
}

// TestNormalizeKeepsKeysWhoseAbsentCaseDiffers pins the one place the
// clone's normalisation differs from the reference's: a key whose absent
// case means something else than its default is kept, because dropping it
// would change what the CLI does.
//
// `toolPermission` is the security-relevant one — the reference's default
// is "request-review" and the clone's absent case is allow-by-default, so
// dropping the key would turn a review request into a silent approval.
// `colorScheme` is the visual one — the reference's default is "terminal"
// and the clone's absent case is the palette's own default scheme.
func TestNormalizeKeepsKeysWhoseAbsentCaseDiffers(t *testing.T) {
	path := scratchSettings(t)
	writeSettings(t, path, `{"toolPermission":"request-review","colorScheme":"terminal"}`, 0600)

	if err := NormalizeOnStartup(); err != nil {
		t.Fatalf("NormalizeOnStartup: %v", err)
	}

	got := readSettings(t, path)
	if !strings.Contains(got, `"toolPermission": "request-review"`) {
		t.Errorf("toolPermission dropped: %s", got)
	}
	if !strings.Contains(got, `"colorScheme": "terminal"`) {
		t.Errorf("colorScheme dropped: %s", got)
	}
}

// TestKeptAtDefaultNamesKnownKeys pins the keep-list against the defaults
// table: a key that is kept must be a key the reference knows, or the
// list is a typo that hides a real key.
func TestKeptAtDefaultNamesKnownKeys(t *testing.T) {
	for key := range keptAtDefault {
		if _, ok := defaults[key]; !ok {
			t.Errorf("kept key %q has no default", key)
		}
		if !ValidKeys[key] {
			t.Errorf("kept key %q is not a valid key", key)
		}
	}
}

// TestNormalizeDropsNullDefaults pins the seven nullable keys: their
// default is null, so a null value is the default and the key goes.
func TestNormalizeDropsNullDefaults(t *testing.T) {
	path := scratchSettings(t)
	writeSettings(t, path, `{"statusLine":null,"title":null,"gcp":null}`, 0600)

	if err := NormalizeOnStartup(); err != nil {
		t.Fatalf("NormalizeOnStartup: %v", err)
	}

	got := readSettings(t, path)
	if strings.Contains(got, "statusLine") || strings.Contains(got, "title") || strings.Contains(got, "gcp") {
		t.Errorf("null defaults survived: %s", got)
	}
}

// TestNormalizeKeepsUnknownKeys pins the reference's own rule: a key the
// CLI does not know is kept, whatever it holds.
func TestNormalizeKeepsUnknownKeys(t *testing.T) {
	path := scratchSettings(t)
	writeSettings(t, path, `{"$schema":"https://example.invalid/s.json","futureKey":{"a":1}}`, 0600)

	if err := NormalizeOnStartup(); err != nil {
		t.Fatalf("NormalizeOnStartup: %v", err)
	}

	got := readSettings(t, path)
	if !strings.Contains(got, "$schema") {
		t.Errorf("unknown key dropped: %s", got)
	}
	if !strings.Contains(got, "futureKey") {
		t.Errorf("unknown key dropped: %s", got)
	}
}

// TestNormalizeLeavesCanonicalFileAlone pins the no-churn rule: a file that
// is already in the clone's own format and holds no default value is not
// written at all — its bytes and its mtime stay the same.
func TestNormalizeLeavesCanonicalFileAlone(t *testing.T) {
	path := scratchSettings(t)
	const canonical = "{\n  \"verbosity\": \"high\"\n}"
	writeSettings(t, path, canonical, 0600)

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if err := NormalizeOnStartup(); err != nil {
		t.Fatalf("NormalizeOnStartup: %v", err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := readSettings(t, path); got != canonical {
		t.Errorf("canonical file rewritten:\ngot  %q\nwant %q", got, canonical)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("canonical file rewritten: mtime %v != %v", after.ModTime(), before.ModTime())
	}
}

// TestNormalizeLeavesMalformedFileAlone pins the safety rule: a file that
// is not a JSON object is reported and kept byte for byte — a settings
// file that cannot be parsed must never be truncated.
func TestNormalizeLeavesMalformedFileAlone(t *testing.T) {
	path := scratchSettings(t)
	const broken = `{"colorScheme": `
	writeSettings(t, path, broken, 0600)

	if err := NormalizeOnStartup(); err == nil {
		t.Fatal("NormalizeOnStartup on a malformed file: got nil error, want an error")
	}

	if got := readSettings(t, path); got != broken {
		t.Errorf("malformed file changed: got %q, want %q", got, broken)
	}
}

// TestNormalizeLeavesNonObjectFileAlone pins the same rule for a
// well-formed document that is not an object.
func TestNormalizeLeavesNonObjectFileAlone(t *testing.T) {
	path := scratchSettings(t)
	const list = "[1,2,3]"
	writeSettings(t, path, list, 0600)

	if err := NormalizeOnStartup(); err == nil {
		t.Fatal("NormalizeOnStartup on a non-object file: got nil error, want an error")
	}

	if got := readSettings(t, path); got != list {
		t.Errorf("non-object file changed: got %q, want %q", got, list)
	}
}

// TestNormalizeMissingFileCreatesNothing pins the no-creation rule: a
// missing settings file is left missing — the start-up path must not
// create one.
func TestNormalizeMissingFileCreatesNothing(t *testing.T) {
	path := scratchSettings(t)

	if err := NormalizeOnStartup(); err != nil {
		t.Fatalf("NormalizeOnStartup: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("missing file: stat error = %v, want not-exist", err)
	}
}

// TestNormalizeWritesOwnerOnlyMode pins the reference's own mode: the
// settings file is readable by its owner only, even when it was not.
func TestNormalizeWritesOwnerOnlyMode(t *testing.T) {
	path := scratchSettings(t)
	writeSettings(t, path, `{"colorScheme":"light"}`, 0644)

	if err := NormalizeOnStartup(); err != nil {
		t.Fatalf("NormalizeOnStartup: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("mode = %o, want 600", got)
	}
}

// TestNormalizeRewritesForeignFormat pins the format rule: the file is
// rewritten in the clone's own format — two-space indent, keys in
// alphabetical order.
func TestNormalizeRewritesForeignFormat(t *testing.T) {
	path := scratchSettings(t)
	writeSettings(t, path, `{"verbosity":"high","editorMode":"vim"}`, 0600)

	if err := NormalizeOnStartup(); err != nil {
		t.Fatalf("NormalizeOnStartup: %v", err)
	}

	want := "{\n  \"editorMode\": \"vim\",\n  \"verbosity\": \"high\"\n}"
	if got := strings.TrimSpace(readSettings(t, path)); got != want {
		t.Errorf("rewritten file:\ngot  %q\nwant %q", got, want)
	}
}
