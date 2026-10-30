package implicit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testStep is the command the reference recorded on 2026-10-22, in the
// reference's own words: `printf 'a\nb\n'`, exit 0, output "a\nb".
func testStep() Step {
	return Step{
		Status: StepStatusDone,
		RunCommand: RunCommand{
			CommandLine:         "printf 'a\\nb\\n'",
			ProposedCommandLine: "printf 'a\\nb\\n'",
			CWD:                 "/private/var/folders/x/T/opencode",
			ExitCode:            0,
			HasExitCode:         true,
			CombinedOutput:      "a\nb",
			Blocking:            true,
			UsedIDETerminal:     true,
		},
		CreatedAt:   time.Unix(1792644998, 180119000).UTC(),
		CompletedAt: time.Unix(1792644998, 196151000).UTC(),
	}
}

// TestRecordRoundTrip pins the whole path: a recorded step is written,
// encrypted, read back and decoded into the same fields.
func TestRecordRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	workspace := WorkspaceURI("/private/var/folders/x/T/opencode")

	if err := store.Record(workspace, testStep()); err != nil {
		t.Fatalf("record: %v", err)
	}
	steps, err := store.Steps(workspace)
	if err != nil {
		t.Fatalf("steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("got %d steps, want 1", len(steps))
	}
	got := steps[0]
	want := testStep()
	if got.Status != want.Status {
		t.Errorf("status = %d, want %d", got.Status, want.Status)
	}
	if got.RunCommand.CommandLine != want.RunCommand.CommandLine {
		t.Errorf("command_line = %q, want %q", got.RunCommand.CommandLine, want.RunCommand.CommandLine)
	}
	if got.RunCommand.ProposedCommandLine != want.RunCommand.ProposedCommandLine {
		t.Errorf("proposed_command_line = %q, want %q",
			got.RunCommand.ProposedCommandLine, want.RunCommand.ProposedCommandLine)
	}
	if got.RunCommand.CWD != want.RunCommand.CWD {
		t.Errorf("cwd = %q, want %q", got.RunCommand.CWD, want.RunCommand.CWD)
	}
	if got.RunCommand.ExitCode != want.RunCommand.ExitCode || !got.RunCommand.HasExitCode {
		t.Errorf("exit_code = %d (set %v), want %d (set)",
			got.RunCommand.ExitCode, got.RunCommand.HasExitCode, want.RunCommand.ExitCode)
	}
	if got.RunCommand.CombinedOutput != want.RunCommand.CombinedOutput {
		t.Errorf("combined_output = %q, want %q",
			got.RunCommand.CombinedOutput, want.RunCommand.CombinedOutput)
	}
	if !got.RunCommand.Blocking {
		t.Error("blocking is not set")
	}
	if !got.RunCommand.UsedIDETerminal {
		t.Error("used_ide_terminal is not set")
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("created_at = %s, want %s", got.CreatedAt, want.CreatedAt)
	}
	if !got.CompletedAt.Equal(want.CompletedAt) {
		t.Errorf("completed_at = %s, want %s", got.CompletedAt, want.CompletedAt)
	}
}

// TestTheFileIsEncrypted pins the reference's storage: the file on disk does
// not hold the command in the clear.
func TestTheFileIsEncrypted(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	workspace := WorkspaceURI("/w")
	if err := store.Record(workspace, testStep()); err != nil {
		t.Fatalf("record: %v", err)
	}
	raw, err := os.ReadFile(store.Path(workspace))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "printf") {
		t.Error("the command is readable in the file on disk")
	}
	// The layout is nonce(12) ‖ ciphertext ‖ tag, and the reference's key
	// opens it.
	plaintext, err := open(raw)
	if err != nil {
		t.Fatalf("the reference's key does not open the file: %v", err)
	}
	if !strings.Contains(string(plaintext), "printf") {
		t.Error("the decrypted file does not hold the command")
	}
}

