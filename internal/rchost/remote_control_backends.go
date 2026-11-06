// Package main — remote_control_backends.go: backends and listings for remote control daemon.
package rchost

import (
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/agentbrowser"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

// BackendPicker is injected by the host process to build the backend.Client
// for a given model. Defaults to backend.NewMock().
var BackendPicker = func(modelName string) backend.Client {
	return backend.NewMock()
}

// buildHostMessageBackend returns the production `remotecontrol.MessageBackend`
// the dispatcher's Config.MessageBackend field gets.
//
// The transport is chosen per turn: the Web UI picks the model when the
// conversation starts, and the daemon is started long before that. A
// fixed client would have to be built for a model that does not exist
// yet — the empty name landed on the shim, and the dashboard answered
// "mock reply from model=…" to every message.
//
// The broker is the agent↔browser bridge: the same instance the
// dispatcher's three LanguageServerService RPCs serve, so the IDE half
// (the Web UI's window) and the agent half (the turn's
// `webview_browser` tool) meet in one place.
func buildHostMessageBackend(workspace, modelName string, broker *agentbrowser.Broker) cascades.Backend {
	be := BackendPicker(resolveModelName(modelName))
	return &remotecontrol.ActorBackend{
		SessionFactory: NewRemoteCascadeSession(
			RemoteCascadeOptions{
				Backend: be,
				BackendFactory: func(m string) backend.Client {
					return BackendPicker(resolveModelName(m))
				},
				Workspace: workspace,
				AgentDir:  auth.AntigravityHome(),
				Browser:   broker,
			},
		),
	}
}

// resolveModelName returns the model a turn runs on: the model's own id
// when the request carries one, the user's configured default or reference default
// otherwise.
func resolveModelName(modelName string) string {
	name := strings.TrimSpace(modelName)
	if name != "" {
		return name
	}
	if env := strings.TrimSpace(os.Getenv("OPENAGY_MODEL")); env != "" {
		return env
	}
	return "gemini-3.8-flash-high"
}

// defaultConfiguredModel returns the first model the user's provider
// configuration serves, or "" when nothing is configured.
func defaultConfiguredModel() string {
	for _, m := range models.CachedRegistry().List() {
		if name := strings.TrimSpace(m.Name); name != "" {
			return name
		}
	}
	return ""
}

// hostListModels returns models supported by reference, filtered by backend.
//
// The reference's own catalogue comes first: Gemini 3.8 Flash (High)
// is the reference default model and the top recommendation in the Web UI picker.
// The user's configured models follow.
//
// A configured model that declares reasoning tiers is listed once per
// tier — the reference's own shape ("Gemini 3.8 Flash (High)"). The
// Web UI's picker shows the label it is handed and sends the row's id
// back, so the tiers reach the picker without a frontend change.
func hostListModels(backend, filter, defaultModel string) ([]remotecontrol.ModelInfo, error) {
	chosen := strings.TrimSpace(backend)
	if chosen == "" {
		chosen = defaultModel
	}
	chosen = strings.ToLower(chosen)

	targetDefault := strings.TrimSpace(os.Getenv("OPENAGY_MODEL"))
	if targetDefault == "" {
		targetDefault = "gemini-3.8-flash-high"
	}

	out := make([]remotecontrol.ModelInfo, 0, 16)
	defaultSet := false

	// Reference models come first so the default reference model (Gemini 3.8 Flash High)
	// is the top choice in the model picker.
	entries := catalog.ReferenceModels()
	for _, e := range entries {
		// The reference's own catalogue states nothing about pictures:
		// the family table decides (remotecontrol.modelTakesImages).
		isDef := false
		if !defaultSet {
			isDef = strings.EqualFold(e.ID, targetDefault) || (targetDefault == "gemini-3.8-flash-high" && e.ID == "gemini-3.8-flash-high")
			if isDef {
				defaultSet = true
			}
		}
		out = append(out, remotecontrol.ModelInfo{
			ID:              e.ID,
			Label:           e.Label,
			Backend:         chosen,
			EffortSupported: true,
			Default:         isDef,
		})
	}

	// Configured models follow.
	for _, m := range models.TierRows(models.CachedRegistry().List()) {
		label := strings.TrimSpace(m.DisplayName)
		if label == "" {
			label = m.Name
		}
		isDef := false
		if !defaultSet {
			isDef = strings.EqualFold(m.Name, targetDefault) || strings.EqualFold(label, targetDefault)
			if isDef {
				defaultSet = true
			}
		}
		out = append(out, remotecontrol.ModelInfo{
			ID:              m.Name,
			Label:           label,
			Backend:         "gateway",
			EffortSupported: effortSupportedOrDefault(m.EffortSupported),
			Default:         isDef,
			SupportsImages:  m.SupportsImages,
			// The model's own attributes, as the configuration states
			// them. A model that says nothing carries nothing, and the
			// Web UI's picker falls back to its own defaults.
			SupportedMimeTypes:         m.SupportedMimeTypes,
			Disabled:                   m.Disabled,
			QuotaInfo:                  m.QuotaInfo,
			SupportsThoughtCirculation: m.SupportsThoughtCirculation,
			ModelURL:                   m.ModelURL,
			ModelID:                    m.ExternalModelName,
			TagTitle:                   m.TagTitle,
			TagDescription:             m.TagDescription,
		})
	}
	if len(out) == 0 {
		return remotecontrol.DefaultModelInfo, nil
	}
	if !defaultSet && len(out) > 0 {
		out[0].Default = true
	}
	return out, nil
}

// effortSupportedOrDefault resolves the model's reasoning-level
// marker: the configuration's own word when it has one, and the
// clone's long-standing default (every model offers an effort level)
// when it does not.
//
// The Web UI's picker does not read the marker at all; the clone's own
// surfaces do — the `/effort` command and the model list. So a
// configuration that says "this model has no reasoning level" turns it
// off, and a configuration that says nothing changes nothing.
func effortSupportedOrDefault(stated *bool) bool {
	if stated == nil {
		return true
	}
	return *stated
}

// hostListConversations lists conversations from the conversation store.
func hostListConversations(limit int, before time.Time) ([]remotecontrol.ConversationInfo, time.Time, error) {
	raw, err := convstore.ListConversations()
	if err != nil {
		return nil, time.Time{}, err
	}
	filtered := make([]convstore.ConversationMeta, 0, len(raw))
	for _, c := range raw {
		if !before.IsZero() && !c.ModTime.Before(before) {
			continue
		}
		filtered = append(filtered, c)
	}
	if limit <= 0 || limit > len(filtered) {
		limit = len(filtered)
	}
	page := filtered[:limit]
	out := make([]remotecontrol.ConversationInfo, 0, len(page))
	var oldest time.Time
	for _, c := range page {
		out = append(out, remotecontrol.ConversationInfo{
			ID:        c.ID,
			Title:     c.Title,
			CreatedAt: c.ModTime.UTC().Format(time.RFC3339Nano),
			UpdatedAt: c.ModTime.UTC().Format(time.RFC3339Nano),
			Model:     "",
		})
		if oldest.IsZero() || c.ModTime.Before(oldest) {
			oldest = c.ModTime
		}
	}
	if len(page) < limit {
		oldest = time.Time{}
	}
	return out, oldest, nil
}

// hostListEnvironments gathers host environment details for remote control.
func hostListEnvironments() (remotecontrol.EnvironmentInfo, error) {
	cwd, _ := os.Getwd()
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	user := strings.TrimSpace(os.Getenv("USER"))
	if user == "" {
		user = strings.TrimSpace(os.Getenv("LOGNAME"))
	}
	keys := remotecontrol.DefaultEnvKeyProbe
	present := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		if v := os.Getenv(k); v != "" {
			present = append(present, k)
		}
	}
	sort.Strings(present)
	return remotecontrol.EnvironmentInfo{
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		Hostname:        hostnameOrEmpty(),
		Cwd:             cwd,
		User:            user,
		Shell:           shell,
		AntigravityHome: auth.AntigravityHome(),
		Version:         cli.ReferenceVersion,
		EnvKeysPresent:  present,
		Capabilities: map[string]any{
			"remote_control": true,
		},
	}, nil
}

// buildHostListBackend returns the production ListBackend the daemon's four
// read-only handlers call into.
func buildHostListBackend() remotecontrol.ListBackend {
	defaultModel := defaultBackendLabel()
	return remotecontrol.ListBackend{
		Models: func(backend, filter string) ([]remotecontrol.ModelInfo, error) {
			return hostListModels(backend, filter, defaultModel)
		},
		SlashCommands: func() ([]remotecontrol.SlashCommandInfo, error) {
			all := remotecontrol.DefaultSlashCommands
			out := make([]remotecontrol.SlashCommandInfo, len(all))
			copy(out, all)
			return out, nil
		},
		Conversations: hostListConversations,
		Environments:  hostListEnvironments,
	}
}

// defaultBackendLabel returns the configured backend label for the model catalog.
func defaultBackendLabel() string {
	if v := strings.TrimSpace(os.Getenv("OPENAGY_BACKEND")); v != "" {
		return strings.ToLower(v)
	}
	return "gateway"
}

// hostnameOrEmpty returns os.Hostname's value, or an empty string on error.
func hostnameOrEmpty() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return ""
}
