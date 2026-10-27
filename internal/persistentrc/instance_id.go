package persistentrc

import (
	"os"
	"path/filepath"
	"strings"

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
func InstallationUUID() string {
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
