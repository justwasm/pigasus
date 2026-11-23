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
	"github.com/justwasm/pigasus/internal/core/clilog"
	"github.com/justwasm/pigasus/internal/core/confhome"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/auth/credential"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
	"github.com/justwasm/pigasus/internal/infra/backend/failover"
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
		primary := newOfficialBackend(modelName, logWriter, catalogueID)
		return buildFailoverChain(fs, modelName, primary, "cloudcode", gatewayTimeout, logWriter, catalogueID)
	case "gateway":
		if !explicit && IsReferenceModel(modelName, catalogueID) {
			primary := newOfficialBackend(modelName, logWriter, catalogueID)
			return buildFailoverChain(fs, modelName, primary, "cloudcode", gatewayTimeout, logWriter, catalogueID)
		}
		primary := newConfiguredBackend(modelName, gatewayTimeout, logWriter, catalogueID)
		return buildFailoverChain(fs, modelName, primary, "configured:"+modelName, gatewayTimeout, logWriter, catalogueID)
	case "shim":
		return backend.NewMock()
	}
	return backend.NewMock()
}

// buildFailoverChain constructs an ordered failover.Client whenever secondary fallback
// providers or models are configured, either per-model or through OPENAGY_FALLBACK_CHAIN.
func buildFailoverChain(fs *flag.FlagSet, modelName string, primary backend.Client, primaryName string, gatewayTimeout int, logWriter io.Writer, catalogueID CatalogueIDFunc) backend.Client {
	targets := []failover.Target{
		{Name: primaryName, Client: primary, LogicalModel: strings.TrimSpace(modelName)},
	}
	seenModels := map[string]bool{
		strings.ToLower(strings.TrimSpace(modelName)): true,
	}
	seenTargets := map[string]bool{
		primaryName: true,
	}

	var candidateFallbacks []string

	// 1. Model-specific fallbacks if configured model
	if m, ok := resolveConfigured(modelName, catalogueID); ok {
		candidateFallbacks = append(candidateFallbacks, m.Fallbacks...)
	}

	// 2. Fallback configured backend for reference models if none explicitly listed
	if len(candidateFallbacks) == 0 && IsReferenceModel(modelName, catalogueID) {
		if _, ok := resolveConfigured(modelName, catalogueID); ok {
			candidateFallbacks = append(candidateFallbacks, modelName)
		}
	}

	// 3. Command-line flag or environment variable fallback chain
	chainVal := ""
	if fs != nil {
		chainVal = cliflags.StringValue(fs, "fallback-chain")
	}
	if chainVal == "" {
		chainVal = os.Getenv("OPENAGY_FALLBACK_CHAIN")
	}
	if strings.TrimSpace(chainVal) != "" {
		for _, part := range strings.Split(chainVal, ",") {
			p := strings.TrimSpace(part)
			if p != "" {
				candidateFallbacks = append(candidateFallbacks, p)
			}
		}
	}

	// 4. Resolve each candidate fallback
	for _, fb := range candidateFallbacks {
		fbClean := strings.TrimSpace(fb)
		if fbClean == "" {
			continue
		}
		normFB := strings.ToLower(fbClean)
		targetName := "fallback:" + fbClean

		// Special case: shim/mock
		if strings.EqualFold(fbClean, "shim") || strings.EqualFold(fbClean, "mock") {
			if !seenTargets[targetName] {
				seenTargets[targetName] = true
				targets = append(targets, failover.Target{
					Name:   targetName,
					Client: backend.NewMock(),
				})
			}
			continue
		}

		if seenModels[normFB] {
			// If primary is cloudcode and the user also configured a gateway endpoint for this same model name,
			// allow it as an explicit cross-provider downgrade fallback once.
			if primaryName == "cloudcode" && !seenTargets[targetName] {
				if _, ok := resolveConfigured(fbClean, catalogueID); !ok {
					continue
				}
			} else {
				continue
			}
		}
		seenModels[normFB] = true

		// Configured model
		if m, ok := resolveConfigured(fbClean, catalogueID); ok {
			if seenTargets[targetName] {
				continue
			}
			seenTargets[targetName] = true
			timeout := gatewayTimeout
			if m.TimeoutSeconds > 0 {
				timeout = m.TimeoutSeconds
			}
			client := newGatewayClientForModel(m, timeout)
			wireModel := strings.TrimSpace(m.ExternalModelName)
			targets = append(targets, failover.Target{
				Name:              targetName,
				Client:            client,
				LogicalModel:      fbClean,
				MaxTokens:         m.DefaultMaxTokens,
				ReasoningEffort:   strings.TrimSpace(m.ReasoningEffort),
				SupportsThinking:  m.SupportsThinking,
				SupportsToolCalls: m.SupportsToolCalls,
				ModelMap: func(string) string {
					if wireModel != "" {
						return wireModel
					}
					return fbClean
				},
			})
			continue
		}

		// Reference model
		if IsReferenceModel(fbClean, catalogueID) && HasOAuthToken() {
			if seenTargets[targetName] {
				continue
			}
			seenTargets[targetName] = true
			client := newOfficialBackend(fbClean, logWriter, catalogueID)
			refSlug := catalogueSlug(fbClean, catalogueID)
			targets = append(targets, failover.Target{
				Name:         targetName,
				Client:       client,
				LogicalModel: fbClean,
				ModelMap: func(string) string {
					if refSlug != "" {
						return refSlug
					}
					return fbClean
				},
			})
			continue
		}
	}

	if len(targets) == 1 {
		return primary
	}

	return failover.New(targets, func(ev failover.Event) {
		writeLog(logWriter, "[failover] %s failed with %v -> switching to %s (model: %s)\n", ev.FromTarget, ev.Err, ev.ToTarget, ev.Model)
	})
}

