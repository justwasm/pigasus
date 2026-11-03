package rcmesh

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// ICECandidate is the browser/Pion candidate-init JSON shape. Keeping it here
// lets a signaling host compile without importing a native WebRTC stack.
type ICECandidate struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdpMLineIndex,omitempty"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}
type ICEServer struct {
	URLs       []string
	Username   string
	Credential string
}

// PeerConfig has no Pion types. AuthorizedKeys must be obtained from the
// account-authenticated signaling service, not supplied by the offerer.
// Handler must honor request cancellation. Zero timeouts use adapter defaults.
//
// SessionID names the mesh session the peer serves. It scopes the session's
// chunk reassembly: the reference keys its reassembly buffers by session and
// answers a chunk that names no session it knows about.
type PeerConfig struct {
	AuthorizedKeys   []string
	ICEServers       []ICEServer
	Handler          http.Handler
	GatherTimeout    time.Duration
	AuthTimeout      time.Duration
	SendTimeout      time.Duration
	SessionID        string
	OnLocalCandidate func(ICECandidate)
}

// Session is an answer-side mesh connection. AnswerSDP includes gathered ICE
// candidates; AddICECandidate additionally accepts remote trickle candidates.
// Done closes after transport shutdown; Err records the first failure.
type Session interface {
	AnswerSDP() string
	AddICECandidate(ICECandidate) error
	Authenticated() bool
	Done() <-chan struct{}
	Err() error
	Close() error
}

// SessionPinner pins server-vouched public keys directly on an active session.
//
// 【反编译】 SetSessionPin 0x102596850.
type SessionPinner interface {
	SetSessionPin([]string) error
}

// SessionIdentity exposes the key a session authenticated with.
//
// 【反编译】 a refreshed authorized bundle replaces the instance-wide pin
// (ReplaceInstancePin 0x102596d50) and revokes the sessions whose
// authenticated key the bundle no longer contains.
type SessionIdentity interface {
	AuthenticatedKey() string
}

// Answerer is the injection point for native Pion or browser WebRTC adapters.
// Merely importing this type does not enable or advertise a mesh transport.
type Answerer func(context.Context, string, PeerConfig) (Session, error)

var (
	answererMu      sync.RWMutex
	defaultAnswerer Answerer
)

// RegisterDefaultAnswerer registers the default Answerer implementation.
func RegisterDefaultAnswerer(a Answerer) {
	answererMu.Lock()
	defer answererMu.Unlock()
	defaultAnswerer = a
}

// DefaultAnswerer returns the registered default Answerer, or nil if none is set.
func DefaultAnswerer() Answerer {
	answererMu.RLock()
	defer answererMu.RUnlock()
	return defaultAnswerer
}
