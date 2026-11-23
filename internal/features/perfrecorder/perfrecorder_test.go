package perfrecorder

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDisabledFastPathZeroAllocs(t *testing.T) {
	r := New(WithEnabled(false))
	if r.Enabled() {
		t.Fatal("expected recorder to be disabled")
	}

	allocs := testing.AllocsPerRun(100, func() {
		r.RecordUpdate("tea.KeyPressMsg", 10*time.Millisecond)
		r.RecordView(5 * time.Millisecond)
		r.RecordMetric("tokens", 42)
		r.SampleRuntime()
	})
	if allocs != 0 {
		t.Fatalf("disabled fast path allocated %v times, want 0", allocs)
	}

	if got := len(r.Frames()); got != 0 {
		t.Fatalf("expected 0 frames when disabled, got %d", got)
	}
	if got := len(r.RuntimeSamples()); got != 0 {
		t.Fatalf("expected 0 runtime samples when disabled, got %d", got)
	}

	r.SetEnabled(true)
	if !r.Enabled() {
		t.Fatal("expected recorder to be enabled after SetEnabled(true)")
	}
	r.RecordUpdate("tea.KeyPressMsg", 2*time.Millisecond)
	if got := len(r.Frames()); got != 1 {
		t.Fatalf("expected 1 frame after enabling, got %d", got)
	}
}

func TestRingBufferWrapAroundAndPercentiles(t *testing.T) {
	base := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	tick := 0
	r := New(
		WithCapacity(100),
		WithSlowThreshold(50*time.Millisecond),
		WithNow(func() time.Time {
			tick++
			return base.Add(time.Duration(tick) * time.Millisecond)
		}),
	)

	// Push 50 old slow samples that will be evicted once 100 newer samples arrive.
	for i := 0; i < 50; i++ {
		r.RecordUpdate("old.SlowMsg", 200*time.Millisecond)
	}

	// Push 100 update samples with durations 1ms..100ms.
	// Since capacity is 100, all 50 old samples are overwritten.
	for i := 1; i <= 100; i++ {
		r.RecordUpdate("tea.KeyMsg", time.Duration(i)*time.Millisecond)
	}

	frames := r.Frames()
	if len(frames) != 100 {
		t.Fatalf("len(Frames()) = %d, want 100", len(frames))
	}
	if frames[0].Label != "tea.KeyMsg" || frames[0].Duration != 1*time.Millisecond {
		t.Fatalf("oldest retained frame = %+v, want tea.KeyMsg 1ms", frames[0])
	}
	if frames[99].Duration != 100*time.Millisecond {
		t.Fatalf("newest retained frame = %+v, want 100ms", frames[99])
	}

	sum := r.Summary()
	if sum.TotalUpdateCount != 150 {
		t.Fatalf("TotalUpdateCount = %d, want 150", sum.TotalUpdateCount)
	}
	if sum.Update.Count != 100 {
		t.Fatalf("Update.Count = %d, want 100", sum.Update.Count)
	}
	if sum.Update.P50 != 50*time.Millisecond {
		t.Errorf("Update.P50 = %v, want 50ms", sum.Update.P50)
	}
	if sum.Update.P95 != 95*time.Millisecond {
		t.Errorf("Update.P95 = %v, want 95ms", sum.Update.P95)
	}
	if sum.Update.P99 != 99*time.Millisecond {
		t.Errorf("Update.P99 = %v, want 99ms", sum.Update.P99)
	}
	if sum.Update.Max != 100*time.Millisecond {
		t.Errorf("Update.Max = %v, want 100ms", sum.Update.Max)
	}
	// 50 old slow frames (>50ms) + 50 new slow frames (51ms..100ms) = 100 slow frames.
	if sum.SlowFrameCount != 100 {
		t.Errorf("SlowFrameCount = %d, want 100", sum.SlowFrameCount)
	}
}

