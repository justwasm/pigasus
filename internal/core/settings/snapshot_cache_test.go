package settings

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newSnapshotCacheForTest(t *testing.T, body string) (*settingsSnapshotCache, string, *atomic.Int64, *atomic.Int64, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var statCalls, readCalls atomic.Int64
	now := time.Now()
	cache := newSettingsSnapshotCache()
	cache.now = func() time.Time { return now }
	cache.stat = func(path string) (os.FileInfo, error) {
		statCalls.Add(1)
		return os.Stat(path)
	}
	cache.read = func(path string) ([]byte, error) {
		readCalls.Add(1)
		return os.ReadFile(path)
	}
	return cache, path, &statCalls, &readCalls, &now
}

func TestSettingsSnapshotWarmCacheAvoidsFilesystemWork(t *testing.T) {
	cache, path, stats, reads, _ := newSnapshotCacheForTest(t, `{"verbosity":"low"}`)
	overlay := map[string]any(nil)
	getOverlay := func() map[string]any { return cloneSettingsMap(overlay) }

	first := cache.get(path, getOverlay)
	if got := first.String("verbosity"); got != "low" {
		t.Fatalf("initial verbosity = %q", got)
	}
	for i := 0; i < 20_000; i++ {
		if got := cache.get(path, getOverlay).String("verbosity"); got != "low" {
			t.Fatalf("warm verbosity = %q", got)
		}
	}
	if got := stats.Load(); got != 1 {
		t.Fatalf("stat calls = %d, want 1", got)
	}
	if got := reads.Load(); got != 1 {
		t.Fatalf("read calls = %d, want 1", got)
	}
}

