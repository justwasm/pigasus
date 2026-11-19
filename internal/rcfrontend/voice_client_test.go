package rcfrontend

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/voice/voicepb"
	"google.golang.org/protobuf/proto"
)

func TestFrontend_VoiceClientAssetServed(t *testing.T) {
	handler := Handler(Options{})

	req := httptest.NewRequest(http.MethodGet, "/voice_client.js", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /voice_client.js returned %d, want 200", rr.Code)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, want javascript", ct)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "OpenAGYVoiceClient") {
		t.Errorf("body missing OpenAGYVoiceClient definition")
	}
	if !strings.Contains(body, "PCM24kPlayer") {
		t.Errorf("body missing PCM24kPlayer definition")
	}
	if !strings.Contains(body, "jetski-voice-proto") {
		t.Errorf("body missing jetski-voice-proto subprotocol")
	}
}

func TestFrontend_IndexHTMLIncludesVoiceClient(t *testing.T) {
	handler := Handler(Options{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / returned %d, want 200", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, `<script src="/voice_client.js"></script>`) {
		t.Errorf("index.html missing /voice_client.js script tag")
	}
}

func TestVoiceClient_JSProtobufCrossCompatibilityWithGo(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available, skipping JS protobuf cross-validation")
	}

	// Run node script to serialize Setup, MicAudio, and UserTranscript to stdout (hex encoded)
	script := `
const { VoiceProto } = require("./assets/voice_client.js");

const setupBytes = VoiceProto.encodeSetup({
  conversationId: "conv-cross-test",
  voice: "Finn",
  sampleRate: 16000,
  playbackSpeed: 1.5
});

const micBytes = VoiceProto.encodeMicAudio(new Uint8Array([10, 20, 30, 40]));
const textBytes = VoiceProto.encodeUserTranscript("hello world", true);

console.log(Buffer.from(setupBytes).toString("hex"));
console.log(Buffer.from(micBytes).toString("hex"));
console.log(Buffer.from(textBytes).toString("hex"));
`
	cmd := exec.Command(nodePath, "-e", script)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node execution failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected 3 hex lines, got: %v", lines)
	}

	// 1. Verify Setup decoding in Go
	setupHex := strings.TrimSpace(lines[0])
	setupBin, err := hexToBytes(setupHex)
	if err != nil {
		t.Fatalf("hexToBytes failed: %v", err)
	}
	var clientSetupMsg voicepb.VoiceClientMessage
	if err := proto.Unmarshal(setupBin, &clientSetupMsg); err != nil {
		t.Fatalf("proto.Unmarshal clientSetupMsg failed: %v", err)
	}
	setup := clientSetupMsg.GetSetup()
	if setup == nil {
		t.Fatalf("expected Setup kind in VoiceClientMessage")
	}
	if setup.CascadeId != "conv-cross-test" {
		t.Errorf("CascadeId = %q, want conv-cross-test", setup.CascadeId)
	}
	if setup.Voice != "Finn" {
		t.Errorf("Voice = %q, want Finn", setup.Voice)
	}
	if setup.SampleRateHertz != 16000 {
		t.Errorf("SampleRateHertz = %d, want 16000", setup.SampleRateHertz)
	}
	if setup.PlaybackSpeed != 1.5 {
		t.Errorf("PlaybackSpeed = %f, want 1.5", setup.PlaybackSpeed)
	}

	// 2. Verify MicAudio decoding in Go
	micHex := strings.TrimSpace(lines[1])
	micBin, err := hexToBytes(micHex)
	if err != nil {
		t.Fatalf("hexToBytes failed: %v", err)
	}
	var clientMicMsg voicepb.VoiceClientMessage
	if err := proto.Unmarshal(micBin, &clientMicMsg); err != nil {
		t.Fatalf("proto.Unmarshal clientMicMsg failed: %v", err)
	}
	micAudio := clientMicMsg.GetMicAudio()
	if !bytes.Equal(micAudio, []byte{10, 20, 30, 40}) {
		t.Errorf("MicAudio = %v, want [10, 20, 30, 40]", micAudio)
	}

	// 3. Verify UserTranscript decoding in Go
	textHex := strings.TrimSpace(lines[2])
	textBin, err := hexToBytes(textHex)
	if err != nil {
		t.Fatalf("hexToBytes failed: %v", err)
	}
	var clientTextMsg voicepb.VoiceClientMessage
	if err := proto.Unmarshal(textBin, &clientTextMsg); err != nil {
		t.Fatalf("proto.Unmarshal clientTextMsg failed: %v", err)
	}
	transcript := clientTextMsg.GetUserTranscript()
	if transcript == nil {
		t.Fatalf("expected UserTranscript kind in VoiceClientMessage")
	}
	if transcript.Text != "hello world" {
		t.Errorf("Text = %q, want 'hello world'", transcript.Text)
	}
	if !transcript.IsFinal {
		t.Errorf("IsFinal = false, want true")
	}
}

func hexToBytes(s string) ([]byte, error) {
	var b []byte
	for i := 0; i < len(s); i += 2 {
		var val byte
		for j := 0; j < 2; j++ {
			c := s[i+j]
			var nibble byte
			switch {
			case c >= '0' && c <= '9':
				nibble = c - '0'
			case c >= 'a' && c <= 'f':
				nibble = c - 'a' + 10
			case c >= 'A' && c <= 'F':
				nibble = c - 'A' + 10
			}
			val = (val << 4) | nibble
		}
		b = append(b, val)
	}
	return b, nil
}