// TestTheFileNameIsPerWorkspace pins the scope: one file per workspace, and
// the same file for the same workspace.
func TestTheFileNameIsPerWorkspace(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	one := WorkspaceURI("/w/one")
	two := WorkspaceURI("/w/two")

	if err := store.Record(one, testStep()); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(two, testStep()); err != nil {
		t.Fatal(err)
	}
	if store.Path(one) == store.Path(two) {
		t.Error("two workspaces share one trajectory file")
	}
	if !strings.HasSuffix(store.Path(one), ".pb") {
		t.Errorf("the file name is %q, want a .pb file", store.Path(one))
	}
	base := strings.TrimSuffix(filepath.Base(store.Path(one)), ".pb")
	if len(base) != 36 || strings.Count(base, "-") != 4 {
		t.Errorf("the file name %q is not a UUID", base)
	}
	entries, err := os.ReadDir(filepath.Join(dir, DirName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("the store holds %d files, want 2", len(entries))
	}
}

// TestTheStepsAccumulate pins the trajectory's long life: it holds every
// command of a workspace, in order.
func TestTheStepsAccumulate(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	workspace := WorkspaceURI("/w")
	first := testStep()
	second := testStep()
	second.RunCommand.CommandLine = "echo hello"
	second.RunCommand.ProposedCommandLine = "echo hello"
	second.RunCommand.CombinedOutput = "hello"

	if err := store.Record(workspace, first); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(workspace, second); err != nil {
		t.Fatal(err)
	}
	steps, err := store.Steps(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}
	if steps[0].RunCommand.CommandLine != first.RunCommand.CommandLine {
		t.Errorf("steps[0] = %q, want %q", steps[0].RunCommand.CommandLine, first.RunCommand.CommandLine)
	}
	if steps[1].RunCommand.CommandLine != second.RunCommand.CommandLine {
		t.Errorf("steps[1] = %q, want %q", steps[1].RunCommand.CommandLine, second.RunCommand.CommandLine)
	}
}

// TestTheRetentionKeepsHundredFiles pins the reference's cap.
func TestTheRetentionKeepsHundredFiles(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	for i := 0; i < MaxFiles+5; i++ {
		if err := store.Record(WorkspaceURI("/w/"+strings.Repeat("x", i)), testStep()); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, DirName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != MaxFiles {
		t.Errorf("the store holds %d files, want %d", len(entries), MaxFiles)
	}
}

// TestAnUnknownFieldSurvivesARewrite pins the round-trip's promise: a field
// the clone does not know about is written back unchanged, so a trajectory
// the reference wrote stays readable by the reference.
func TestAnUnknownFieldSurvivesARewrite(t *testing.T) {
	// A trajectory with an unknown field 99 in it.
	traj := appendStringField(nil, fieldTrajectoryID, "id")
	traj = appendBytesField(traj, 99, []byte("unknown"))
	traj = appendVarintField(traj, fieldTrajectoryType, uint64(TrajectoryTypeUserMainline))

	rewritten := appendStep(traj, testStep())
	fields := parseFields(rewritten)
	found := false
	for _, f := range fields {
		if f.num == 99 {
			found = true
			if string(f.data) != "unknown" {
				t.Errorf("the unknown field is %q, want %q", f.data, "unknown")
			}
		}
	}
	if !found {
		t.Error("the unknown field was dropped")
	}
}

// TestTheWireShape pins the field numbers and the enum values the reference
// writes — the ones a reference-side reader depends on.
func TestTheWireShape(t *testing.T) {
	step := testStep()
	encoded := encodeStep(step)
	fields := parseFields(encoded)

	typeName, ok := firstVarint(fields, fieldStepType)
	if !ok || typeName != uint64(StepTypeRunCommand) {
		t.Errorf("the step's type = %d, want %d", typeName, StepTypeRunCommand)
	}
	status, ok := firstVarint(fields, fieldStepStatus)
	if !ok || status != uint64(StepStatusDone) {
		t.Errorf("the step's status = %d, want %d", status, StepStatusDone)
	}
	meta, ok := firstMessage(fields, fieldStepMeta)
	if !ok {
		t.Fatal("the step carries no metadata")
	}
	source, ok := firstVarint(parseFields(meta), fieldMetaSource)
	if !ok || source != uint64(StepSourceUserExplicit) {
		t.Errorf("the step's source = %d, want %d", source, StepSourceUserExplicit)
	}
	rc, ok := firstMessage(fields, fieldStepRunCommand)
	if !ok {
		t.Fatal("the step carries no run_command")
	}
	rcFields := parseFields(rc)
	exit, ok := firstVarint(rcFields, fieldCmdExitCode)
	if !ok || exit != 0 {
		t.Errorf("the exit code = %d (set %v), want 0 — the reference writes the field explicitly",
			exit, ok)
	}
	blocking, ok := firstVarint(rcFields, fieldCmdBlocking)
	if !ok || blocking != 1 {
		t.Errorf("blocking = %d, want 1", blocking)
	}
	terminal, ok := firstVarint(rcFields, fieldCmdUsedIDETerm)
	if !ok || terminal != 1 {
		t.Errorf("used_ide_terminal = %d, want 1", terminal)
	}
}

// TestACanceledCommandHasNoExitCode pins the reference's canceled record: it
// never set the exit code, so the field is absent.
func TestACanceledCommandHasNoExitCode(t *testing.T) {
	step := testStep()
	step.Status = StepStatusCanceled
	step.RunCommand.HasExitCode = false
	step.RunCommand.CombinedOutput = "partial"

	encoded := encodeStep(step)
	rc, ok := firstMessage(parseFields(encoded), fieldStepRunCommand)
	if !ok {
		t.Fatal("the step carries no run_command")
	}
	if _, ok := firstVarint(parseFields(rc), fieldCmdExitCode); ok {
		t.Error("the canceled step carries an exit code")
	}
}

// TestTheStoreDirIsUnderTheGeminiDir pins where the store lives.
func TestTheStoreDirIsUnderTheGeminiDir(t *testing.T) {
	store := Open("/tmp/gemini")
	if store.Dir() != filepath.Join("/tmp/gemini", "implicit") {
		t.Errorf("the store's dir = %q", store.Dir())
	}
}

// referenceStep reads the reference's own record — the first step of
// `~/.gemini/antigravity-cli/implicit/3a75e51c-a757-4e3c-9b6c-3f17f7c9b3f8.pb`,
// decrypted with the reference's key on 2026-10-26.
func referenceStep(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "reference_step_1.4.7.pb"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestTheReferenceRecordDecodes pins the clone's reader against the
// reference's own bytes.
//
// 【一手】 the record is the `printf 'a\nb\n'` command of 2026-10-22: the
// command runs in the workspace, exits 0, prints "a\nb", and the reference
// wrote the command twice — as `command_line` and as
// `proposed_command_line` — with `blocking` and `used_ide_terminal` set.
func TestTheReferenceRecordDecodes(t *testing.T) {
	step, ok := decodeStep(referenceStep(t))
	if !ok {
		t.Fatal("the reference's step did not decode")
	}
	if step.Status != StepStatusDone {
		t.Errorf("status = %d, want %d", step.Status, StepStatusDone)
	}
	if step.RunCommand.CommandLine != "printf 'a\\nb\\n'" {
		t.Errorf("command_line = %q", step.RunCommand.CommandLine)
	}
	if step.RunCommand.ProposedCommandLine != step.RunCommand.CommandLine {
		t.Errorf("proposed_command_line = %q, want %q",
			step.RunCommand.ProposedCommandLine, step.RunCommand.CommandLine)
	}
	if step.RunCommand.CombinedOutput != "a\nb" {
		t.Errorf("combined_output = %q, want %q", step.RunCommand.CombinedOutput, "a\nb")
	}
	if !step.RunCommand.HasExitCode || step.RunCommand.ExitCode != 0 {
		t.Errorf("exit_code = %d (set %v), want 0 (set) — the reference writes the field explicitly",
			step.RunCommand.ExitCode, step.RunCommand.HasExitCode)
	}
	if !step.RunCommand.Blocking {
		t.Error("blocking is not set")
	}
	if !step.RunCommand.UsedIDETerminal {
		t.Error("used_ide_terminal is not set")
	}
	if !strings.HasSuffix(step.RunCommand.CWD, "/T/opencode") {
		t.Errorf("cwd = %q", step.RunCommand.CWD)
	}
	if step.CreatedAt.IsZero() || step.CompletedAt.IsZero() {
		t.Error("the record carries no timestamps")
	}
}

// TestTheEncoderReproducesTheReferenceRecord is the strongest check the
// clone can make: the bytes the clone writes for the reference's own record
// are the reference's bytes.
//
// It pins the field *order* as well as the field numbers — the reference
// writes the step's type, status, metadata and run_command in that order,
// and the metadata's created_at, source and completed_at in that order.
func TestTheEncoderReproducesTheReferenceRecord(t *testing.T) {
	raw := referenceStep(t)
	step, ok := decodeStep(raw)
	if !ok {
		t.Fatal("the reference's step did not decode")
	}
	encoded := encodeStep(step)
	if !bytes.Equal(encoded, raw) {
		t.Errorf("the clone's encoding is not the reference's bytes\n got: %x\nwant: %x",
			encoded, raw)
	}
}

// TestTheNotificationTimeoutField pins the reference's own field number for
// the notification timeout: `uint64 notification_timeout_seconds = 30` in
// CortexStepRunCommand — 【一手】 the reference's own descriptor
// (out/local-macos/protos/cortex_1.4.6.proto).
func TestTheNotificationTimeoutField(t *testing.T) {
	step := testStep()
	step.RunCommand.NotificationTimeoutSeconds = 300

	encoded := encodeStep(step)
	rc, ok := firstMessage(parseFields(encoded), fieldStepRunCommand)
	if !ok {
		t.Fatal("the step carries no run_command")
	}
	got, ok := firstVarint(parseFields(rc), fieldCmdNotifyTimeout)
	if !ok || got != 300 {
		t.Errorf("notification_timeout_seconds = %d (set %v), want 300", got, ok)
	}

	back, ok := decodeStep(encoded)
	if !ok {
		t.Fatal("the step does not decode")
	}
	if back.RunCommand.NotificationTimeoutSeconds != 300 {
		t.Errorf("the decoded notification timeout is %d, want 300",
			back.RunCommand.NotificationTimeoutSeconds)
	}

	// A zero timeout is not written at all: proto3 omits a zero.
	step.RunCommand.NotificationTimeoutSeconds = 0
	encoded = encodeStep(step)
	rc, _ = firstMessage(parseFields(encoded), fieldStepRunCommand)
	if _, ok := firstVarint(parseFields(rc), fieldCmdNotifyTimeout); ok {
		t.Error("a zero notification timeout was written; proto3 omits it")
	}
}
