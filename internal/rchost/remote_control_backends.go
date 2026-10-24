// Package main — remote_control_backends.go: backends and listings for remote control daemon.
package rchost

import (
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// BackendPicker is injected by the host process to build the backend.Client
// for a given model. Defaults to backend.NewMock().
var BackendPicker = func(modelName string) backend.Client {
	return backend.NewMock()
}

// buildHostMessageBackend returns the production `remotecontrol.MessageBackend`
// the dispatcher's Config.MessageBackend field gets.
func buildHostMessageBackend(workspace, modelName string) remotecontrol.MessageBackend {
	be := BackendPicker(modelName)
	return &remotecontrol.PiagentBackend{
		SessionFactory: piagentbridge.NewRemoteCascadeSession(
			piagentbridge.RemoteCascadeOptions{
				Backend:   be,
				Workspace: workspace,
				AgentDir:  auth.AntigravityHome(),
			},
		),
	}
}

// hostListModels returns models supported by reference, filtered by backend.
func hostListModels(backend, filter, defaultModel string) ([]remotecontrol.ModelInfo, error) {
	chosen := strings.TrimSpace(backend)
	if chosen == "" {
		chosen = defaultModel
	}
	chosen = strings.ToLower(chosen)
	entries := cli.ReferenceModels()
	if len(entries) == 0 {
		return remotecontrol.DefaultModelInfo, nil
	}
	out := make([]remotecontrol.ModelInfo, 0, len(entries))
	firstForBackend := true
	for _, e := range entries {
		out = append(out, remotecontrol.ModelInfo{
			ID:              e.ID,
			Label:           e.Label,
			Backend:         chosen,
			EffortSupported: true,
			Default:         firstForBackend,
		})
		firstForBackend = false
	}
	return out, nil
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
