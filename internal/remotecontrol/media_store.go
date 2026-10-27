// Package remotecontrol — media_store.go: the media artifacts the Web
// UI uploaded.
//
// The Web UI uploads a picture before it sends the message: it calls
// SaveMediaAsArtifact with the media it holds and expects a uri back.
// The uri is the artifact's handle — the frontend recognises the
// scheme, and it keeps the bytes inline for such a uri, so the clone
// never has to read them back from the store to answer the turn.
//
// 【一手】 2026-10-26, the reference's own frontend bundle
// (internal/rcfrontend/assets/main.js):
//
//	function ZA(a){return!!a?.startsWith("remote-artifact://")}
//
// and the composer's media list keeps the inline payload for a uri
// with that scheme:
//
//	…&&t.uri&&!ZA(t?.uri)&&k.current.has(t)
//	    ?{…t,payload:{case:"inlineData",value:new Uint8Array}}:t
//
// — the payload is emptied only for a uri that points at a *local*
// file. A `remote-artifact://` uri keeps the bytes, and the bytes are
// what the turn carries.
//
// The store is process-wide: the artifacts live as long as the daemon
// does, and the daemon is one process.
package remotecontrol

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// mediaArtifactScheme is the uri scheme the reference's own frontend
// recognises.
const mediaArtifactScheme = "remote-artifact://"

const (
	// mediaStoreMaxItems is how many artifacts the store keeps.
	mediaStoreMaxItems = 64
	// mediaStoreMaxBytes is how many bytes of payload it keeps.
	mediaStoreMaxBytes = 64 << 20
)

// mediaArtifact is one stored picture.
type mediaArtifact struct {
	// URI is the artifact's handle.
	URI string
	// MIMEType is the media type the picture was uploaded with.
	MIMEType string
	// Data is the picture's bytes.
	Data []byte
}

// mediaStore keeps the media artifacts the Web UI uploaded.
//
// The oldest artifact is dropped when the store is full: a picture the
// user attached ten minutes ago and never sent is worth less than the
// one they are attaching now.
type mediaStore struct {
	mu    sync.Mutex
	items map[string]mediaArtifact
	order []string
	bytes int
}

// newMediaStore returns an empty store.
func newMediaStore() *mediaStore {
	return &mediaStore{items: map[string]mediaArtifact{}}
}

// mediaArtifacts is the process-wide store the RPCs use.
var mediaArtifacts = newMediaStore()

// save stores a picture and returns its uri.
//
// The uri is the scheme the reference's frontend recognises, followed
// by a random id: two uploads of the same picture are two artifacts.
func (s *mediaStore) save(mimeType string, data []byte) string {
	id := randomMediaID()
	uri := mediaArtifactScheme + id
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[string]mediaArtifact{}
	}
	s.items[id] = mediaArtifact{URI: uri, MIMEType: mimeType, Data: data}
	s.order = append(s.order, id)
	s.bytes += len(data)
	s.evictLocked()
	return uri
}

// get returns the artifact a uri names.
func (s *mediaStore) get(uri string) (mediaArtifact, bool) {
	id, ok := mediaArtifactID(uri)
	if !ok {
		return mediaArtifact{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	artifact, ok := s.items[id]
	return artifact, ok
}

// delete drops the artifact a uri names, and reports whether there
// was one.
func (s *mediaStore) delete(uri string) bool {
	id, ok := mediaArtifactID(uri)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	artifact, ok := s.items[id]
	if !ok {
		return false
	}
	delete(s.items, id)
	s.bytes -= len(artifact.Data)
	for i, existing := range s.order {
		if existing == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return true
}

// evictLocked drops the oldest artifacts until the store is inside its
// bounds. The caller holds the lock.
func (s *mediaStore) evictLocked() {
	for len(s.order) > 0 && (len(s.order) > mediaStoreMaxItems || s.bytes > mediaStoreMaxBytes) {
		oldest := s.order[0]
		s.order = s.order[1:]
		if artifact, ok := s.items[oldest]; ok {
			s.bytes -= len(artifact.Data)
			delete(s.items, oldest)
		}
	}
}

// mediaArtifactID returns the id a uri names, and whether the uri is
// one of ours.
func mediaArtifactID(uri string) (string, bool) {
	if len(uri) <= len(mediaArtifactScheme) {
		return "", false
	}
	if uri[:len(mediaArtifactScheme)] != mediaArtifactScheme {
		return "", false
	}
	return uri[len(mediaArtifactScheme):], true
}

// randomMediaID mints an artifact id.
func randomMediaID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// The id only has to be unique within the store; the clock is
		// good enough when the random source fails.
		return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))[:32]
	}
	return hex.EncodeToString(buf)
}
