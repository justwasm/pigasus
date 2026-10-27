// persistentrc_test.go: cycle-11 dry-run + plist tests for the
// persistentrc package.
//
// All tests run with Spec.SkipLaunchd=true so the suite is
// hermetic: nothing touches ~/Library/LaunchAgents/ and no
// launchctl is invoked.  The dry-run flag is global (the
// package's dryRun var); tests set / reset it via SetDryRun.
//
// On non-darwin the build tag below excludes every test that
// exercises the launchd-specific paths; the platform-neutral
// tests (TestValidateName, TestValidateAddr, TestResolveSpec_*
// etc.) live in this file so a single `go test ./internal/persistentrc/`
// covers every platform.
//
// The cycle's per-cycle preview (`go test -race -count=1
// -run TestStatus_RunningDryRun ./internal/persistentrc/`) is
// the canonical smoke test.
package persistentrc

import (
	"context"
	"runtime"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testSetup initialises the dry-run flag and returns a cleanup
// that restores the previous state.  Tests should defer
// testCleanup(t) as the first line.
func testSetup(t *testing.T) {
	t.Helper()
	prev := dryRun
	dryRun = true
	t.Cleanup(func() { dryRun = prev })
}

// tempPlistDir returns a temp directory for the LaunchAgent plist
// + pidfile.  The directory is created with mode 0o755 and
// cleaned up automatically by t.TempDir's cleanup.  The
// directory is also registered as the package's plist override
// so PlistPathFor(name) returns a path under tempdir, not the
// real ~/Library/LaunchAgents.
func tempPlistDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	prevDir := plistDirOverride
	plistDirOverride = d
	t.Cleanup(func() { plistDirOverride = prevDir })
	return d
}

// fillSpec returns a Spec populated with valid defaults for
// tests.  Callers override the fields they care about.
func fillSpec(name string) Spec {
	return Spec{
		Name:       name,
		Addr:       DefaultAddr,
		LogFile:    LogFilePath(name),
		BinaryPath: "/usr/local/bin/wagyu",
		SkipLaunchd: true,
	}
}

// TestValidateName pins the conservative character class the
// package accepts for Spec.Name.  Letters / digits / '.' / '-' /
// '_' are the launchd-label-safe subset; anything else is
// refused with *ErrInvalidName.
func TestValidateName(t *testing.T) {
	tests := []struct {
		in    string
		valid bool
	}{
		{"foo", true},
		{"foo-bar", true},
		{"foo.bar", true},
		{"foo_bar", true},
		{"Foo123", true},
		{"a", true},
		{"", false},
		{"foo bar", false},
		{"foo/bar", false},
		{"foo:bar", false},
		{"foo=bar", false},
		{strings.Repeat("a", 201), false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			err := ValidateName(tc.in)
			if tc.valid && err != nil {
				t.Errorf("ValidateName(%q) = %v, want nil", tc.in, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("ValidateName(%q) = nil, want error", tc.in)
			}
		})
	}
}

// TestValidateAddr pins the address-validation rules.  Empty /
// malformed / loopback / link-local addresses are refused.
func TestValidateAddr(t *testing.T) {
	tests := []struct {
		in    string
		valid bool
		why   string
	}{
		{"0.0.0.0:8787", true, ""},
		{"0.0.0.0:1", true, ""},
		{"192.168.1.10:8080", true, ""},
		{"[::]:8787", true, ""},
		{"example.com:8787", true, ""},
		{"127.0.0.1:8787", false, "loopback"},
		{"localhost:8787", false, "loopback"},
		{"[::1]:8787", false, "loopback"},
		{"169.254.1.1:8787", false, "link-local"},
		{"", false, "empty"},
		{"no-port", false, "missing port"},
		{":8787", false, "missing host"},
		{"0.0.0.0:0", false, "port range"},
		{"0.0.0.0:99999", false, "port range"},
		{"0.0.0.0:-1", false, "port range"},
		{"0.0.0.0:abc", false, "port range"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			err := ValidateAddr(tc.in)
			if tc.valid && err != nil {
				t.Errorf("ValidateAddr(%q) = %v, want nil", tc.in, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("ValidateAddr(%q) = nil, want error", tc.in)
			}
		})
	}
}

