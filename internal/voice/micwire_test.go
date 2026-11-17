package voice

import (
	"bytes"
	"io"
	"testing"
)

func TestMicwire_FrameRoundtrip(t *testing.T) {
	buf := new(bytes.Buffer)
	payload := []byte("pcm-audio-sample-data-12345678")

	if err := WriteFrame(buf, FrameAudio, payload); err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	ft, gotPayload, err := ReadFrame(buf)
	if err != nil {
		t.Fatalf("ReadFrame failed: %v", err)
	}
	if ft != FrameAudio {
		t.Fatalf("expected frameType == FrameAudio (0x01), got 0x%02x", ft)
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("payload mismatch: got %q, want %q", gotPayload, payload)
	}
}

func TestMicwire_Handshake(t *testing.T) {
	rate, err := ParseSpeakerHello("JETSKI-AUDIO/1 speaker rate=24000\n")
	if err != nil {
		t.Fatalf("ParseSpeakerHello failed: %v", err)
	}
	if rate != 24000 {
		t.Fatalf("expected rate == 24000, got %d", rate)
	}

	// Test SpeakerHello roundtrip with mock pipe
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	clientRW := struct {
		io.Reader
		io.Writer
	}{clientReader, clientWriter}

	serverDone := make(chan error, 1)
	go func() {
		line, err := ReadLine(serverReader)
		if err != nil {
			serverDone <- err
			return
		}
		r, err := ParseSpeakerHello(line)
		if err != nil || r != 24000 {
			serverDone <- err
			return
		}
		serverDone <- ReplyOK(serverWriter)
	}()

	if err := SpeakerHello(clientRW, 24000); err != nil {
		t.Fatalf("SpeakerHello client failed: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("SpeakerHello server failed: %v", err)
	}
}

func TestMicwire_ServerClientRoundtrip(t *testing.T) {
	server, err := NewMicServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewMicServer failed: %v", err)
	}
	defer server.Close()

	go func() {
		_ = server.Serve()
	}()

	addr := server.Addr().String()
	receivedCh := make(chan []byte, 10)
	onAudio := func(chunk []byte) {
		receivedCh <- chunk
	}

	stream, err := DialRemoteAudio(addr, onAudio)
	if err != nil {
		t.Fatalf("DialRemoteAudio failed: %v", err)
	}
	defer stream.Close()

	player := NewRemoteAudioPlayer(stream)
	capture := NewRemoteAudioCapture(stream)

	if capture.IsMuted() {
		t.Fatal("expected unmuted initially")
	}
	capture.ToggleMute()
	if !capture.IsMuted() {
		t.Fatal("expected muted after toggle")
	}
	capture.ToggleMute()

	// Send speaker frame from client to server
	testPCM := make([]byte, 1024)
	n, err := player.Write(testPCM)
	if err != nil {
		t.Fatalf("player.Write failed: %v", err)
	}
	if n != len(testPCM) {
		t.Fatalf("player.Write wrote %d bytes, want %d", n, len(testPCM))
	}

	player.Clear()
}
