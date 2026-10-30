//go:build !darwin && !linux

// Package clipboard media_other.go — the platforms with no clipboard
// image reader.
//
// The reference ships a reader per platform family (its own build lists
// `clipboard_darwin.go`, `clipboard_native_other.go` and the Wayland
// helper). This clone implements the two it can test — macOS and Linux —
// and answers ErrNoMedia everywhere else, so the paste key says what the
// reference says instead of failing in an unfamiliar way.
package clipboard

// readMedia reports that this platform has no clipboard image reader.
func readMedia(opts Options) (Media, error) {
	return Media{}, ErrNoMedia
}
