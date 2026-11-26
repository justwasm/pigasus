package models

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newRegistryCacheForTest(t *testing.T, body string) (*registryCache, string, *atomic.Int64, *atomic.Int64, *time.Time) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := newRegistryCache(250 * time.Millisecond)
	now := time.Now()
	cache.now = func() time.Time { return now }
	var stats, reads atomic.Int64
	cache.stat = func(path string) (os.FileInfo, error) { stats.Add(1); return os.Stat(path) }
	cache.read = func(path string) ([]byte, error) { reads.Add(1); return os.ReadFile(path) }
	return cache, path, &stats, &reads, &now
}

func TestRegistrySnapshotWarmAvoidsFilesystemWork(t *testing.T) {
	cache, path, stats, reads, _ := newRegistryCacheForTest(t, `{"providers":{"test":{"models":["first"]}}}`)
	first := cache.get(path)
	if first.Len() != 1 {
		t.Fatalf("initial registry size = %d", first.Len())
	}
	for i := 0; i < 20_000; i++ {
		if got := cache.get(path); got != first {
			t.Fatal("warm registry was rebuilt")
		}
	}
	if stats.Load() != 1 || reads.Load() != 1 {
		t.Fatalf("filesystem operations = stat %d/read %d, want 1/1", stats.Load(), reads.Load())
	}
}

