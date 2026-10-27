// Package remotecontrol — dispatch_codeassist.go: the account's
// code-assist state, for the Web UI's credits panel.
//
// The panel asks the language server for it:
//
//	/exa.language_server_pb.LanguageServerService/GetLoadCodeAssist
//
// 【反编译】 the request is
// exa.language_server_pb.GetLoadCodeAssistRequest{force_refresh = 1},
// the answer is exa.language_server_pb.GetLoadCodeAssistResponse,
// whose only member is `response` — a
// google.internal.cloud.code.v1internal.LoadCodeAssistResponse.
//
// 【一手】 the Web UI's own bundle (internal/rcfrontend/assets/main.js)
// calls it from the credits panel and reads exactly one thing out of
// the answer:
//
//	e.loadCodeAssist(void 0,void 0,G).catch(F=>{b(ig.CREDITS_FETCH_FAILED,F)})
//
// and the service wrapper throws when the member is missing:
//
//	if(!r.response)throw Error("[CloudCodeService] loadCodeAssist returned no response")
//
// Without a route the transport answers `{}`, the panel throws, and
// the clone logs "ui_client_credits_fetch_failed" — the error the
// operator's log carried.
//
// The answer the Web UI wants is the cloud's own
// `v1internal:loadCodeAssist` answer, so the clone asks the cloud for
// it and passes it through. The clone's cache is longer than the
// Web UI's (five seconds): the tier changes slowly and every read
// costs a round trip.
package remotecontrol

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
)

// codeAssistFreshness is how long the account's code-assist state is
// served from the clone's own cache.
const codeAssistFreshness = 2 * time.Minute

// codeAssistStateFetcher reads the account's code-assist state from
// the cloud. It is a variable so a test can answer without a network.
var codeAssistStateFetcher = auth.FetchCodeAssistState

// codeAssistCache holds the last state the clone read, and when it
// read it. The state belongs to the account behind the shared
// credential, so one cache serves the whole rig.
var codeAssistCache struct {
	mu        sync.Mutex
	account   string
	state     map[string]any
	fetchedAt time.Time
}

// routeCodeAssistMethods serves the code-assist state RPCs the Web
// UI's credits panel needs. Returns ok=true when the method was
// handled.
func (d *Dispatcher) routeCodeAssistMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "GetLoadCodeAssist":
		return d.handleLSGetLoadCodeAssist(ctx, params)
	}
	return nil, nil, false
}

// handleLSGetLoadCodeAssist answers the Web UI's credits-panel call.
//
// The answer is the cloud's own state, read live and cached. A failed
// read is not an error: the panel is a read, not a command, and the
// clone answers with the account's cached tier so the panel has
// something to render.
func (d *Dispatcher) handleLSGetLoadCodeAssist(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		ForceRefresh bool `json:"forceRefresh"`
	}
	if len(params) > 0 {
		// A malformed request is answered from the cache: the
		// credits panel is a read, not a command.
		_ = json.Unmarshal(params, &req)
	}
	state, ok := codeAssistState(ctx, req.ForceRefresh)
	if !ok {
		state = cachedTierState()
	}
	return map[string]any{"response": state}, nil, true
}

// codeAssistState returns the account's code-assist state, reading the
// cloud when the cache is cold or stale.
//
// It reports whether a live read produced the state; a false means
// the caller should fall back.
func codeAssistState(ctx context.Context, force bool) (map[string]any, bool) {
	account := activeAccountEmail()
	if !force {
		if state, ok := cachedCodeAssistState(account); ok {
			return state, true
		}
	}
	raw, err := codeAssistStateFetcher(ctx)
	if err != nil {
		log.Printf("remotecontrol: loadCodeAssist: %v", err)
		return nil, false
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		log.Printf("remotecontrol: loadCodeAssist: %v", err)
		return nil, false
	}
	codeAssistCache.mu.Lock()
	codeAssistCache.account = account
	codeAssistCache.state = state
	codeAssistCache.fetchedAt = time.Now()
	codeAssistCache.mu.Unlock()
	return state, true
}

// cachedCodeAssistState returns the cached state when it is still
// fresh and belongs to the active account.
//
// The state belongs to the account behind the shared credential, so a
// switch must not be answered with the previous account's tier.
func cachedCodeAssistState(account string) (map[string]any, bool) {
	codeAssistCache.mu.Lock()
	defer codeAssistCache.mu.Unlock()
	if codeAssistCache.state == nil || codeAssistCache.fetchedAt.IsZero() {
		return nil, false
	}
	if codeAssistCache.account != account {
		return nil, false
	}
	if time.Since(codeAssistCache.fetchedAt) > codeAssistFreshness {
		return nil, false
	}
	return codeAssistCache.state, true
}

// activeAccountEmail returns the active account's address — the one
// behind the shared credential, the same credential the session's
// turns use.
func activeAccountEmail() string {
	_, email := accounts.Current()
	return strings.TrimSpace(email)
}

// cachedTierState renders the active account's cached plan tier as a
// code-assist state.
//
// It is the fallback for an offline machine: the tier the account
// registry remembers is the label the cloud last answered with. The
// tier's *id* is not known here, and the Web UI does not read one —
// it only needs the `response` member to be there.
func cachedTierState() map[string]any {
	tier := cachedPlanTier()
	if tier == "" {
		return map[string]any{}
	}
	return map[string]any{
		"currentTier": map[string]any{"name": tier, "description": tier},
		"paidTier":    map[string]any{"name": tier, "description": tier},
	}
}

// cachedPlanTier returns the active account's cached plan tier.
func cachedPlanTier() string {
	email := activeAccountEmail()
	if email == "" {
		return ""
	}
	acc, ok, err := accounts.Get(email)
	if err != nil || !ok {
		return ""
	}
	return strings.TrimSpace(acc.PlanTier)
}
