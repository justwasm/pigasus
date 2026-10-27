// Package remotecontrol — ls_streams.go: the server streams the
// Antigravity Web UI keeps open for the life of the session.
//
//   - JetboxSubscribeToState     — the app state and the user config.
//   - JetboxSubscribeToSummaries — the conversation index.
//   - ProjectUpdatesStream       — the project ids.
//   - SubscribeToMcpServerStates — the MCP servers.
//   - SubscribeToPlugins         — the plugins.
//
// The reference keeps all of them open: the frontend reconnects the
// moment a stream ends. A clone that answers them with a single frame
// is re-asked thousands of times a minute.
//
// 【一手】 2026-10-26, live: one browser session against the clone's
// daemon produced 5,706 inbound JetboxSubscribeToSummaries calls,
// 5,699 JetboxSubscribeToState calls and 677 ProjectUpdatesStream
// calls — the reconnect storm a one-frame answer produces.
//
// 【一手】 2026-10-26, live: the two subscription streams the clone did
// not keep open were worse than a storm. The Web UI re-subscribed to
// them every 2.4 s and re-fetched the whole bootstrap with them — the
// skills, the workflows, the slash commands and the MCP prompts — so
// the page never settled: 176 full bootstraps in ten minutes against
// a clone whose three other streams stayed open the whole time.
package remotecontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
)

// lsStreamPoll is how often a stream looks for a change.
const lsStreamPoll = 2 * time.Second

// streamJetboxSubscribeToState serves the app-state stream. It emits
// the current state, then one frame per change.
//
// The frame is exa.language_server_pb.JetboxSubscribeToStateResponse:
// the `appState` and `userConfig` blocks.
func (d *Dispatcher) streamJetboxSubscribeToState(ctx context.Context, payload string, emit func(json.RawMessage) error) (bool, error) {
	frame, err := marshalAnswer("JetboxSubscribeToState", d.state.snapshot())
	if err != nil {
		return true, err
	}
	if err := emit(frame); err != nil {
		return true, err
	}
	prev := string(frame)
	for {
		if !lsSleep(ctx, lsStreamPoll) {
			return true, nil
		}
		frame, err := marshalAnswer("JetboxSubscribeToState", d.state.snapshot())
		if err != nil || string(frame) == prev {
			continue
		}
		prev = string(frame)
		if err := emit(frame); err != nil {
			return true, err
		}
	}
}

// streamJetboxSubscribeToSummaries serves the conversation index. It
// emits the whole index first, then one frame per change.
//
// The frame is exa.language_server_pb.JetboxSubscribeToSummariesResponse:
// the `updates` map — the conversation id → the summary — and the
// `deletes` list, which the clone never fills.
func (d *Dispatcher) streamJetboxSubscribeToSummaries(ctx context.Context, payload string, emit func(json.RawMessage) error) (bool, error) {
	updates := lsSummaryUpdates()
	if err := emitLSJSON(emit, "JetboxSubscribeToSummaries", map[string]any{"updates": updates, "deletes": []any{}}); err != nil {
		return true, err
	}
	stamp := lsSummariesStamp()
	for {
		if !lsSleep(ctx, lsStreamPoll) {
			return true, nil
		}
		next := lsSummariesStamp()
		if next == stamp {
			continue
		}
		stamp = next
		updates = lsSummaryUpdates()
		if err := emitLSJSON(emit, "JetboxSubscribeToSummaries", map[string]any{"updates": updates, "deletes": []any{}}); err != nil {
			return true, err
		}
	}
}

// streamProjectUpdates serves the project list. It emits the ids
// first, then one frame per change.
//
// The frame is exa.language_server_pb.ProjectUpdatesStreamResponse:
// the oneof's `projectList` member, carrying the project ids.
func (d *Dispatcher) streamProjectUpdates(ctx context.Context, payload string, emit func(json.RawMessage) error) (bool, error) {
	ids := d.lsProjectIDs()
	if err := emitLSJSON(emit, "ProjectUpdatesStream", map[string]any{"projectList": map[string]any{"projectIds": ids}}); err != nil {
		return true, err
	}
	stamp := lsProjectsStamp()
	for {
		if !lsSleep(ctx, lsStreamPoll) {
			return true, nil
		}
		next := lsProjectsStamp()
		if next == stamp {
			continue
		}
		stamp = next
		ids = d.lsProjectIDs()
		if err := emitLSJSON(emit, "ProjectUpdatesStream", map[string]any{"projectList": map[string]any{"projectIds": ids}}); err != nil {
			return true, err
		}
	}
}

// streamSubscribeToMcpServerStates serves the MCP server states. It
// emits the current list and then holds the stream open.
//
// The frame is exa.language_server_pb.SubscribeToMcpServerStatesResponse:
// the `states` list (a repeated McpServerState) and the `isLoading`
// flag. The clone runs no MCP servers of its own, so the list is
// empty — but the stream stays open, because the Web UI treats it as a
// subscription and re-subscribes the moment it ends.
func (d *Dispatcher) streamSubscribeToMcpServerStates(ctx context.Context, payload string, emit func(json.RawMessage) error) (bool, error) {
	if err := emitLSJSON(emit, "SubscribeToMcpServerStates", map[string]any{"states": []any{}, "isLoading": false}); err != nil {
		return true, err
	}
	return true, lsHoldStream(ctx)
}

// streamSubscribeToPlugins serves the plugin list. It emits the
// current list and then holds the stream open.
//
// The frame is exa.language_server_pb.SubscribeToPluginsResponse: the
// `plugins` list. The clone has no plugins, so the list is empty — and
// the stream stays open, for the same reason as the MCP one: the Web
// UI reads the first frame as the current list and every later frame
// as "the plugins changed".
func (d *Dispatcher) streamSubscribeToPlugins(ctx context.Context, payload string, emit func(json.RawMessage) error) (bool, error) {
	if err := emitLSJSON(emit, "SubscribeToPlugins", map[string]any{"plugins": []any{}}); err != nil {
		return true, err
	}
	return true, lsHoldStream(ctx)
}

// lsHoldStream keeps a stream open until the client goes away.
//
// The streams the clone serves have nothing to push: the MCP servers
// and the plugins never change under it. The stream still has to stay
// open, because an ended stream is a re-subscription — and a
// re-subscription is the whole page's bootstrap again.
func lsHoldStream(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// emitLSJSON marshals one frame and hands it to the transport.
//
// The frame is a proto message in the reference, so its fields go out
// in the descriptor's declaration order — the same rule the unary
// answers follow.
func emitLSJSON(emit func(json.RawMessage) error, method string, frame any) error {
	raw, err := marshalAnswer(method, frame)
	if err != nil {
		return err
	}
	return emit(raw)
}

// lsSleep waits for the given duration. It reports false when the
// context ended first.
func lsSleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// lsSummariesStamp is a cheap change detector for the conversation
// index: the modification time and the size of the store's database
// and of its conversation directory.
func lsSummariesStamp() string {
	dir := convstore.StoreDir()
	return lsPathStamp(filepath.Join(dir, "conversation_summaries.db")) +
		"|" + lsPathStamp(filepath.Join(dir, "conversations"))
}

// lsProjectsStamp is the change detector for the project store.
func lsProjectsStamp() string {
	return lsPathStamp(lsConfigProjectsDir())
}

// lsPathStamp renders a path's modification time and size.
func lsPathStamp(path string) string {
	if path == "" {
		return "-"
	}
	st, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return fmt.Sprintf("%d:%d", st.ModTime().UnixNano(), st.Size())
}
