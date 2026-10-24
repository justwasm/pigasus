// Package language_server / session_types.go — audio and narration session managers.
//
// Recovered from google3/third_party/jetski/language_server/audiotranscribe/audiotranscribe.*.
package language_server

import (
	"errors"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Audio session manager (recovered from
// google3/third_party/jetski/language_server/audiotranscribe/audiotranscribe.*)
// ---------------------------------------------------------------------------

// AudioSessionManager is the recovered audio session manager.
type AudioSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*AudioSession
}

// AudioSession is the recovered audio session.
type AudioSession struct {
	ID        string
	StartedAt time.Time
}

// NewAudioSessionManager is the recovered constructor.
func NewAudioSessionManager() *AudioSessionManager {
	return &AudioSessionManager{sessions: make(map[string]*AudioSession)}
}

// Add is the recovered method.
func (m *AudioSessionManager) Add(id string) *AudioSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &AudioSession{ID: id, StartedAt: time.Now()}
	m.sessions[id] = s
	return s
}

// Get is the recovered method.
func (m *AudioSessionManager) Get(id string) *AudioSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Remove is the recovered method.
func (m *AudioSessionManager) Remove(id string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// GetVoiceContextCache is the recovered method.
func (m *AudioSessionManager) GetVoiceContextCache(id string) []byte {
	if s := m.Get(id); s != nil {
		return nil
	}
	return nil
}

// SetVoiceContextCache is the recovered method.
func (m *AudioSessionManager) SetVoiceContextCache(id string, cache []byte) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		_ = s
		_ = cache
	}
}

// processBuffer is the recovered method on AudioSession.
func (s *AudioSession) processBuffer(buf []byte) ([]byte, error) {
	if s == nil {
		return nil, errors.New("language_server: audio session is nil")
	}
	return buf, nil
}

// ---------------------------------------------------------------------------
// Narration session manager (recovered).
// ---------------------------------------------------------------------------

// NarrationSessionManager is the recovered narration session manager.
type NarrationSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*NarrationSession
}

// NarrationSession is the recovered narration session.
type NarrationSession struct {
	ID        string
	StartedAt time.Time
}

// NewNarrationSessionManager is the recovered constructor.
func NewNarrationSessionManager() *NarrationSessionManager {
	return &NarrationSessionManager{sessions: make(map[string]*NarrationSession)}
}

// Add is the recovered method.
func (m *NarrationSessionManager) Add(id string) *NarrationSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &NarrationSession{ID: id, StartedAt: time.Now()}
	m.sessions[id] = s
	return s
}

// Get is the recovered method.
func (m *NarrationSessionManager) Get(id string) *NarrationSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Remove is the recovered method.
func (m *NarrationSessionManager) Remove(id string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}
