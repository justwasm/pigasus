// windowtitle.go — the terminal window title.
//
// The title script runs on the tick goroutine and the renderer reads the
// title on the model's goroutine, so the value needs its own lock: an
// unsynchronised string write can be read as an empty title (a string is
// two words, and the writer's store of the length can be observed before
// its store of the pointer).
package cliapp

import "sync"

// windowTitleStore holds the title the title script produced.
type windowTitleStore struct {
	mu    sync.Mutex
	title string
}

// set stores a new title.
func (s *windowTitleStore) set(title string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.title = title
	s.mu.Unlock()
}

// get returns the current title.
func (s *windowTitleStore) get() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

// windowTitleInitMu guards the creation of a runMode's title store.
//
// The store is created with the model, so this lock is only taken on the
// path where a model was built without the constructor (the tests).
var windowTitleInitMu sync.Mutex

// titleStoreFor returns the model's title store, creating it if the model
// was built without one.
func (m *runMode) titleStoreFor() *windowTitleStore {
	if m == nil {
		return nil
	}
	windowTitleInitMu.Lock()
	defer windowTitleInitMu.Unlock()
	if m.titleStore == nil {
		m.titleStore = &windowTitleStore{}
	}
	return m.titleStore
}

// setWindowTitle stores the title the title script produced.
func (m *runMode) setWindowTitle(title string) {
	m.titleStoreFor().set(title)
}

// windowTitleValue returns the title the title script produced.
func (m *runMode) windowTitleValue() string {
	return m.titleStoreFor().get()
}
