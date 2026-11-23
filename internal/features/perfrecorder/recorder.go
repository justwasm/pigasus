// Package perfrecorder provides a lightweight, thread-safe ring-buffer
// performance recorder and diagnostic bundle exporter for pigasus.
package perfrecorder

import (
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultCapacity is the default number of frame samples retained in the ring buffer.
	DefaultCapacity = 2048
	// DefaultRuntimeCapacity is the default number of runtime snapshots retained in the ring buffer.
	DefaultRuntimeCapacity = 2048
	// DefaultSlowEventCapacity is the default number of slow-frame events retained in the ring buffer.
	DefaultSlowEventCapacity = 256
	// DefaultSlowThreshold is the default duration above which a frame is recorded as slow.
	DefaultSlowThreshold = 50 * time.Millisecond
)

// FrameKind identifies the phase of a recorded frame sample.
type FrameKind string

const (
	FrameUpdate FrameKind = "update"
	FrameView   FrameKind = "view"
)

// FrameSample records a single Update or View execution duration.
type FrameSample struct {
	Timestamp time.Time     `json:"timestamp"`
	Kind      FrameKind     `json:"kind"`
	Label     string        `json:"label"`
	Duration  time.Duration `json:"duration"`
	Slow      bool          `json:"slow,omitempty"`
}

// SlowEvent records a frame whose duration exceeded the configured slow threshold.
type SlowEvent struct {
	Timestamp time.Time     `json:"timestamp"`
	Kind      FrameKind     `json:"kind"`
	Label     string        `json:"label"`
	Duration  time.Duration `json:"duration"`
}

// RuntimeSample captures a point-in-time Go runtime memory and goroutine snapshot.
type RuntimeSample struct {
	Timestamp    time.Time `json:"timestamp"`
	HeapAlloc    uint64    `json:"heap_alloc_bytes"`
	HeapInuse    uint64    `json:"heap_inuse_bytes"`
	Sys          uint64    `json:"sys_bytes"`
	NumGC        uint32    `json:"num_gc"`
	PauseTotalNs uint64    `json:"pause_total_ns"`
	NumGoroutine int       `json:"num_goroutine"`
}

// LatencyStats summarizes latency percentiles for a set of frame samples.
type LatencyStats struct {
	Count int           `json:"count"`
	P50   time.Duration `json:"-"`
	P95   time.Duration `json:"-"`
	P99   time.Duration `json:"-"`
	Max   time.Duration `json:"-"`
	P50Ms float64       `json:"p50_ms"`
	P95Ms float64       `json:"p95_ms"`
	P99Ms float64       `json:"p99_ms"`
	MaxMs float64       `json:"max_ms"`
}

// Summary holds aggregated performance statistics and runtime metadata.
type Summary struct {
	GeneratedAt       time.Time          `json:"generated_at"`
	StartedAt         time.Time          `json:"started_at"`
	UptimeSeconds     float64            `json:"uptime_seconds"`
	GoVersion         string             `json:"go_version"`
	GOOS              string             `json:"goos"`
	GOARCH            string             `json:"goarch"`
	Enabled           bool               `json:"enabled"`
	Capacity          int                `json:"capacity"`
	SlowThresholdMs   float64            `json:"slow_threshold_ms"`
	Update            LatencyStats       `json:"update"`
	View              LatencyStats       `json:"view"`
	SlowFrameCount    int64              `json:"slow_frame_count"`
	TotalUpdateCount  int64              `json:"total_update_count"`
	TotalViewCount    int64              `json:"total_view_count"`
	PeakHeapAlloc     uint64             `json:"peak_heap_alloc_bytes"`
	PeakHeapInuse     uint64             `json:"peak_heap_inuse_bytes"`
	CurrentGoroutines int                `json:"current_goroutines"`
	Metrics           map[string]float64 `json:"metrics,omitempty"`
	RecentSlowEvents  []SlowEvent        `json:"recent_slow_events,omitempty"`
}

// Option configures a Recorder created by New.
type Option func(*options)