func TestRegistrySnapshotEventuallyRefreshesByMtimeAndSize(t *testing.T) {
	cache, path, stats, reads, now := newRegistryCacheForTest(t, `{"providers":{"test":{"models":["first"]}}}`)
	first := cache.get(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Same size, changed mtime.
	if err := os.WriteFile(path, []byte(`{"providers":{"test":{"models":["other"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	newTime := info.ModTime().Add(time.Second)
	if err := os.Chtimes(path, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	if cache.get(path) != first {
		t.Fatal("snapshot refreshed within bounded interval")
	}
	*now = now.Add(cache.interval)
	if _, ok := cache.get(path).GetByName("other"); !ok {
		t.Fatal("mtime change not detected after interval")
	}
	// Same mtime, changed size.
	if err := os.WriteFile(path, []byte(`{"providers":{"test":{"models":["much-longer-name"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(cache.interval)
	if _, ok := cache.get(path).GetByName("much-longer-name"); !ok {
		t.Fatal("size change not detected after interval")
	}
	if stats.Load() != 3 || reads.Load() != 3 {
		t.Fatalf("filesystem operations = stat %d/read %d, want 3/3", stats.Load(), reads.Load())
	}
}

func TestRegistrySnapshotBadStampNegativeCacheAndRecovery(t *testing.T) {
	cache, path, stats, reads, now := newRegistryCacheForTest(t, `{"providers":{"test":{"models":["first"]}}}`)
	good := cache.get(path)
	if err := os.WriteFile(path, []byte(`{"providers":`), 0o600); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(cache.interval)
	if got := cache.get(path); got != good {
		t.Fatal("malformed file did not retain last good registry")
	}
	for i := 0; i < 100; i++ {
		cache.get(path)
	}
	*now = now.Add(cache.interval)
	if got := cache.get(path); got != good {
		t.Fatal("unchanged malformed file lost last good registry")
	}
	if stats.Load() != 3 || reads.Load() != 2 {
		t.Fatalf("bad stamp operations = stat %d/read %d, want 3/2", stats.Load(), reads.Load())
	}
	if err := os.WriteFile(path, []byte(`{"providers":{"test":{"models":["recovered"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(cache.interval)
	if _, ok := cache.get(path).GetByName("recovered"); !ok {
		t.Fatal("valid edit did not recover registry")
	}
}

func TestRegistrySnapshotMissingCreateDeletePathSwitch(t *testing.T) {
	cache, path, stats, reads, now := newRegistryCacheForTest(t, `{"providers":{"test":{"models":["first"]}}}`)
	cache.get(path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(cache.interval)
	if got := cache.get(path); got.Len() != 0 || got.Path() != path {
		t.Fatalf("deleted file registry = size %d/path %q", got.Len(), got.Path())
	}
	if err := os.WriteFile(path, []byte(`{"providers":{"test":{"models":["created"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(cache.interval)
	if _, ok := cache.get(path).GetByName("created"); !ok {
		t.Fatal("created file not detected")
	}
	other := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(other, []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := cache.get(other); got.Len() != 0 || got.Path() != other {
		t.Fatalf("bad new path inherited old config: size %d/path %q", got.Len(), got.Path())
	}
	if stats.Load() != 4 || reads.Load() != 3 {
		t.Fatalf("filesystem operations = stat %d/read %d, want 4/3", stats.Load(), reads.Load())
	}
}

func TestRegistryCacheExplicitInvalidationAndSaveProviders(t *testing.T) {
	cache, path, stats, reads, _ := newRegistryCacheForTest(t, `{"providers":{"test":{"models":["first"]}}}`)
	t.Setenv("OPENAGY_PROVIDERS_CONFIG", path)
	oldSnapshot, oldImmediate := globalRegistryCache, globalImmediateRegistryCache
	globalRegistryCache = cache
	globalImmediateRegistryCache = newRegistryCache(0)
	t.Cleanup(func() { globalRegistryCache = oldSnapshot; globalImmediateRegistryCache = oldImmediate })
	if _, ok := CachedRegistrySnapshot().GetByName("first"); !ok {
		t.Fatal("missing initial model")
	}
	if err := os.WriteFile(path, []byte(`{"providers":{"test":{"models":["external"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	InvalidateProvidersCache()
	if _, ok := CachedRegistrySnapshot().GetByName("external"); !ok {
		t.Fatal("explicit invalidation did not refresh snapshot")
	}
	if err := SaveProviders(path, map[string]ProviderConfig{"test": {Models: []ProviderModel{{ID: "saved"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := CachedRegistrySnapshot().GetByName("saved"); !ok {
		t.Fatal("SaveProviders did not immediately refresh snapshot")
	}
	if stats.Load() != 3 || reads.Load() != 3 {
		t.Fatalf("filesystem operations = stat %d/read %d, want 3/3", stats.Load(), reads.Load())
	}
}

func TestCachedRegistryRetainsImmediateExternalEditDetection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	t.Setenv("OPENAGY_PROVIDERS_CONFIG", path)
	if err := os.WriteFile(path, []byte(`{"providers":{"test":{"models":["first"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := CachedRegistry().GetByName("first"); !ok {
		t.Fatal("missing initial model")
	}
	if err := os.WriteFile(path, []byte(`{"providers":{"test":{"models":["immediate-external"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := CachedRegistry().GetByName("immediate-external"); !ok {
		t.Fatal("CachedRegistry no longer immediately observes external edits")
	}
}

func TestRegistrySnapshotConcurrentReadsAndInvalidation(t *testing.T) {
	cache, path, _, _, _ := newRegistryCacheForTest(t, `{"providers":{"test":{"models":["stable"]}}}`)
	cache.get(path)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if j%31 == 0 {
					cache.invalidate(path)
				}
				if _, ok := cache.get(path).GetByName("stable"); !ok {
					t.Error("concurrent snapshot lost model")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkRegistry_CachedSnapshot(b *testing.B) {
	path := filepath.Join(b.TempDir(), "providers.json")
	if err := os.WriteFile(path, []byte(`{"providers":{"test":{"models":["first"]}}}`), 0o600); err != nil {
		b.Fatal(err)
	}
	cache := newRegistryCache(250 * time.Millisecond)
	cache.get(path)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.get(path)
	}
}

func TestRegistrySnapshotReadFailureRecoveryWithoutStampChange(t *testing.T) {
	cache, path, _, reads, now := newRegistryCacheForTest(t, `{"providers":{"test":{"models":["stable"]}}}`)
	good := cache.get(path)
	failRead := true
	cache.read = func(path string) ([]byte, error) {
		reads.Add(1)
		if failRead {
			return nil, &os.PathError{Op: "read", Path: path, Err: os.ErrPermission}
		}
		return os.ReadFile(path)
	}
	cache.invalidate(path)
	if got := cache.get(path); got != good {
		t.Fatal("read error lost last good registry")
	}
	before := reads.Load()
	for i := 0; i < 1_000; i++ {
		cache.get(path)
	}
	if got := reads.Load(); got != before {
		t.Fatalf("read failures retried within window: %d, want %d", got, before)
	}
	failRead = false
	*now = now.Add(cache.interval)
	if got := cache.get(path); got == good {
		t.Fatal("unchanged-stamp read recovery was never retried")
	} else if _, ok := got.GetByName("stable"); !ok {
		t.Fatal("recovered registry missing model")
	}
}

func TestRegistrySnapshotStatErrorIsBoundedAndRecovers(t *testing.T) {
	cache, path, stats, _, now := newRegistryCacheForTest(t, `{"providers":{"test":{"models":["stable"]}}}`)
	good := cache.get(path)
	failStat := true
	cache.stat = func(path string) (os.FileInfo, error) {
		stats.Add(1)
		if failStat {
			return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrPermission}
		}
		return os.Stat(path)
	}
	cache.invalidate(path)
	if got := cache.get(path); got != good {
		t.Fatal("stat error lost last good registry")
	}
	for i := 0; i < 1_000; i++ {
		cache.get(path)
	}
	if got := stats.Load(); got != 2 {
		t.Fatalf("stat error retried per frame: calls = %d, want 2", got)
	}
	failStat = false
	*now = now.Add(cache.interval)
	if _, ok := cache.get(path).GetByName("stable"); !ok {
		t.Fatal("stat recovery lost model")
	}
	if got := stats.Load(); got != 3 {
		t.Fatalf("stat recovery calls = %d, want 3", got)
	}
}

func BenchmarkRegistry_CachedImmediate(b *testing.B) {
	path := filepath.Join(b.TempDir(), "providers.json")
	if err := os.WriteFile(path, []byte(`{"providers":{"test":{"models":["first"]}}}`), 0o600); err != nil {
		b.Fatal(err)
	}
	cache := newRegistryCache(0)
	cache.get(path)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.get(path)
	}
}
