package remotecontrol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
)

// TestTheMendelAnswerCarriesTheLocalFlagsFirst pins the head of the
// answer.
//
// 【一手】 the reference's own answer carries the six remote-control
// flags first, in the language server's own order — and the backend's
// flags after them.
func TestTheMendelAnswerCarriesTheLocalFlagsFirst(t *testing.T) {
	installMendelFetcher(t, []auth.MendelFlag{
		{Name: "some-api-flag", Disabled: true},
	})

	answer := mendelFlagsAnswer(context.Background(), "")
	got := answer.ExperimentConfig.Experiments
	if len(got) != 7 {
		t.Fatalf("the answer carries %d experiments, want 7", len(got))
	}
	want := []string{
		"remote-control-version",
		webRTCMeshExperimentKey,
		"remote-control-proxy-server-url",
		"remote-control-silence-timeout-seconds",
		"remote-control-setting-enabled",
		"remote-control-companion-url",
		"some-api-flag",
	}
	for i, key := range want {
		if got[i].KeyString != key {
			t.Errorf("experiment %d is %q, want %q", i, got[i].KeyString, key)
		}
	}
	// 【一手】 the reference's own answer marks every flag it serves
	// with the language server's source — the ones it fetched from the
	// backend included.
	for _, e := range got {
		if e.Source != experimentSourceLanguageServer {
			t.Errorf("experiment %q has the source %q, want %q",
				e.KeyString, e.Source, experimentSourceLanguageServer)
		}
	}
}

// TestTheMendelAnswerKeepsTheMeshOff pins the one value the clone
// deliberately differs on: the clone does not implement the WebRTC
// mesh, so it reports the flag as disabled — the dashboard's host page
// then takes its WebChannel path instead of waiting for a mesh that
// would never come up.
//
// The `webrtcMeshReady` field itself is *there* either way: it is an
// `optional bool` in the schema, and the reference's own answer carries
// it with `false` — its IDE language server has no mesh either.
// 【一手】
func TestTheMendelAnswerKeepsTheMeshOff(t *testing.T) {
	installMendelFetcher(t, []auth.MendelFlag{
		// The backend says the mesh is on; the clone's own flag says
		// it is not implemented.
		{Name: webRTCMeshExperimentKey, Disabled: false},
	})

	answer := mendelFlagsAnswer(context.Background(), "")
	for _, e := range answer.ExperimentConfig.Experiments {
		if e.KeyString != webRTCMeshExperimentKey {
			continue
		}
		if !e.Disabled {
			t.Fatal("the mesh flag is enabled, want it disabled")
		}
		if e.Source != experimentSourceLanguageServer {
			t.Errorf("the mesh flag has the source %q, want %q", e.Source, experimentSourceLanguageServer)
		}
		break
	}
	if answer.WebRTCMeshReady == nil {
		t.Fatal("the answer has no webrtcMeshReady field, want it carried as false")
	}
	if *answer.WebRTCMeshReady {
		t.Error("the answer says the mesh is ready, want false")
	}
}

// TestTheMendelAnswerConvertsEveryValueKind pins the conversion of the
// backend's flag kinds.
//
// 【一手】 the reference's own answer carries the silence timeout —
// "60", an int on the wire — as the string "60"; a list in the `json`
// field, as the JSON text of the array; and a flag whose value is the
// empty string as `""` — not as a flag without a value.
func TestTheMendelAnswerConvertsEveryValueKind(t *testing.T) {
	installMendelFetcher(t, []auth.MendelFlag{
		{Name: "on", Disabled: false},
		{Name: "off", Disabled: true},
		{Name: "text", Value: "v2", HasValue: true},
		{Name: "number", Value: "60", HasValue: true},
		{Name: "empty", Value: "", HasValue: true},
		{Name: "list", ListJSON: `["a","b"]`, HasList: true},
	})

	answer := mendelFlagsAnswer(context.Background(), "")
	byKey := map[string]mendelExperiment{}
	for _, e := range answer.ExperimentConfig.Experiments {
		byKey[e.KeyString] = e
	}
	if e := byKey["on"]; e.Disabled {
		t.Error("the flag that is on is disabled")
	}
	if e := byKey["off"]; !e.Disabled {
		t.Error("the flag that is off is not disabled")
	}
	if e := byKey["text"]; e.String == nil || *e.String != "v2" {
		t.Errorf("the text flag carries %v, want %q", e.String, "v2")
	}
	if e := byKey["number"]; e.String == nil || *e.String != "60" {
		t.Errorf("the number flag carries %v, want %q", e.String, "60")
	}
	if e := byKey["empty"]; e.String == nil || *e.String != "" {
		t.Errorf("the empty flag carries %v, want an empty string", e.String)
	}
	if e := byKey["list"]; e.JSON != `["a","b"]` {
		t.Errorf("the list flag carries %q, want %q", e.JSON, `["a","b"]`)
	}
}