func TestViewPercentilesAndMetrics(t *testing.T) {
	base := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	now := base
	r := New(
		WithCapacity(200),
		WithSlowThreshold(50*time.Millisecond),
		WithNow(func() time.Time { return now }),
	)

	for i := 1; i <= 20; i++ {
		r.RecordView(time.Duration(i) * time.Millisecond)
	}
	r.RecordView(75 * time.Millisecond) // slow view

	r.RecordMetric("active_streams", 3)
	r.AddMetric("dropped_frames", 1)
	r.AddMetric("dropped_frames", 2)

	now = base.Add(10 * time.Second)
	sum := r.Summary()

	if sum.View.Count != 21 {
		t.Fatalf("View.Count = %d, want 21", sum.View.Count)
	}
	if sum.View.P50 != 11*time.Millisecond {
		t.Errorf("View.P50 = %v, want 11ms", sum.View.P50)
	}
	if sum.View.P95 != 20*time.Millisecond {
		t.Errorf("View.P95 = %v, want 20ms", sum.View.P95)
	}
	if sum.View.P99 != 75*time.Millisecond {
		t.Errorf("View.P99 = %v, want 75ms", sum.View.P99)
	}
	if sum.View.Max != 75*time.Millisecond {
		t.Errorf("View.Max = %v, want 75ms", sum.View.Max)
	}
	if sum.SlowFrameCount != 1 {
		t.Errorf("SlowFrameCount = %d, want 1", sum.SlowFrameCount)
	}
	if sum.UptimeSeconds != 10 {
		t.Errorf("UptimeSeconds = %v, want 10", sum.UptimeSeconds)
	}
	if sum.Metrics["active_streams"] != 3 {
		t.Errorf("Metrics[active_streams] = %v, want 3", sum.Metrics["active_streams"])
	}
	if sum.Metrics["dropped_frames"] != 3 {
		t.Errorf("Metrics[dropped_frames] = %v, want 3", sum.Metrics["dropped_frames"])
	}
}

func TestConcurrentRecordingUnderRace(t *testing.T) {
	r := New(WithCapacity(256), WithRuntimeCapacity(64), WithSlowThreshold(10*time.Millisecond))

	const workers = 8
	const iters = 200

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				r.RecordUpdate("msg.Tick", time.Duration(i%25)*time.Millisecond)
				r.RecordView(time.Duration(i%15) * time.Millisecond)
				r.RecordMetric("worker_iter", float64(i))
				if i%25 == 0 {
					r.SampleRuntime()
				}
				if i%50 == 0 {
					_ = r.Summary()
				}
			}
		}(w)
	}
	wg.Wait()

	sum := r.Summary()
	if sum.TotalUpdateCount != workers*iters {
		t.Fatalf("TotalUpdateCount = %d, want %d", sum.TotalUpdateCount, workers*iters)
	}
	if sum.TotalViewCount != workers*iters {
		t.Fatalf("TotalViewCount = %d, want %d", sum.TotalViewCount, workers*iters)
	}
	if sum.PeakHeapAlloc == 0 {
		t.Fatal("expected PeakHeapAlloc > 0 after SampleRuntime")
	}
	if sum.CurrentGoroutines <= 0 {
		t.Fatalf("expected CurrentGoroutines > 0, got %d", sum.CurrentGoroutines)
	}

	var buf bytes.Buffer
	if err := r.WriteBundleTo(&buf); err != nil {
		t.Fatalf("WriteBundleTo failed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty zip bundle")
	}
}

