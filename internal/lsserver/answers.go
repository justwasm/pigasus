// Package lsserver — answers.go: the reference's own answer shapes.
//
// The clone's `LanguageServerService` answers the web UI's calls with
// the clone's own values — the clone's account, the clone's
// conversations, the clone's capability set. The reference answers
// the same calls with the reference's values, and the language server
// is the reference's replica: its answers are the reference's.
//
// The shapes below were captured from the reference binary in both of
// its modes (out/langserver/answers.json).
//
// Evidence grade: 一手.
package lsserver

import (
	"encoding/json"
	"fmt"
	"github.com/justwasm/pigasus/endpoints"
	"os"
	"path/filepath"
	"strings"
)

// AnswerContext is what the reference's answers are built from.
type AnswerContext struct {
	// Flags are the process's flags.
	Flags *Flags
	// Layout is the resolved on-disk layout.
	Layout Layout
	// InstanceID is the process's instance id.
	InstanceID string
	// Mocked is true when the process runs with the mocked data.
	Mocked bool
}

// serverConfiguration is the reference's GetServerConfiguration answer.
//
// The reference's own answer, standalone:
//
//	{"config":{"sidecars":{"enabled":true,"allowAll":true},"standalone":true,
//	 "appDataDir":"antigravity-ide","maxNumTrackedWorkspaces":10,
//	 "configDir":"config","sandboxAvailable":true}}
//
// and in the IDE's hub mode:
//
//	{"config":{"sidecars":{"enabled":true},"standalone":true,
//	 "appDataDir":"antigravity-ide","antigravityHub":true,
//	 "maxNumTrackedWorkspaces":10,"subclientType":"hub",
//	 "configDir":"config","sandboxAvailable":true}}
//
// Three things vary with the mode: the sidecar block, the hub marker and
// the subclient type.
//
// The key order is the reference's — it is the proto's field order,
// and a byte-for-byte answer needs it.
func (a AnswerContext) serverConfiguration() string {
	f := a.Flags
	var b strings.Builder
	b.WriteString(`{"config":{`)
	b.WriteString(`"sidecars":`)
	b.WriteString(a.sidecars())
	b.WriteString(`,"standalone":`)
	b.WriteString(fmt.Sprintf("%t", f.Standalone))
	b.WriteString(`,"appDataDir":`)
	b.WriteString(jsonString(f.AppDataDir))
	if a.antigravityHub() {
		b.WriteString(`,"antigravityHub":true`)
	}
	b.WriteString(`,"maxNumTrackedWorkspaces":`)
	b.WriteString(fmt.Sprintf("%d", f.MaxNumTrackedWorkspaces))
	if f.SubclientType != "" {
		b.WriteString(`,"subclientType":`)
		b.WriteString(jsonString(f.SubclientType))
	}
	b.WriteString(`,"configDir":`)
	b.WriteString(jsonString(f.ConfigDir))
	b.WriteString(`,"sandboxAvailable":true}}`)
	return b.String()
}

// sidecars is the reference's sidecar block.
//
// 【一手】 the block has three shapes, and which one it takes does not
// follow from one flag:
//
//	no subclient type, no --enable_sidecars → {"enabled":true,"allowAll":true}
//	--enable_sidecars                        → {"enabled":true}
//	a subclient type, no --enable_sidecars   → {}
func (a AnswerContext) sidecars() string {
	if a.Flags.EnableSidecars {
		return `{"enabled":true}`
	}
	if a.Flags.SubclientType != "" {
		return `{}`
	}
	return `{"enabled":true,"allowAll":true}`
}

// antigravityHub reports whether the reference answers as the
// Antigravity hub.
//
// 【一手】 the switch is the IDE's name: the reference adds
// `antigravityHub` and reports a "-v2" instance id only when
// `--override_ide_name` is exactly "antigravity" *and* the subclient
// type is "hub". The user agent name is not the switch, the name is
// case-sensitive, and the standalone mode ignores it.
func (a AnswerContext) antigravityHub() bool {
	return a.Flags.SubclientType == "hub" && a.Flags.OverrideIDEName == "antigravity"
}

// remoteControlInfo is the reference's GetRemoteControlInfo answer.
//
// The reference's instance id carries the remote-control version:
// "-v1" for the standalone server, the process's own id with "-v2"
// behind it for the IDE's hub.
func (a AnswerContext) remoteControlInfo() string {
	return `{"instanceId":` + jsonString(a.instanceID()) +
		`,"companionOrigin":` + jsonString(endpoints.Current().CompanionOrigin) + `}`
}

// instanceID is the instance id the reference reports.
//
// 【一手】 the cli and the hub subclients report an instance id of
// their own; the standalone server and the sdk subclient report none.
func (a AnswerContext) instanceID() string {
	switch a.Flags.SubclientType {
	case "cli", "hub":
	default:
		return "-v1"
	}
	if a.antigravityHub() {
		return a.InstanceID + "-v2"
	}
	return a.InstanceID + "-v1"
}

// localUserInfo is the reference's GetLocalUserInfo answer.
//
// The reference names the user and the home directory; the clone adds
// a machine type of its own, which the reference's message does not
// carry.
func (a AnswerContext) localUserInfo() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "antigravity"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return `{"username":` + jsonString(user) + `,"homeDirUri":"file://` + filepath.ToSlash(home) + `"}`
}

// defaultProjectDir is the reference's GetDefaultProjectDir answer.
//
// The reference's default project directory is the user's Documents
// folder, not the home directory.
func (a AnswerContext) defaultProjectDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return `{"defaultProjectDirUri":"file://` + filepath.ToSlash(filepath.Join(home, "Documents")) + `"}`
}

// standaloneDir is the reference's GetStandaloneDir answer.
//
// The reference's scratch directory lives under the application data
// directory — the same root the `-gemini_dir` flag moves.
func (a AnswerContext) standaloneDir() string {
	return `{"standaloneDir":` + jsonString(filepath.Join(a.Layout.AppDataDir, "scratch")) + `}`
}

// jsonString renders a Go string as a JSON string.
func jsonString(s string) string {
	body, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(body)
}
