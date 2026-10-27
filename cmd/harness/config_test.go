// config_test.go — the client's configuration, resolved.
//
// The `HarnessConfig` the SDK sends is authoritative for a session: the
// workspace, the agent home, the prompt, the policy, the skills and the
// tool surface all come from it. Only the two things an operator can set
// on a spawned binary — the workspace and the agent home — override it.
package main

import (
	"testing"

	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

func TestConfigTheFirstWorkspaceIsTheWorkingDirectory(t *testing.T) {
	first := "/tmp/one"
	second := "/tmp/two"
	cfg := pb.HarnessConfig_builder{
		Workspaces: []*pb.Workspace{
			pb.Workspace_builder{
				FilesystemWorkspace: pb.FilesystemWorkspace_builder{Directory: &first}.Build(),
			}.Build(),
			pb.Workspace_builder{
				FilesystemWorkspace: pb.FilesystemWorkspace_builder{Directory: &second}.Build(),
			}.Build(),
		},
	}.Build()
	settings := resolveSettings(cfg, &pb.InputConfig{}, options{})
	if settings.CWD != first {
		t.Errorf("the working directory = %q, want %q", settings.CWD, first)
	}
	if len(settings.Workspaces) != 2 || settings.Workspaces[0] != first {
		t.Errorf("the workspaces = %v, want the client's own", settings.Workspaces)
	}
}

func TestConfigTheCommandLineOverridesTheWorkspace(t *testing.T) {
	dir := "/tmp/client"
	cfg := pb.HarnessConfig_builder{
		Workspaces: []*pb.Workspace{
			pb.Workspace_builder{
				FilesystemWorkspace: pb.FilesystemWorkspace_builder{Directory: &dir}.Build(),
			}.Build(),
		},
	}.Build()
	opts := options{workspace: "/tmp/operator", workspaceExplicit: true}
	settings := resolveSettings(cfg, &pb.InputConfig{}, opts)
	if settings.CWD != "/tmp/operator" {
		t.Errorf("the working directory = %q, want the operator's", settings.CWD)
	}
}

func TestConfigTheBehaviorSelectsThePrompt(t *testing.T) {
	behavior := pb.AgentBehavior_AGENT_BEHAVIOR_INTERACTIVE
	cfg := pb.HarnessConfig_builder{AgentBehavior: &behavior}.Build()
	settings := resolveSettings(cfg, &pb.InputConfig{}, options{})
	if !settings.Behavior.Interactive() {
		t.Errorf("the behaviour = %q, want the interactive one", settings.Behavior)
	}
}

func TestConfigTheAppDataDirDefaultsToTheSdkOne(t *testing.T) {
	cfg := &pb.HarnessConfig{}
	settings := resolveSettings(cfg, &pb.InputConfig{}, options{})
	if settings.AppDataDir == "" {
		t.Error("the application-data directory is empty")
	}
	dir := "/tmp/appdata"
	named := pb.HarnessConfig_builder{AppDataDir: &dir}.Build()
	settings = resolveSettings(named, &pb.InputConfig{}, options{})
	if settings.AppDataDir != dir {
		t.Errorf("the application-data directory = %q, want %q", settings.AppDataDir, dir)
	}
}

func TestConfigTheSkillsPathsComeFromTheClient(t *testing.T) {
	one := "/tmp/skills/one"
	two := "/tmp/skills/two"
	cfg := pb.HarnessConfig_builder{SkillsPaths: []string{one, two}}.Build()
	settings := resolveSettings(cfg, &pb.InputConfig{}, options{})
	if len(settings.SkillsPaths) != 2 || settings.SkillsPaths[0] != one {
		t.Errorf("the skill paths = %v, want the client's own", settings.SkillsPaths)
	}
}

func TestConfigTheConversationIDIsTheCascadeID(t *testing.T) {
	cascade := "cascade-conformance-000000000000000"
	cfg := pb.HarnessConfig_builder{CascadeId: &cascade}.Build()
	settings := resolveSettings(cfg, &pb.InputConfig{}, options{})
	if settings.ConversationID != cascade {
		t.Errorf("the conversation id = %q, want %q", settings.ConversationID, cascade)
	}
}

func TestConfigTheClientInfoNamesTheOperatingSystem(t *testing.T) {
	os := "darwin"
	input := pb.InputConfig_builder{
		ClientInfo: pb.ClientInfo_builder{Os: &os}.Build(),
	}.Build()
	settings := resolveSettings(&pb.HarnessConfig{}, input, options{})
	if settings.OSName != "darwin" {
		t.Errorf("the operating system = %q, want darwin", settings.OSName)
	}
}
