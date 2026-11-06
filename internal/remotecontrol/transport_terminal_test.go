package remotecontrol_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/lsproto"
	"github.com/justwasm/pigasus/originpolicy"
	"github.com/justwasm/pigasus/rcterminal"
	"github.com/justwasm/pigasus/rctoken"
	"github.com/justwasm/pigasus/remotetransport"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestTCPServer_CreateTerminalAndStreamOutput(t *testing.T) {
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}

	termReg := rcterminal.NewRegistry()
	sess := remotecontrol.NewSessionState("1.4.7-test")
	sess.Workspace = t.TempDir()
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:      sess,
		Agents:       remotecontrol.DefaultAgentLister,
		Reader:       remotecontrol.DefaultOutputReader{},
		WebTerminals: termReg,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	policy := originpolicy.OriginPolicy{Patterns: []string{"https://antigravity.google.com"}}
	srv, err := remotetransport.NewTCPServer(context.Background(), disp, remotetransport.TCPConfig{
		Addr:         "127.0.0.1:0",
		Path:         "/rpc",
		OriginPolicy: &policy,
		WebUIHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	})
	if err != nil {
		t.Fatalf("NewTCPServer: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// 1. Send CreateTerminal with binary proto
	inMsg, ok := codec.InputMessage("CreateTerminal")
	if !ok {
		t.Fatalf("InputMessage(CreateTerminal) not found")
	}
	dynReq := dynamicpb.NewMessage(inMsg)
	bCreateReq, err := proto.Marshal(dynReq)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/CreateTerminal"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bCreateReq))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(rctoken.ViaRemoteControlHeader, "true")
	req.Header.Set("Content-Type", "application/proto")
	createResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("CreateTerminal status = %d", createResp.StatusCode)
	}
	createBody, err := io.ReadAll(createResp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	decCreate, err := codec.DecodeResponse("CreateTerminal", createBody)
	if err != nil {
		t.Fatalf("DecodeResponse CreateTerminal: %v (%x)", err, createBody)
	}
	t.Logf("CreateTerminal response: %s", string(decCreate))

	var createAns struct {
		Terminal struct {
			TerminalID string `json:"terminalId"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal(decCreate, &createAns); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	terminalID := createAns.Terminal.TerminalID
	if terminalID == "" {
		t.Fatalf("no terminalId in response: %s", string(decCreate))
	}

	// 2. Send StreamTerminalOutput with streaming Connect proto
	streamInMsg, ok := codec.InputMessage("StreamTerminalOutput")
	if !ok {
		t.Fatalf("InputMessage(StreamTerminalOutput) not found")
	}
	streamDynReq := dynamicpb.NewMessage(streamInMsg)
	streamDynReq.Set(streamInMsg.Fields().ByName("terminal_id"), protoreflect.ValueOfString(terminalID))
	bStreamReq, err := proto.Marshal(streamDynReq)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	// Connect envelope around stream request
	envelopedReq := append([]byte{0x00, 0, 0, 0, byte(len(bStreamReq))}, bStreamReq...)

	streamURL := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/StreamTerminalOutput"
	streamReq, err := http.NewRequest(http.MethodPost, streamURL, bytes.NewReader(envelopedReq))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	streamReq.Header.Set(rctoken.ViaRemoteControlHeader, "true")
	streamReq.Header.Set("X-Jetski-Streaming", "true")
	streamReq.Header.Set("Content-Type", "application/connect+proto")

	resp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("POST StreamTerminalOutput: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StreamTerminalOutput status = %d", resp.StatusCode)
	}

	// Read first envelope: header (5 bytes)
	hdr := make([]byte, 5)
	if _, err := io.ReadFull(resp.Body, hdr); err != nil {
		t.Fatalf("read first envelope header: %v", err)
	}
	flags := hdr[0]
	length := binary.BigEndian.Uint32(hdr[1:5])
	payload := make([]byte, length)
	if _, err := io.ReadFull(resp.Body, payload); err != nil {
		t.Fatalf("read first envelope payload: %v", err)
	}
	t.Logf("First envelope: flags=%#x, len=%d", flags, length)

	decFrame, err := codec.DecodeResponse("StreamTerminalOutput", payload)
	if err != nil {
		t.Fatalf("DecodeResponse StreamTerminalOutput frame: %v", err)
	}
	t.Logf("First frame: %s", string(decFrame))
}
