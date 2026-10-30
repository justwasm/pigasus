package urlchunk

import (
	"testing"
)

func TestFacadeURLChunk(t *testing.T) {
	chunks := Chunks("https://example.com/very/long/url", 10)
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
}
