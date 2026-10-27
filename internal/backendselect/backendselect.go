// backendselect.go — the transport dispatch: which backend serves a model.
//
// The clone has three transports:
//
//   - cloudcode — the reference's own. It serves the six models the server
//     advertises and needs an OAuth token.
//   - gateway  — one configured endpoint, reached with the endpoint, the
//     credential and the upstream model id the user's provider
//     configuration names. It serves the models the user added.
//   - shim     — the deterministic stand-in.
//
// The default is "gateway", and the default *model* decides what that
// means: a model in the reference's catalogue goes to CloudCode, a model the
// user configured goes to its own endpoint. An explicit `--backend gateway`
// forces the configured endpoint even for a reference model.
//
// The rule lives here so cmd/wagyu/ stays a shell: the TUI's per-model
// re-dispatch (runMode.backendFor) and the print path's dispatch both call
// the same entry points.

package backendselect

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/accountrouting"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/cliflags"
	"github.com/justwasm/pigasus/internal/models"
)

// CatalogueIDFunc resolves a display label or catalogue id to the
// catalogue id the transports key on. It is injected so this package does
// not depend on the picker's own lookup.
type CatalogueIDFunc func(val string) string

// Pick implements the backend dispatch.
func Pick(fs *flag.FlagSet, modelName string, catalogueID CatalogueIDFunc) backend.Client {
	return PickWithLog(fs, modelName, io.Discard, backend.GatewayTimeoutUnlimited, catalogueID)
}

// PickWithLog is the underlying dispatch with a caller-supplied log writer.
func PickWithLog(fs *flag.FlagSet, modelName string, logWriter io.Writer, gatewayTimeout int, catalogueID CatalogueIDFunc) backend.Client {
	if logWriter == nil {
		logWriter = io.Discard
	}
	resolved, fromFlag := Resolve(fs, logWriter)
	explicit := fromFlag || strings.TrimSpace(os.Getenv("OPENAGY_BACKEND")) != ""
	switch resolved {
	case "cloudcode":
		return newOfficialBackend(modelName, logWriter, catalogueID)
	case "gateway":
		if !explicit && IsReferenceModel(modelName, catalogueID) {
			return newOfficialBackend(modelName, logWriter, catalogueID)
		}
		return newConfiguredBackend(modelName, gatewayTimeout, logWriter, catalogueID)
	case "shim":
		return backend.NewMock()
	}
	return backend.NewMock()
}

// IsReferenceModel reports whether name resolves to a model the reference
// serves itself — one of the models in its own catalogue.
//
// A model the user configured is not one of those: it has its own endpoint,
// and the configured transport is what reaches it.
func IsReferenceModel(name string, catalogueID CatalogueIDFunc) bool {
	if catalogueID == nil {
		return false
	}
	slug := catalogueID(name)
	if slug == "" {
		return false
	}
	for _, m := range cli.ReferenceModels() {
		if m.ID == slug {
			return true
		}
	}
	return false
}

// newConfiguredBackend builds the transport for a model the user configured.
//
// The endpoint, the credential, the upstream model id and the wire protocol
// all come from the user's provider configuration. Nothing is hard-coded: a
// model nobody configured has no endpoint, and the caller gets the shim
// instead — with a warning, because a model that silently answers from the
// shim would look like a working model.
func newConfiguredBackend(modelName string, timeoutSeconds int, logWriter io.Writer, catalogueID CatalogueIDFunc) backend.Client {
	id := ""
	if catalogueID != nil {
		id = catalogueID(modelName)
	}
	if id == "" {
		id = strings.TrimSpace(modelName)
	}
	m, ok := models.CachedRegistry().GetByName(id)
	if !ok {
		fmt.Fprintf(logWriter, "[warn] model %q is not configured: add it under a provider in %s\n", modelName, models.ProvidersPath())
		return backend.NewMock()
	}
	timeout := timeoutSeconds
	if m.TimeoutSeconds > 0 {
		timeout = m.TimeoutSeconds
	}
	return backend.NewGatewayWithConfig(backend.GatewayConfig{
		URL:              strings.TrimSpace(m.APIURL),
		APIKey:           strings.TrimSpace(m.APIKey),
		ExternalModel:    strings.TrimSpace(m.ExternalModelName),
		Type:             strings.TrimSpace(m.Type),
		Headers:          m.ExtraHeaders,
		TimeoutSeconds:   timeout,
		DefaultMaxTokens: m.DefaultMaxTokens,
		// The reasoning level the model runs at. On a tier row
		// ("minimax-m3.1-flash-xhigh") this is the tier itself, so
		// the tier rides the row into the request.
		ReasoningEffort: strings.TrimSpace(m.ReasoningEffort),
	})
}

