// Package refbackend — connect.go: the Connect streaming codec and the
// reference's server-streaming method list.
//
// The reference's server-streaming calls answer with the Connect
// streaming protocol: the body is a sequence of envelopes, each one a
// flags byte, a big-endian uint32 length and the message bytes. The
// flags byte's 0x02 bit marks the last envelope of the stream.
//
// Evidence grade: 一手 (the reference's own `JetboxSubscribeToState`
// answer, captured off the wire with a raw socket).
package refbackend

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/justwasm/pigasus/remotetransport"
)

// streamingMethods are the reference's server-streaming methods. The
// list comes from the IDE's own service descriptor, recovered from the
// frontend bundle (out/ide-language-server/extract_ls_descriptor.py).
var streamingMethods = map[string]bool{
	"GetSidecarLogs":                        true,
	"HandleStreamingCommand":                true,
	"JetboxSubscribeToGcertState":           true,
	"JetboxSubscribeToOAuthState":           true,
	"JetboxSubscribeToState":                true,
	"JetboxSubscribeToSummaries":            true,
	"ProjectUpdatesStream":                  true,
	"SetupJetskiChat":                       true,
	"StartNarration":                        true,
	"StreamAgentStateUpdates":               true,
	"StreamAudioTranscription":              true,
	"StreamCascadePanelReactiveUpdates":     true,
	"StreamCascadeReactiveUpdates":          true,
	"StreamCascadeSummariesReactiveUpdates": true,
	"StreamSearchCode":                      true,
	"StreamTerminalOutput":                  true,
	"StreamUserTrajectoryReactiveUpdates":   true,
	"SubscribeToAgentBrowserRequests":       true,
	"SubscribeToMcpServerStates":            true,
	"SubscribeToPlugins":                    true,
	"SubscribeToSidecars":                   true,
	"WatchDirectory":                        true,
	"WatchVersionControlState":              true,
}

// isStreamingMethod reports whether the reference serves the method as a
// server stream.
func isStreamingMethod(method string) bool {
	return streamingMethods[strings.TrimSpace(method)]
}

// DispatchStream serves one server-streaming RPC by relaying the
// reference's frames. It reports handled=false for a method the reference
// does not stream, so the transport can fall back.
//
// The dispatcher owns the frame cadence: one emit per reference frame,
// and the call returns when the reference's stream ends.
func (d *Dispatcher) DispatchStream(ctx context.Context, method string, payloadText string, emit func(json.RawMessage) error) (bool, error) {
	if !isStreamingMethod(method) {
		return false, nil
	}
	body := strings.TrimSpace(payloadText)
	if body == "" {
		body = "{}"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url(method),
		strings.NewReader(string(connectEnvelope([]byte(body)))))
	if err != nil {
		return true, err
	}
	req.Header.Set("Content-Type", "application/connect+json")
	req.Header.Set("Accept", "application/connect+json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if token := d.csrfToken(ctx); token != "" {
		req.Header.Set(csrfHeader, token)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return true, d.rpcError(method, resp.StatusCode, string(raw))
	}
	return true, pumpEnvelopes(resp.Body, emit)
}

// connectEnvelope frames one message for the Connect streaming protocol.
func connectEnvelope(payload []byte) []byte {
	return remotetransport.ConnectEnvelope(remotetransport.ConnectFlagMessage, payload)
}

// pumpEnvelopes reads the envelope stream and hands every message to
// emit. It stops at the end-of-stream envelope.
//
// The framing is remotetransport's codec — the same one the
// transport writes towards the caller — so the two halves of the clone
// cannot drift apart.
func pumpEnvelopes(r io.Reader, emit func(json.RawMessage) error) error {
	br := bufio.NewReader(r)
	for {
		flags, payload, err := remotetransport.ReadConnectEnvelope(br)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				// The stream ended — at an envelope boundary or
				// inside a header. Either way there is no more
				// message to hand over.
				return nil
			}
			if errors.Is(err, remotetransport.ErrTruncatedMessage) {
				// The peer broke the protocol mid-message.
				return io.ErrUnexpectedEOF
			}
			return err
		}
		if flags&remotetransport.ConnectFlagEndStream != 0 {
			return nil
		}
		if err := emit(json.RawMessage(payload)); err != nil {
			return err
		}
	}
}
