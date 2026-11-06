package stream

import (
	"sync"
	"time"
)

// NotificationQueue manages the buffering and lifecycle of agent progress notices.
type NotificationQueue struct {
	mu    sync.Mutex
	items []Notification
	limit int
}

// NewNotificationQueue creates a queue with a maximum retention limit.
func NewNotificationQueue(limit int) *NotificationQueue {
	if limit <= 0 {
		limit = 100
	}
	return &NotificationQueue{
		limit: limit,
		items: make([]Notification, 0, limit),
	}
}

// Push appends a notification to the queue, trimming older items if limit is exceeded.
func (q *NotificationQueue) Push(n Notification) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	if len(q.items) >= q.limit {
		q.items = q.items[1:]
	}
	q.items = append(q.items, n)
}

// Pop retrieves and removes the oldest notification from the queue.
func (q *NotificationQueue) Pop() (Notification, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return Notification{}, false
	}
	item := q.items[0]
	q.items = q.items[1:]
	return item, true
}

// List returns a snapshot copy of all active notifications.
func (q *NotificationQueue) List() []Notification {
	q.mu.Lock()
	defer q.mu.Unlock()

	out := make([]Notification, len(q.items))
	copy(out, q.items)
	return out
}

// Clear flushes all buffered notifications.
func (q *NotificationQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = q.items[:0]
}