// newOfficialBackend builds the CloudCode transport for one catalogue model.
//
// CloudCode is the reference's own transport and it needs an OAuth token. A
// machine without one gets the shim instead, with a warning — the same
// choice the reference makes when it cannot sign in.
//
// The token comes from the account pool: the router hands the transport the
// credential of the account the policy picked, and the decision is taken
// per turn. With no policy in the configuration file the router answers
// with the transport's own credential — the same source the client would
// have built for itself — so the default path is unchanged.
//
// The router is installed **always**, not only when a policy is present:
// the policy is read per turn, so a policy the user turns on from the
// TUI's own panel takes effect on the next turn, without a restart.
func newOfficialBackend(modelName string, logWriter io.Writer, catalogueID CatalogueIDFunc) backend.Client {
	if !HasOAuthToken() {
		fmt.Fprintln(logWriter, "[warn] no OAuth token found (need $JETSKI_OAUTH_TOKEN or ~/.config/antigravity/oauth-token); falling back to shim backend")
		return backend.NewMock()
	}
	slug := ""
	if catalogueID != nil {
		slug = catalogueID(modelName)
	}
	if slug == "" {
		slug = strings.TrimSpace(modelName)
	}
	router := accountrouting.Shared()
	// The router answers with the transport's own credential while the
	// policy is off. It is installed here, at build time, so the answer
	// is the one this transport would have given by itself — and a
	// rebuilt transport installs a fresh one.
	router.SetFallback(backend.DefaultTokenSource())
	client := backend.NewCloudCodeWithConfig(backend.CloudCodeConfig{
		Model:     slug,
		TokenFunc: router.TokenFunc,
	})
	return accountrouting.Wrap(client, logWriter)
}

// Resolve decides which backend to use.
func Resolve(fs *flag.FlagSet, logWriter io.Writer) (string, bool) {
	if logWriter == nil {
		logWriter = io.Discard
	}
	raw := ""
	fromFlag := false
	source := "default"
	if v := cliflags.StringValue(fs, "backend"); v != "" {
		raw = v
		fromFlag = true
		source = "--backend"
	} else if v := strings.ToLower(strings.TrimSpace(os.Getenv("OPENAGY_BACKEND"))); v != "" {
		raw = v
		source = "OPENAGY_BACKEND"
	}
	resolved := Normalise(raw, logWriter)
	if resolved == "cloudcode" && !HasOAuthToken() {
		fmt.Fprintf(logWriter, "[warn] %s=%s requested but no OAuth token found (need $JETSKI_OAUTH_TOKEN or ~/.config/antigravity/oauth-token); falling back to shim backend\n", source, resolved)
		resolved = "shim"
	}
	return resolved, fromFlag
}

// Normalise maps accepted values to "gateway", "cloudcode" or "shim".
func Normalise(raw string, logWriter io.Writer) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "gateway"
	case "gateway", "real":
		return "gateway"
	case "cloudcode", "cloud-code", "cloud_code", "google", "gemini", "cc":
		return "cloudcode"
	case "shim", "mock", "off", "false", "test":
		return "shim"
	default:
		fmt.Fprintf(logWriter, "[warn] unknown backend %q; expected gateway|cloudcode|shim (falling back to default 'gateway')\n", raw)
		return "gateway"
	}
}

// HasOAuthToken returns true if an OAuth token exists in env, on disk, or in Keychain.
func HasOAuthToken() bool {
	if v := strings.TrimSpace(os.Getenv("JETSKI_OAUTH_TOKEN")); v != "" {
		return true
	}
	if v := strings.TrimSpace(os.Getenv("ANTIGRAVITY_REFRESH_TOKEN")); v != "" {
		return true
	}
	candidates := []string{
		filepath.Join(AuthHome(), "oauth-token"),
		filepath.Join(AuthHome(), "antigravity-auth.json"),
		auth.TokenFilePath(),
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return true
		}
	}
	return auth.HasKeychainToken()
}

// AuthHome returns $XDG_CONFIG_HOME/antigravity or $HOME/.config/antigravity.
func AuthHome() string {
	if h := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); h != "" {
		return filepath.Join(h, "antigravity")
	}
	if h := strings.TrimSpace(os.Getenv("HOME")); h != "" {
		return filepath.Join(h, ".config", "antigravity")
	}
	return ""
}
