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

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/auth/credential"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
	"github.com/justwasm/pigasus/internal/infra/backend/gateway"
)

// CatalogueIDFunc resolves a display label or catalogue id to the
// catalogue id the transports key on. It is injected so this package does
// not depend on the picker's own lookup.
type CatalogueIDFunc func(val string) string

// Pick implements the backend dispatch.
func Pick(fs *flag.FlagSet, modelName string, catalogueID CatalogueIDFunc) backend.Client {
	return PickWithLog(fs, modelName, io.Discard, gateway.GatewayTimeoutUnlimited, catalogueID)
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

// writeLog emits a warning message to the provided logWriter and also appends
// it to the CLI log file (confhome.Antigravity()/cli.log) if available.
func writeLog(w io.Writer, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if w != nil && w != io.Discard {
		_, _ = io.WriteString(w, msg)
	}
	logPath := filepath.Join(confhome.Antigravity(), "cli.log")
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		defer f.Close()
		_, _ = io.WriteString(f, msg)
	}
}

// IsConfiguredModel reports whether name matches a configured provider model.
func IsConfiguredModel(name string, catalogueID CatalogueIDFunc) bool {
	_, ok := resolveConfigured(name, catalogueID)
	return ok
}

// ModelSupported reports whether name is either a reference model or a configured model.
func ModelSupported(name string, catalogueID CatalogueIDFunc) bool {
	return IsReferenceModel(name, catalogueID) || IsConfiguredModel(name, catalogueID)
}

// IsReferenceModel reports whether name resolves to a model the reference
// serves itself — one of the models in its own catalogue.
//
// A model the user configured is not one of those: it has its own endpoint,
// and the configured transport is what reaches it.
func IsReferenceModel(name string, catalogueID CatalogueIDFunc) bool {
	slug := catalogueSlug(name, catalogueID)
	if slug == "" {
		return false
	}
	for _, m := range catalog.ReferenceModels() {
		if m.ID == slug {
			return true
		}
	}
	return false
}

// catalogueSlug resolves a name the dispatch was handed — a catalogue id, a
// display label, a tier-less label or an upstream spelling — to the catalogue
// id the transports key on.
//
// The picker's own lookup is exact and runs first. It is not enough by itself:
// a host that sets the model from settings.json or from its own model menu can
// hand over a display label the picker has no row for ("MiniMax M3",
// "DeepSeek-V4-Flash"), and a label that resolves to "" silently turned a
// configured model into the shim. The catalogue's own resolution knows the
// rest: the label of a tiered model maps to its default tier row.
func catalogueSlug(name string, catalogueID CatalogueIDFunc) string {
	if catalogueID != nil {
		if id := strings.TrimSpace(catalogueID(name)); id != "" {
			return id
		}
	}
	if label := catalog.CanonicalModelLabel(name); label != "" {
		if id, ok := catalog.CatalogueID(label); ok {
			return id
		}
	}
	if id, err := catalog.ResolveModelSelection(strings.TrimSpace(name), ""); err == nil && id != "" {
		return id
	}
	return ""
}

// newConfiguredBackend builds the transport for a model the user configured.
//
// The endpoint, the credential, the upstream model id and the wire protocol
// all come from the user's provider configuration. Nothing is hard-coded: a
// model nobody configured has no endpoint, and the caller gets the shim
// instead — with a warning, because a model that silently answers from the
// shim would look like a working model.
func newConfiguredBackend(modelName string, timeoutSeconds int, logWriter io.Writer, catalogueID CatalogueIDFunc) backend.Client {
	m, ok := resolveConfigured(modelName, catalogueID)
	if !ok {
		writeLog(logWriter, "[warn] model %q is not configured: add it under a provider in %s\n", modelName, models.ProvidersPath())
		return backend.NewMock()
	}
	timeout := timeoutSeconds
	if m.TimeoutSeconds > 0 {
		timeout = m.TimeoutSeconds
	}
	return gateway.NewGatewayWithConfig(gateway.GatewayConfig{
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

// resolveConfigured returns the configured model a requested name names, and
// whether it names one at all.
//
// The name arrives in whatever form the caller had: the picker's display label
// ("MiniMax M3"), the catalogue id ("minimax-m3"), the upstream spelling
// ("MiniMax-M3.1-Flash"), or the tier-less label of a model that declares
// reasoning tiers ("DeepSeek-V4-Flash", whose catalogue rows are the "(Low)"
// / "(High)" / "(Max)" spellings). All of those name a model the user
// configured, and none of them may fall through to the shim: a configured
// model that answered with the mock client looked like a working model and
// produced a turn of synthetic text (the 2026-11-14 report).
func resolveConfigured(name string, catalogueID CatalogueIDFunc) (models.Model, bool) {
	reg := models.CachedRegistry()
	if reg == nil {
		return models.Model{}, false
	}
	requested := strings.TrimSpace(name)
	if requested == "" {
		return models.Model{}, false
	}
	candidates := []string{requested}
	if id := strings.TrimSpace(catalogueSlug(requested, catalogueID)); id != "" {
		candidates = append(candidates, id)
	}
	for _, cand := range candidates {
		if m, ok := reg.GetByName(cand); ok {
			return *m, true
		}
	}
	// The registry's own Name is the catalogue id; the label and the upstream
	// spelling are what a picker row and an external model menu carry.
	want := strings.ToLower(requested)
	for _, m := range reg.List() {
		for _, v := range []string{m.DisplayName, m.ExternalModelName, m.QualifiedName()} {
			if v != "" && strings.ToLower(strings.TrimSpace(v)) == want {
				return m, true
			}
		}
	}
	return models.Model{}, false
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
		writeLog(logWriter, "[warn] no OAuth token found (need $JETSKI_OAUTH_TOKEN or ~/.config/antigravity/oauth-token); falling back to shim backend\n")
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
	router.SetFallback(credential.DefaultTokenSource())
	client := cloudcode.NewCloudCodeWithConfig(cloudcode.CloudCodeConfig{
		Model:     slug,
		TokenFunc: router.TokenFuncForModel(slug),
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
		writeLog(logWriter, "[warn] %s=%s requested but no OAuth token found (need $JETSKI_OAUTH_TOKEN or ~/.config/antigravity/oauth-token); falling back to shim backend\n", source, resolved)
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
		writeLog(logWriter, "[warn] unknown backend %q; expected gateway|cloudcode|shim (falling back to default 'gateway')\n", raw)
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
