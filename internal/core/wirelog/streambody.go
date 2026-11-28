// streambody.go — recording a response while it is being read.
//
// # Why the response body cannot be read up front
//
// The obvious implementation reads resp.Body to EOF inside RoundTrip and
// hands the caller a fresh reader over the bytes. It is wrong for this
// tree, and the failure is not subtle: the model's answer arrives as an
// SSE stream, and the caller renders each frame as it lands. Buffering the
// whole body first means the TUI shows nothing until the turn is over —
// the recorder would have turned a streaming client into a blocking one,
// and the only symptom would be "the spinner hangs and then everything
// appears at once".
//
// So the body is recorded as it flows: every Read is teed into a capped
// buffer, and the record is finalised when the stream ends or is closed.
// The caller keeps its stream, the record keeps the bytes.
//
// # What the duration then means
//
// RoundTrip returns when the headers arrive, not when the stream ends, so
// the elapsed time at that point is the time to first byte. A reader
// asking "why was that turn slow" wants the total, so the record's
// Duration is stamped at finalise time and the first-byte time is kept
// separately as TTFB.
package wirelog

import (
	"errors"
	"io"
	"sync"
	"time"
)

// recordingBody tees a response body into the store's ring.
//
// It finalises exactly once: a stream ends at EOF, and the caller may also
// close it early (a cancelled turn), and both are the end of the exchange.
// The guard is a sync.Once so a double Close cannot add the record twice.
type recordingBody struct {
	rc      io.ReadCloser
	store   *Store
	rec     Record
	reqBody Captured
	start   time.Time
	now     func() time.Time

	mu      sync.Mutex
	head    []byte
	tail    *tailBuffer
	total   int64
	readErr error

	once sync.Once
}

// newRecordingBody wraps rc so the bytes it yields are also recorded.
//
// reqBody is the request half of the exchange, already read and restored
// by the transport: it is passed through here rather than kept on the
// record so the record is built once, at finalise time, with both sizes.
func newRecordingBody(rc io.ReadCloser, store *Store, rec Record, reqBody Captured, started time.Time, now func() time.Time) io.ReadCloser {
	if rc == nil {
		return nil
	}
	return &recordingBody{
		rc:      rc,
		store:   store,
		rec:     rec,
		reqBody: reqBody,
		start:   started,
		now:     now,
		tail:    newTailBuffer(TailBodyBytes),
	}
}

// Read reads from the wrapped body and tees what it read into head and tail buffers.
func (b *recordingBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		b.mu.Lock()
		b.total += int64(n)
		chunk := p[:n]
		if len(b.head) < HeadBodyBytes {
			room := HeadBodyBytes - len(b.head)
			take := room
			if take > len(chunk) {
				take = len(chunk)
			}
			b.head = append(b.head, chunk[:take]...)
			chunk = chunk[take:]
		}
		if len(chunk) > 0 {
			b.tail.Write(chunk)
		}
		b.mu.Unlock()
	}
	if err != nil && !errors.Is(err, io.EOF) {
		b.mu.Lock()
		b.readErr = err
		b.mu.Unlock()
	}
	if err != nil {
		// EOF, a read error and an early close all end the exchange.
		b.finish()
	}
	return n, err
}

// Close closes the wrapped body and finalises the record.
func (b *recordingBody) Close() error {
	err := b.rc.Close()
	b.finish()
	return err
}

// finish adds the record, once.
func (b *recordingBody) finish() {
	b.once.Do(func() {
		// The recovery is installed first, not last: finish runs inside
		// the caller's Read, so a panic anywhere below — a broken clock,
		// a store that misbehaves — would surface as a crash in the turn
		// the recorder was only supposed to observe.
		defer func() { _ = recover() }()

		b.mu.Lock()
		total := b.total
		head := b.head
		var tailBytes []byte
		if total > int64(MaxBodyBytes) {
			tailBytes = b.tail.Bytes()
		} else if total > int64(HeadBodyBytes) {
			tailBytes = b.tail.Bytes()
			combined := make([]byte, len(head)+len(tailBytes))
			copy(combined, head)
			copy(combined[len(head):], tailBytes)
			head = combined
			tailBytes = nil
		}
		readErr := b.readErr
		b.mu.Unlock()

		rec := b.rec
		rec.Duration = b.now().Sub(b.start)
		// A stream that never yielded a byte still took the time to its
		// close, and TTFB has no answer for it: leave it at the header
		// time rather than reporting a first byte that never arrived.
		if total == 0 {
			rec.TTFB = 0
		}
		rec.ResponseBytes = total
		if readErr != nil && rec.Err == "" {
			// A stream that broke mid-body is a failed exchange even
			// though the status was 200: the caller saw a truncated
			// answer, and the table should say so.
			rec.Err = Redact(readErr.Error())
		}
		b.store.Capture(rec, b.reqBody, Captured{Head: head, Tail: tailBytes, Total: total})
	})
}

// tailBuffer holds the trailing bytes of a stream in a fixed-size ring.
type tailBuffer struct {
	buf  []byte
	size int
	pos  int
	full bool
}

func newTailBuffer(size int) *tailBuffer {
	return &tailBuffer{buf: make([]byte, size), size: size}
}

func (t *tailBuffer) Write(p []byte) {
	if len(p) == 0 || t.size == 0 {
		return
	}
	if len(p) >= t.size {
		copy(t.buf, p[len(p)-t.size:])
		t.pos = 0
		t.full = true
		return
	}
	first := copy(t.buf[t.pos:], p)
	if first < len(p) {
		copy(t.buf, p[first:])
		t.pos = len(p) - first
		t.full = true
	} else {
		t.pos += first
		if t.pos >= t.size {
			t.pos = 0
			t.full = true
		}
	}
}

func (t *tailBuffer) Bytes() []byte {
	if !t.full {
		out := make([]byte, t.pos)
		copy(out, t.buf[:t.pos])
		return out
	}
	out := make([]byte, t.size)
	n := copy(out, t.buf[t.pos:])
	copy(out[n:], t.buf[:t.pos])
	return out
}
