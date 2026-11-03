package rcmesh

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The ICE configuration the cloud delivers in an "ice_config" signaling
// payload.
//
// 【反编译】 the reference's own struct — Go type 0x1097d07e8, recovered
// with the same reader that produced out/rcmesh-1.4.11/recovered-types.json —
// is
//
//	stunServers           []string `json:"stunServers,omitempty"`
//	turnServers           []string `json:"turnServers,omitempty"`
//	signalingTimeoutMs    int64    `json:"signalingTimeoutMs,omitempty"`
//	maxConcurrentSessions int64    `json:"maxConcurrentSessions,omitempty"`
//
// The TURN servers are **strings**, not objects. An entry is either a bare
// TURN URL or a JSON object encoded as a string.
//
// 【反编译】 the reference's parseICEConfigPayload (0x10259da00) trims each
// entry, and when the trimmed entry starts with "{" it decodes it into
// {urls []string `json:"urls"`; username string; credential string}
// (Go type 0x10900d890) and runs the result through decodeICEServerURLs
// (0x10259e110). Its own words for the three rejections are
//
//	Skipping TURN server entry: JSON object has no urls
//	Skipping TURN server entry: urls is not a string or a list: %v
//	Skipping TURN server entry: could not decode the JSON object: %v
//
// A malformed entry is skipped; it never fails the whole configuration.
type iceConfig struct {
	StunServers           []string `json:"stunServers,omitempty"`
	TurnServers           []string `json:"turnServers,omitempty"`
	SignalingTimeoutMs    int64    `json:"signalingTimeoutMs,omitempty"`
	MaxConcurrentSessions int64    `json:"maxConcurrentSessions,omitempty"`
}

// iceTurnEntry is the JSON object a TURN entry may carry as a string.
//
// 【反编译】 Go type 0x10900d890.
type iceTurnEntry struct {
	URLs       iceURLs `json:"urls"`
	Username   string  `json:"username,omitempty"`
	Credential string  `json:"credential,omitempty"`
}

// iceURLs is the "urls" member of a TURN entry.
//
// 【反编译】 the reference's decodeICEServerURLs rejects a "urls" that is
// "not a string or a list", so both shapes are accepted: a bare string and a
// list of strings.
type iceURLs []string

// UnmarshalJSON accepts a string or a list of strings.
func (u *iceURLs) UnmarshalJSON(data []byte) error {
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*u = list
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err != nil {
		return fmt.Errorf("urls is not a string or a list: %w", err)
	}
	*u = []string{one}
	return nil
}

// defaultSTUNServer is the fallback the reference uses when the cloud never
// delivered an ICE configuration.
//
// 【反编译】 the reference's own string: "stun:stun.l.google.com:19302",
// logged as "No ICE config delivered; falling back to public STUN".
const defaultSTUNServer = "stun:stun.l.google.com:19302"

// parseICEConfig decodes an "ice_config" payload into ICE servers.
//
// It returns the servers in the reference's order — STUN first, then TURN —
// and the session cap the payload carries. A payload that is not an ICE
// configuration at all is an error; a malformed TURN *entry* is skipped.
//
// 【反编译】 parseICEConfigPayload 0x10259da00.
func parseICEConfig(payload []byte) ([]ICEServer, int64, error) {
	var cfg iceConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return nil, 0, fmt.Errorf("rcmesh: invalid ice_config payload: %w", err)
	}
	var servers []ICEServer
	for _, stun := range cfg.StunServers {
		stun = strings.TrimSpace(stun)
		if stun == "" {
			continue
		}
		servers = append(servers, ICEServer{URLs: []string{stun}})
	}
	for _, turn := range cfg.TurnServers {
		entry, ok := parseTurnServer(turn)
		if !ok {
			continue
		}
		servers = append(servers, entry)
	}
	return servers, cfg.MaxConcurrentSessions, nil
}

// parseTurnServer turns one "turnServers" entry into an ICE server.
//
// 【反编译】 the reference trims the entry, skips an empty one, and decodes
// the JSON object form into {urls, username, credential}. The object form is
// dropped when it carries no urls.
func parseTurnServer(turn string) (ICEServer, bool) {
	turn = strings.TrimSpace(turn)
	if turn == "" {
		return ICEServer{}, false
	}
	if !strings.HasPrefix(turn, "{") {
		return ICEServer{URLs: []string{turn}}, true
	}
	var entry iceTurnEntry
	if err := json.Unmarshal([]byte(turn), &entry); err != nil {
		return ICEServer{}, false
	}
	if len(entry.URLs) == 0 {
		return ICEServer{}, false
	}
	return ICEServer{URLs: []string(entry.URLs), Username: entry.Username, Credential: entry.Credential}, true
}