// TestResolveSpec_Defaults pins the default-fill behaviour:
// Addr → DefaultAddr, LogFile → LogFilePath(Name), Name/Addr
// validated.  Returns a copy; the input is not mutated.
func TestResolveSpec_Defaults(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	spec := fillSpec("machine1")
	spec.Addr = ""
	spec.LogFile = ""
	out, err := ResolveSpec(spec)
	if err != nil {
		t.Fatalf("ResolveSpec: %v", err)
	}
	if out.Addr != DefaultAddr {
		t.Errorf("Addr default: got %q, want %q", out.Addr, DefaultAddr)
	}
	if out.LogFile == "" {
		t.Errorf("LogFile default: got empty, want populated")
	}
	if !strings.HasSuffix(out.LogFile, "machine1.log") {
		t.Errorf("LogFile default: %q does not end with machine1.log", out.LogFile)
	}
	// Input not mutated.
	if spec.Addr != "" || spec.LogFile != "" {
		t.Errorf("input mutated: addr=%q log=%q", spec.Addr, spec.LogFile)
	}
}

// TestResolveSpec_InvalidName confirms an empty / bad name is
// rejected before any defaults are applied.
func TestResolveSpec_InvalidName(t *testing.T) {
	_, err := ResolveSpec(Spec{Name: "bad name", Addr: DefaultAddr, BinaryPath: "/x"})
	if err == nil {
		t.Fatalf("ResolveSpec with bad name: got nil error")
	}
	var inv *ErrInvalidName
	if !errors.As(err, &inv) {
		t.Errorf("ResolveSpec with bad name: got %T, want *ErrInvalidName", err)
	}
}

// TestResolveSpec_InvalidAddr confirms a loopback Addr is
// rejected.
func TestResolveSpec_InvalidAddr(t *testing.T) {
	_, err := ResolveSpec(Spec{Name: "machine1", Addr: "127.0.0.1:8787", BinaryPath: "/x"})
	if err == nil {
		t.Fatalf("ResolveSpec with loopback: got nil error")
	}
	var inv *ErrInvalidAddr
	if !errors.As(err, &inv) {
		t.Errorf("ResolveSpec with loopback: got %T, want *ErrInvalidAddr", err)
	}
}

// TestBuildPlist_RoundTrip verifies that the plist buildPlist
// emits parses back through parsePlistProgramArgsFromBytes and
// surfaces the same ProgramArguments we put in.
func TestBuildPlist_RoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("plist is macOS only")
	}
	spec := fillSpec("machine1")
	spec.Addr = "192.168.1.42:9000"
	spec.LogFile = "/tmp/logfile1.log"
	out := buildPlist(spec)
	if out == "" {
		t.Fatalf("buildPlist: empty output")
	}
	for _, want := range []string{
		"<plist",
		"<key>Label</key>",
		"<string>" + Label(spec.Name) + "</string>",
		"<key>ProgramArguments</key>",
		"<string>" + spec.BinaryPath + "</string>",
		"<string>remote-control-foreground</string>",
		"<string>--addr</string>",
		"<string>" + spec.Addr + "</string>",
		"<string>--name</string>",
		"<string>" + spec.Name + "</string>",
		"<string>--log-file</string>",
		"<string>" + spec.LogFile + "</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"<key>ProcessType</key>",
		"<string>Background</string>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q\n--- plist ---\n%s\n--- end ---", want, out)
		}
	}
	args, err := parsePlistProgramArgsFromBytes([]byte(out))
	if err != nil {
		t.Fatalf("parsePlistProgramArgsFromBytes: %v", err)
	}
	want := []string{
		spec.BinaryPath,
		"remote-control-foreground",
		"--addr", spec.Addr,
		"--name", spec.Name,
		"--log-file", spec.LogFile,
	}
	if len(args) != len(want) {
		t.Fatalf("parsed args: got %d, want %d (%v vs %v)", len(args), len(want), args, want)
	}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d]: got %q, want %q", i, args[i], w)
		}
	}
}

