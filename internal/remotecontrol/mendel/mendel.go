// Package mendel — the reference's GetMendelFlags answer.
//
// 【一手】 2026-10-26, live: the dashboard's host page asks the
// instance for its Mendel flags and reads the answer with JSON.parse:
//
//	fetchMendelFlags() {
//	  const {payload} = await this.proxyCommand(
//	    "/exa.language_server_pb.LanguageServerService/GetMendelFlags",
//	    new TextEncoder().encode("{}"));
//	  return payload;
//	}
//
//	MHe = (body) => body.webrtcMeshReady === false ? false
//	    : (body.experimentConfig?.experiments ?? []).some(
//	        e => e.keyString === "remote-control-webrtc-mesh" && !e.disabled);
//
// The reference answers with JSON text and the content type
// "application/json" — captured through the cloud's ProxyCommand API:
//
//	["eyJleHBlcmltZW50Q29uZmlnIjp7…", [["content-type","application/json"]]]
//
// The payload decodes to 56,758 bytes: 508 experiments and
// `webrtcMeshReady: true`.
//
// # Where the set comes from
//
// The reference's language server keeps a *Mendel state cache*: it
// polls the CloudCode RPC
// google.internal.cloud.code.v1internal.CloudCode/ListExperiments and
// holds the account's flag set. The clone reads the same set (see
// internal/auth/mendel.go), so the answer carries the real flags
// instead of a hand-written subset.
//
// The reference's own answer marks *every* flag it serves
// EXPERIMENT_SOURCE_LANGUAGE_SERVER — measured live, 508 of 508,
// against the reference's own IDE language server:
//
//	{"keyString":"enable-deepagent","source":"EXPERIMENT_SOURCE_LANGUAGE_SERVER"}
//	{"keyString":"remote-control-version","string":"v2",
//	 "source":"EXPERIMENT_SOURCE_LANGUAGE_SERVER"}
//
// 【一手】 the source says *who resolved* the flag, not who stores it:
// the language server resolves the flags it serves — the ones it
// defines itself and the ones it fetched from the backend — and the
// API server's own payloads are the ones that carry
// EXPERIMENT_SOURCE_API_SERVER. The app merges both: "Flags come from
// Mendel experiments and from the host app" (the app bundle's own
// flags panel). The app never reads the `source` field, and neither
// does the host page.
//
// The clone defines the same six flags itself, with the same values,
// so the answer's head is the reference's head:
//
//	remote-control-version                 = "v2"
//	remote-control-webrtc-mesh             = (the clone's own: disabled)
//	remote-control-proxy-server-url        = "jetski-webchannel.googleapis.com:443"
//	remote-control-silence-timeout-seconds = "60"
//	remote-control-setting-enabled         = (enabled)
//	remote-control-companion-url           = "https://antigravity.google.com"
//
// One value differs on purpose: the mesh experiment is disabled and
// `webrtcMeshReady` is false. The clone does not implement the WebRTC
// mesh — no data channel, no signalling — so it reports the mesh as
// off, and the dashboard's host page takes its WebChannel path instead
// of waiting for a mesh that would never come up. The reference, which
// does implement the mesh, answers with the mesh enabled.
//
// The field is *there* either way: `webrtc_mesh_ready` is an
// `optional bool` in the schema, so protojson emits it even when it is
// false — the reference's own answer carries "webrtcMeshReady": false
// (the IDE's language server, which has no mesh either), and the
// clone's answer carries the same field with the same value.
//
// # The field order
//
// The reference's answer is protojson, and protojson writes a message's
// fields in the order the descriptor declares them — not in field
// number order, and not alphabetically. The declaration order of
// exa.codeium_common_pb.ExperimentWithVariant (lsproto/
// descriptor/language_server.protoset) is:
//
//	key (1), key_string (5), disabled (6), string (2), json (3),
//	csv (4), source (7)
//
// `key` is an enum whose default is UNSPECIFIED, so protojson leaves it
// out, and the answer's own order is the one below.

package mendel

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/rcmesh"
)

