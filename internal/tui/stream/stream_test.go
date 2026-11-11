package stream_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/tui/stream"
)

type mockHost struct {
	model     string
	streaming bool
	deltas    []string
	completed string
	err       error
	notices   []stream.Notification
}

func (m *mockHost) ActiveModel() string            { return m.model }
func (m *mockHost) IsStreaming() bool              { return m.streaming }
func (m *mockHost) AppendStreamDelta(delta string) { m.deltas = append(m.deltas, delta) }
func (m *mockHost) OnTurnComplete(reply string, err error) tea.Cmd {
	m.completed = reply
	m.err = err
	return nil
}
func (m *mockHost) Notify(n stream.Notification) { m.notices = append(m.notices, n) }

func TestStreamHandler(t *testing.T) {
	host := &mockHost{model: "test-model", streaming: true}
	h := stream.NewHandler(host)

	h.HandleEvent(stream.StreamEvent{Delta: "Hello "})
	h.HandleEvent(stream.StreamEvent{Delta: "World!"})
	h.HandleEvent(stream.StreamEvent{Content: "Hello World!", Done: true})

	if len(host.deltas) != 2 || host.deltas[0] != "Hello " || host.deltas[1] != "World!" {
		t.Fatalf("unexpected deltas: %v", host.deltas)
	}
	if host.completed != "Hello World!" {
		t.Fatalf("expected completed reply %q, got %q", "Hello World!", host.completed)
	}
}

func TestNotificationQueue(t *testing.T) {
	q := stream.NewNotificationQueue(2)
	q.Push(stream.Notification{Title: "notice 1"})
	q.Push(stream.Notification{Title: "notice 2"})
	q.Push(stream.Notification{Title: "notice 3"})

	items := q.List()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Title != "notice 2" || items[1].Title != "notice 3" {
		t.Fatalf("unexpected items: %v", items)
	}

	item, ok := q.Pop()
	if !ok || item.Title != "notice 2" {
		t.Fatalf("unexpected popped item: %v", item)
	}
	q.Clear()
	if len(q.List()) != 0 {
		t.Fatalf("expected queue to be empty after clear")
	}
}
