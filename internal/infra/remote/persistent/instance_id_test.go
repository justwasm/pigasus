// instance_id_test.go — the instance id helpers.
package persistent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// freshInstallationUUID clears the memoised installation uuid so a
// test starts from a clean slate, and restores that afterwards.
//
// The memoisation is process-global on purpose — a running daemon must
// never see the id change under it — but that makes the id a piece of
// shared state between tests: one that binds the id to its own
// temporary configuration directory has to ask for the reset.
func freshInstallationUUID(t *testing.T) {
	t.Helper()
	resetInstallationUUIDForTest()
	t.Cleanup(resetInstallationUUIDForTest)
}

// TestInstallationUUIDIsStableWithinAProcess pins that the uuid a
// process reports never changes under it.
//
// It is the invariant the printed link depends on: the daemon
// registers one id with the cloud, and the link it prints names the
// *same* id. A configuration directory that cannot be written would
// otherwise mint a fresh uuid on every call, and the two would
// differ.
//
// 【一手】 2026-10-29, Linux container: the configuration directory
// was mounted read-only. The daemon registered
// "3da6eeed-0f99-4998-91cd-a7617327d4ac-v2" while the link it
// printed named "7327d0cc-f915-45a4-884a-3b7592f1d1f6-v2".
func TestInstallationUUIDIsStableWithinAProcess(t *testing.T) {
	// A configuration directory that cannot be written: the uuid
	// cannot be persisted, so every call would have to mint one.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	t.Setenv("XDG_CONFIG_HOME", dir)
	freshInstallationUUID(t)

	first := InstallationUUID()
	for i := 0; i < 5; i++ {
		if got := InstallationUUID(); got != first {
			t.Fatalf("InstallationUUID() = %q on call %d, want the stable %q", got, i+2, first)
		}
	}
	if got := InstanceID(); got != first+InstanceIDSuffix {
		t.Errorf("InstanceID() = %q, want %q", got, first+InstanceIDSuffix)
	}
}

// TestInstallationUUIDIsPersisted pins that the uuid survives a
// restart: the dashboard keeps one record per installation, not one
// per process.
func TestInstallationUUIDIsPersisted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	first := loadOrMintInstallationUUID()
	path := InstanceIDPath()
	if path == "" {
		t.Fatalf("InstanceIDPath() is empty")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the uuid was not persisted at %s: %v", path, err)
	}
	second := loadOrMintInstallationUUID()
	if first != second {
		t.Errorf("the persisted uuid changed: %q then %q", first, second)
	}
	// The file lives in the configuration directory, one level under
	// the XDG root the package names.
	if filepath.Dir(path) != filepath.Join(dir, "openagy") {
		t.Errorf("the uuid was persisted at %s, want it under %s", path, filepath.Join(dir, "openagy"))
	}
}

// TestEnsureInstanceIDPersistsTheID pins that the id is written to
// the file, so a *separate* process — the launchd daemon
// `remote-control start` bootstraps — reads the same one.
//
// The starter and the daemon are different processes, and the link the
// starter prints has to name the id the daemon registered. Resolving
// the id without persisting it would leave the two to each mint one.
func TestEnsureInstanceIDPersistsTheID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	freshInstallationUUID(t)

	id, persisted := EnsureInstanceID()
	if !persisted {
		t.Fatalf("EnsureInstanceID() did not persist the id")
	}
	b, err := os.ReadFile(InstanceIDPath())
	if err != nil {
		t.Fatalf("the id was not written: %v", err)
	}
	// The file holds the **uuid**, not the full instance id: the id
	// adds the "-v2" suffix, and the uuid is what the reader expects.
	// A "-v2" in the file would be rejected as a uuid and a fresh one
	// would be minted on the next start.
	got := strings.TrimSpace(string(b))
	if got != InstallationUUID() {
		t.Errorf("the file holds %q, want the uuid %q", got, InstallationUUID())
	}
	if !isUUID(got) {
		t.Errorf("the file holds %q, which is not a uuid", got)
	}
	if want := got + InstanceIDSuffix; id != want {
		t.Errorf("EnsureInstanceID() = %q, want %q", id, want)
	}
	// A second call reads the same id back — it does not mint a new one.
	again, persisted := EnsureInstanceID()
	if !persisted {
		t.Errorf("EnsureInstanceID() did not persist the id the second time")
	}
	if again != id {
		t.Errorf("EnsureInstanceID() = %q the second time, want %q", again, id)
	}
}