const (
	// experimentSourceLanguageServer is the reference's source for the
	// experiments the language server resolves.
	//
	// 【一手】 the reference's own answer marks every flag it serves
	// with this source — the ones it fetched from the backend
	// included.
	experimentSourceLanguageServer = "EXPERIMENT_SOURCE_LANGUAGE_SERVER"

	// webRTCMeshExperimentKey is the experiment that turns the
	// WebRTC mesh on in the dashboard's host page.
	webRTCMeshExperimentKey = "remote-control-webrtc-mesh"
)

// Experiment is one entry of the experiment list.
//
// The json tags are the protojson names; the field order is the
// descriptor's declaration order.
//
// `String` is a pointer because the reference's answer carries an
// *empty* value as `""` — a flag whose value is the empty string is
// not a flag without a value. 【一手】
type Experiment struct {
	KeyString string  `json:"keyString,omitempty"`
	Disabled  bool    `json:"disabled,omitempty"`
	String    *string `json:"string,omitempty"`
	JSON      string  `json:"json,omitempty"`
	Source    string  `json:"source,omitempty"`
}

// ExperimentConfig is exa.codeium_common_pb.ExperimentConfig.
type ExperimentConfig struct {
	Experiments []Experiment `json:"experiments,omitempty"`
}

// Flags is exa.language_server_pb.GetMendelFlagsResponse.
//
// The two fields are the proto's own: experiment_config is field 1 and
// webrtc_mesh_ready is field 2.
//
// `WebRTCMeshReady` is a pointer because the field is an *optional*
// bool in the schema: protojson emits it whenever it is set, even when
// it is false. The reference's own answer carries the field with
// `false` (its IDE language server has no mesh either), so the clone
// carries it too. 【一手】
type Flags struct {
	ExperimentConfig ExperimentConfig `json:"experimentConfig"`
	WebRTCMeshReady  *bool            `json:"webrtcMeshReady,omitempty"`
}

func webRTCMeshEnabled() bool {
	if os.Getenv("OPENAGY_DISABLE_WEBRTC_MESH") != "" {
		return false
	}
	return rcmesh.DefaultAnswerer() != nil || os.Getenv("OPENAGY_EXPERIMENTAL_WEBRTC_MESH") != ""
}

// LocalExperiments are the flags the clone itself defines — the
// six the reference's language server defines, in the reference's
// order and with the reference's values.
//
// They shadow the backend's copies of the same flags: the reference's
// own answer carries them with EXPERIMENT_SOURCE_LANGUAGE_SERVER, and
// the backend's copies of the remote-control flags are the very same
// values (measured live, 2026-10-26).
func LocalExperiments() []Experiment {
	return []Experiment{
		{
			KeyString: "remote-control-version",
			String:    stringPtr("v2"),
			Source:    experimentSourceLanguageServer,
		},
		{
			KeyString: webRTCMeshExperimentKey,
			Disabled:  !webRTCMeshEnabled(),
			Source:    experimentSourceLanguageServer,
		},
		{
			KeyString: "remote-control-proxy-server-url",
			String:    stringPtr(endpoints.Current().WebChannelHostPort()),
			Source:    experimentSourceLanguageServer,
		},
		{
			KeyString: "remote-control-silence-timeout-seconds",
			String:    stringPtr("60"),
			Source:    experimentSourceLanguageServer,
		},
		{
			KeyString: "remote-control-setting-enabled",
			Source:    experimentSourceLanguageServer,
		},
		{
			KeyString: "remote-control-companion-url",
			String:    stringPtr(endpoints.Current().CompanionOrigin),
			Source:    experimentSourceLanguageServer,
		},
	}
}

// stringPtr returns a pointer to a copy of s.
//
// The answer's `string` field is a pointer so that a flag whose value
// is the empty string is carried as `""` — the reference's answer does
// exactly that, and a flag without a value carries no field at all.
// 【一手】
func stringPtr(s string) *string { return &s }

