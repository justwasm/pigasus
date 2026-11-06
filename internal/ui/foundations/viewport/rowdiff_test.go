package viewport

import (
	"testing"
)

func TestHashRow(t *testing.T) {
	h1 := HashRow("hello world")
	h2 := HashRow("hello world")
	h3 := HashRow("hello world!")

	if h1 != h2 {
		t.Fatalf("hashes for identical strings differ: %d != %d", h1, h2)
	}
	if h1 == h3 {
		t.Fatalf("hash collision on trivial difference: %d == %d", h1, h3)
	}
}

func TestRowDiff(t *testing.T) {
	d := NewRowDiff()

	// Initial frame
	initial := []string{"row 1", "row 2", "row 3", "row 4"}
	res := d.Compare(initial)
	if !res.Changed || res.FirstChanged != 0 || res.LastChanged != 3 {
		t.Fatalf("initial compare failed: %+v", res)
	}

	// Unchanged frame
	res = d.Compare(initial)
	if res.Changed {
		t.Fatalf("identical rows should report Changed: false, got %+v", res)
	}

	// Append streaming line at bottom
	stream1 := []string{"row 1", "row 2", "row 3", "row 4", "row 5 (streaming...)"}
	res = d.Compare(stream1)
	if !res.Changed || res.FirstChanged != 4 || res.LastChanged != 4 {
		t.Fatalf("append streaming line failed: %+v", res)
	}

	// Update only the bottom streaming line
	stream2 := []string{"row 1", "row 2", "row 3", "row 4", "row 5 (streaming... chunk2)"}
	res = d.Compare(stream2)
	if !res.Changed || res.FirstChanged != 4 || res.LastChanged != 4 {
		t.Fatalf("streaming delta failed: %+v", res)
	}

	// Middle modification
	middle := []string{"row 1", "row 2 - MODIFIED", "row 3", "row 4", "row 5 (streaming... chunk2)"}
	res = d.Compare(middle)
	if !res.Changed || res.FirstChanged != 1 || res.LastChanged != 1 {
		t.Fatalf("middle modification failed: %+v", res)
	}

	// Reset
	d.Reset()
	res = d.Compare(middle)
	if !res.Changed || res.FirstChanged != 0 || res.LastChanged != 4 {
		t.Fatalf("after reset compare failed: %+v", res)
	}
}
