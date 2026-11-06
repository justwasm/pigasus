// rowdiff.go — row-level diffing and hash comparison for terminal output.
//
// Ports screen row-level hash caching from PiG (tui_surface.go).
// Enables partial line updates, unchanged prefix reuse, and flicker reduction
// during high-frequency streaming.

package viewport

// FNV-1a 64-bit constants
const (
	offset64 = 14695981039346656037
	prime64  = 1099511628211
)

// HashRow computes a fast 64-bit FNV-1a hash of a single rendered row.
func HashRow(s string) uint64 {
	var h uint64 = offset64
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// RowDiff tracks per-row hashes across frames to detect which rows changed.
type RowDiff struct {
	hashes []uint64
}

// NewRowDiff creates an initialized RowDiff tracker.
func NewRowDiff() *RowDiff {
	return &RowDiff{}
}

// DiffResult describes which row slice changed between two frames.
type DiffResult struct {
	Changed      bool
	FirstChanged int
	LastChanged  int // inclusive index in newRows
	TotalRows    int
}

// Compare hashes newRows and compares against previous frame.
// If no rows changed, it returns Changed: false.
// Otherwise it identifies the minimal [FirstChanged, LastChanged] window.
func (d *RowDiff) Compare(newRows []string) DiffResult {
	n := len(newRows)
	newHashes := make([]uint64, n)
	for i, r := range newRows {
		newHashes[i] = HashRow(r)
	}

	oldN := len(d.hashes)
	if oldN == 0 && n == 0 {
		return DiffResult{Changed: false, TotalRows: 0}
	}
	if oldN == 0 {
		d.hashes = newHashes
		return DiffResult{Changed: true, FirstChanged: 0, LastChanged: n - 1, TotalRows: n}
	}

	// Find common prefix
	prefix := 0
	for prefix < n && prefix < oldN && newHashes[prefix] == d.hashes[prefix] {
		prefix++
	}

	// If all match and lengths are equal
	if prefix == n && prefix == oldN {
		return DiffResult{Changed: false, TotalRows: n}
	}

	// Find common suffix
	suffix := 0
	for suffix < (n-prefix) && suffix < (oldN-prefix) &&
		newHashes[n-1-suffix] == d.hashes[oldN-1-suffix] {
		suffix++
	}

	firstChanged := prefix
	lastChanged := n - 1 - suffix
	if lastChanged < firstChanged {
		// Row count shrank or inserted exactly at boundary
		lastChanged = firstChanged
		if lastChanged >= n && n > 0 {
			lastChanged = n - 1
		}
	}

	d.hashes = newHashes
	return DiffResult{
		Changed:      true,
		FirstChanged: firstChanged,
		LastChanged:  lastChanged,
		TotalRows:    n,
	}
}

// Reset clears the cached hashes.
func (d *RowDiff) Reset() {
	d.hashes = nil
}
