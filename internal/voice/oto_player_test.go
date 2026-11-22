package voice

import (
	"bytes"
	"testing"
)

func TestAudioBuffer_JitterPrebuffering(t *testing.T) {
	b := newAudioBuffer()
	b.minPrebufferBytes = 100 // Smaller threshold for unit testing

	// 1. Initial state: buffer is empty and in prebuffering mode
	readBuf := make([]byte, 50)
	n, err := b.Read(readBuf)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if n != 50 {
		t.Fatalf("read %d bytes, want 50", n)
	}
	for i, v := range readBuf {
		if v != 0 {
			t.Fatalf("expected silence byte at %d during prebuffering, got %d", i, v)
		}
	}

	// 2. Write partial data (50 bytes < 100 min threshold)
	chunk1 := bytes.Repeat([]byte{0x42}, 50)
	_, _ = b.Write(chunk1)

	// Still prebuffering: Read returns silence to avoid jitter stutter
	n, _ = b.Read(readBuf)
	if n != 50 {
		t.Fatalf("read %d bytes, want 50", n)
	}
	for i, v := range readBuf {
		if v != 0 {
			t.Fatalf("expected silence byte at %d during partial prebuffering, got %d", i, v)
		}
	}

	// 3. Write another 60 bytes (total 110 >= 100 min threshold)
	chunk2 := bytes.Repeat([]byte{0x43}, 60)
	_, _ = b.Write(chunk2)

	// Now prebuffering gate is lifted; actual audio data streams out
	n, _ = b.Read(readBuf)
	if n != 50 {
		t.Fatalf("read %d bytes, want 50", n)
	}
	if readBuf[0] != 0x42 {
		t.Fatalf("expected first chunk data 0x42, got %x", readBuf[0])
	}
}

func TestAudioBuffer_Flush(t *testing.T) {
	b := newAudioBuffer()
	b.minPrebufferBytes = 1000

	// Write only 20 bytes
	data := bytes.Repeat([]byte{0x77}, 20)
	_, _ = b.Write(data)

	// Flush immediately releases prebuffer gate
	b.Flush()

	readBuf := make([]byte, 20)
	n, _ := b.Read(readBuf)
	if n != 20 {
		t.Fatalf("read %d bytes, want 20", n)
	}
	if readBuf[0] != 0x77 {
		t.Fatalf("expected 0x77, got %x", readBuf[0])
	}
}

func TestAudioBuffer_Clear(t *testing.T) {
	b := newAudioBuffer()
	b.minPrebufferBytes = 100

	_, _ = b.Write(bytes.Repeat([]byte{0x88}, 500))
	if b.Buffered() != 500 {
		t.Fatalf("buffered = %d, want 500", b.Buffered())
	}

	// Barge-in Clear
	b.Clear()
	if b.Buffered() != 0 {
		t.Fatalf("buffered after clear = %d, want 0", b.Buffered())
	}

	// Should be back in prebuffering mode
	readBuf := make([]byte, 10)
	_, _ = b.Read(readBuf)
	for i, v := range readBuf {
		if v != 0 {
			t.Fatalf("expected silence at %d after clear, got %d", i, v)
		}
	}
}

func TestOtoPlayer_MultipleInit(t *testing.T) {
	p1, err := NewOtoPlayer(24000, 1)
	if err != nil {
		t.Fatalf("p1 err: %v", err)
	}
	t.Logf("p1: disabled=%v, ready=%v", p1.disabled, p1.ready)
	_ = p1.Close()

	p2, err := NewOtoPlayer(24000, 1)
	if err != nil {
		t.Fatalf("p2 err: %v", err)
	}
	t.Logf("p2: disabled=%v, ready=%v", p2.disabled, p2.ready)

	if p2.disabled {
		t.Fatalf("BUG CONFIRMED: p2 is disabled because oto.NewContext cannot be called multiple times!")
	}

	// Verify p2 can write and buffer audio
	pcm := make([]byte, 4800)
	n, err := p2.Write(pcm)
	if err != nil || n != 4800 {
		t.Fatalf("p2.Write failed: n=%d err=%v", n, err)
	}
	if p2.Buffered() != 4800 {
		t.Fatalf("expected 4800 buffered in p2, got %d", p2.Buffered())
	}
	_ = p2.Close()
}