// RemoteExperiments converts the backend's flag set into the
// answer's shape.
//
// The flags the clone defines itself are left out: the local set
// already carries them.
//
// A boolean flag that is off is `disabled`; a boolean flag that is on
// carries nothing (protojson omits a false boolean). A flag with a
// value carries it as text — the reference's own answer carries the
// silence timeout ("60", an int on the wire) as the string "60".
// 【一手】
//
// A list flag is carried in the `json` field, as the JSON text of the
// array: `["document","comment"]`, and `null` for a list that carries
// no elements. 【一手】
//
// Every flag is marked EXPERIMENT_SOURCE_LANGUAGE_SERVER, like the
// reference's own answer: the source names who *resolved* the flag,
// and the language server resolves the ones it serves. 【一手】
func RemoteExperiments(flags []auth.MendelFlag) []Experiment {
	local := map[string]bool{}
	for _, e := range LocalExperiments() {
		local[e.KeyString] = true
	}
	out := make([]Experiment, 0, len(flags))
	for _, f := range flags {
		if local[f.Name] {
			continue
		}
		e := Experiment{KeyString: f.Name, Source: experimentSourceLanguageServer}
		switch {
		case f.HasList:
			e.JSON = f.ListJSON
		case f.HasValue:
			e.String = stringPtr(f.Value)
		case f.Disabled:
			e.Disabled = true
		}
		out = append(out, e)
	}
	return out
}

// ttl is how long a live Mendel set is served before it is read
// again.
//
// 【推测】 the reference's Mendel state cache polls on a timer; the
// interval was not recovered from the binary. Ten minutes is short
// enough that a rollout shows up and long enough that the page's
// bootstrap does not wait for the network.
const ttl = 10 * time.Minute

// failureTTL is how long a failed read is remembered, so that an
// offline machine does not pay the timeout on every call.
const failureTTL = time.Minute

// Fetcher reads the live flag set. It is a variable so a test
// can answer without a network.
//
// The identity is a parameter: the backend serves a different flag set
// per product (see internal/auth's package comment), so the cache is
// keyed by it.
//
// The seam is exported because the host's own dispatcher answers the
// same method: a test that pins the *dispatched* answer's byte order
// has to install a Fetcher too, and it lives in the host's package.
var Fetcher = auth.FetchMendelFlagsWithUserAgent

// cache holds the last live set and when it was read, per
// identity.
var cache struct {
	mu   sync.Mutex
	sets map[string]cacheEntry
}

// cacheEntry is one identity's cached set.
type cacheEntry struct {
	flags   []auth.MendelFlag
	fetched time.Time
	failed  bool
}

// liveExperiments returns the backend's flags, read from the
// backend when the cache is stale.
//
// A read that fails leaves the answer with the local flags alone: the
// page's own flags are the ones it reads, and a broken network must
// not take the remote-control page down.
func liveExperiments(ctx context.Context, userAgent string) []auth.MendelFlag {
	cache.mu.Lock()
	entry, ok := cache.sets[userAgent]
	fresh := ok && !entry.fetched.IsZero() && time.Since(entry.fetched) < ttl
	if fresh || (entry.failed && time.Since(entry.fetched) < failureTTL) {
		flags := entry.flags
		cache.mu.Unlock()
		return flags
	}
	cache.mu.Unlock()

	flags, err := Fetcher(ctx, userAgent)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.sets == nil {
		cache.sets = map[string]cacheEntry{}
	}
	if err != nil {
		cache.sets[userAgent] = cacheEntry{fetched: time.Now(), failed: true}
		return nil
	}
	cache.sets[userAgent] = cacheEntry{flags: flags, fetched: time.Now()}
	return flags
}

// fetchTimeout bounds the read of the live set.
//
// The remote-control page asks for the flags while it boots, and its
// own liveness probe gives up after ten seconds — so the read has to
// be over long before that.
const fetchTimeout = 4 * time.Second

// Answer builds the answer the remote-control page reads.
//
// The local flags come first, in the reference's order, and the
// backend's flags follow.
//
// The identity decides which set the backend serves: the CLI's own by
// default, the hub's when the caller asks for the desktop UI — see
// internal/auth's package comment for the measurement.
//
// `webrtcMeshReady` is carried as `false`: the clone has no WebRTC
// mesh, so the dashboard's host page takes its WebChannel path. The
// field is an `optional bool` and the reference's answer carries it
// even when it is false, so the clone carries it too. 【一手】
func Answer(ctx context.Context, userAgent string) Flags {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	experiments := LocalExperiments()
	experiments = append(experiments, RemoteExperiments(liveExperiments(ctx, userAgent))...)
	meshReady := webRTCMeshEnabled()
	return Flags{
		ExperimentConfig: ExperimentConfig{
			Experiments: experiments,
		},
		WebRTCMeshReady: &meshReady,
	}
}
