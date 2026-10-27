package lsserver

import (
	"encoding/json"
	"github.com/justwasm/pigasus/endpoints"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestServerConfigurationIsTheReferenceAnswer checks the reference's
// own answer, byte for byte, in the modes that matter.
//
// 【一手】 captured from the reference binary in every combination
// (out/langserver/answers.json).
func TestServerConfigurationIsTheReferenceAnswer(t *testing.T) {
	base := func() *Flags {
		return &Flags{Standalone: true, AppDataDir: "antigravity-ide", MaxNumTrackedWorkspaces: 10, ConfigDir: "config"}
	}
	cases := []struct {
		name  string
		flags *Flags
		want  string
	}{
		{
			name:  "the standalone server",
			flags: base(),
			want:  `{"config":{"sidecars":{"enabled":true,"allowAll":true},"standalone":true,"appDataDir":"antigravity-ide","maxNumTrackedWorkspaces":10,"configDir":"config","sandboxAvailable":true}}`,
		},
		{
			name:  "the standalone server with the sidecar flag",
			flags: &Flags{Standalone: true, AppDataDir: "antigravity-ide", MaxNumTrackedWorkspaces: 10, ConfigDir: "config", EnableSidecars: true},
			want:  `{"config":{"sidecars":{"enabled":true},"standalone":true,"appDataDir":"antigravity-ide","maxNumTrackedWorkspaces":10,"configDir":"config","sandboxAvailable":true}}`,
		},
		{
			name:  "a subclient type empties the sidecar block",
			flags: &Flags{Standalone: true, AppDataDir: "antigravity-ide", MaxNumTrackedWorkspaces: 10, ConfigDir: "config", SubclientType: "sdk"},
			want:  `{"config":{"sidecars":{},"standalone":true,"appDataDir":"antigravity-ide","maxNumTrackedWorkspaces":10,"subclientType":"sdk","configDir":"config","sandboxAvailable":true}}`,
		},
		{
			name:  "the hub is not the Antigravity hub yet",
			flags: &Flags{Standalone: true, AppDataDir: "antigravity-ide", MaxNumTrackedWorkspaces: 10, ConfigDir: "config", SubclientType: "hub"},
			want:  `{"config":{"sidecars":{},"standalone":true,"appDataDir":"antigravity-ide","maxNumTrackedWorkspaces":10,"subclientType":"hub","configDir":"config","sandboxAvailable":true}}`,
		},
		{
			name:  "the IDE's hub",
			flags: &Flags{Standalone: true, AppDataDir: "antigravity-ide", MaxNumTrackedWorkspaces: 10, ConfigDir: "config", SubclientType: "hub", OverrideIDEName: "antigravity", EnableSidecars: true},
			want:  `{"config":{"sidecars":{"enabled":true},"standalone":true,"appDataDir":"antigravity-ide","antigravityHub":true,"maxNumTrackedWorkspaces":10,"subclientType":"hub","configDir":"config","sandboxAvailable":true}}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ServerConfiguration(c.flags)
			if got != c.want {
				t.Errorf("answer =\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

// TestTheAntigravityHubNeedsTheIDESName pins the switch.
//
// 【一手】 the user agent name is not the switch, and neither is the
// capitalised IDE name.
func TestTheAntigravityHubNeedsTheIDESName(t *testing.T) {
	base := func() *Flags {
		return &Flags{Standalone: true, AppDataDir: "antigravity-ide", MaxNumTrackedWorkspaces: 10, ConfigDir: "config", SubclientType: "hub", EnableSidecars: true}
	}
	flags := base()
	flags.OverrideIDEName = "Antigravity"
	if got := ServerConfiguration(flags); strings.Contains(got, "antigravityHub") {
		t.Errorf("the capitalised IDE name made the hub: %s", got)
	}
	flags = base()
	flags.OverrideUserAgentName = "antigravity"
	if got := ServerConfiguration(flags); strings.Contains(got, "antigravityHub") {
		t.Errorf("the user agent name made the hub: %s", got)
	}
}

// TestServerConfigurationSidecarsFollowTheFlag checks the one thing
// the hidden flag changes: the sidecar block loses `allowAll`.
func TestServerConfigurationSidecarsFollowTheFlag(t *testing.T) {
	flags := &Flags{Standalone: true, AppDataDir: "antigravity-ide", MaxNumTrackedWorkspaces: 10, ConfigDir: "config", EnableSidecars: true}
	got := ServerConfiguration(flags)
	want := `{"config":{"sidecars":{"enabled":true},"standalone":true,"appDataDir":"antigravity-ide","maxNumTrackedWorkspaces":10,"configDir":"config","sandboxAvailable":true}}`
	if got != want {
		t.Errorf("answer =\n%s\nwant\n%s", got, want)
	}
}

// TestAnswersAreTheReferenceShapes checks the answers the reference
// gives for the methods the clone does not answer itself.
func TestAnswersAreTheReferenceShapes(t *testing.T) {
	layout := Layout{GeminiDir: "/tmp/ls", ConfigDir: "/tmp/ls/config", AppDataDir: "/tmp/ls/antigravity-ide"}
	flags := &Flags{Standalone: true, AppDataDir: "antigravity-ide", MaxNumTrackedWorkspaces: 10, ConfigDir: "config"}
	d := NewDispatcher(nil, flags, layout, true)
	cases := map[string]string{
		"GetStatus":            `{}`,
		"GetCapabilities":      `{"supportsHookResultProtoBytes":true}`,
		"GetRemoteControlInfo": `{"instanceId":"-v1","companionOrigin":` + jsonString(endpoints.Current().CompanionOrigin) + `}`,
		"GetStandaloneDir":     `{"standaloneDir":"/tmp/ls/antigravity-ide/scratch"}`,
		"GetAuthStatus":        `{"authResult":{"hasValidAuth":true}}`,
		"GetMendelFlags":       `{"experimentConfig":{}}`,
	}
	for method, want := range cases {
		got, ok := d.answer(method)
		if !ok {
			t.Errorf("%s is not answered", method)
			continue
		}
		if got != want {
			t.Errorf("%s =\n%s\nwant\n%s", method, got, want)
		}
	}
}

// TestTheInstanceIDFollowsTheReference pins the instance id the
// reference reports.
//
// 【一手】 the cli and the hub subclients report an instance id of
// their own; the standalone server and the sdk subclient report none.
// The Antigravity hub reports the second version.
func TestTheInstanceIDFollowsTheReference(t *testing.T) {
	cases := []struct {
		name  string
		flags *Flags
		want  string
	}{
		{
			name:  "the standalone server has none",
			flags: &Flags{SubclientType: ""},
			want:  "-v1",
		},
		{
			name:  "the sdk subclient has none",
			flags: &Flags{SubclientType: "sdk"},
			want:  "-v1",
		},
		{
			name:  "the cli subclient has one",
			flags: &Flags{SubclientType: "cli"},
			want:  "the-instance-v1",
		},
		{
			name:  "the hub has one",
			flags: &Flags{SubclientType: "hub"},
			want:  "the-instance-v1",
		},
		{
			name:  "the Antigravity hub has the second version",
			flags: &Flags{SubclientType: "hub", OverrideIDEName: "antigravity"},
			want:  "the-instance-v2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := AnswerContext{Flags: c.flags, InstanceID: "the-instance"}
			if got := ctx.instanceID(); got != c.want {
				t.Errorf("instance id = %q, want %q", got, c.want)
			}
		})
	}
}

// TestLocalUserInfoAndProjectDir checks the two answers that name the
// user's own directories.
func TestLocalUserInfoAndProjectDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	layout := Layout{AppDataDir: "/tmp/ls/antigravity-ide"}
	flags := &Flags{}
	ctx := AnswerContext{Flags: flags, Layout: layout}
	user := ctx.localUserInfo()
	if !strings.HasSuffix(user, `,"homeDirUri":"file://`+filepath.ToSlash(home)+`"}`) {
		t.Errorf("local user info = %s", user)
	}
	dir := ctx.defaultProjectDir()
	want := `{"defaultProjectDirUri":"file://` + filepath.ToSlash(filepath.Join(home, "Documents")) + `"}`
	if dir != want {
		t.Errorf("default project dir = %s, want %s", dir, want)
	}
}

// TestMockedCatalogueIsValidJSON checks that the mocked answers parse
// as the JSON the reference answers with.
func TestMockedCatalogueIsValidJSON(t *testing.T) {
	for _, answer := range []string{
		mockedUserStatus(),
		mockedCascadeModelConfigData(),
		`{"response":{}}`,
		`{}`,
	} {
		var out map[string]any
		if err := json.Unmarshal([]byte(answer), &out); err != nil {
			t.Errorf("the answer does not parse: %v (%s)", err, answer)
		}
	}
}