// TestTheMendelAnswerSurvivesAFailedRead pins that a broken network
// leaves the page's own flags alone.
func TestTheMendelAnswerSurvivesAFailedRead(t *testing.T) {
	installMendelFetcher(t, nil)

	answer := mendelFlagsAnswer(context.Background(), "")
	got := answer.ExperimentConfig.Experiments
	if len(got) != 6 {
		t.Fatalf("the answer carries %d experiments, want the 6 local ones", len(got))
	}
	if got[0].KeyString != "remote-control-version" {
		t.Errorf("the first experiment is %q, want %q", got[0].KeyString, "remote-control-version")
	}
}

// TestTheMendelAnswerIsProtoJSONOrdered pins the answer's own field
// order.
//
// The reference's answer is protojson, and protojson writes a message's
// fields in the order the descriptor declares them.
func TestTheMendelAnswerIsProtoJSONOrdered(t *testing.T) {
	installMendelFetcher(t, []auth.MendelFlag{{Name: "x", Value: "1", HasValue: true}})

	raw, err := json.Marshal(mendelFlagsAnswer(context.Background(), ""))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"experimentConfig":{"experiments":[{"keyString":"remote-control-version","string":"v2","source":"EXPERIMENT_SOURCE_LANGUAGE_SERVER"},`
	if !strings.HasPrefix(got, want) {
		t.Errorf("the answer starts with %q, want %q", got[:len(want)], want)
	}
	if !strings.Contains(got, `"webrtcMeshReady":false`) {
		t.Errorf("the answer does not carry webrtcMeshReady:false (%s)", got)
	}
}

// TestTheMendelAnswerReadsWithTheConfiguredIdentity pins that the
// answer's backend read carries the identity the caller configured.
//
// 【一手】 2026-10-27: the backend serves a *different* flag set per
// product — the hub's set carries the desktop app's features (the
// voice input, the jetbox terminal, the aux pane redesign), the CLI's
// does not. The identity is the whole lever. See internal/auth's
// package comment.
func TestTheMendelAnswerReadsWithTheConfiguredIdentity(t *testing.T) {
	old := mendelFetcher
	var got string
	mendelFetcher = func(_ context.Context, userAgent string) ([]auth.MendelFlag, error) {
		got = userAgent
		return nil, nil
	}
	mendelCache.mu.Lock()
	mendelCache.sets = nil
	mendelCache.mu.Unlock()
	t.Cleanup(func() {
		mendelFetcher = old
		mendelCache.mu.Lock()
		mendelCache.sets = nil
		mendelCache.mu.Unlock()
	})

	const hub = "antigravity/hub/1.4.7 (aidev_client; os_type=darwin; arch=arm64)"
	mendelFlagsAnswer(context.Background(), hub)
	if got != hub {
		t.Errorf("the read carried %q, want the configured identity", got)
	}
}

// TestTheMendelCacheIsKeyedByTheIdentity pins that two identities do
// not share one cached set.
//
// The clone's page and the desktop page read the same backend with
// different identities, and the backend answers each one its own set.
func TestTheMendelCacheIsKeyedByTheIdentity(t *testing.T) {
	old := mendelFetcher
	var seen []string
	mendelFetcher = func(_ context.Context, userAgent string) ([]auth.MendelFlag, error) {
		seen = append(seen, userAgent)
		return []auth.MendelFlag{{Name: "flag-" + userAgent}}, nil
	}
	mendelCache.mu.Lock()
	mendelCache.sets = nil
	mendelCache.mu.Unlock()
	t.Cleanup(func() {
		mendelFetcher = old
		mendelCache.mu.Lock()
		mendelCache.sets = nil
		mendelCache.mu.Unlock()
	})

	first := mendelFlagsAnswer(context.Background(), "cli")
	second := mendelFlagsAnswer(context.Background(), "hub")
	if len(seen) != 2 {
		t.Fatalf("the backend was read %d times, want 2 (one per identity)", len(seen))
	}
	if seen[0] == seen[1] {
		t.Errorf("both reads carried %q, want two identities", seen[0])
	}
	if last(first.ExperimentConfig.Experiments) != "flag-cli" {
		t.Errorf("the cli identity answered %q, want its own set", last(first.ExperimentConfig.Experiments))
	}
	if last(second.ExperimentConfig.Experiments) != "flag-hub" {
		t.Errorf("the hub identity answered %q, want its own set", last(second.ExperimentConfig.Experiments))
	}

	// The second read of an identity must come from the cache.
	mendelFlagsAnswer(context.Background(), "cli")
	if len(seen) != 2 {
		t.Errorf("the backend was read %d times, want 2 (the cache is not per identity)", len(seen))
	}
}

// installMendelFetcher installs a fetcher for one test and restores
// the live one afterwards.
func installMendelFetcher(t *testing.T, flags []auth.MendelFlag) {
	t.Helper()
	old := mendelFetcher
	mendelFetcher = func(context.Context, string) ([]auth.MendelFlag, error) {
		return flags, nil
	}
	mendelCache.mu.Lock()
	mendelCache.sets = nil
	mendelCache.mu.Unlock()
	t.Cleanup(func() {
		mendelFetcher = old
		mendelCache.mu.Lock()
		mendelCache.sets = nil
		mendelCache.mu.Unlock()
	})
}

// last returns the last experiment's key of an answer.
func last(experiments []mendelExperiment) string {
	if len(experiments) == 0 {
		return ""
	}
	return experiments[len(experiments)-1].KeyString
}