type options struct {
	enabled         bool
	capacity        int
	runtimeCapacity int
	slowEventCap    int
	slowThreshold   time.Duration
	now             func() time.Time
	onDump          func(path string, err error)
}

// WithEnabled sets whether the recorder starts enabled (default true).
func WithEnabled(enabled bool) Option {
	return func(o *options) {
		o.enabled = enabled
	}
}

// WithCapacity sets the frame ring-buffer capacity (default DefaultCapacity).
func WithCapacity(capacity int) Option {
	return func(o *options) {
		if capacity > 0 {
			o.capacity = capacity
		}
	}
}

// WithRuntimeCapacity sets the runtime snapshot ring-buffer capacity (default DefaultRuntimeCapacity).
func WithRuntimeCapacity(capacity int) Option {
	return func(o *options) {
		if capacity > 0 {
			o.runtimeCapacity = capacity
		}
	}
}

// WithSlowEventCapacity sets the slow-event ring-buffer capacity (default DefaultSlowEventCapacity).
func WithSlowEventCapacity(capacity int) Option {
	return func(o *options) {
		if capacity > 0 {
			o.slowEventCap = capacity
		}
	}
}

// WithSlowThreshold sets the duration threshold above which frames are flagged as slow (default 50ms).
func WithSlowThreshold(d time.Duration) Option {
	return func(o *options) {
		if d >= 0 {
			o.slowThreshold = d
		}
	}
}

// WithNow overrides the clock function used for timestamps (defaults to time.Now).
func WithNow(fn func() time.Time) Option {
	return func(o *options) {
		if fn != nil {
			o.now = fn
		}
	}
}

// WithOnDump sets an optional callback invoked after DumpOnSignal writes a bundle.
func WithOnDump(fn func(path string, err error)) Option {
	return func(o *options) {
		o.onDump = fn
	}
}

type ringBuffer[T any] struct {
	buf   []T
	cap   int
	head  int
	count int
}

func newRingBuffer[T any](capacity int) ringBuffer[T] {
	return ringBuffer[T]{
		buf: make([]T, capacity),
		cap: capacity,
	}
}

func (rb *ringBuffer[T]) push(v T) {
	if rb.cap <= 0 {
		return
	}
	rb.buf[rb.head] = v
	rb.head = (rb.head + 1) % rb.cap
	if rb.count < rb.cap {
		rb.count++
	}
}

func (rb *ringBuffer[T]) snapshot() []T {
	if rb.count == 0 {
		return nil
	}
	out := make([]T, rb.count)
	start := (rb.head - rb.count + rb.cap) % rb.cap
	for i := 0; i < rb.count; i++ {
		out[i] = rb.buf[(start+i)%rb.cap]
	}
	return out
}

func (rb *ringBuffer[T]) reset() {
	clear(rb.buf)
	rb.head = 0
	rb.count = 0
}

// Recorder collects frame latencies, slow-frame events, runtime snapshots,
// and custom metrics in bounded ring buffers.
type Recorder struct {
	enabled atomic.Bool

	now           func() time.Time
	startedAt     time.Time
	slowThreshold time.Duration

	mu             sync.Mutex
	frames         ringBuffer[FrameSample]
	runtimeSamples ringBuffer[RuntimeSample]
	slowEvents     ringBuffer[SlowEvent]
	metrics        map[string]float64
	totalUpdates   int64
	totalViews     int64
	slowCount      int64
	peakHeapAlloc  uint64
	peakHeapInuse  uint64
	onDump         func(path string, err error)
}

