// Package cortex — workspace.go: workspace resolution and cleanup operations.
//
// pclntab entries:
//   - ResolveWorkspaceURIs         0x7fd72e0
//   - CleanupMappedWorkspaces      0x7fd73e0
//   - CleanUpWorkspaces            0x7fd74e0
//   - CleanUpWorktreesInArtifacts  0x7fd7bc0
package cortex

// ResolveWorkspaceURIs resolves workspace URIs. v1.3.5 binary addr
// 0x7fd72e0, name=ResolveWorkspaceURIs, signature=(cm
// *CascadeManager) ([]string, error).
func (cm *CascadeManager) ResolveWorkspaceURIs() ([]string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return append([]string(nil), cm.workspaceURIs...), nil
}

// CleanupMappedWorkspaces cleans up mapped workspaces. v1.3.5 binary
// addr 0x7fd73e0, name=CleanupMappedWorkspaces, signature=(cm
// *CascadeManager) error.
func (cm *CascadeManager) CleanupMappedWorkspaces() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.workspaceURIs = nil
	return nil
}

// CleanUpWorkspaces cleans up workspaces. v1.3.5 binary addr
// 0x7fd74e0, name=CleanUpWorkspaces, signature=(cm *CascadeManager)
// error.
func (cm *CascadeManager) CleanUpWorkspaces() error {
	return cm.CleanupMappedWorkspaces()
}

// CleanUpWorktreesInArtifacts cleans up worktrees in artifacts. v1.3.5
// binary addr 0x7fd7bc0, name=CleanUpWorktreesInArtifacts,
// signature=(cm *CascadeManager) error.
func (cm *CascadeManager) CleanUpWorktreesInArtifacts() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return nil
}
