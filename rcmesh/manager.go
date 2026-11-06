package rcmesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Signaler is account-authenticated by the host. Implementations must honor ctx.
type Signaler interface {
	Poll(context.Context, PollRequest) (PollResponse, error)
	Send(context.Context, SendRequest) error
}

// Manager composes signaling and an injected native/browser answerer. Run owns
// its peers; a manager must not be run concurrently or copied while running.
// Unknown signal types fail closed until their wire is recovered.
type Manager struct {
	InstanceID   string
	Signaler     Signaler
	Answer       Answerer
	PeerConfig   PeerConfig
	MaxPeers     int
	PollInterval time.Duration
	// IdlePollInterval is the cadence used while no mesh session is live.
	// Zero means the reference's own idle cadence.
	IdlePollInterval time.Duration
}

// Run stops and closes every peer on cancellation or any signaling failure.
// Readiness and fallback remain the responsibility of the host.
func (m *Manager) Run(ctx context.Context) error {
	if m == nil || m.InstanceID == "" || m.Signaler == nil {
		return errors.New("rcmesh: incomplete manager configuration")
	}
	if m.Answer == nil {
		m.Answer = DefaultAnswerer()
	}
	if m.Answer == nil {
		return errors.New("rcmesh: incomplete manager configuration: no answerer registered")
	}
	if m.MaxPeers <= 0 {
		m.MaxPeers = 8
	}
	if m.PollInterval <= 0 {
		m.PollInterval = time.Second
	}
	if m.IdlePollInterval <= 0 {
		m.IdlePollInterval = idlePollInterval
	}
	peers := map[string]Session{}
	pendingCandidates := map[string][]ICECandidate{}
	// pendingPins holds the server-vouched keys of a session whose handshake
	// has not completed yet.
	//
	// 【反编译】 the reference keeps a pendingPins map on the MeshDaemon and
	// drops an entry that "no handshake arrived within %v"
	// (expirePendingPinsLocked, 0x102596b90 — the deadline is the pin's
	// creation time plus five seconds, 0x12A05F200 in the same function).
	// The map is bounded: "[MeshDaemon] pendingPins full, dropping oldest
	// pin for session %s" (evictOldestPendingPinLocked, 0x102597260).
	pendingPins := map[string]pendingPin{}
	defer func() {
		for _, p := range peers {
			_ = p.Close()
		}
	}()
	keys := append([]string(nil), m.PeerConfig.AuthorizedKeys...)
	seen := map[string]bool{}
	// Inclusive cursor overlap avoids dropping distinct messages sharing a timestamp.
	cursor := ""
	backoff := time.Duration(0)
	for {
		response, err := m.Signaler.Poll(ctx, PollRequest{InstanceID: m.InstanceID, AfterTimestamp: cursor, IncludeAuthorizedKeys: true})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// 【反编译】 a failed poll is not fatal in the reference: its own words
			// are "Signaling poll failed: %v" and the loop keeps running. The
			// wait doubles on every failure and is capped at 30s.
			backoff = nextPollBackoff(backoff)
			if err := sleepPoll(ctx, m.pollCadence(len(peers)), backoff); err != nil {
				return err
			}
			continue
		}
		backoff = 0
		if bundle := response.AuthorizedKeyBundle; bundle != nil && bundle.Delivered {
			var normalized []string
			for _, key := range bundle.WebCryptoPubKeys {
				if pk, err := ParsePublicKey(key); err == nil {
					normalized = append(normalized, publicKeyPEM(pk))
				}
			}
			changed := !sameKeys(keys, normalized)
			keys = normalized
			// 【反编译】 a refreshed bundle replaces the instance-wide pin
			// (ReplaceInstancePin, 0x102596d50) and revokes the sessions whose
			// authenticated key the bundle no longer contains: "[MeshDaemon]
			// Revoking session %q: a refreshed authorized bundle no longer
			// contains its authenticated key". A session that authenticated
			// with a key the bundle still carries keeps running.
			if changed {
				for id, p := range peers {
					if identity, ok := p.(SessionIdentity); ok {
						if authenticated := identity.AuthenticatedKey(); authenticated != "" && !containsKey(keys, authenticated) {
							_ = p.Close()
							delete(peers, id)
							delete(pendingCandidates, id)
							delete(pendingPins, id)
							continue
						}
					}
				}
			}
		}
		expirePendingPins(pendingPins, time.Now())
		for id, p := range peers {
			select {
			case <-p.Done():
				delete(peers, id)
				delete(pendingCandidates, id)
				delete(pendingPins, id)
			default:
			}
		}
		for _, detail := range response.Messages {
			var signal SignalingEnvelope
			if err := json.Unmarshal([]byte(detail.Payload), &signal); err != nil {
				// 【反编译】 processSignalingMessage (0x1025a3430) logs "Failed to unmarshal signaling message: %v"
				// and returns 1 (marked processed); single malformed envelope does not abort the manager loop.
				continue
			}
			// 【反编译】 Self-echo check: if sender is self, drop message immediately.
			if signal.SenderID != "" && signal.SenderID == m.InstanceID {
				continue
			}
			// If message is explicitly addressed to another recipient, reject misdirected offer/signal.
			if signal.RecipientID != "" && signal.RecipientID != m.InstanceID {
				return errors.New("rcmesh: invalid signaling identity")
			}
			// Resolve session ID from envelope or detail
			sessionID := detail.SessionID
			if sessionID == "" {
				sessionID = signal.SessionID
			}

			// Dedup check: only dedup when MessageID is present.
			// Server-injected messages (authorized_key, ice_config) have empty MessageID.
			if signal.MessageID != "" {
				id := sessionID + "\x00" + signal.MessageID
				if seen[id] {
					continue
				}
				seen[id] = true
				if len(seen) >= 4096 {
					pruned := 0
					for k := range seen {
						delete(seen, k)
						pruned++
						if pruned >= 2048 {
							break
						}
					}
				}
			}
			stamp, _ := time.Parse(time.RFC3339Nano, detail.CreatedAt)

			switch strings.ToLower(signal.PayloadType) {
			case "offer", "sdp_offer":
				if sessionID == "" {
					continue
				}
				if len(keys) == 0 && len(pendingPins[sessionID].keys) == 0 {
					return ErrAuthentication
				}
				if peers[sessionID] != nil {
					continue
				}
				if len(peers) >= m.MaxPeers {
					continue
				}
				var offer SessionDescription
				if err := json.Unmarshal(signal.Payload, &offer); err != nil {
					continue
				}
				if offer.Type == "" && offer.SDP != "" {
					offer.Type = "offer"
				}
				if offer.Type != "offer" {
					continue
				}
				config := m.PeerConfig
				// 【反编译】 the reference pins the server-vouched keys on the
				// session (SetSessionPin, 0x102596850) and the challenge-response
				// is verified against that pin (ExecuteChallengeResponse,
				// 0x10259b090). A session that already has a pending pin uses
				// it; otherwise the instance-wide bundle is the trust set.
				if pin, ok := pendingPins[sessionID]; ok {
					config.AuthorizedKeys = append([]string(nil), pin.keys...)
				} else {
					config.AuthorizedKeys = append([]string(nil), keys...)
				}
				if config.ICEServers == nil {
					// 【反编译】 the reference falls back to the public STUN server
					// when no ICE configuration was delivered: "No ICE config
					// delivered; falling back to public STUN".
					config.ICEServers = []ICEServer{{URLs: []string{defaultSTUNServer}}}
				}
				config.SessionID = sessionID
				recipientID := signal.SenderID
				var candidateMu sync.Mutex
				var pendingOutbound []ICECandidate
				answerSent := false

				sendCandidate := func(cand ICECandidate) {
					candBytes, err := json.Marshal(cand)
					if err != nil {
						return
					}
					// 【反编译】 sendSignalingCandidate (0x1025a6b00) formats "cand-%d"
					// with current Unix nanosecond clock (0x1034d25a2), and PayloadType "candidate".
					// The payload is the bare pion ICECandidateInit JSON — the daemon
					// marshals it (setupPeerConnectionCallbacks.func1, 0x1025ba880) and
					// the message carries it through unchanged.
					_ = m.Signaler.Send(ctx, SendRequest{
						SessionID:   sessionID,
						Payload:     string(candBytes),
						MessageID:   fmt.Sprintf("cand-%d", time.Now().UnixNano()),
						SenderID:    m.InstanceID,
						RecipientID: recipientID,
						PayloadType: "candidate",
					})
				}

				config.OnLocalCandidate = func(cand ICECandidate) {
					candidateMu.Lock()
					if !answerSent {
						pendingOutbound = append(pendingOutbound, cand)
						candidateMu.Unlock()
						return
					}
					candidateMu.Unlock()
					sendCandidate(cand)
				}

				peer, err := m.Answer(ctx, offer.SDP, config)
				if err != nil || peer == nil {
					continue
				}
				peers[sessionID] = peer
				if pending := pendingCandidates[sessionID]; len(pending) > 0 {
					for _, c := range pending {
						_ = peer.AddICECandidate(c)
					}
					delete(pendingCandidates, sessionID)
				}
				// 【反编译】 the reference's answer message id is "ans-%d" with the
				// current Unix nanosecond clock (0x1034c0719, fmt.Sprintf in
				// sendSignalingAnswer 0x1025a6690), and the payload is the bare
				// pion SessionDescription JSON ({"type":"answer","sdp":"…"}).
				payload, _ := json.Marshal(SessionDescription{Type: "answer", SDP: peer.AnswerSDP()})
				if err := m.Signaler.Send(ctx, SendRequest{
					SessionID:   sessionID,
					Payload:     string(payload),
					MessageID:   fmt.Sprintf("ans-%d", time.Now().UnixNano()),
					SenderID:    m.InstanceID,
					RecipientID: recipientID,
					PayloadType: "answer",
				}); err != nil {
					_ = peer.Close()
					delete(peers, sessionID)
					continue
				}

				// Flush any outbound trickle candidates gathered before the answer was sent.
				candidateMu.Lock()
				answerSent = true
				toFlush := pendingOutbound
				pendingOutbound = nil
				candidateMu.Unlock()

				for _, cand := range toFlush {
					sendCandidate(cand)
				}
			case "candidate", "ice_candidate":
				// 【反编译】 the reference routes both "candidate" and
				// "ice_candidate" to handleRemoteCandidate (0x1025a46b0): the
				// nine-byte "candidat" compare and the thirteen-byte
				// "ice_cand" compare share one target in
				// processSignalingMessage 0x1025a3430.
				if sessionID == "" {
					continue
				}
				var cand ICECandidate
				if err := json.Unmarshal(signal.Payload, &cand); err != nil {
					continue
				}
				if peer := peers[sessionID]; peer != nil {
					_ = peer.AddICECandidate(cand)
				} else {
					pendingCandidates[sessionID] = append(pendingCandidates[sessionID], cand)
				}
			case "ice_config":
				// 【反编译】 the reference parses the payload with
				// parseICEConfigPayload (0x10259da00): the TURN servers are
				// strings, and an entry is either a bare URL or a JSON object
				// encoded as a string.
				servers, maxSessions, err := parseICEConfig(signal.Payload)
				if err != nil {
					continue
				}
				if len(servers) > 0 {
					m.PeerConfig.ICEServers = servers
				}
				if maxSessions > 0 {
					m.MaxPeers = int(maxSessions)
				}
			case "authorized_key":
				// 【反编译】 the reference has a dedicated handler for this
				// payload type — handleAuthorizedKey 0x1025a42a0, whose own
				// words are "Failed to unmarshal authorized_key payload: %v"
				// and "Ignoring authorized_key signaling message with empty
				// WebCryptoPubKey". It pins the server-vouched keys onto
				// the session (SetSessionPin 0x102596850): a session that is
				// already up is pinned right away, a session that is still
				// staging keeps the pin until the handshake arrives.
				authorized, err := parseAuthorizedKeyPayload(signal.Payload)
				if err != nil {
					continue
				}
				var validKeys []string
				for _, key := range authorized {
					if pk, err := ParsePublicKey(key); err == nil {
						validKeys = append(validKeys, publicKeyPEM(pk))
					}
				}
				if len(validKeys) == 0 {
					// The reference ignores an empty bundle rather than
					// failing the session.
					break
				}
				if sessionID != "" {
					recordPendingPin(pendingPins, sessionID, validKeys, time.Now())
				}
				for _, k := range validKeys {
					if !containsKey(keys, k) {
						keys = append(keys, k)
					}
				}
				if sessionID != "" {
					if p := peers[sessionID]; p != nil {
						if pinner, ok := p.(SessionPinner); ok {
							if err := pinner.SetSessionPin(validKeys); err != nil {
								// The pin does not cover the key the session
								// authenticated with: the session is revoked.
								_ = p.Close()
								delete(peers, sessionID)
								delete(pendingCandidates, sessionID)
								delete(pendingPins, sessionID)
							}
						}
					}
				}
			default:
				// Unrecognized signaling type: skip and mark processed matching reference behavior
			}
			if !stamp.IsZero() {
				if cursor == "" {
					cursor = stamp.Format(time.RFC3339Nano)
				} else if previous, err := time.Parse(time.RFC3339Nano, cursor); err == nil && stamp.After(previous) {
					cursor = stamp.Format(time.RFC3339Nano)
				}
			}
		}
		if err := sleepPoll(ctx, m.pollCadence(len(peers)), backoff); err != nil {
			return err
		}
	}
}

