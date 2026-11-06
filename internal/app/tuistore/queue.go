package tuistore

// Queue holds prompt queueing, stream preemption, and in-flight steer sending state.
type Queue struct {
	QueueMode       bool
	QueuedPrompt    string
	StreamQueue     []string
	SendingMessages []string
}

// ClearStreamQueue empties the stream queue.
func (s *Store) ClearStreamQueue() {
	s.StreamQueue = nil
	s.QueuedPrompt = ""
}

// ClearSendingMessages empties in-flight sending steer messages.
func (s *Store) ClearSendingMessages() {
	s.SendingMessages = nil
}
