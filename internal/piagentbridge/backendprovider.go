package piagentbridge

import (
	"context"
	"strings"

	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
)

// BackendProvider adapts *any* backend.Client into an ai.Provider, so PiG's
// coding.Session can drive every transport the clone has — the agw gateway, the
// offline mock the shims and parity harnesses use, and any future one — not
// only the client that describes an agw endpoint.
//
// Provider (agwprovider.go) is the agw-shaped sibling: it re-implements the
// OpenAI SSE wire format so the transport reports tool-call deltas natively.
// BackendProvider takes the other route: it reuses BackendStreamFn, the
// translation already exercised by the clone's own loop, and lets the backend
// do the talking. The two must stay behaviourally identical in what the model
// sees, which is why the wire model still comes from the backend's config (for
// agw, AgwK0sConfig.ExternalModel) rather than from the label.
//
// It exists so the print path can drop its jetski fallback — see
// docs/cycles/cycle-210/.
type BackendProvider struct {
	client backend.Client
	// wire is the model id placed on the request. The label PiG records is
	// separate: callers pass an Antigravity name, backends that remap (agw)
	// still put their own id on the wire.
	wire  string
	model *pi.Model
}

// NewBackendProvider wraps client. wire is the model id sent on the request;
// pass the same value the caller names the model if the backend does not remap.
func NewBackendProvider(client backend.Client, wire string) *BackendProvider {
	wire = strings.TrimSpace(wire)
	p := &BackendProvider{client: client, wire: wire}
	p.model = &pi.Model{
		Type:         pi.ModelTypeChat,
		ID:           wire,
		DisplayName:  wire,
		Provider:     p,
		ProviderMeta: pi.ProviderMetadata{ProviderID: ProviderID, API: BridgeAPI},
		Input:        []string{"text"},
	}
	return p
}

// ID implements ai.Provider. It reports the clone's provider name so a session
// it drives is indistinguishable from one on the native agw provider.
func (p *BackendProvider) ID() string { return ProviderID }

// Close implements ai.Provider. The provider owns no resources: the backend
// client it wraps does, and its owner closes that.
func (p *BackendProvider) Close() error { return nil }

// Model returns a PiG model handle bound to this provider, mirroring
// (*Provider).Model. An empty label uses the wire model.
func (p *BackendProvider) Model(label string) *pi.Model {
	if strings.TrimSpace(label) == "" {
		label = p.wire
	}
	m := *p.model
	m.ID = label
	m.DisplayName = label
	return &m
}

// Stream implements ai.Provider over the backend's stream. The *ai.Model PiG
// owns is unavailable here — ai.Provider.Stream does not receive it — so the
// assistant message is stamped from this provider's model, exactly as
// pinnedProvider does for the agw transport.
func (p *BackendProvider) Stream(ctx context.Context, transcript pi.TranscriptContext, opts pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
	return BackendStreamFn(p.client, p.wire)(ctx, p.model, transcript, opts)
}
