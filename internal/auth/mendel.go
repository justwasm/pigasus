// mendel.go — the live Mendel (experiment) set.
//
// The language server resolves its experiments from Mendel's backend:
// the CloudCode RPC
// google.internal.cloud.code.v1internal.CloudCode/ListExperiments
// answers with the account's flag set, and the reference's own
// language server polls it.
//
// 【反编译】 2026-10-26, the reference's IDE language server —
// codeassistclient.ListExperiments (0x101acd210) tries the AICode
// endpoint first and falls back to the CloudCode one:
//
//	"AICode FetchConfig failed, falling back to CCPA ListExperiments: %v"
//
// and the CloudCode path is the POST of the reference's own request:
//
//	POST https://cloudcode-pa.googleapis.com/v1internal:listExperiments
//	{"project":"aicode-consumers"}
//
// The AICode path is the GET the reference builds with getAICodeURL
// (0x101abd840):
//
//	GET ${AICODE_ENDPOINT_URL:-https://aicode.googleapis.com}/v1alpha:fetchConfig
//
// 【一手】 the CloudCode path is the one that carries the *complete*
// set: on this machine it answers with 508 flags, while the AICode
// path answers with 310 — and every one of the 310 is also in the 508
// (measured, 2026-10-26). The reference's own GetMendelFlags answer
// carries 508 experiments, so the CloudCode path is the one the clone
// reads.
//
// The request body matters: with `{"project":"aicode-consumers"}` the
// remote-control flags come back *enabled*, which is what the
// reference's own answer carries; with an empty body they come back
// disabled. 【一手】
//
// # The identity decides the set
//
// 【一手】 2026-10-27, the same token, the same body, the same
// endpoint, only the `User-Agent` differing — the backend answers a
// *different flag set* per product:
//
//	antigravity/hub/2.26.0 (aidev_client; …)   → 508 flags,
//	    enable-audio-transcription   boolValue: true
//	    enable-jetbox-terminal-v2    boolValue: true
//	    enable-aux-pane-redesign     boolValue: true
//	antigravity/cli/1.4.4 (aidev_client; …)   → the same 508 flags,
//	    enable-audio-transcription   boolValue: false
//	    enable-jetbox-terminal-v2    boolValue: false
//	    enable-aux-pane-redesign     boolValue: false
//	antigravity/ide/2.26.0 (aidev_client; …)   → like the CLI's
//
// The version and the build number do not matter — `hub/0.0.1` answers
// the hub's set — only the product token does. One read out of ~120
// came back reduced (the backend is a rollout); see
// out/web-desktop-mode/mendel-ua-matrix.txt.
//
// The reference's own language server runs with the hub identity
// (`antigravity/hub/2.26.0 (aidev_client; os_type=darwin;
// arch=arm64; cl=1005016104)`, measured off the wire), which is why
// its page gets the desktop app's features and the CLI's page does
// not.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
)

// MendelUserAgentCLI is the identity the CLI's own Mendel read carries.
//
// It is the CLI's own — the reference's CLI sends the same one, and
// the backend answers it the CLI's flag set.
const MendelUserAgentCLI = "antigravity/cli/1.4.4 (aidev_client; os_type=darwin; arch=arm64; auth_method=consumer)"

// MendelHubUserAgent returns the hub identity for a version.
//
// The hub's flag set is the desktop app's set: the voice input, the
// jetbox terminal and the aux pane redesign are on in it and off in
// the CLI's. The Web UI reads those flags and drops the features they
// gate, so this is the identity the desktop UI needs.
//
// See the package comment for the measurement.
func MendelHubUserAgent(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "0.0.0"
	}
	return fmt.Sprintf("antigravity/hub/%s (aidev_client; os_type=%s; arch=%s)",
		version, runtime.GOOS, runtime.GOARCH)
}

// mendelHostsOverride replaces the resolver's hosts when it is non-nil.
// It is the test seam: a test that points the call at an httptest server
// sets it, and it wins over the configuration.
var mendelHostsOverride []string

// mendelHosts returns the CloudCode endpoints the ListExperiments call
// goes to, in the order the clone tries them: the production endpoint
// first (the reference's live answer comes from it), the daily one
// second.
//
// The hosts are read from the endpoint resolver, so a configuration that
// points the CloudCode host at a local service moves this call with it.
func mendelHosts() []string {
	if len(mendelHostsOverride) > 0 {
		return mendelHostsOverride
	}
	e := endpoints.Current()
	return []string{e.CloudCodeProd, e.CloudCode}
}

// mendelTimeout bounds one ListExperiments call.
const mendelTimeout = 10 * time.Second

// MendelFlag is one flag of the Mendel set.
//
// A flag is either a *boolean* flag (on or off) or a flag with a
// value: a string, a number, or a list of them.
type MendelFlag struct {
	// Name is the flag's key — "remote-control-version" and so on.
	Name string
	// Disabled is true when the flag is a boolean flag that is off.
	Disabled bool
	// Value is the flag's value as text: the string itself, or the
	// decimal text of a number.
	Value string
	// HasValue tells a boolean flag from a flag whose value is
	// empty.
	HasValue bool
	// ListJSON is a list flag's value as JSON text — the array
	// itself, the way the reference's answer carries it.
	//
	// 【一手】 the reference's own answer puts a list value in the
	// `json` field, as the JSON text of the array:
	// `["document","comment"]` for a list of strings, and `null` for
	// a list that carries no elements at all.
	ListJSON string
	// HasList tells a list flag from a scalar one.
	HasList bool
}

