package modelcatalog

import (
	"strings"

	"github.com/justwasm/pigasus/internal/models"
)

// # The catalogue id and the wire id are two different namespaces
//
// The catalogue id is the CLI's spelling of a model — what the user types
// after `--model`, what the /model picker row carries, what
// settings.json holds. The wire id is what the provider is asked for.
//
// For the CloudCode models the two are the same string, because the server
// advertises the same ids the CLI accepts. For a configured model they are
// not necessarily: a provider may serve its models under its own spelling,
// and the catalogue id is then whatever the user chose for it.
//
// Nothing here is hard-coded. The translation comes from the user's
// provider configuration, where each model lists its upstream id next to
// its catalogue id:
//
//	{"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3"}
//
// A model with no upstream id listed is asked for under its catalogue id.

// WireModel returns the upstream model id for a catalogue id or display
// label, as the user configured it.
//
// The second result is false when the name is not a configured model, which
// is the caller's cue that there is no upstream id to send.
func WireModel(name string) (string, bool) {
	id, ok := CatalogueID(name)
	if !ok {
		return "", false
	}
	reg := models.CachedRegistry()
	if reg == nil {
		return "", false
	}
	m, ok := reg.GetByName(id)
	if !ok {
		return "", false
	}
	wire := strings.TrimSpace(m.ExternalModelName)
	if wire == "" {
		return "", false
	}
	return wire, true
}

// WireLabel returns the catalogue label for an upstream model id — the
// inverse of WireModel. It is what the TUI shows after it has read the
// model back off the transport, so the label on screen always names the
// model that is actually running.
//
// A reference model is its own upstream id, so the catalogue answers for
// those too.
func WireLabel(wire string) (string, bool) {
	wire = strings.TrimSpace(wire)
	if wire == "" {
		return "", false
	}
	reg := models.CachedRegistry()
	if reg != nil {
		for _, m := range reg.List() {
			if strings.TrimSpace(m.ExternalModelName) != wire {
				continue
			}
			label := strings.TrimSpace(m.DisplayName)
			if label == "" {
				label = strings.TrimSpace(m.Name)
			}
			return label, true
		}
	}
	if label := CatalogueLabel(wire); label != wire {
		return label, true
	}
	return "", false
}
