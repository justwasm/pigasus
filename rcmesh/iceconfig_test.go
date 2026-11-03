package rcmesh

import "testing"

// The reference's ICE configuration carries the TURN servers as strings, and
// an entry is either a bare URL or a JSON object encoded as a string.
//
// 【反编译】 Go type 0x1097d07e8 and parseICEConfigPayload 0x10259da00.
func TestParseICEConfigTurnServersAreStrings(t *testing.T) {
	payload := []byte(`{
		"stunServers":["stun:stun.l.google.com:19302"],
		"turnServers":[
			"turn:turn.example.com:3478?transport=udp",
			"{\"urls\":[\"turn:turn.example.com:3478?transport=tcp\"],\"username\":\"u\",\"credential\":\"p\"}",
			"{\"urls\":\"turn:turn.example.com:3478?transport=tls\",\"username\":\"u2\"}",
			"",
			"{\"username\":\"no-urls\"}",
			"{\"urls\":"
		],
		"signalingTimeoutMs":45000,
		"maxConcurrentSessions":16
	}`)
	servers, maxSessions, err := parseICEConfig(payload)
	if err != nil {
		t.Fatalf("parseICEConfig: %v", err)
	}
	if maxSessions != 16 {
		t.Errorf("maxSessions = %d, want 16", maxSessions)
	}
	if len(servers) != 4 {
		t.Fatalf("servers = %d, want 4: %+v", len(servers), servers)
	}
	if servers[0].URLs[0] != "stun:stun.l.google.com:19302" {
		t.Errorf("stun = %q", servers[0].URLs[0])
	}
	if servers[1].URLs[0] != "turn:turn.example.com:3478?transport=udp" {
		t.Errorf("bare turn = %q", servers[1].URLs[0])
	}
	if servers[2].Username != "u" || servers[2].Credential != "p" {
		t.Errorf("object turn = %+v", servers[2])
	}
	if servers[2].URLs[0] != "turn:turn.example.com:3478?transport=tcp" {
		t.Errorf("object turn urls = %q", servers[2].URLs[0])
	}
	if servers[3].URLs[0] != "turn:turn.example.com:3478?transport=tls" || servers[3].Username != "u2" {
		t.Errorf("single-url object turn = %+v", servers[3])
	}
}

// A "urls" member is a string or a list of strings.
func TestParseICEConfigURLsAcceptsBothShapes(t *testing.T) {
	servers, _, err := parseICEConfig([]byte(`{"turnServers":["{\"urls\":\"turn:one.example.com\"}"]}`))
	if err != nil {
		t.Fatalf("parseICEConfig: %v", err)
	}
	if len(servers) != 1 || servers[0].URLs[0] != "turn:one.example.com" {
		t.Fatalf("servers = %+v", servers)
	}
}

// A payload that is not an ICE configuration at all is an error.
func TestParseICEConfigRejectsGarbage(t *testing.T) {
	if _, _, err := parseICEConfig([]byte(`{"stunServers":`)); err == nil {
		t.Fatal("expected an error for a malformed ice_config payload")
	}
}
