package tuistore

// Queue holds prompt queueing and stream preemption state.
type Queue struct {
	QueueMode    bool
	QueuedPrompt string
	StreamQueue  []string
}

// ClearStreamQueue empties the stream queue.
func (s *Store) ClearStreamQueue() {
	s.StreamQueue = nil
	s.QueuedPrompt = ""
}