// writeLog emits a warning message to the provided logWriter, the session log
// (~/.openagy/log/cli-*.log), and confhome.Antigravity()/cli.log if available.
func writeLog(w io.Writer, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	clilog.Logf("backendselect", "%s", strings.TrimSpace(msg))
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
// serves itself — one of the models in its own catalogue, or an undocumented
// id the CloudCode endpoint serves without advertising (catalog's
// undocumented table; the gate decides whether it is in force).
//
// A model the user configured is not one of those: it has its own endpoint,
// and the configured transport is what reaches it.
func IsReferenceModel(name string, catalogueID CatalogueIDFunc) bool {
	slug := catalogueSlug(name, catalogueID)
	if slug == "" {
		return false
	}
	for _, m := range catalog.ReferenceServedModels() {
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
func newGatewayClientForModel(m models.Model, timeout int) backend.Client {
	accounts := gatewayAccountsFromModel(m)
	var pool *gateway.KeyPool
	if len(accounts) > 0 {
		var keyParts []string
		for _, a := range accounts {
			keyParts = append(keyParts, a.ID+":"+a.APIKey)
		}
		cacheKey := strings.TrimSpace(m.ProviderID) + "|" + strings.TrimSpace(m.APIURL) + "|" + strings.Join(keyParts, ",")
		pool = gateway.SharedKeyPool(cacheKey, accounts)
	}
	return gateway.NewGatewayWithConfig(gateway.GatewayConfig{
		URL:               strings.TrimSpace(m.APIURL),
		APIKey:            strings.TrimSpace(m.APIKey),
		ExternalModel:     strings.TrimSpace(m.ExternalModelName),
		Type:              strings.TrimSpace(m.Type),
		Headers:           m.ExtraHeaders,
		TimeoutSeconds:    timeout,
		DefaultMaxTokens:  m.DefaultMaxTokens,
		ReasoningEffort:   strings.TrimSpace(m.ReasoningEffort),
		SupportsThinking:  m.SupportsThinking,
		SupportsToolCalls: m.SupportsToolCalls,
		Accounts:          accounts,
		KeyPool:           pool,
	})
}

func gatewayAccountsFromModel(m models.Model) []gateway.KeyAccount {
	if len(m.Accounts) == 0 {
		return nil
	}
	out := make([]gateway.KeyAccount, 0, len(m.Accounts)+1)
	for _, acc := range m.Accounts {
		if acc.Disabled {
			continue
		}
		k := strings.TrimSpace(acc.APIKey)
		if k == "" {
			k = strings.TrimSpace(acc.Token)
		}
		if k == "" {
			continue
		}
		out = append(out, gateway.KeyAccount{
			ID:     strings.TrimSpace(acc.ID),
			APIKey: k,
		})
	}
	if len(out) > 0 && strings.TrimSpace(m.APIKey) != "" {
		out = append(out, gateway.KeyAccount{
			ID:     "default",
			APIKey: strings.TrimSpace(m.APIKey),
		})
	}
	return out
}

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
	return newGatewayClientForModel(m, timeout)
}

// fallbackConfiguredBackend checks if a configured fallback backend exists for a reference model.
func fallbackConfiguredBackend(modelName string, gatewayTimeout int, logWriter io.Writer, catalogueID CatalogueIDFunc) (backend.Client, bool) {
	if m, ok := resolveConfigured(modelName, catalogueID); ok {
		timeout := gatewayTimeout
		if m.TimeoutSeconds > 0 {
			timeout = m.TimeoutSeconds
		}
		return newGatewayClientForModel(m, timeout), true
	}
	return nil, false
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

	// 1. If requested matches a model's DisplayName (e.g. "OpenRouter: DeepSeek V4.1 Flash (High)"),
	// honor that first. The display label is explicit and avoids ambiguous prefix collisions.
	want := strings.ToLower(requested)
	allModels := append(models.TierRows(reg.List()), reg.List()...)
	for _, m := range allModels {
		if m.DisplayName != "" && strings.ToLower(strings.TrimSpace(m.DisplayName)) == want {
			return m, true
		}
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
	for _, m := range allModels {
		for _, v := range []string{m.DisplayName, m.ExternalModelName, m.QualifiedName(), m.Name} {
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
		writeLog(logWriter, "[warn] no OAuth token found (need $JETSKI_OAUTH_TOKEN, system keychain, ~/.gemini/antigravity-cli/antigravity-oauth-token, or ~/.openagy/accounts.json); falling back to shim backend\n")
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
		writeLog(logWriter, "[warn] %s=%s requested but no OAuth token found (need $JETSKI_OAUTH_TOKEN, system keychain, ~/.gemini/antigravity-cli/antigravity-oauth-token, or ~/.openagy/accounts.json); falling back to shim backend\n", source, resolved)
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

// HasOAuthToken returns true if an OAuth token exists in env, on disk, in Keychain,
// or in ~/.openagy/accounts.json.
func HasOAuthToken() bool {
	if v := strings.TrimSpace(os.Getenv("JETSKI_OAUTH_TOKEN")); v != "" {
		return true
	}
	if v := strings.TrimSpace(os.Getenv("ANTIGRAVITY_REFRESH_TOKEN")); v != "" {
		return true
	}
	if fi, err := os.Stat(auth.TokenFilePath()); err == nil && !fi.IsDir() && fi.Size() > 0 {
		return true
	}
	if auth.HasKeychainToken() {
		return true
	}
	tok, err := auth.LoadTokenUnified()
	return err == nil && tok != nil && (tok.Token.AccessToken != "" || tok.Token.RefreshToken != "")
}