func TestWriteBundleContents(t *testing.T) {
	base := time.Date(2026, 11, 23, 15, 0, 0, 0, time.UTC)
	now := base
	r := New(
		WithCapacity(64),
		WithSlowThreshold(50*time.Millisecond),
		WithNow(func() time.Time { return now }),
	)

	r.RecordUpdate("tea.WindowSizeMsg", 12500*time.Microsecond) // 12.5ms
	now = base.Add(100 * time.Millisecond)
	r.RecordView(3250 * time.Microsecond) // 3.25ms
	now = base.Add(200 * time.Millisecond)
	r.RecordUpdate("stream.ChunkMsg", 65*time.Millisecond) // slow
	r.RecordMetric("queue_depth", 7)
	rtSample := r.SampleRuntime()
	now = base.Add(5 * time.Second)

	bundlePath := filepath.Join(t.TempDir(), "nested", "perf-bundle.zip")
	if err := r.WriteBundle(bundlePath); err != nil {
		t.Fatalf("WriteBundle failed: %v", err)
	}

	zr, err := zip.OpenReader(bundlePath)
	if err != nil {
		t.Fatalf("zip.OpenReader failed: %v", err)
	}
	defer zr.Close()

	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", f.Name, err)
		}
		files[f.Name] = data
	}

	for _, required := range []string{"summary.json", "frames.csv", "runtime.csv", "goroutines.txt"} {
		if _, ok := files[required]; !ok {
			t.Fatalf("missing required bundle entry %q", required)
		}
	}

	// Verify summary.json
	var gotSummary Summary
	if err := json.Unmarshal(files["summary.json"], &gotSummary); err != nil {
		t.Fatalf("unmarshal summary.json: %v", err)
	}
	if gotSummary.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", gotSummary.GoVersion, runtime.Version())
	}
	if gotSummary.GOOS != runtime.GOOS || gotSummary.GOARCH != runtime.GOARCH {
		t.Errorf("OS/Arch = %s/%s, want %s/%s", gotSummary.GOOS, gotSummary.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	if gotSummary.UptimeSeconds != 5 {
		t.Errorf("UptimeSeconds = %v, want 5", gotSummary.UptimeSeconds)
	}
	if gotSummary.Update.Count != 2 || gotSummary.View.Count != 1 {
		t.Errorf("counts = update:%d view:%d, want 2/1", gotSummary.Update.Count, gotSummary.View.Count)
	}
	if gotSummary.SlowFrameCount != 1 {
		t.Errorf("SlowFrameCount = %d, want 1", gotSummary.SlowFrameCount)
	}
	if gotSummary.Metrics["queue_depth"] != 7 {
		t.Errorf("Metrics[queue_depth] = %v, want 7", gotSummary.Metrics["queue_depth"])
	}

	// Verify frames.csv
	frameRows, err := csv.NewReader(bytes.NewReader(files["frames.csv"])).ReadAll()
	if err != nil {
		t.Fatalf("parse frames.csv: %v", err)
	}
	if len(frameRows) != 4 {
		t.Fatalf("frames.csv rows = %d, want 4 (header + 3 samples)", len(frameRows))
	}
	wantFrameHeader := []string{"timestamp", "kind", "label", "duration_ms"}
	if strings.Join(frameRows[0], ",") != strings.Join(wantFrameHeader, ",") {
		t.Errorf("frames.csv header = %v, want %v", frameRows[0], wantFrameHeader)
	}
	if frameRows[1][1] != "update" || frameRows[1][2] != "tea.WindowSizeMsg" || frameRows[1][3] != "12.5" {
		t.Errorf("frames.csv row 1 = %v, unexpected", frameRows[1])
	}
	if frameRows[2][1] != "view" || frameRows[2][2] != "view" || frameRows[2][3] != "3.25" {
		t.Errorf("frames.csv row 2 = %v, unexpected", frameRows[2])
	}
	if frameRows[3][1] != "update" || frameRows[3][2] != "stream.ChunkMsg" || frameRows[3][3] != "65" {
		t.Errorf("frames.csv row 3 = %v, unexpected", frameRows[3])
	}

	// Verify runtime.csv
	rtRows, err := csv.NewReader(bytes.NewReader(files["runtime.csv"])).ReadAll()
	if err != nil {
		t.Fatalf("parse runtime.csv: %v", err)
	}
	if len(rtRows) != 2 {
		t.Fatalf("runtime.csv rows = %d, want 2 (header + 1 sample)", len(rtRows))
	}
	wantRTHeader := []string{"timestamp", "heap_alloc_bytes", "heap_inuse_bytes", "sys_bytes", "num_gc", "num_goroutine"}
	if strings.Join(rtRows[0], ",") != strings.Join(wantRTHeader, ",") {
		t.Errorf("runtime.csv header = %v, want %v", rtRows[0], wantRTHeader)
	}
	gotHeapAlloc, err := strconv.ParseUint(rtRows[1][1], 10, 64)
	if err != nil || gotHeapAlloc != rtSample.HeapAlloc {
		t.Errorf("runtime.csv heap_alloc_bytes = %q (err=%v), want %d", rtRows[1][1], err, rtSample.HeapAlloc)
	}
	gotGoroutines, err := strconv.Atoi(rtRows[1][5])
	if err != nil || gotGoroutines != rtSample.NumGoroutine {
		t.Errorf("runtime.csv num_goroutine = %q (err=%v), want %d", rtRows[1][5], err, rtSample.NumGoroutine)
	}

	// Verify goroutines.txt
	goroutinesTxt := string(files["goroutines.txt"])
	if !strings.Contains(goroutinesTxt, "goroutine ") {
		t.Errorf("goroutines.txt missing stack dump header, got %q", goroutinesTxt)
	}
}
