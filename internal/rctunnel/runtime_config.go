package rctunnel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// StripBOM removes UTF-8 Byte Order Mark if present.
func StripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		return b[3:]
	}
	return b
}

// SetupRuntimeConfig creates an isolated configuration directory for the language server.
// The user's ~/.gemini/config/config.json is never modified; the instance name and
// remote control enablement are written only to this isolated runtime directory.
func SetupRuntimeConfig(geminiDir, instanceName string) (configDir string, cleanup func(), err error) {
	if geminiDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil, fmt.Errorf("resolve user home dir: %w", err)
		}
		geminiDir = filepath.Join(home, ".gemini")
	}

	srcConfigPath := filepath.Join(geminiDir, "config", "config.json")
	var raw map[string]interface{}
	if data, readErr := os.ReadFile(srcConfigPath); readErr == nil {
		data = StripBOM(data)
		_ = json.Unmarshal(data, &raw)
	}
	if raw == nil {
		raw = make(map[string]interface{})
	}

	userSettings, ok := raw["userSettings"].(map[string]interface{})
	if !ok {
		userSettings = make(map[string]interface{})
		raw["userSettings"] = userSettings
	}

	userSettings["remoteControlEnabled"] = true
	userSettings["cliRemoteControlHostname"] = instanceName
	userSettings["remoteControlHostname"] = instanceName

	// Create isolated directory inside geminiDir so relative config_dir flags resolve cleanly
	cfgDirName := fmt.Sprintf("pigasus-rc-%d", os.Getpid())
	absConfigDir := filepath.Join(geminiDir, cfgDirName)
	if err := os.MkdirAll(absConfigDir, 0700); err != nil {
		// Fallback to temp directory if ~/.gemini is read-only
		tmpDir, tmpErr := os.MkdirTemp("", "pigasus-rc-*")
		if tmpErr != nil {
			return "", nil, fmt.Errorf("create runtime config dir: %w", err)
		}
		absConfigDir = tmpDir
		cfgDirName = absConfigDir
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		_ = os.RemoveAll(absConfigDir)
		return "", nil, fmt.Errorf("marshal runtime config: %w", err)
	}

	dstConfigPath := filepath.Join(absConfigDir, "config.json")
	if err := os.WriteFile(dstConfigPath, out, 0600); err != nil {
		_ = os.RemoveAll(absConfigDir)
		return "", nil, fmt.Errorf("write runtime config: %w", err)
	}

	cleanup = func() {
		_ = os.RemoveAll(absConfigDir)
	}
	return cfgDirName, cleanup, nil
}