// mendelResponse is the ListExperiments answer.
type mendelResponse struct {
	ExperimentIDs []int64         `json:"experimentIds"`
	Flags         []mendelFlagWire `json:"flags"`
}

// mendelFlagWire is one flag of the answer.
//
// The value fields are pointers: a flag carries exactly one of them,
// and a flag whose value is absent is a boolean flag that is off.
type mendelFlagWire struct {
	Name           string            `json:"name"`
	FlagID         int64             `json:"flagId"`
	BoolValue      *bool             `json:"boolValue"`
	StringValue    *string           `json:"stringValue"`
	IntValue       *mendelScalar     `json:"intValue"`
	FloatValue     *mendelScalar     `json:"floatValue"`
	StringListVal  *mendelStringList `json:"stringListValue"`
	Int32ListValue *mendelInt32List  `json:"int32ListValue"`
}

// mendelStringList is the wire form of a list of strings.
type mendelStringList struct {
	Values []string `json:"values"`
}

// mendelInt32List is the wire form of a list of 32 bit integers.
type mendelInt32List struct {
	Values []int32 `json:"values"`
}

// mendelScalar is a number the wire may carry as text.
//
// protojson writes an int64 as a JSON string ("60") and a float as a
// number (0.8), so the decoder takes both.
type mendelScalar string

// UnmarshalJSON accepts a number or a quoted number.
func (s *mendelScalar) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*s = mendelScalar(str)
		return nil
	}
	*s = mendelScalar(b)
	return nil
}

// FetchMendelFlags reads the Mendel flag set of the active account.
//
// The read carries the CLI's own identity, so the backend answers the
// CLI's flag set.
func FetchMendelFlags(ctx context.Context) ([]MendelFlag, error) {
	return FetchMendelFlagsWithUserAgent(ctx, MendelUserAgentCLI)
}

// FetchMendelFlagsWithUserAgent reads the Mendel flag set of the
// active account with an explicit identity.
//
// The identity decides which set the backend serves — see the package
// comment. An empty userAgent falls back to the CLI's own.
func FetchMendelFlagsWithUserAgent(ctx context.Context, userAgent string) ([]MendelFlag, error) {
	if strings.TrimSpace(userAgent) == "" {
		userAgent = MendelUserAgentCLI
	}
	tok, err := cloudCodeAccessToken()
	if err != nil {
		return nil, err
	}
	return FetchMendelFlagsWithTokenAndUserAgent(ctx, tok, userAgent)
}

// FetchMendelFlagsWithToken reads the Mendel flag set with an explicit
// bearer token.
//
// The flags belong to the account behind the token, so the
// multi-account registry reads one account at a time.
func FetchMendelFlagsWithToken(ctx context.Context, accessToken string) ([]MendelFlag, error) {
	return FetchMendelFlagsWithTokenAndUserAgent(ctx, accessToken, MendelUserAgentCLI)
}

// FetchMendelFlagsWithTokenAndUserAgent reads the Mendel flag set with
// an explicit bearer token and an explicit identity.
func FetchMendelFlagsWithTokenAndUserAgent(ctx context.Context, accessToken string, userAgent string) ([]MendelFlag, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("listExperiments: no access token")
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = MendelUserAgentCLI
	}
	body := []byte(`{"project":"aicode-consumers"}`)
	var lastErr error
	for _, host := range mendelHosts() {
		flags, err := fetchMendelFlagsFrom(ctx, host, accessToken, body, userAgent)
		if err == nil {
			return flags, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// fetchMendelFlagsFrom calls ListExperiments on one endpoint.
func fetchMendelFlagsFrom(ctx context.Context, host, accessToken string, body []byte, userAgent string) ([]MendelFlag, error) {
	ctx, cancel := context.WithTimeout(ctx, mendelTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		host+"/v1internal:listExperiments", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{Timeout: mendelTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listExperiments: HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	return ParseMendelFlags(raw)
}

// ParseMendelFlags decodes a ListExperiments answer.
//
// A flag that carries no value at all is a boolean flag that is off;
// a boolean flag that is on carries `boolValue: true` and no other
// value.
func ParseMendelFlags(body []byte) ([]MendelFlag, error) {
	var wire mendelResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	out := make([]MendelFlag, 0, len(wire.Flags))
	for _, f := range wire.Flags {
		flag := MendelFlag{Name: f.Name}
		switch {
		case f.BoolValue != nil:
			flag.Disabled = !*f.BoolValue
		case f.StringValue != nil:
			flag.Value = *f.StringValue
			flag.HasValue = true
		case f.IntValue != nil:
			flag.Value = string(*f.IntValue)
			flag.HasValue = true
		case f.FloatValue != nil:
			flag.Value = string(*f.FloatValue)
			flag.HasValue = true
		case f.StringListVal != nil:
			flag.ListJSON = mendelListJSON(f.StringListVal.Values)
			flag.HasList = true
		case f.Int32ListValue != nil:
			flag.ListJSON = mendelListJSON(f.Int32ListValue.Values)
			flag.HasList = true
		}
		out = append(out, flag)
	}
	return out, nil
}

// mendelListJSON renders a list flag's value as JSON text.
//
// A list that carries no elements is `null` — the reference's answer
// carries exactly that. 【一手】
func mendelListJSON[T any](values []T) string {
	if len(values) == 0 {
		return "null"
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "null"
	}
	return string(raw)
}

// snippet is a short, printable excerpt of a failed answer.
func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
