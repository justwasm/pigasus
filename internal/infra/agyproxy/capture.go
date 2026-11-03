package agyproxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// captureRecord is one JSON file on disk. Field names and semantics follow
// docs/cycles/cycle-274/CONTRACT.md §Capture.
type captureRecord struct {
	TS             time.Time           `json:"ts"`
	Kind           string              `json:"kind"` // "request" | "response"
	Method         string              `json:"method"`
	HTTPMethod     string              `json:"httpMethod"`
	Path           string              `json:"path"`
	Model          string              `json:"model,omitempty"`
	Custom         bool                `json:"custom"`
	Status         int                 `json:"status,omitempty"`
	RequestHeaders map[string][]string `json:"requestHeaders,omitempty"`
	RequestBody    any                 `json:"requestBody,omitempty"`
	ResponseBody   any                 `json:"responseBody,omitempty"`
}

// DefaultCaptureMaxBytes is the ceiling the capture directory is held to.
//
// The capture is a debugging store, and it grew without a bound: two weeks of
// use left a machine carrying 14 GiB of it, every file with a live OAuth
// token in its headers. A ceiling keeps the last day or two of evidence and
// leaves the disk alone. Zero disables the ceiling.
const DefaultCaptureMaxBytes int64 = 1 << 30

// capturePruneInterval is how many bytes may be written between two prune
// passes. A pass is a directory scan — O(files) — so it runs on a budget
// rather than once per record.
const capturePruneInterval int64 = 64 << 20

// capture persists records on a dedicated goroutine so a slow disk can never
// delay a model response, and a full queue drops records instead of blocking.
// Dropping is the documented trade: capture must never fail a request.
type capture struct {
	dir      string
	log      func(string, ...any)
	ch       chan captureRecord
	wg       sync.WaitGroup
	seq      atomic.Int64
	maxBytes int64 // the ceiling; 0 means no ceiling
	since    int64 // bytes written since the last prune pass
}

func newCapture(dir string, logf func(string, ...any), maxBytes int64) *capture {
	_ = os.MkdirAll(dir, 0o700)
	c := &capture{dir: dir, log: logf, ch: make(chan captureRecord, 256), maxBytes: maxBytes}
	// The first record written prunes: a directory that is already over its
	// ceiling is trimmed as soon as the proxy sees traffic, not after the
	// next 64 MiB.
	c.since = capturePruneInterval
	c.wg.Add(1)
	go c.loop()
	return c
}

func (c *capture) write(rec captureRecord) {
	defer func() { _ = recover() }()
	select {
	case c.ch <- rec:
	default:
		if c.log != nil {
			c.log("capture queue full, dropping %s %s", rec.Kind, rec.Method)
		}
	}
}

func (c *capture) loop() {
	defer c.wg.Done()
	for rec := range c.ch {
		c.persist(rec)
	}
}

func (c *capture) persist(rec captureRecord) {
	defer func() {
		if r := recover(); r != nil && c.log != nil {
			c.log("capture panic: %v", r)
		}
	}()
	if rec.TS.IsZero() {
		rec.TS = time.Now()
	}
	name := fmt.Sprintf("%d-%03d-%s.json", rec.TS.UnixMilli(), c.seq.Add(1), rec.Method)
	buf, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		if c.log != nil {
			c.log("capture marshal %s: %v", name, err)
		}
		return
	}
	path := filepath.Join(c.dir, name)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		if c.log != nil {
			c.log("capture write %s: %v", path, err)
		}
		return
	}
	c.noteBytes(int64(len(buf)))
}

// noteBytes counts what was written and prunes when enough has accumulated.
//
// The pass is amortised: a directory scan is O(files), so it runs once per
// capturePruneInterval bytes rather than once per record. It runs on the
// capture goroutine, never on the response path.
func (c *capture) noteBytes(n int64) {
	if c.maxBytes <= 0 {
		return
	}
	c.since += n
	if c.since < capturePruneInterval {
		return
	}
	c.since = 0
	c.prune()
}

// prune deletes the oldest capture files until the directory is under its
// ceiling. The newest evidence survives; the oldest goes.
func (c *capture) prune() {
	files, total := scanCaptures(c.dir)
	if total <= c.maxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	freed := 0
	for _, f := range files {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(f.path); err != nil {
			continue
		}
		total -= f.size
		freed++
	}
	if c.log != nil {
		c.log("capture pruned %d file(s), now %s", freed, humanBytes(total))
	}
}

// captureFile is one file of the capture directory, with the two facts the
// rotation needs: how big it is and when it was written.
type captureFile struct {
	path    string
	size    int64
	modTime time.Time
}

