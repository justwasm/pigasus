package sidecars

import (
	"os"
	"testing"
)

func TestResolveSidecarTemplates_BuiltinHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	raw := map[string]any{
		"agentPermissions": map[string]any{
			"workspaceUris": []any{
				"file://${HOME}/worktrees",
				"file://${PROJECT}/src",
			},
			"accessGrants": []any{
				"read_file(${HOME}/.config/app)",
			},
		},
	}

	args := map[string]string{
		"PROJECT": "/tmp/myproject",
	}

	resolved := ResolveSidecarTemplates(raw, args)

	perms, ok := resolved["agentPermissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected agentPermissions map, got %T", resolved["agentPermissions"])
	}

	uris := perms["workspaceUris"].([]any)
	wantURI0 := "file://" + home + "/worktrees"
	wantURI1 := "file:///tmp/myproject/src"
	if uris[0] != wantURI0 {
		t.Errorf("uris[0] = %v, want %v", uris[0], wantURI0)
	}
	if uris[1] != wantURI1 {
		t.Errorf("uris[1] = %v, want %v", uris[1], wantURI1)
	}

	grants := perms["accessGrants"].([]any)
	wantGrant0 := "read_file(" + home + "/.config/app)"
	if grants[0] != wantGrant0 {
		t.Errorf("grants[0] = %v, want %v", grants[0], wantGrant0)
	}
}

func TestResolveSidecarTemplates_ExplicitHomePrecedence(t *testing.T) {
	raw := map[string]any{
		"agent_permissions": map[string]any{
			"workspace_uris": []any{
				"file://${HOME}/worktrees",
			},
			"access_grants": []any{
				"read_file(${HOME}/custom)",
			},
		},
	}

	args := map[string]string{
		"HOME": "/custom/override/home",
	}

	resolved := ResolveSidecarTemplates(raw, args)

	perms, ok := resolved["agent_permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected agent_permissions map, got %T", resolved["agent_permissions"])
	}

	uris := perms["workspace_uris"].([]any)
	wantURI := "file:///custom/override/home/worktrees"
	if uris[0] != wantURI {
		t.Errorf("uris[0] = %v, want %v", uris[0], wantURI)
	}

	grants := perms["access_grants"].([]any)
	wantGrant := "read_file(/custom/override/home/custom)"
	if grants[0] != wantGrant {
		t.Errorf("grants[0] = %v, want %v", grants[0], wantGrant)
	}
}