func TestSettingsSnapshotRefreshesAfterInvalidationAndOverlayChange(t *testing.T) {
	cache, path, stats, reads, _ := newSnapshotCacheForTest(t, `{"verbosity":"low"}`)
	overlay := map[string]any(nil)
	get := func() *SettingsSnapshot {
		return cache.get(path, func() map[string]any { return cloneSettingsMap(overlay) })
	}
	if got := get().String("verbosity"); got != "low" {
		t.Fatal(got)
	}
	overlay = map[string]any{"verbosity": "session"}
	cache.invalidate()
	if got := get().String("verbosity"); got != "session" {
		t.Fatalf("overlay verbosity = %q", got)
	}
	if err := os.WriteFile(path, []byte(`{"verbosity":"external"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cache.invalidate()
	if got := get().String("verbosity"); got != "session" {
		t.Fatalf("overlay should win after invalidation, got %q", got)
	}
	if stats.Load() != 3 || reads.Load() != 3 {
		t.Fatalf("filesystem operations = stat %d/read %d, want 3/3", stats.Load(), reads.Load())
	}
}

func TestSettingsSnapshotEventuallyDetectsExternalChangeAndPathSwitch(t *testing.T) {
	cache, firstPath, stats, reads, now := newSnapshotCacheForTest(t, `{"value":"first"}`)
	get := func(path string) *SettingsSnapshot { return cache.get(path, func() map[string]any { return nil }) }
	if got := get(firstPath).String("value"); got != "first" {
		t.Fatal(got)
	}
	if err := os.WriteFile(firstPath, []byte(`{"value":"updated-size"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := get(firstPath).String("value"); got != "first" {
		t.Fatalf("changed before refresh window: %q", got)
	}
	*now = now.Add(settingsSnapshotRefreshInterval)
	if got := get(firstPath).String("value"); got != "updated-size" {
		t.Fatalf("not refreshed after interval: %q", got)
	}

	secondPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(secondPath, []byte(`{"value":"second-path"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := get(secondPath).String("value"); got != "second-path" {
		t.Fatalf("path switch value = %q", got)
	}
	if stats.Load() != 3 || reads.Load() != 3 {
		t.Fatalf("filesystem operations = stat %d/read %d, want 3/3", stats.Load(), reads.Load())
	}
}

func TestSettingsSnapshotMissingCreateDeleteAndBadConfigRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	var stats, reads atomic.Int64
	cache := newSettingsSnapshotCache()
	cache.stat = func(path string) (os.FileInfo, error) { stats.Add(1); return os.Stat(path) }
	cache.read = func(path string) ([]byte, error) { reads.Add(1); return os.ReadFile(path) }
	cache.now = func() time.Time {
		return time.Unix(1, 0).Add(time.Duration(stats.Load()) * settingsSnapshotRefreshInterval)
	}
	get := func() *SettingsSnapshot { return cache.get(path, func() map[string]any { return nil }) }
	if snap := get(); snap.Err() != nil || snap.Has("value") {
		t.Fatalf("missing snapshot = (%v, has value %v)", snap.Err(), snap.Has("value"))
	}
	if err := os.WriteFile(path, []byte(`{"value":"created"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := get().String("value"); got != "created" {
		t.Fatalf("created file value = %q", got)
	}
	if err := os.WriteFile(path, []byte(`{"value":`), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := get()
	if bad.Err() == nil || bad.String("value") != "created" {
		t.Fatalf("bad config should retain last good value; err=%v value=%q", bad.Err(), bad.String("value"))
	}
	readsBefore := reads.Load()
	badAgain := get()
	if badAgain.Err() == nil || reads.Load() != readsBefore {
		t.Fatalf("bad stamp was not negatively cached: err=%v reads=%d before=%d", badAgain.Err(), reads.Load(), readsBefore)
	}
	if err := os.WriteFile(path, []byte(`{"value":"recovered"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := get().String("value"); got != "recovered" || get().Err() != nil {
		t.Fatalf("recovery value = %q, err=%v", got, get().Err())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if snap := get(); snap.Err() != nil || snap.Has("value") {
		t.Fatalf("deleted file snapshot = (%v, has value %v)", snap.Err(), snap.Has("value"))
	}
}

func TestCachedSnapshotAndExplicitInvalidateCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	path := filepath.Join(home, "settings.json")
	if err := os.WriteFile(path, []byte(`{"value":"initial"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cache, _, stats, reads, _ := newSnapshotCacheForTest(t, `{"value":"unused"}`)
	cache.stat = func(path string) (os.FileInfo, error) { stats.Add(1); return os.Stat(path) }
	cache.read = func(path string) ([]byte, error) { reads.Add(1); return os.ReadFile(path) }
	old := cachedSettingsSnapshot
	cachedSettingsSnapshot = cache
	t.Cleanup(func() { cachedSettingsSnapshot = old; SetSessionOverlay(nil); InvalidateCache() })

	if got := CachedSnapshot().String("value"); got != "initial" {
		t.Fatal(got)
	}
	SetSessionOverlay(map[string]any{"value": "overlay"})
	if got := CachedSnapshot().String("value"); got != "overlay" {
		t.Fatalf("overlay value = %q", got)
	}
	SetSessionOverlay(nil)
	if got := CachedSnapshot().String("value"); got != "initial" {
		t.Fatalf("cleared overlay value = %q", got)
	}
	if err := os.WriteFile(path, []byte(`{"value":"new"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	InvalidateCache()
	if got := CachedSnapshot().String("value"); got != "new" {
		t.Fatalf("explicit invalidation value = %q", got)
	}
	if stats.Load() != 4 || reads.Load() != 4 {
		t.Fatalf("filesystem operations = stat %d/read %d, want 4/4", stats.Load(), reads.Load())
	}
}

func TestSettingsSnapshotConcurrentReadsAndInvalidation(t *testing.T) {
	cache, path, _, _, _ := newSnapshotCacheForTest(t, `{"nested":{"value":"stable"}}`)
	get := func() *SettingsSnapshot { return cache.get(path, func() map[string]any { return nil }) }
	if got := get().String("nested.value"); got != "stable" {
		t.Fatal(got)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if j%31 == 0 {
					cache.invalidate()
				}
				if got := get().String("nested.value"); got != "stable" {
					t.Errorf("snapshot value = %q", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkSettings_CachedSnapshot(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"verbosity":"medium","nested":{"enabled":true}}`), 0o600); err != nil {
		b.Fatal(err)
	}
	cache := newSettingsSnapshotCache()
	cache.get(path, func() map[string]any { return nil })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.get(path, func() map[string]any { return nil }).String("verbosity")
	}
}

func TestSettingsSnapshotMtimeAndReadFailureRecovery(t *testing.T) {
	cache, path, _, reads, now := newSnapshotCacheForTest(t, `{"value":"first"}`)
	get := func() *SettingsSnapshot { return cache.get(path, func() map[string]any { return nil }) }
	get()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"value":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime().Add(time.Second)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(cache.interval)
	if got := get().String("value"); got != "other" {
		t.Fatalf("same-size mtime update = %q", got)
	}

	failRead := true
	cache.read = func(path string) ([]byte, error) {
		reads.Add(1)
		if failRead {
			return nil, &os.PathError{Op: "read", Path: path, Err: os.ErrPermission}
		}
		return os.ReadFile(path)
	}
	cache.invalidate()
	if snap := get(); snap.Err() == nil || snap.String("value") != "other" {
		t.Fatalf("read failure = (%v, %q)", snap.Err(), snap.String("value"))
	}
	before := reads.Load()
	for i := 0; i < 1_000; i++ {
		get()
	}
	if reads.Load() != before {
		t.Fatal("read errors retried within refresh interval")
	}
	failRead = false
	*now = now.Add(cache.interval)
	if snap := get(); snap.Err() != nil || snap.String("value") != "other" {
		t.Fatalf("unchanged-stamp read recovery = (%v, %q)", snap.Err(), snap.String("value"))
	}
}

func TestSettingsSnapshotCompoundValuesAndOverlayAreIsolated(t *testing.T) {
	cache, path, _, _, _ := newSnapshotCacheForTest(t, `{"object":{"value":"disk"},"list":[{"value":"disk"}],"legacy":"on","null":null,"model":" model "}`)
	overlay := map[string]any{"object": map[string]any{"value": "session"}}
	get := func() *SettingsSnapshot { return cache.get(path, func() map[string]any { return overlay }) }
	snap := get()
	if got := snap.String("object.value"); got != "session" {
		t.Fatal(got)
	}
	overlay["object"].(map[string]any)["value"] = "caller mutation"
	value, err := snap.Field("object")
	if err != nil {
		t.Fatal(err)
	}
	value.(map[string]any)["value"] = "returned map mutation"
	list, _ := snap.Field("list")
	list.([]any)[0].(map[string]any)["value"] = "returned slice mutation"
	if got := snap.String("object.value"); got != "session" {
		t.Fatalf("snapshot mutated through overlay or returned value: %q", got)
	}
	list, _ = snap.Field("list")
	if got := list.([]any)[0].(map[string]any)["value"]; got != "disk" {
		t.Fatalf("snapshot mutated through returned slice: %v", got)
	}
	if value, ok := snap.Bool("legacy"); !ok || !value {
		t.Fatalf("legacy Bool = %v/%v", value, ok)
	}
	if !snap.Has("null") || snap.Has("missing") || snap.ResolveModel("fallback") != "model" {
		t.Fatal("snapshot accessors changed presence/model semantics")
	}
}

func TestCachedSnapshotLocalWritesAndConcurrentOverlayUpdates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("OPENAGY_DATA_DIR", "")
	t.Setenv("ANTIGRAVITY_DATA_DIR", "")
	path := filepath.Join(home, "settings.json")
	if err := os.WriteFile(path, []byte(`{"verbosity":"disk"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	SetSessionOverlay(nil)
	t.Cleanup(func() { SetSessionOverlay(nil) })
	if got := CachedSnapshot().String("verbosity"); got != "disk" {
		t.Fatal(got)
	}
	if err := SetString("verbosity", "saved"); err != nil {
		t.Fatal(err)
	}
	if got := CachedSnapshot().String("verbosity"); got != "saved" {
		t.Fatalf("local write not visible immediately: %q", got)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				SetSessionOverlay(map[string]any{"verbosity": "session"})
				if got := CachedSnapshot().String("verbosity"); got != "session" {
					t.Errorf("concurrent overlay = %q", got)
				}
				_ = String("verbosity")
			}
		}()
	}
	wg.Wait()
	if err := SetField("verbosity", "updated-session"); err != nil {
		t.Fatal(err)
	}
	if got := CachedSnapshot().String("verbosity"); got != "updated-session" {
		t.Fatalf("SetField overlay update not visible: %q", got)
	}
	if err := DeleteField("verbosity"); err != nil {
		t.Fatal(err)
	}
	if CachedSnapshot().Has("verbosity") {
		t.Fatal("DeleteField not visible immediately")
	}
}
