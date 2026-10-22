package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
)

// pickBackend implements the backend dispatch.
func pickBackend(fs *flag.FlagSet, modelName string) backend.Client {
	return pickBackendWithLog(fs, modelName, io.Discard, backend.AgwTimeoutUnlimited)
}

// pickBackendWithLog is the underlying dispatch with a caller-supplied log writer.
func pickBackendWithLog(fs *flag.FlagSet, modelName string, logWriter io.Writer, agwTimeout int) backend.Client {
	resolved, fromFlag := resolveBackend(fs, logWriter)
	_ = logWriter
	explicit := fromFlag || strings.TrimSpace(os.Getenv("OPENAGY_BACKEND")) != ""
	switch resolved {
	case "cloudcode":
		return newOfficialBackend(modelName)
	case "agw":
		if !explicit && isOfficialModel(modelName) {
			return newOfficialBackend(modelName)
		}
		return backend.NewAgwK0sWithConfig(backend.AgwK0sConfig{TimeoutSeconds: agwTimeout})
	case "shim":
		return backend.NewMock()
	}
	return backend.NewMock()
}

// isOfficialModel reports whether name resolves to a model served via CloudCode.
func isOfficialModel(name string) bool {
	slug := resolveCatalogueID(name)
	return slug != "" && !isMiniMax(slug)
}

// newOfficialBackend builds the CloudCode transport for one catalogue model.
func newOfficialBackend(modelName string) backend.Client {
	slug := resolveCatalogueID(modelName)
	if slug == "" {
		slug = strings.TrimSpace(modelName)
	}
	return backend.NewCloudCodeWithConfig(backend.CloudCodeConfig{Model: slug})
}

// resolveBackend decides which backend to use.
func resolveBackend(fs *flag.FlagSet, logWriter io.Writer) (string, bool) {
	raw := ""
	fromFlag := false
	source := "default"
	if v := flagStringValue(fs, "backend"); v != "" {
		raw = v
		fromFlag = true
		source = "--backend"
	} else if v := strings.ToLower(strings.TrimSpace(os.Getenv("OPENAGY_BACKEND"))); v != "" {
		raw = v
		source = "OPENAGY_BACKEND"
	}
	resolved := normaliseBackend(raw, logWriter)
	if (resolved == "agw" || resolved == "cloudcode") && !hasOAuthToken() {
		fmt.Fprintf(logWriter, "[warn] %s=%s requested but no OAuth token found (need $JETSKI_OAUTH_TOKEN or ~/.config/antigravity/oauth-token); falling back to shim backend\n", source, resolved)
		resolved = "shim"
	}
	return resolved, fromFlag
}

// normaliseBackend maps accepted values to "agw", "cloudcode" or "shim".
func normaliseBackend(raw string, logWriter io.Writer) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "agw"
	case "agw", "agw.k0s.io", "agw_k0s", "real", "k0s":
		return "agw"
	case "cloudcode", "cloud-code", "cloud_code", "google", "gemini", "cc":
		return "cloudcode"
	case "shim", "mock", "off", "false", "test":
		return "shim"
	default:
		fmt.Fprintf(logWriter, "[warn] unknown backend %q; expected agw|cloudcode|shim (falling back to default 'agw')\n", raw)
		return "agw"
	}
}

// hasOAuthToken returns true if an OAuth token exists in env, on disk, or in Keychain.
func hasOAuthToken() bool {
	if v := strings.TrimSpace(os.Getenv("JETSKI_OAUTH_TOKEN")); v != "" {
		return true
	}
	if v := strings.TrimSpace(os.Getenv("ANTIGRAVITY_REFRESH_TOKEN")); v != "" {
		return true
	}
	candidates := []string{
		filepath.Join(authHome(), "oauth-token"),
		filepath.Join(authHome(), "antigravity-auth.json"),
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

// authHome returns $XDG_CONFIG_HOME/antigravity or $HOME/.config/antigravity.
func authHome() string {
	if h := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); h != "" {
		return filepath.Join(h, "antigravity")
	}
	if h := strings.TrimSpace(os.Getenv("HOME")); h != "" {
		return filepath.Join(h, ".config", "antigravity")
	}
	return ""
}
