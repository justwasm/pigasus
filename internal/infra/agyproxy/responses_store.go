package agyproxy

import (
	"container/list"
	"encoding/json"
	"sync"
	"time"
)

const (
	prevTTL = 6 * time.Hour
	prevCap = 256 << 20 // 256 MB
)

type prevTurn struct {
	id    string
	items []json.RawMessage
	size  int
	at    time.Time
}

type prevStore struct {
	mu    sync.Mutex
	m     map[string]*list.Element
	order list.List
	size  int
	cap   int
	ttl   time.Duration
}

var globalPrevStore = newPrevStore(prevCap, prevTTL)

func newPrevStore(cap int, ttl time.Duration) *prevStore {
	return &prevStore{
		m:   make(map[string]*list.Element),
		cap: cap,
		ttl: ttl,
	}
}

func (s *prevStore) get(id string) ([]json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[id]
	if !ok {
		return nil, false
	}
	t := e.Value.(*prevTurn)
	if time.Since(t.at) > s.ttl {
		s.drop(e)
		return nil, false
	}
	s.order.MoveToFront(e)
	return t.items, true
}

func (s *prevStore) put(id string, items []json.RawMessage) {
	if id == "" || len(items) == 0 {
		return
	}
	size := 0
	for _, it := range items {
		size += len(it)
	}
	if size > s.cap/2 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.m[id]; ok {
		t := e.Value.(*prevTurn)
		s.size -= t.size
		t.items = items
		t.size = size
		t.at = time.Now()
		s.size += size
		s.order.MoveToFront(e)
		return
	}

	t := &prevTurn{
		id:    id,
		items: items,
		size:  size,
		at:    time.Now(),
	}
	for s.size+size > s.cap && s.order.Len() > 0 {
		s.drop(s.order.Back())
	}
	e := s.order.PushFront(t)
	s.m[id] = e
	s.size += size
}

func (s *prevStore) drop(e *list.Element) {
	s.order.Remove(e)
	t := e.Value.(*prevTurn)
	delete(s.m, t.id)
	s.size -= t.size
}