// idlePollInterval is the cadence the reference uses when no mesh session is
// live.
//
// 【反编译】 pollStateCadence (0x1025a1e00) returns one second while the
// mesh is busy and, when it is idle, a value derived from the staging
// timeout — five seconds with the reference's default staging timeout —
// clamped between one and thirty seconds.
const idlePollInterval = 5 * time.Second

// maxPollBackoff is the ceiling the reference puts on the failed-poll wait.
//
// 【反编译】 pollSignalingLoop (0x1025a2090) doubles the wait after every
// failure and clamps it at 0x6fc23ac01 — thirty seconds.
const maxPollBackoff = 30 * time.Second

// pollCadence is the reference's poll interval for the current mesh state.
func (m *Manager) pollCadence(peers int) time.Duration {
	if peers > 0 {
		return m.PollInterval
	}
	return m.IdlePollInterval
}

// sleepPoll waits for the next poll.
//
// 【反编译】 the reference's interval is the larger of the backoff and the
// cadence.
func sleepPoll(ctx context.Context, cadence, backoff time.Duration) error {
	wait := cadence
	if backoff > wait {
		wait = backoff
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// nextPollBackoff doubles the wait after a failed poll.
//
// 【反编译】 pollSignalingLoop: the backoff starts at one second, doubles on
// every failure and is clamped at thirty seconds.
func nextPollBackoff(current time.Duration) time.Duration {
	if current < time.Second {
		current = time.Second
	}
	next := current * 2
	if next > maxPollBackoff {
		next = maxPollBackoff
	}
	return next
}

// containsKey reports whether the trust set already carries the key.
func containsKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// pendingPin is a server-vouched key set waiting for the session's handshake.
//
// 【反编译】 the reference's pendingPins map holds the pin and its creation
// time; a pin that no handshake claims within pendingPinTimeout is dropped.
type pendingPin struct {
	keys    []string
	created time.Time
}

// pendingPinTimeout is how long a pending pin waits for its handshake.
//
// 【反编译】 expirePendingPinsLocked (0x102596b90) compares the pin's
// creation time plus five seconds (0x12A05F200) against the clock; the
// reference's own words are "[MeshDaemon] Dropping the pending pin for
// session %s: no handshake arrived within %v".
const pendingPinTimeout = 5 * time.Second

// maxPendingPins bounds the pending-pin map.
//
// 【反编译】 "[MeshDaemon] pendingPins full, dropping oldest pin for session
// %s" (evictOldestPendingPinLocked, 0x102597260) — the oldest pin goes.
const maxPendingPins = 32

// recordPendingPin stores a session's pin, evicting the oldest one when the
// map is full.
func recordPendingPin(pins map[string]pendingPin, sessionID string, keys []string, now time.Time) {
	if _, ok := pins[sessionID]; !ok && len(pins) >= maxPendingPins {
		oldest := ""
		var oldestAt time.Time
		for id, pin := range pins {
			if oldest == "" || pin.created.Before(oldestAt) {
				oldest = id
				oldestAt = pin.created
			}
		}
		delete(pins, oldest)
	}
	pins[sessionID] = pendingPin{keys: append([]string(nil), keys...), created: now}
}

// expirePendingPins drops the pins no handshake claimed in time.
func expirePendingPins(pins map[string]pendingPin, now time.Time) {
	for id, pin := range pins {
		if now.Sub(pin.created) >= pendingPinTimeout {
			delete(pins, id)
		}
	}
}

// parseAuthorizedKeyPayload reads the WebCrypto public keys an
// "authorized_key" signaling message carries.
//
// 【反编译】 handleAuthorizedKey (0x1025a42a0) unmarshals struct { WebCryptoPubKey string }
// (Go type 0x10945d308) and sets the session pin. The payload also accepts plural
// bundle {webCryptoPubKeys: [...]}, a bare list of string keys, or a bare string key.
func parseAuthorizedKeyPayload(payload []byte) ([]string, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil, nil
	}
	// 1. Singular object: {"webCryptoPubKey": "..."}
	var singular struct {
		WebCryptoPubKey string `json:"webCryptoPubKey"`
	}
	if err := json.Unmarshal(trimmed, &singular); err == nil && singular.WebCryptoPubKey != "" {
		return []string{singular.WebCryptoPubKey}, nil
	}
	// 2. Plural bundle: {"webCryptoPubKeys": ["..."]}
	var bundle struct {
		WebCryptoPubKeys []string `json:"webCryptoPubKeys"`
	}
	if err := json.Unmarshal(trimmed, &bundle); err == nil && len(bundle.WebCryptoPubKeys) > 0 {
		return bundle.WebCryptoPubKeys, nil
	}
	// 3. Array of strings: ["..."]
	var list []string
	if err := json.Unmarshal(trimmed, &list); err == nil && len(list) > 0 {
		return list, nil
	}
	// 4. Single JSON string: "..."
	var str string
	if err := json.Unmarshal(trimmed, &str); err == nil && str != "" {
		return []string{str}, nil
	}
	return nil, fmt.Errorf("rcmesh: invalid authorized_key payload: %s", string(payload))
}
func sameKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]int{}
	for _, key := range a {
		set[key]++
	}
	for _, key := range b {
		set[key]--
		if set[key] < 0 {
			return false
		}
	}
	return true
}
