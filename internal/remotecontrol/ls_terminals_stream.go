// ls_terminals_stream.go: the Web UI's terminal output stream.
//
// 【一手】 2026-10-27, the reference's own stream, captured off the
// wire with a raw socket (a Connect stream: an envelope per
// message, the last one flagged 0x02):
//
//	{"output":"VGhlIG9wZXJhdGlvbiBjb3VsZG7igJl0IGJlIGNvbXBsZXRlZC4g…"}
//	{"output":"ZWNobyBNQVJLRVJfQQ0K"}
//	{"title":"bash"}
//
// The frames are the reference's StreamTerminalOutputResponse: the
// `event` oneof written the way proto-JSON writes it — the member
// name at the top level — plus the separate `isScrollback` flag that
// marks the replay frames.
//
// The Web UI reads them through protobuf-es, whose JavaScript
// object shape is `{case:"output",value:bytes}`; the transport
// flattens that into the canonical form above.
package remotecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/justwasm/pigasus/internal/rcterminal"
)

// lsTerminalOutputRequest is the reference's StreamTerminalOutputRequest.
type lsTerminalOutputRequest struct {
	TerminalID string `json:"terminalId"`
}

// lsStreamTerminalOutput serves the Web UI's StreamTerminalOutput.
//
// The registry owns the cadence: one frame per message it produces,
// until the terminal ends or the client goes away.
//
// 【一手】 2026-10-27, measured against the reference binary: a
// StreamTerminalOutput call with an empty body answers HTTP 200 with
// the gRPC-Web content type and no frame at all — the connection
// stays open. The clone holds the stream open for the same case
// instead of failing it.
func (d *Dispatcher) lsStreamTerminalOutput(ctx context.Context, payload string, emit func(json.RawMessage) error) (bool, error) {
	var req lsTerminalOutputRequest
	if len(payload) > 0 {
		if err := json.Unmarshal([]byte(payload), &req); err != nil {
			return true, err
		}
	}
	if strings.TrimSpace(req.TerminalID) == "" {
		<-ctx.Done()
		return true, nil
	}
	registry := d.cfg.WebTerminals
	if registry == nil {
		return false, nil
	}
	return true, registry.Stream(req.TerminalID, func(frame rcterminal.Frame) error {
		return emit(lsTerminalFrame(frame))
	})
}

// lsTerminalFrame renders one frame the way the reference's
// proto-JSON does.
func lsTerminalFrame(frame rcterminal.Frame) json.RawMessage {
	switch frame.Kind {
	case rcterminal.FrameTitle:
		return lsTerminalJSON(map[string]any{"title": frame.Title})
	case rcterminal.FrameCwd:
		return lsTerminalJSON(map[string]any{"cwd": frame.Cwd})
	case rcterminal.FrameExitCode:
		return lsTerminalJSON(map[string]any{"exitCode": frame.ExitCode})
	default:
		out := map[string]any{"output": base64.StdEncoding.EncodeToString(frame.Output)}
		if frame.IsScrollback {
			out["isScrollback"] = true
		}
		return lsTerminalJSON(out)
	}
}

// lsTerminalJSON renders a map as compact JSON.
func lsTerminalJSON(value map[string]any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}
