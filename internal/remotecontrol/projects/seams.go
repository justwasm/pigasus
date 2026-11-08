// Package projects — seams.go: what the host hands the projects
// domain.
//
// The domain owns the project record, the store, the wire shape of
// the four dashboard methods and the sentinel-error map. It does
// not own the dispatcher that routes the methods — that is the
// host's.
//
// One seam, a narrow view of something the host owns:
//
//   - Store is the domain's own store (the interface the package
//     declares). A nil Store makes the domain's methods answer
//     -32603 ("no project store installed").
//
// Stdlib-only. No third-party deps.
package projects

// Seams are the host-supplied dependencies of the projects
// domain.
type Seams struct {
	// Store is the project store. A nil Store makes the
	// domain's methods answer -32603 ("no project store
	// installed").
	Store Store
	// Uploads is where UploadProjectFile writes a project's
	// uploaded files. A nil Uploads makes that one method answer
	// -32603 ("no upload store installed"); the CRUD methods do
	// not need it.
	Uploads UploadStore
}