// TestBuildPlist_SessionFlag verifies that the --session
// variant emits a plist WITHOUT RunAtLoad / KeepAlive — the
// daemon is one-shot, not auto-restarted.  Everything else
// (Label, ProgramArguments, ProcessType, StandardOutPath) is
// identical to the boot-persistent variant.
func TestBuildPlist_SessionFlag(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("plist is macOS only")
	}
	spec := fillSpec("machine1")
	spec.Session = true
	out := buildPlist(spec)
	for _, want := range []string{
		"<key>Label</key>",
		"<key>ProgramArguments</key>",
		"<key>ProcessType</key>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist (session) missing %q", want)
		}
	}
	for _, absent := range []string{
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
	} {
		if strings.Contains(out, absent) {
			t.Errorf("plist (session) unexpectedly contains %q", absent)
		}
	}
}

// TestBuildPlist_DesktopFlag verifies that the desktop switch
// reaches the daemon through the plist's ProgramArguments: the
// foreground subcommand reads `--desktop` and turns it into the
// hub's Mendel identity.
//
// Without the switch the plist is byte-identical to the reference's
// shape — the flag is appended, never substituted.
func TestBuildPlist_DesktopFlag(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("plist is macOS only")
	}
	spec := fillSpec("machine1")
	spec.Desktop = true
	out := buildPlist(spec)
	if !strings.Contains(out, "<string>--desktop</string>") {
		t.Errorf("plist (desktop) missing --desktop\n--- plist ---\n%s\n--- end ---", out)
	}
	args, err := parsePlistProgramArgsFromBytes([]byte(out))
	if err != nil {
		t.Fatalf("parsePlistProgramArgsFromBytes: %v", err)
	}
	if len(args) == 0 || args[len(args)-1] != "--desktop" {
		t.Errorf("parsed args = %v; want the last one to be --desktop", args)
	}

	// The default plist must NOT carry the switch.
	plain := buildPlist(fillSpec("machine1"))
	if strings.Contains(plain, "--desktop") {
		t.Errorf("plist (default) unexpectedly carries --desktop\n--- plist ---\n%s\n--- end ---", plain)
	}
	plainArgs, err := parsePlistProgramArgsFromBytes([]byte(plain))
	if err != nil {
		t.Fatalf("parsePlistProgramArgsFromBytes (default): %v", err)
	}
	for _, a := range plainArgs {
		if a == "--desktop" {
			t.Errorf("default parsed args = %v; want no --desktop", plainArgs)
		}
	}
}

// TestBuildOfficialPlist_DesktopFlag: the official daemon's plist
// (the one `remote-control start` writes on macOS) hands the switch
// to `remote-control serve`.
func TestBuildOfficialPlist_DesktopFlag(t *testing.T) {
	plain := BuildOfficialPlist("/usr/local/bin/pigasus", false, false)
	if strings.Contains(plain, "--desktop") {
		t.Errorf("official plist (default) unexpectedly carries --desktop")
	}
	if !strings.Contains(plain, "exec '/usr/local/bin/pigasus' 'remote-control' 'serve'") {
		t.Errorf("official plist lost its command line\n%s", plain)
	}
	desktop := BuildOfficialPlist("/usr/local/bin/pigasus", false, true)
	if !strings.Contains(desktop, "exec '/usr/local/bin/pigasus' 'remote-control' 'serve' '--desktop'") {
		t.Errorf("official plist (desktop) did not carry the switch\n%s", desktop)
	}
}