// scanCaptures lists the capture files in dir and their total size.
//
// Only the files this proxy wrote are counted: the name is the epoch
// milliseconds, the sequence number, the method, and the .json suffix. A
// capture directory the user pointed at something else keeps its other files.
func scanCaptures(dir string) ([]captureFile, int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0
	}
	out := make([]captureFile, 0, len(entries))
	var total int64
	for _, e := range entries {
		if e.IsDir() || !isCaptureName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, captureFile{
			path:    filepath.Join(dir, e.Name()),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		total += info.Size()
	}
	return out, total
}

// isCaptureName reports whether a file name is one of ours.
//
// The shape is "<epochmillis>-<seq>-<method>.json". The method may itself
// contain dashes, so only the first two fields are split off.
func isCaptureName(name string) bool {
	if !strings.HasSuffix(name, ".json") {
		return false
	}
	parts := strings.SplitN(strings.TrimSuffix(name, ".json"), "-", 3)
	if len(parts) != 3 {
		return false
	}
	return isDigits(parts[0]) && isDigits(parts[1]) && parts[2] != ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// purgeCaptures deletes the capture files in dir and reports what went.
//
// It removes only the files this proxy wrote — the same name rule the
// rotation uses — so a directory that holds anything else keeps it.
func purgeCaptures(dir string) (files int, freed int64, err error) {
	list, _ := scanCaptures(dir)
	for _, f := range list {
		if rmErr := os.Remove(f.path); rmErr != nil {
			if err == nil {
				err = rmErr
			}
			continue
		}
		files++
		freed += f.size
	}
	return files, freed, err
}

// close drains the queue. Bounded: a stuck writer cannot hang shutdown.
func (c *capture) close() {
	close(c.ch)
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

// rawBody embeds bytes in a capture file without re-encoding them. JSON bodies
// stay JSON (so they can be diffed structurally); anything else — SSE text in
// particular — is stored as a string.
func rawBody(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	if json.Valid(b) {
		return json.RawMessage(append([]byte(nil), b...))
	}
	return string(b)
}

// ---- capture-aware ResponseWriter --------------------------------------

// captureResponseWriter remembers the status and a bounded copy of the body so
// ServeHTTP can persist the exchange after the handler returns. When capture is
// off it is a transparent wrapper.
type captureResponseWriter struct {
	http.ResponseWriter
	status  int
	limited bool
	buf     bytes.Buffer
}

// captureBodyLimit bounds how much of a response is stored in memory. A turn
// with a large tool result can exceed it; the capture is truncated, the request
// is not.
const captureBodyLimit = 16 << 20

func newCaptureResponseWriter(w http.ResponseWriter, capture bool) *captureResponseWriter {
	return &captureResponseWriter{ResponseWriter: w, limited: capture}
}

func (c *captureResponseWriter) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *captureResponseWriter) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.limited && c.buf.Len() < captureBodyLimit {
		room := captureBodyLimit - c.buf.Len()
		if room > len(p) {
			room = len(p)
		}
		c.buf.Write(p[:room])
	}
	return c.ResponseWriter.Write(p)
}

// Flush keeps SSE working through the wrapper.
func (c *captureResponseWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack enables WebSocket upgrade through the wrapper.
func (c *captureResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := c.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
}

func (c *captureResponseWriter) statusCode() int {
	if c.status == 0 {
		return http.StatusOK
	}
	return c.status
}

func (c *captureResponseWriter) captured() []byte { return c.buf.Bytes() }

func (s *Server) captureRequest(started time.Time, method string, r *http.Request, info reqInfo, body []byte) {
	if s.cap == nil {
		return
	}
	s.cap.write(captureRecord{
		TS:             started,
		Kind:           "request",
		Method:         method,
		HTTPMethod:     r.Method,
		Path:           r.URL.Path,
		Model:          info.model,
		Custom:         info.custom,
		RequestHeaders: r.Header.Clone(),
		RequestBody:    rawBody(body),
	})
}

func (s *Server) captureResponse(started time.Time, method string, r *http.Request, info reqInfo, body []byte, cw *captureResponseWriter) {
	if s.cap == nil {
		return
	}
	s.cap.write(captureRecord{
		TS:             started,
		Kind:           "response",
		Method:         method,
		HTTPMethod:     r.Method,
		Path:           r.URL.Path,
		Model:          info.model,
		Custom:         info.custom,
		Status:         cw.statusCode(),
		RequestHeaders: r.Header.Clone(),
		RequestBody:    rawBody(body),
		ResponseBody:   rawBody(cw.captured()),
	})
}

// methodFromPath turns "/v1internal:streamGenerateContent" into
// "streamGenerateContent". Capture filenames use it.
func methodFromPath(path string) string {
	p := strings.TrimPrefix(path, "/")
	if i := strings.LastIndex(p, ":"); i >= 0 {
		if m := sanitizeName(p[i+1:]); m != "" {
			return m
		}
	}
	if m := sanitizeName(p); m != "" {
		return m
	}
	return "root"
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func captureState(dir string) string {
	if dir == "" {
		return "off"
	}
	return dir
}