// New constructs a Recorder with the provided options. By default, the recorder
// is enabled with DefaultCapacity (2048) and DefaultSlowThreshold (50ms).
func New(opts ...Option) *Recorder {
	cfg := options{
		enabled:         true,
		capacity:        DefaultCapacity,
		runtimeCapacity: DefaultRuntimeCapacity,
		slowEventCap:    DefaultSlowEventCapacity,
		slowThreshold:   DefaultSlowThreshold,
		now:             time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	r := &Recorder{
		now:            cfg.now,
		startedAt:      cfg.now(),
		slowThreshold:  cfg.slowThreshold,
		frames:         newRingBuffer[FrameSample](cfg.capacity),
		runtimeSamples: newRingBuffer[RuntimeSample](cfg.runtimeCapacity),
		slowEvents:     newRingBuffer[SlowEvent](cfg.slowEventCap),
		metrics:        make(map[string]float64),
		onDump:         cfg.onDump,
	}
	r.enabled.Store(cfg.enabled)
	return r
}

// Enabled reports whether recording is currently active.
func (r *Recorder) Enabled() bool {
	return r != nil && r.enabled.Load()
}

// SetEnabled toggles recording on or off. When disabled, Record* and
// SampleRuntime return immediately without locking or allocating.
func (r *Recorder) SetEnabled(enabled bool) {
	if r == nil {
		return
	}
	r.enabled.Store(enabled)
}

// SetOnDump sets or replaces the callback invoked by DumpOnSignal.
func (r *Recorder) SetOnDump(fn func(path string, err error)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.onDump = fn
	r.mu.Unlock()
}

// RecordUpdate records a Bubble Tea Update duration for the given message type.
func (r *Recorder) RecordUpdate(msgType string, d time.Duration) {
	if !r.Enabled() {
		return
	}
	now := r.now()
	slow := r.slowThreshold > 0 && d > r.slowThreshold

	r.mu.Lock()
	r.totalUpdates++
	if slow {
		r.slowCount++
		r.slowEvents.push(SlowEvent{
			Timestamp: now,
			Kind:      FrameUpdate,
			Label:     msgType,
			Duration:  d,
		})
	}
	r.frames.push(FrameSample{
		Timestamp: now,
		Kind:      FrameUpdate,
		Label:     msgType,
		Duration:  d,
		Slow:      slow,
	})
	r.mu.Unlock()
}

// RecordView records a Bubble Tea View render duration.
func (r *Recorder) RecordView(d time.Duration) {
	if !r.Enabled() {
		return
	}
	now := r.now()
	slow := r.slowThreshold > 0 && d > r.slowThreshold

	r.mu.Lock()
	r.totalViews++
	if slow {
		r.slowCount++
		r.slowEvents.push(SlowEvent{
			Timestamp: now,
			Kind:      FrameView,
			Label:     "view",
			Duration:  d,
		})
	}
	r.frames.push(FrameSample{
		Timestamp: now,
		Kind:      FrameView,
		Label:     "view",
		Duration:  d,
		Slow:      slow,
	})
	r.mu.Unlock()
}

// SampleRuntime captures a Go runtime memory and goroutine snapshot and stores
// it in the runtime ring buffer.
func (r *Recorder) SampleRuntime() RuntimeSample {
	if !r.Enabled() {
		return RuntimeSample{}
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	sample := RuntimeSample{
		Timestamp:    r.now(),
		HeapAlloc:    ms.HeapAlloc,
		HeapInuse:    ms.HeapInuse,
		Sys:          ms.Sys,
		NumGC:        ms.NumGC,
		PauseTotalNs: ms.PauseTotalNs,
		NumGoroutine: runtime.NumGoroutine(),
	}

	r.mu.Lock()
	r.runtimeSamples.push(sample)
	if sample.HeapAlloc > r.peakHeapAlloc {
		r.peakHeapAlloc = sample.HeapAlloc
	}
	if sample.HeapInuse > r.peakHeapInuse {
		r.peakHeapInuse = sample.HeapInuse
	}
	r.mu.Unlock()

	return sample
}

// RecordMetric sets a named custom counter or gauge value.
func (r *Recorder) RecordMetric(name string, val float64) {
	if !r.Enabled() || name == "" {
		return
	}
	r.mu.Lock()
	r.metrics[name] = val
	r.mu.Unlock()
}

// AddMetric increments a named custom counter by delta.
func (r *Recorder) AddMetric(name string, delta float64) {
	if !r.Enabled() || name == "" {
		return
	}
	r.mu.Lock()
	r.metrics[name] += delta
	r.mu.Unlock()
}

// Frames returns a chronological copy of the frame samples currently in the ring buffer.
func (r *Recorder) Frames() []FrameSample {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.frames.snapshot()
}

// RuntimeSamples returns a chronological copy of the runtime snapshots currently in the ring buffer.
func (r *Recorder) RuntimeSamples() []RuntimeSample {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runtimeSamples.snapshot()
}

// SlowEvents returns a chronological copy of recent slow-frame events.
func (r *Recorder) SlowEvents() []SlowEvent {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.slowEvents.snapshot()
}

// Reset clears all recorded samples, slow events, counters, and metrics.
func (r *Recorder) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.frames.reset()
	r.runtimeSamples.reset()
	r.slowEvents.reset()
	clear(r.metrics)
	r.totalUpdates = 0
	r.totalViews = 0
	r.slowCount = 0
	r.peakHeapAlloc = 0
	r.peakHeapInuse = 0
	r.mu.Unlock()
}

// Summary computes aggregated latency percentiles, slow-frame counts, peak heap,
// current goroutine count, and runtime metadata.
func (r *Recorder) Summary() Summary {
	if r == nil {
		return Summary{
			GoVersion:         runtime.Version(),
			GOOS:              runtime.GOOS,
			GOARCH:            runtime.GOARCH,
			CurrentGoroutines: runtime.NumGoroutine(),
		}
	}

	now := r.now()
	r.mu.Lock()
	frames := r.frames.snapshot()
	slowEvents := r.slowEvents.snapshot()
	totalUpdates := r.totalUpdates
	totalViews := r.totalViews
	slowCount := r.slowCount
	peakHeapAlloc := r.peakHeapAlloc
	peakHeapInuse := r.peakHeapInuse
	capacity := r.frames.cap
	var metricsCopy map[string]float64
	if len(r.metrics) > 0 {
		metricsCopy = make(map[string]float64, len(r.metrics))
		for k, v := range r.metrics {
			metricsCopy[k] = v
		}
	}
	r.mu.Unlock()

	var updateDurs, viewDurs []time.Duration
	for _, f := range frames {
		switch f.Kind {
		case FrameUpdate:
			updateDurs = append(updateDurs, f.Duration)
		case FrameView:
			viewDurs = append(viewDurs, f.Duration)
		}
	}

	uptime := now.Sub(r.startedAt).Seconds()
	if uptime < 0 {
		uptime = 0
	}

	return Summary{
		GeneratedAt:       now.UTC(),
		StartedAt:         r.startedAt.UTC(),
		UptimeSeconds:     uptime,
		GoVersion:         runtime.Version(),
		GOOS:              runtime.GOOS,
		GOARCH:            runtime.GOARCH,
		Enabled:           r.Enabled(),
		Capacity:          capacity,
		SlowThresholdMs:   durationToMs(r.slowThreshold),
		Update:            computeLatencyStats(updateDurs),
		View:              computeLatencyStats(viewDurs),
		SlowFrameCount:    slowCount,
		TotalUpdateCount:  totalUpdates,
		TotalViewCount:    totalViews,
		PeakHeapAlloc:     peakHeapAlloc,
		PeakHeapInuse:     peakHeapInuse,
		CurrentGoroutines: runtime.NumGoroutine(),
		Metrics:           metricsCopy,
		RecentSlowEvents:  slowEvents,
	}
}

func computeLatencyStats(durs []time.Duration) LatencyStats {
	n := len(durs)
	if n == 0 {
		return LatencyStats{}
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	p50 := percentile(durs, 50)
	p95 := percentile(durs, 95)
	p99 := percentile(durs, 99)
	maxD := durs[n-1]
	return LatencyStats{
		Count: n,
		P50:   p50,
		P95:   p95,
		P99:   p99,
		Max:   maxD,
		P50Ms: durationToMs(p50),
		P95Ms: durationToMs(p95),
		P99Ms: durationToMs(p99),
		MaxMs: durationToMs(maxD),
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100.0*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

func durationToMs(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
