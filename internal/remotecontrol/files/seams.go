// Package files — seams.go: what the host hands the files
// domain.
//
// The domain owns the provider seam, the production scope
// implementation, the in-memory fixture, the wire shape of the
// two dashboard methods and the sentinel-error map. It does not
// own the dispatcher that routes the methods — that is the
// host's.
//
// One seam, a narrow view of something the host owns:
//
//   - Provider is the filesystem provider the host installed.
//     nil is not a no-op here: a nil Provider makes the
//     domain's methods answer -32603 ("no FS provider
//     installed"), because there is nothing to read through.
//
// Stdlib-only. No third-party deps.
package files

// Seams are the host-supplied dependencies of the files domain.
type Seams struct {
	// Provider is the filesystem provider. A nil Provider
	// makes the domain's methods answer -32603 ("no FS
	// provider installed").
	Provider Provider
}
