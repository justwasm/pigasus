package persistent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// InstanceIDPrefix is the prefix the reference puts in front of the
// installation uuid when it mints the id it registers with the remote
// service.
//
// It is empty: the reference registers the bare "<uuid>-v2".
//
// 【一手】 2026-10-26, live: the reference's own captured registration
// body — the one its traffic capture through a local proxy recorded —
// is
//
//	count=1&ofs=0&req0___data__={"metadata":{"name":"pigasus-test-inst",
//	"version":"2.24.0-v2"},"instanceId":"0d159523-6e02-4362-abab-93e0c69edea4-v2",
//	"supportsPayloadText":true}
//
// and the cloud's own instance registry lists that instance under the
// same id ("0d159523-6e02-4362-abab-93e0c69edea4-v2"). The
// "remote-control-" that appears in the reference's log lines is the
// log tag of its remote-control session, not part of the id:
//
//	[remote-control-0d159523-6e02-4362-abab-93e0c69edea4-v2] Starting V2
//	remote control connection to: https://jetski-webchannel.googleapis.com:443
//
// The clone used to register "remote-control-<uuid>-v2" — the log tag
// mistaken for the id. The id's *shape* is a parity question (the
// reference registers the bare uuid), not the reason a view ever failed
// to open: 【一手】 a prefixed id's registry row went live exactly like a
// bare one — its statusLastUpdatedAt advanced every few seconds for as
// long as its tunnel was up. What the cloud refuses to open is a row it
// cannot find, or one it believes is disconnected, and which row that is
// depends on the *account*: the registry rows are per Google account
// (X-Goog-AuthUser), and the dashboard's "/u/<N>/" path segment selects
// one — the index of the account in the browser's account chooser. The
// bare URL the CLI prints ("/r/<id>") resolves against the browser's
// default account instead, and the cloud then answers "No instance found.
// Try switching to a different account." See
// docs/cycles/rc-cloud-remote-view/deliverable.md §7.4.
const InstanceIDPrefix = ""

// InstanceIDSuffix is the suffix the reference appends to the uuid.
const InstanceIDSuffix = "-v2"

// InstanceID returns the id this installation registers with the remote
// service: "<installation uuid>-v2".
//
// The uuid is persisted, so the id survives restarts. That matters for
// two reasons:
//
//   - The reference does the same. Its id is derived from the
//     installation uuid it keeps in
//     ~/.gemini/antigravity-cli/antigravity_state.pbtxt
//     ("installation_uuid: \"0d159523-…\""), so the reference reuses
//     the very same id every time it starts — the dashboard keeps one
//     record per installation instead of one per process.
//
//   - A fresh id per start means a fresh record per start. The
//     dashboard would fill up with dead instances and every restart
//     would invalidate the "Connect" link the user copied.
//
// The clone keeps its own uuid in its own configuration directory
// (~/.config/openagy/remote-control-instance-id) rather than in the
// shared Antigravity home: the reference reads its uuid from the
// shared state file, and the clone must not end up registering the
// same instance id as the reference running on the same machine.
func InstanceID() string {
	return InstanceIDPrefix + InstallationUUID() + InstanceIDSuffix
}

// InstallationUUID returns the persisted uuid this installation is
// known by, minting and storing one on the first call.
//
// The value is **memoised for the life of the process**: a read-only
// or missing configuration directory would otherwise mint a fresh
// uuid on every call, so the daemon would register one id and the
// link it prints would name another — the dashboard would answer "No
// instance found" for a link the daemon itself printed.
//
// 【一手】 2026-10-29, Linux container: the configuration directory
// was mounted read-only, so the file could not be written. The daemon
// registered "3da6eeed-0f99-4998-91cd-a7617327d4ac-v2" while the link
// it printed named "7327d0cc-f915-45a4-884a-3b7592f1d1f6-v2" —
// two ids for one daemon.
func InstallationUUID() string {
	installationUUIDOnce.Do(func() {
		installationUUID = loadOrMintInstallationUUID()
	})
	return installationUUID
}

// installationUUIDOnce guards the memoisation of the installation
// uuid; installationUUID holds the memoised value.
var (
	installationUUIDOnce sync.Once
	installationUUID     string
)

// resetInstallationUUIDForTest clears the memoised installation uuid
// so a test can start from a clean slate.
//
// The memoisation is process-global by design — the id must not change
// under a running daemon — so a test that wants a *fresh* id (one
// bound to its own temporary configuration directory) has to ask for
// the reset explicitly.
func resetInstallationUUIDForTest() {
	installationUUIDOnce = sync.Once{}
	installationUUID = ""
}

// loadOrMintInstallationUUID reads the persisted uuid, or mints one
// and tries to persist it.
func loadOrMintInstallationUUID() string {
	path := InstanceIDPath()
	if path == "" {
		// No home to persist into: fall back to a per-process uuid.
		return mintUUID()
	}
	if b, err := os.ReadFile(path); err == nil {
		if u := strings.TrimSpace(string(b)); isUUID(u) {
			return u
		}
	}
	u := mintUUID()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err == nil {
		_ = os.WriteFile(path, []byte(u+"\n"), 0600)
	}
	return u
}

// EnsureInstanceID resolves the installation's instance id **and
// persists it**, so that a *separate* process — the launchd daemon
// `remote-control start` bootstraps — reads the same id.
//
// The persistent daemon is a different process from the one that
// starts it, and the id has to be the same in both: the daemon
// registers it with the cloud, and the link the starter prints has to
// name it. Persisting it *before* the daemon starts removes the race —
// without it, the daemon and the starter could each mint one.
//
// The file holds the **uuid**, not the full instance id: the id adds
// the "-v2" suffix, and the uuid is what
// loadOrMintInstallationUUID reads back.
//
// The second return value reports whether the file now holds the id.
// When it is false the id lives only in this process, and a separate
// daemon would mint its own.
func EnsureInstanceID() (string, bool) {
	uuid := InstallationUUID()
	id := InstanceIDPrefix + uuid + InstanceIDSuffix
	path := InstanceIDPath()
	if path == "" {
		return id, false
	}
	if b, err := os.ReadFile(path); err == nil {
		if strings.TrimSpace(string(b)) == uuid {
			return id, true
		}
		// The file holds a *different* uuid — it changed under this
		// process. The starter owns the daemon it is about to launch,
		// so it writes its own uuid back: the daemon reads the file,
		// and the two have to agree.
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return id, false
	}
	if err := os.WriteFile(path, []byte(uuid+"\n"), 0600); err != nil {
		return id, false
	}
	return id, true
}

// InstanceIDPath returns the file the installation uuid is kept in.
func InstanceIDPath() string {
	dir := configDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "remote-control-instance-id")
}

// configDir returns the clone's own configuration directory.
func configDir() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "openagy")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "openagy")
	}
	return ""
}

// mintUUID returns a random RFC 4122 version 4 uuid.
func mintUUID() string {
	u, err := uuid.NewRandom()
	if err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	return u.String()
}

// isUUID reports whether s has the shape of a uuid.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	return uuid.Validate(s) == nil
}