// TestStartStop_DryRun exercises the Start → Status → Stop
// lifecycle with SkipLaunchd=true.  No launchctl is invoked;
// the dry-run pidfile + plist are the source of truth.
func TestStartStop_DryRun(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("persistent remote-control daemon is macOS only")
	}
	testSetup(t)
	dir := tempPlistDir(t)
	spec := fillSpec("machine1")
	spec.PlistDir = dir

	if err := Start(context.Background(), spec); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Plist and pidfile must exist.
	plistP := filepath.Join(dir, Label(spec.Name)+".plist")
	pidP := plistP + ".pid"
	if _, err := os.Stat(plistP); err != nil {
		t.Errorf("plist not written: %v", err)
	}
	if _, err := os.Stat(pidP); err != nil {
		t.Errorf("pidfile not written: %v", err)
	}

	// Status should report running=true with a fake PID.
	rep, err := Status(context.Background(), spec.Name)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !rep.Running {
		t.Errorf("Status.Running = false, want true")
	}
	if rep.PID <= 0 {
		t.Errorf("Status.PID = %d, want >0", rep.PID)
	}
	if rep.Addr != spec.Addr {
		t.Errorf("Status.Addr = %q, want %q", rep.Addr, spec.Addr)
	}
	if rep.Name != spec.Name {
		t.Errorf("Status.Name = %q, want %q", rep.Name, spec.Name)
	}

	// Stop should remove the plist and the pidfile.
	if err := Stop(context.Background(), spec.Name); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := os.Stat(plistP); !os.IsNotExist(err) {
		t.Errorf("plist not removed: %v", err)
	}
	if _, err := os.Stat(pidP); !os.IsNotExist(err) {
		t.Errorf("pidfile not removed: %v", err)
	}

	// Status after Stop → running=false with hint.
	rep, err = Status(context.Background(), spec.Name)
	if err != nil {
		t.Fatalf("Status (post-stop): %v", err)
	}
	if rep.Running {
		t.Errorf("Status (post-stop).Running = true, want false")
	}
	if rep.Hint == "" {
		t.Errorf("Status (post-stop).Hint empty, want populated")
	}
}

// TestStatus_NotRunning confirms Status returns the canonical
// not-running report for a name that was never started.
func TestStatus_NotRunning(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("persistent remote-control daemon is macOS only")
	}
	testSetup(t)
	tempPlistDir(t)
	rep, err := Status(context.Background(), "never-started")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if rep.Running {
		t.Errorf("Status.Running = true, want false")
	}
	if rep.Name != "never-started" {
		t.Errorf("Status.Name = %q, want %q", rep.Name, "never-started")
	}
	if rep.Hint == "" {
		t.Errorf("Status.Hint empty; want populated")
	}
	if !strings.Contains(rep.Hint, "remote-control start") {
		t.Errorf("Status.Hint %q does not mention start command", rep.Hint)
	}
}

// TestStatus_RunningDryRun confirms Status returns running=true
// after a dry-run Start.  This is the per-cycle preview test:
// `go test -race -run TestStatus_RunningDryRun ./internal/persistentrc/`
// must pass on every platform.
func TestStatus_RunningDryRun(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("persistent remote-control daemon is macOS only")
	}
	testSetup(t)
	dir := tempPlistDir(t)
	spec := fillSpec("machine1")
	spec.PlistDir = dir
	// Pin the timestamp so the StartedAt field is reproducible.
	spec.Now = func() int64 { return 1700000000 } // 2023-11-14T22:13:20Z

	if err := Start(context.Background(), spec); err != nil {
		t.Fatalf("Start: %v", err)
	}

	rep, err := Status(context.Background(), spec.Name)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !rep.Running {
		t.Errorf("Status.Running = false, want true")
	}
	if rep.PID <= 0 {
		t.Errorf("Status.PID = %d, want >0", rep.PID)
	}
	if rep.Addr != spec.Addr {
		t.Errorf("Status.Addr = %q, want %q", rep.Addr, spec.Addr)
	}
	if rep.StartedAtEpoch != 1700000000 {
		t.Errorf("Status.StartedAtEpoch = %d, want 1700000000", rep.StartedAtEpoch)
	}
	if rep.StartedAt == "" {
		t.Errorf("Status.StartedAt empty; want populated")
	}
	if rep.Uptime == "" {
		t.Errorf("Status.Uptime empty; want populated")
	}
}

// TestStatus_InvalidName confirms Status validates the name
// before touching the filesystem.
func TestStatus_InvalidName(t *testing.T) {
	_, err := Status(context.Background(), "bad name")
	if err == nil {
		t.Fatalf("Status with bad name: got nil error")
	}
	var inv *ErrInvalidName
	if !errors.As(err, &inv) {
		t.Errorf("Status with bad name: got %T, want *ErrInvalidName", err)
	}
}

// TestStop_NotRegistered confirms Stop is idempotent: calling
// it on a name that was never started returns nil and does not
// error.
func TestStop_NotRegistered(t *testing.T) {
	testSetup(t)
	tempPlistDir(t)
	if err := Stop(context.Background(), "never-started"); err != nil {
		t.Errorf("Stop (never started) = %v, want nil", err)
	}
}

