package agyproxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// capture persists records on a dedicated goroutine so a slow disk can never
// delay a model response, and a full queue drops records instead of blocking.
// Dropping is the documented trade: capture must never fail a request.
type capture struct {
	dir string
	log func(string, ...any)
	ch  chan captureRecord
	wg  sync.WaitGroup
	seq atomic.Int64
}

func newCapture(dir string, logf func(string, ...any)) *capture {
	_ = os.MkdirAll(dir, 0o700)
	c := &capture{dir: dir, log: logf, ch: make(chan captureRecord, 256)}
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
	}
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
