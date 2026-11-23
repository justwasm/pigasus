// undocumented.go — models the CloudCode family serves but the reference's
// picker never draws.
//
// # Why this table exists
//
// The reference's catalogue (reference.go) is what `agy models` prints and
// what the reference's own /model panel lists. It is *not* the whole set of
// wire ids the CloudCode endpoint answers: the endpoint accepts model ids the
// binary has no row for, and a turn that names one succeeds even though no
// picker offers it.
//
// 【一手】 2026-11-17, `gemini-4-hydrogen-sonar`:
//
//   - the vendor binary's tier table has no `sonar` tier — Hydrogen publishes
//     low, medium, high and xhigh only, and `agy models` prints those four;
//   - a Jio account reached through a reverse-proxied CloudCode host answers
//     `--model gemini-4-hydrogen-sonar` against that host, with the id passed
//     through verbatim. The model is served, it is simply not advertised.
//
// So the row is real, and hiding it forever would mean the picker cannot
// reach a model the transport serves. Listing it everywhere would mean the
// clone's catalogue stops matching the reference's on a stock machine, which
// is the parity this package exists to keep (reference_models_test.go pins the
// reference's fourteen rows byte for byte).
//
// # The gate
//
// The rows are offered only when the run is demonstrably *not* a stock one:
//
//   - OPENAGY_UNDOCUMENTED_MODELS=1/true/on/yes turns them on explicitly, and
//     =0/false/off/no turns them off explicitly (the explicit answer always
//     wins, so a user can pin either side);
//   - otherwise they follow the CloudCode host: a host that is neither
//     Google's own nor a loopback listener is a reverse proxy, and a reverse
//     proxy is exactly the setup the model is reachable through.
//
// Loopback is excluded on purpose. The alignment harnesses boot the clone
// against a local mock CloudCode proxy (tuialign) and `pigasus agy` points the
// vendor binary at the clone's own loopback relay; a row that appeared there
// would make the clone's picker differ from the reference's for a reason that
// has nothing to do with the model.
package catalog

import (
	"os"
	"strings"

	"github.com/justwasm/pigasus/endpoints"
)

// UndocumentedModelsEnv is the variable that overrides the gate. It accepts
// the on/off vocabulary the clone's other switches accept.
const UndocumentedModelsEnv = "OPENAGY_UNDOCUMENTED_MODELS"

// The ids this table carries.
const (
	// SonarModelID is the wire id a reverse-proxied CloudCode host serves.
	SonarModelID = "gemini-4-hydrogen-sonar"
	// SonarModelLabel is the display label, in the reference's own
	// "Family (Variant)" shape.
	SonarModelLabel = "Gemini 4 Hydrogen (Sonar)"

	SonarModelXHighID = "gemini-4-hydrogen-sonar-xhigh"
	SonarModelHighID  = "gemini-4-hydrogen-sonar-high"
	SonarModelMedID   = "gemini-4-hydrogen-sonar-medium"
	SonarModelLowID   = "gemini-4-hydrogen-sonar-low"
)

// undocumentedModels is the table. Every entry is a wire id the transport
// accepts and the reference's catalogue does not carry.
//
// An entry belongs here only with a capture — a request that the endpoint
// answered — because the row's whole purpose is to be selectable: an id that
// is not answered routes a turn to a model that does not exist.
var undocumentedModels = []ModelEntry{
	{ID: SonarModelXHighID, Label: "Gemini 4 Hydrogen (Sonar) (XHigh)"},
	{ID: SonarModelHighID, Label: "Gemini 4 Hydrogen (Sonar) (High)"},
	{ID: SonarModelMedID, Label: "Gemini 4 Hydrogen (Sonar) (Medium)"},
	{ID: SonarModelLowID, Label: "Gemini 4 Hydrogen (Sonar) (Low)"},
	{ID: SonarModelID, Label: SonarModelLabel},
}

// UndocumentedModelsEnabled reports whether the picker and the catalogue may
// list the undocumented rows on this run.
//
// By default, Pigasus enables verified live models (such as gemini-4-hydrogen-sonar),
// which have been confirmed to respond directly on CloudCode upstreams.
// A user can explicitly disable them by setting OPENAGY_UNDOCUMENTED_MODELS=0/false/off/no.
func UndocumentedModelsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(UndocumentedModelsEnv))) {
	case "0", "false", "off", "no", "disable", "disabled":
		return false
	}
	return true
}

// UndocumentedModels returns the undocumented rows when the gate is open, and
// nil when it is not. The slice is fresh on every call, so a caller that
// appends to it cannot corrupt the table.
func UndocumentedModels() []ModelEntry {
	if !UndocumentedModelsEnabled() {
		return nil
	}
	out := make([]ModelEntry, len(undocumentedModels))
	copy(out, undocumentedModels)
	return out
}

// IsUndocumentedModel reports whether id is one of the undocumented ids —
// whether or not the gate is open. The dispatch asks this to know that the
// model is served by the CloudCode transport rather than by a configured
// endpoint; selection asks UndocumentedModelsEnabled separately.
func IsUndocumentedModel(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, m := range undocumentedModels {
		if m.ID == id || m.Label == id {
			return true
		}
	}
	return false
}

// ReferenceServedModels is ReferenceModels() plus the undocumented rows the
// CloudCode transport serves: the set a turn may name and be answered by the
// reference's own transport.
//
// It is deliberately *not* what the catalogue prints — see ModelCatalogue for
// that — but it is what the transport dispatch keys on, so a model the picker
// offered is not sent to the shim or to a configured endpoint that never
// heard of it.
func ReferenceServedModels() []ModelEntry {
	out := ReferenceModels()
	for _, m := range UndocumentedModels() {
		out = appendUniqueEntry(out, m)
	}
	return out
}

// appendUniqueEntry appends m unless an entry with its id is already present.
func appendUniqueEntry(out []ModelEntry, m ModelEntry) []ModelEntry {
	for _, e := range out {
		if e.ID == m.ID {
			return out
		}
	}
	return append(out, m)
}

// cloudCodeRelayed reports whether the run's CloudCode host is a reverse
// proxy: configured, not Google's own, and not a loopback listener.
func cloudCodeRelayed() bool {
	host := strings.TrimRight(strings.TrimSpace(endpoints.Current().CloudCode), "/")
	if host == "" {
		return false
	}
	for _, def := range []string{endpoints.DefaultCloudCode, endpoints.DefaultCloudCodeProd} {
		if strings.EqualFold(host, strings.TrimRight(def, "/")) {
			return false
		}
	}
	return !isLoopbackHost(host)
}

// isLoopbackHost reports whether an endpoint URL names a loopback listener —
// the shape every local harness and the clone's own agy relay use.
func isLoopbackHost(raw string) bool {
	host := raw
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	// Strip the port, including the IPv6 bracket form ("[::1]:8080").
	if strings.HasPrefix(host, "[") {
		if i := strings.Index(host, "]"); i >= 0 {
			host = strings.Trim(host[1:i], "[]")
		}
	} else if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0", "host.docker.internal":
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(host)), "127.")
}
