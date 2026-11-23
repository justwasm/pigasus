package perfrecorder

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"sync"
	"time"
)

// WriteBundle writes a diagnostic .zip bundle to path atomically via a temporary
// file in the destination directory.
func (r *Recorder) WriteBundle(path string) error {
	if r == nil {
		return errors.New("perfrecorder: nil recorder")
	}
	if path == "" {
		return errors.New("perfrecorder: empty bundle path")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("perfrecorder: create bundle dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".pigasus-perf-*.zip.tmp")
	if err != nil {
		return fmt.Errorf("perfrecorder: create temp bundle: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if err := r.WriteBundleTo(tmp); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("perfrecorder: close temp bundle: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("perfrecorder: rename bundle: %w", err)
	}
	cleanup = false
	return nil
}

// WriteBundleTo writes a diagnostic .zip archive containing summary.json,
// frames.csv, runtime.csv, goroutines.txt, and best-effort heap_profile.pb.gz.
func (r *Recorder) WriteBundleTo(w io.Writer) error {
	if r == nil {
		return errors.New("perfrecorder: nil recorder")
	}
	if w == nil {
		return errors.New("perfrecorder: nil writer")
	}

	summary := r.Summary()
	frames := r.Frames()
	runtimeSamples := r.RuntimeSamples()

	zw := zip.NewWriter(w)

	if err := writeSummaryJSON(zw, summary); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeFramesCSV(zw, frames); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeRuntimeCSV(zw, runtimeSamples); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeGoroutinesTxt(zw); err != nil {
		_ = zw.Close()
		return err
	}
	// Best-effort heap profile; ignore errors so the rest of the bundle succeeds.
	_ = writeHeapProfile(zw)

	if err := zw.Close(); err != nil {
		return fmt.Errorf("perfrecorder: close zip writer: %w", err)
	}
	return nil
}

func writeSummaryJSON(zw *zip.Writer, summary Summary) error {
	fw, err := zw.Create("summary.json")
	if err != nil {
		return fmt.Errorf("perfrecorder: create summary.json: %w", err)
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("perfrecorder: marshal summary.json: %w", err)
	}
	data = append(data, '\n')
	if _, err := fw.Write(data); err != nil {
		return fmt.Errorf("perfrecorder: write summary.json: %w", err)
	}
	return nil
}

func writeFramesCSV(zw *zip.Writer, frames []FrameSample) error {
	fw, err := zw.Create("frames.csv")
	if err != nil {
		return fmt.Errorf("perfrecorder: create frames.csv: %w", err)
	}
	cw := csv.NewWriter(fw)
	if err := cw.Write([]string{"timestamp", "kind", "label", "duration_ms"}); err != nil {
		return fmt.Errorf("perfrecorder: write frames.csv header: %w", err)
	}
	for _, f := range frames {
		row := []string{
			f.Timestamp.UTC().Format(time.RFC3339Nano),
			string(f.Kind),
			f.Label,
			strconv.FormatFloat(durationToMs(f.Duration), 'f', -1, 64),
		}
		if err := cw.Write(row); err != nil {
			return fmt.Errorf("perfrecorder: write frames.csv row: %w", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("perfrecorder: flush frames.csv: %w", err)
	}
	return nil
}

func writeRuntimeCSV(zw *zip.Writer, samples []RuntimeSample) error {
	fw, err := zw.Create("runtime.csv")
	if err != nil {
		return fmt.Errorf("perfrecorder: create runtime.csv: %w", err)
	}
	cw := csv.NewWriter(fw)
	if err := cw.Write([]string{
		"timestamp",
		"heap_alloc_bytes",
		"heap_inuse_bytes",
		"sys_bytes",
		"num_gc",
		"num_goroutine",
	}); err != nil {
		return fmt.Errorf("perfrecorder: write runtime.csv header: %w", err)
	}
	for _, s := range samples {
		row := []string{
			s.Timestamp.UTC().Format(time.RFC3339Nano),
			strconv.FormatUint(s.HeapAlloc, 10),
			strconv.FormatUint(s.HeapInuse, 10),
			strconv.FormatUint(s.Sys, 10),
			strconv.FormatUint(uint64(s.NumGC), 10),
			strconv.Itoa(s.NumGoroutine),
		}
		if err := cw.Write(row); err != nil {
			return fmt.Errorf("perfrecorder: write runtime.csv row: %w", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("perfrecorder: flush runtime.csv: %w", err)
	}
	return nil
}

func writeGoroutinesTxt(zw *zip.Writer) error {
	fw, err := zw.Create("goroutines.txt")
	if err != nil {
		return fmt.Errorf("perfrecorder: create goroutines.txt: %w", err)
	}
	prof := pprof.Lookup("goroutine")
	if prof == nil {
		return nil
	}
	if err := prof.WriteTo(fw, 2); err != nil {
		return fmt.Errorf("perfrecorder: write goroutines.txt: %w", err)
	}
	return nil
}

func writeHeapProfile(zw *zip.Writer) error {
	var buf bytes.Buffer
	if err := pprof.WriteHeapProfile(&buf); err != nil {
		return err
	}
	if buf.Len() == 0 {
		return nil
	}
	fw, err := zw.Create("heap_profile.pb.gz")
	if err != nil {
		return err
	}
	_, err = fw.Write(buf.Bytes())
	return err
}

// DumpOnSignal listens for the given OS signals (defaulting to SIGUSR1/SIGUSR2
// on Unix) and writes a timestamped pigasus-perf-<ts>.zip bundle into dir.
// If an onDump callback was configured via WithOnDump or SetOnDump, it is
// invoked after each dump attempt. Call stop to unregister the signal handler
// and wait for the listener goroutine to exit.
func (r *Recorder) DumpOnSignal(ctx context.Context, dir string, sigs ...os.Signal) (stop func()) {
	return r.DumpOnSignalWithCallback(ctx, dir, nil, sigs...)
}

// DumpOnSignalWithCallback behaves like DumpOnSignal and invokes onDump (if
// non-nil, otherwise falling back to the recorder's configured callback) after
// each dump attempt.
func (r *Recorder) DumpOnSignalWithCallback(ctx context.Context, dir string, onDump func(path string, err error), sigs ...os.Signal) (stop func()) {
	if r == nil {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(sigs) == 0 {
		sigs = defaultDumpSignals()
	}
	if len(sigs) == 0 {
		return func() {}
	}
	if dir == "" {
		dir = "."
	}

	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, sigs...)

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		defer signal.Stop(sigCh)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-sigCh:
				ts := r.now().UTC().Format("20060102-150405.000000000")
				path := filepath.Join(dir, fmt.Sprintf("pigasus-perf-%s.zip", ts))
				err := r.WriteBundle(path)
				cb := onDump
				if cb == nil {
					r.mu.Lock()
					cb = r.onDump
					r.mu.Unlock()
				}
				if cb != nil {
					cb(path, err)
				}
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(sigCh)
			close(done)
			wg.Wait()
		})
	}
}
