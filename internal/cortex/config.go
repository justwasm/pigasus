// Package cortex — config.go: static configuration, settings, and script resolution.
//
// pclntab entries:
//   - ResolveStaticConfig            0x7fd91c0
//   - ResolveExecutableScript        0x7fd9140
//   - GetResolvedCascadeConfig       (inferred)
//   - PreviousContribVariant         0x7fd9240
//   - SetInMemoryCLISettingsProvider 0x7fdaec0
//   - GetInMemoryCLISettingsProvider 0x7fdb040
package cortex

import "errors"

// ResolveStaticConfig resolves the static config. v1.3.5 binary addr
// 0x7fd91c0, name=ResolveStaticConfig, signature=(cm
// *CascadeManager) (StaticConfig, error).
func (cm *CascadeManager) ResolveStaticConfig() (StaticConfig, error) {
	return StaticConfig{}, nil
}

// ResolveExecutableScript resolves the executable script. v1.3.5
// binary addr 0x7fd9140, name=ResolveExecutableScript, signature=(cm
// *CascadeManager) (string, error).
func (cm *CascadeManager) ResolveExecutableScript() (string, error) {
	return "", nil
}

// GetResolvedCascadeConfig returns the resolved cascade config. v1.3.5
// binary addr (per pclntab .GetResolvedCascadeConfig),
// name=GetResolvedCascadeConfig, signature=(cm *CascadeManager)
// (CascadeConfig, error).
func (cm *CascadeManager) GetResolvedCascadeConfig() (CascadeConfig, error) {
	return CascadeConfig{}, nil
}

// PreviousContribVariant returns the previous contrib variant. v1.3.5
// binary addr 0x7fd9240, name=PreviousContribVariant, signature=(cm
// *CascadeManager) (string, error).
func (cm *CascadeManager) PreviousContribVariant() (string, error) {
	return "", nil
}

// SetInMemoryCLISettingsProvider sets the in-memory CLI settings
// provider. v1.3.5 binary addr 0x7fdaec0,
// name=SetInMemoryCLISettingsProvider, signature=(cm
// *CascadeManager, p *CLISettingsProvider) error.
func (cm *CascadeManager) SetInMemoryCLISettingsProvider(p *CLISettingsProvider) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cliSettings = p
	return nil
}

// GetInMemoryCLISettingsProvider returns the in-memory CLI settings
// provider. v1.3.5 binary addr 0x7fdb040,
// name=GetInMemoryCLISettingsProvider, signature=(cm
// *CascadeManager) (*CLISettingsProvider, error).
func (cm *CascadeManager) GetInMemoryCLISettingsProvider() (*CLISettingsProvider, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.cliSettings == nil {
		return nil, errors.New("cortex: no in-memory CLI settings provider")
	}
	return cm.cliSettings, nil
}

// CascadeConfig is the recovered cascade config (mirrors v1.3.5
// `CascadeConfig` proto, see decompiled cortex_pb/cortex_go_proto
// at 0x68323a0).
type CascadeConfig struct {
	AgentAPIConfig *AgentAPIConfig
	Model          string
	Tools          []ToolConfig
}

// AgentAPIConfig is the recovered agent API config.
type AgentAPIConfig struct {
	URL    string
	APIKey string
	Model  string
}

// ToolConfig is the recovered tool config.
type ToolConfig struct {
	Name     string
	Enabled  bool
	Settings map[string]any
}

// StaticConfig is the recovered static config (mirrors the v1.3.5
// `(*CascadeManager).ResolveStaticConfig` return).
type StaticConfig struct {
	Models  []string
	Plugins []string
	Flags   map[string]any
}