// TestStop_InvalidName confirms Stop validates the name.
func TestStop_InvalidName(t *testing.T) {
	errInvalidName := Stop(context.Background(), "bad name")
	if errInvalidName == nil {
		t.Fatalf("Stop with bad name: got nil error")
	}
	var inv *ErrInvalidName
	if !errors.As(errInvalidName, &inv) {
		t.Errorf("Stop with bad name: got %T, want *ErrInvalidName", errInvalidName)
	}
}

// TestParsePIDFromLaunchctl pins the regex that pulls the PID
// out of `launchctl print` output.  The shape of the input is
// a launchctl-print-formatted text blob with a "pid = N" line
// somewhere in it.
func TestParsePIDFromLaunchctl(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchctl is macOS only")
	}
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"single_line", "service.PID = {\n\tpid = 12345\n}\n", 12345},
		{"multi_section", "stuff = 1\n\tpid = 98765\nstuff = 3\n", 98765},
		{"trailing_whitespace", "\tpid = 42   \n", 42},
		{"missing", "no pid line here\n", 0},
		{"non_pid_number", "\tlast exit code = 7\n", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePIDFromLaunchctl(tc.in)
			if tc.want == 0 {
				if err == nil && got != 0 {
					t.Errorf("parsePIDFromLaunchctl: got pid=%d err=nil, want error", got)
				}
				return
			}
			if err != nil {
				t.Errorf("parsePIDFromLaunchctl: %v", err)
				return
			}
			if got != tc.want {
				t.Errorf("parsePIDFromLaunchctl: got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestFormatUptime pins the duration-rendering helper.  The
// output is what StatusReport.Uptime surfaces.
func TestFormatUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{-1 * time.Second, ""},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m30s"},
		{60 * time.Minute, "1h0m"},
		{83 * time.Minute, "1h23m"},
		{25 * time.Hour, "1d1h"},
		{49 * time.Hour, "2d1h"},
	}
	for _, tc := range tests {
		t.Run(tc.d.String(), func(t *testing.T) {
			got := formatUptime(tc.d)
			if got != tc.want {
				t.Errorf("formatUptime(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

// TestFormatTimestamp pins the timestamp renderer.  Zero and
// negative epochs yield ""; positive epochs yield the
// RFC3339Nano UTC form.
func TestFormatTimestamp(t *testing.T) {
	if got := formatTimestamp(0); got != "" {
		t.Errorf("formatTimestamp(0) = %q, want empty", got)
	}
	if got := formatTimestamp(1700000000); got != "2023-11-14T22:13:20Z" {
		t.Errorf("formatTimestamp(1700000000) = %q, want 2023-11-14T22:13:20Z", got)
	}
}

// TestLabel pins the launchd-label format.  Used by Status
// reports and by the plist builder.
func TestLabel(t *testing.T) {
	if got := Label("foo"); got != "com.openagy.remote-control-foo" {
		t.Errorf("Label(foo) = %q, want com.openagy.remote-control-foo", got)
	}
}

// TestPlistPathFor pins the production LaunchAgent plist path
// the package uses when Spec.PlistDir is empty.
func TestPlistPathFor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	got := PlistPathFor("machine1")
	want := filepath.Join(dir, "Library", "LaunchAgents", "com.openagy.remote-control-machine1.plist")
	if got != want {
		t.Errorf("PlistPathFor(machine1) = %q, want %q", got, want)
	}
}

// TestLogFilePath pins the production log-file path.
func TestLogFilePath(t *testing.T) {
	t.Setenv("TMPDIR", "/var/folders/abc/T")
	got := LogFilePath("machine1")
	want := "/var/folders/abc/T/openagy-remote-control-machine1.log"
	if got != want {
		t.Errorf("LogFilePath(machine1) = %q, want %q", got, want)
	}
}

// TestLogFilePath_FallbackToTempDir confirms that when TMPDIR
// is unset the helper falls back to os.TempDir().
func TestLogFilePath_FallbackToTempDir(t *testing.T) {
	t.Setenv("TMPDIR", "")
	got := LogFilePath("machine1")
	if got == "" {
		t.Fatalf("LogFilePath: empty (os.TempDir also empty?)")
	}
	if !strings.HasSuffix(got, "openagy-remote-control-machine1.log") {
		t.Errorf("LogFilePath: %q does not end with the expected suffix", got)
	}
}