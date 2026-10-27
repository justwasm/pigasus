// Package lsserver — mocked.go: the reference's mocked catalogue.
//
// `-use_mocked_data=true` is the reference's offline switch. With it
// the server answers the model-catalogue methods from a fixed table
// instead of from `loadCodeAssist` (which needs the network and an
// account). That makes the reference's own answers reproducible
// without credentials, which is what the clone's mocked mode
// reproduces.
//
// The mocked answers, captured from the reference binary with
// `-use_mocked_data=true` (out/langserver/mocked.json):
//
//	GetUserStatus             the mock user, the mock catalogue, a mock plan
//	GetCascadeModelConfigData the mock catalogue on its own
//	GetAvailableModels        an empty response
//	GetCascadeModelConfigs    an empty object
//	GetUserSettings           an "unimplemented" error
//
// Evidence grade: 一手.
package lsserver

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// mockedUserStatus is the reference's mocked GetUserStatus answer.
func mockedUserStatus() string {
	return fmt.Sprintf(`{"userStatus":{"pro":true,"name":"Mock User","email":"mock-user@google.com",`+
		`"cascadeModelConfigData":%s},"planInfo":{"planName":"Mock Plan"}}`,
		mockedCascadeModelConfigData())
}

// mockedCascadeModelConfigData is the reference's mocked catalogue.
//
// The three models are the reference's own placeholders:
// `MODEL_PLACEHOLDER_M8` (gemini-3.1-pro), `M80` (gemini-3.1-ultra)
// and `M81` (claude-4.5-opus). The second and the third are disabled;
// the third carries a quota reset four hours out.
//
// The reset time is recomputed per call — the reference's own answers
// move with the clock, which is how the two captures in
// out/langserver/mocked.json were told apart.
func mockedCascadeModelConfigData() string {
	reset := time.Now().UTC().Add(4 * time.Hour).Format("2006-01-02T15:04:05.000000Z")
	return fmt.Sprintf(`{"clientModelConfigs":[`+
		`{"label":"gemini-3.1-pro","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"},`+
		`"provider":"MODEL_PROVIDER_GOOGLE","supportedMimeTypes":{"image/jpeg":true,"image/png":true}},`+
		`{"label":"gemini-3.1-ultra","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M80"},"disabled":true,`+
		`"provider":"MODEL_PROVIDER_GOOGLE","tagTitle":"Unavailable",`+
		`"tagDescription":"Temporarily disabled due to capacity constraints"},`+
		`{"label":"claude-4.5-opus","modelOrAlias":{"model":"MODEL_PLACEHOLDER_M81"},"disabled":true,`+
		`"provider":"MODEL_PROVIDER_ANTHROPIC","quotaInfo":{"resetTime":"%s"}}],`+
		`"clientModelSorts":[{"name":"Recommended","groups":[{"modelLabels":[`+
		`"gemini-3.1-pro","gemini-3.1-ultra","claude-4.5-opus"]}]}],`+
		`"defaultOverrideModelConfig":{"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"}}}`,
		reset)
}

// mockedUnimplemented is the reference's answer for a method the
// mocked server does not implement.
//
// The error id is a fresh tracing id per call — the reference's own
// answers differ on every request.
func mockedUnimplemented() string {
	return fmt.Sprintf(`{"code":"unimplemented","message":"unimplemented: unimplemented (error ID: %s)"}`,
		errorID())
}

// errorID returns a 32 hex character tracing id.
func errorID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(buf[:])
}