// TestEnsureInstanceIDReportsAnUnwritableDirectory pins the failure
// mode: an id that cannot be persisted lives only in this process, and
// a separate daemon would mint its own. The caller has to be able to
// say so.
func TestEnsureInstanceIDReportsAnUnwritableDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	t.Setenv("XDG_CONFIG_HOME", dir)
	freshInstallationUUID(t)

	if _, persisted := EnsureInstanceID(); persisted {
		t.Errorf("EnsureInstanceID() reported the id as persisted, but the directory is read-only")
	}
}

// TestEnsureInstanceIDTakesTheFileBack pins that the starter owns
// the file: when the file holds a *different* uuid, EnsureInstanceID
// writes its own back.
//
// The daemon reads the file, so the starter and the daemon have to
// agree on the id. A file that changed under the starter would
// otherwise leave the two disagreeing — the daemon registering one id
// and the link naming another.
func TestEnsureInstanceIDTakesTheFileBack(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	freshInstallationUUID(t)

	// The process resolves its uuid — and writes it to the file.
	mine := InstallationUUID()
	path := InstanceIDPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(mine+"\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Now the file changes under the process.
	other := mintUUID()
	if other == mine {
		t.Fatalf("the test's uuid collided")
	}
	if err := os.WriteFile(path, []byte(other+"\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	id, persisted := EnsureInstanceID()
	if !persisted {
		t.Fatalf("EnsureInstanceID() did not persist the id")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := strings.TrimSpace(string(b)); got != mine {
		t.Errorf("the file holds %q, want the starter's uuid %q", got, mine)
	}
	if want := mine + InstanceIDSuffix; id != want {
		t.Errorf("EnsureInstanceID() = %q, want %q", id, want)
	}
}

// TestMintUUID_Shape pins the v4 shape: the version nibble is 4 and
// the variant nibble is one of 8/9/a/b.
func TestMintUUID_Shape(t *testing.T) {
	for i := 0; i < 16; i++ {
		id := mintUUID()
		if len(id) != 36 {
			t.Fatalf("mintUUID() = %q (len %d, want 36)", id, len(id))
		}
		if id[14] != '4' {
			t.Fatalf("mintUUID() = %q, want version nibble 4", id)
		}
		switch id[19] {
		case '8', '9', 'a', 'b':
		default:
			t.Fatalf("mintUUID() = %q, want a RFC 4122 variant nibble", id)
		}
	}
}

// TestIsUUID pins the accepted shapes. The check is deliberately
// strict — the id lands in a file name and in the remote service's
// registration metadata, so anything that is not the canonical
// 8-4-4-4-12 form is rejected.
func TestIsUUID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"6ba7b810-9dad-11d1-80b4-00c04fd430c8", true},
		{"6BA7B810-9DAD-11D1-80B4-00C04FD430C8", true},
		{"00000000-0000-4000-8000-000000000000", true},
		{"", false},
		{"not-a-uuid", false},
		{"6ba7b8109dad11d180b400c04fd430c8", false},              // no dashes
		{"6ba7b810-9dad-11d1-80b4-00c04fd430c", false},           // too short
		{"6ba7b810-9dad-11d1-80b4-00c04fd430c88", false},         // too long
		{"6ba7b810_9dad-11d1-80b4-00c04fd430c8", false},          // wrong separator
		{"6ba7b810-9dad-11d1-80b4-00c04fd430cg", false},          // non-hex
		{"urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8", false}, // prefixed
	}
	for _, c := range cases {
		if got := isUUID(c.in); got != c.want {
			t.Errorf("isUUID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
