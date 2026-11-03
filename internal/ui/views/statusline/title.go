package statusline

import "sync"

// TitleStore holds the window title the title script produced.
//
// The title script runs on the tick goroutine and the renderer reads the
// title on the model's goroutine, so the value needs its own lock: an
// unsynchronised string write can be read as an empty title (a string is
// two words, and the writer's store of the length can be observed before
// its store of the pointer).
type TitleStore struct {
	mu    sync.Mutex
	title string
}

// Set stores a new title.
func (s *TitleStore) Set(title string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.title = title
	s.mu.Unlock()
}

// Get returns the current title.
func (s *TitleStore) Get() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}
