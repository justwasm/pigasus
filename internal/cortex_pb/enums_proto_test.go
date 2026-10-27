package cortex_pb

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// The recovered .proto is the source of truth for the enum numbering: it is
// the reference's own descriptor, read out of the 1.4.7 binary.
//
//	out/local-macos/protos/cortex_1.4.6.proto
//
// The test skips when the file is absent — the `out/` tree is evidence, not
// part of the build.
const protoPath = "../../out/local-macos/protos/cortex_1.4.6.proto"

type protoEnum struct {
	name   string
	values map[string]int
}

func parseProtoEnums(t *testing.T) map[string]protoEnum {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(protoPath))
	if err != nil {
		t.Skipf("the recovered .proto is not here (%v); nothing to compare against", err)
	}
	text := string(raw)
	out := map[string]protoEnum{}
	reEnum := regexp.MustCompile(`enum\s+(\w+)\s*\{([^}]*)\}`)
	reValue := regexp.MustCompile(`([A-Z0-9_]+)\s*=\s*(-?\d+)\s*;`)
	for _, m := range reEnum.FindAllStringSubmatch(text, -1) {
		values := map[string]int{}
		for _, line := range reValue.FindAllStringSubmatch(m[2], -1) {
			n, err := strconv.Atoi(line[2])
			if err != nil {
				t.Fatalf("enum %s: %s", m[1], err)
			}
			values[line[1]] = n
		}
		out[m[1]] = protoEnum{name: m[1], values: values}
	}
	if len(out) == 0 {
		t.Fatal("the recovered .proto holds no enum")
	}
	return out
}

// stringer is the shape every recovered enum's String() method has.
type stringer interface {
	String() string
}

// enumValue returns the enum value for a given enum type and number.
func enumValue(t *testing.T, enumType string, number int) stringer {
	t.Helper()
	switch enumType {
	case "AgentMode":
		return AgentMode(number)
	case "AgentSurface":
		return AgentSurface(number)
	case "BrowserSubagentMode":
		return BrowserSubagentMode(number)
	case "CascadeRunStatus":
		return CascadeRunStatus(number)
	case "CheckpointStrategy":
		return CheckpointStrategy(number)
	case "CheckpointType":
		return CheckpointType(number)
	case "CortexStepStatus":
		return CortexStepStatus(number)
	case "CortexStepSource":
		return CortexStepSource(number)
	case "CortexStepType":
		return CortexStepType(number)
	case "CortexTrajectoryType":
		return CortexTrajectoryType(number)
	case "EphemeralMessagePersistenceLevel":
		return EphemeralMessagePersistenceLevel(number)
	case "ExecutorTerminationReason":
		return ExecutorTerminationReason(number)
	case "FileChangeType":
		return FileChangeType(number)
	case "McpServerStatus":
		return McpServerStatus(number)
	case "ModelTier":
		return ModelTier(number)
	case "PermissionScope":
		return PermissionScope(number)
	case "PlanStatus":
		return PlanStatus(number)
	case "SubagentState":
		return SubagentState(number)
	case "TaskStatus":
		return TaskStatus(number)
	case "TokenSource":
		return TokenSource(number)
	case "TokenType":
		return TokenType(number)
	case "TruncationReason":
		return TruncationReason(number)
	case "WorkspaceType":
		return WorkspaceType(number)
	}
	t.Fatalf("no Go type for the %s enum", enumType)
	return nil
}

// TestEnumsMatchTheRecoveredProto compares every enum the package carries
// against the reference's own descriptor: the member names and their
// numbers must be identical.
func TestEnumsMatchTheRecoveredProto(t *testing.T) {
	protos := parseProtoEnums(t)
	for enumType, members := range EnumMembers {
		proto, ok := protos[enumType]
		if !ok {
			t.Errorf("EnumMembers holds %s, which the recovered .proto does not have", enumType)
			continue
		}
		if len(members) != len(proto.values) {
			t.Errorf("%s: the clone has %d members, the .proto has %d",
				enumType, len(members), len(proto.values))
		}
		for name, number := range proto.values {
			got, ok := members[name]
			if !ok {
				t.Errorf("%s: the clone has no member %s", enumType, name)
				continue
			}
			if got != int32(number) {
				t.Errorf("%s.%s = %d, the .proto says %d", enumType, name, got, number)
			}
		}
		for name := range members {
			if _, ok := proto.values[name]; !ok {
				t.Errorf("%s: the clone has a member %s the .proto does not", enumType, name)
			}
		}
	}
}

// TestEnumStringReturnsTheProtoName checks the other half: a member's
// number must map back to the reference's own spelling.
func TestEnumStringReturnsTheProtoName(t *testing.T) {
	protos := parseProtoEnums(t)
	for enumType, members := range EnumMembers {
		proto, ok := protos[enumType]
		if !ok {
			continue
		}
		if len(members) != len(proto.values) {
			t.Errorf("%s: the clone has %d members, the .proto has %d",
				enumType, len(members), len(proto.values))
		}
		for name, number := range proto.values {
			if got := enumValue(t, enumType, number).String(); got != name {
				t.Errorf("%s(%d).String() = %q, want %q", enumType, number, got, name)
			}
		}
	}
}

// GoalState is the clone's own: the recovered .proto has no such enum, so it
// must stay out of the machine-readable table — otherwise a reader would
// take its numbers for the reference's.
func TestGoalStateIsNotInTheTable(t *testing.T) {
	if _, ok := EnumMembers["GoalState"]; ok {
		t.Error("GoalState is the clone's own enum; it does not belong in EnumMembers")
	}
	protos := parseProtoEnums(t)
	if _, ok := protos["GoalState"]; ok {
		t.Error("the recovered .proto has a GoalState enum; the clone's is redundant")
	}
}

// The numbers the reference actually puts on the wire, read out of a real
// implicit-trajectory record (2026-10-22). They are the reason the enums
// were worth fixing.
func TestTheNumbersTheImplicitTrajectoryCarries(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"CORTEX_STEP_TYPE_RUN_COMMAND", int(CortexStepTypeRunCommand), 21},
		{"CORTEX_STEP_TYPE_USER_INPUT", int(CortexStepTypeUserInput), 14},
		{"CORTEX_STEP_STATUS_DONE", int(CortexStepStatusDone), 3},
		{"CORTEX_STEP_STATUS_CANCELED", int(CortexStepStatusCanceled), 6},
		{"CORTEX_STEP_SOURCE_USER_EXPLICIT", int(CortexStepSourceUserExplicit), 4},
		{"CORTEX_TRAJECTORY_TYPE_USER_MAINLINE", int(CortexTrajectoryTypeUserMainline), 1},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}
