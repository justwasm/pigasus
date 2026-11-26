package remotecontrol

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/remotecontrol/files"
	"github.com/justwasm/pigasus/lsproto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestLSBackgroundTasksUpdateAndListRPC(t *testing.T) {
	convID := "conv-bg-tasks-test"
	otherConvID := "conv-bg-tasks-other"
	t.Cleanup(func() {
		shell.KillConversationTasks(convID)
		shell.KillConversationTasks(otherConvID)
	})

	home := t.TempDir()
	ws := t.TempDir()
	cmd := &shell.RunCommand{
		Workspace:      ws,
		ConversationID: convID,
		Home:           home,
	}
	res, err := cmd.Run(context.Background(), json.RawMessage(`{"CommandLine":"sleep 10","WaitMsBeforeAsync":50,"toolSummary":"Run long sleep"}`))
	if err != nil {
		t.Fatalf("RunCommand.Run: %v", err)
	}
	if res == nil {
		t.Fatal("RunCommand.Run returned nil result")
	}

	otherCmd := &shell.RunCommand{
		Workspace:      ws,
		ConversationID: otherConvID,
		Home:           home,
	}
	if _, err := otherCmd.Run(context.Background(), json.RawMessage(`{"CommandLine":"sleep 10","WaitMsBeforeAsync":50,"toolSummary":"Other sleep"}`)); err != nil {
		t.Fatalf("other RunCommand.Run: %v", err)
	}

	d, err := NewDispatcher(Config{Session: NewSessionState("test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	traj := d.trajectories.Get(convID)
	snap := traj.Snapshot()
	if len(snap.BackgroundTasks) != 1 {
		t.Fatalf("len(snap.BackgroundTasks) = %d, want 1", len(snap.BackgroundTasks))
	}

	frame := lsAgentStateFrame(snap)
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("json.Marshal(frame): %v", err)
	}
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	body, err := codec.EncodeResponse("StreamAgentStateUpdates", raw)
	if err != nil {
		t.Fatalf("EncodeResponse(StreamAgentStateUpdates): %v", err)
	}
	md, ok := codec.OutputMessage("StreamAgentStateUpdates")
	if !ok {
		t.Fatal("missing StreamAgentStateUpdates output descriptor")
	}
	decoded := dynamicpb.NewMessage(md)
	if err := proto.Unmarshal(body, decoded); err != nil {
		t.Fatalf("proto.Unmarshal: %v", err)
	}
	update := decoded.Get(fdByName(md, "update")).Message()
	bgUpdate := update.Get(fdByName(update.Descriptor(), "background_tasks_update")).Message()
	if !bgUpdate.IsValid() {
		t.Fatal("missing background_tasks_update in decoded AgentStateUpdate")
	}
	bgList := bgUpdate.Get(fdByName(bgUpdate.Descriptor(), "background_tasks")).List()
	if bgList.Len() != 1 {
		t.Fatalf("background_tasks len = %d, want 1", bgList.Len())
	}
	taskSnap := bgList.Get(0).Message().Get(fdByName(bgList.Get(0).Message().Descriptor(), "task_snapshot")).Message()
	if got := taskSnap.Get(fdByName(taskSnap.Descriptor(), "tool_summary")).String(); got != "Run long sleep" {
		t.Errorf("tool_summary = %q, want %q", got, "Run long sleep")
	}
	if got := taskSnap.Get(fdByName(taskSnap.Descriptor(), "is_daemon")).Bool(); got {
		t.Errorf("is_daemon = true, want false")
	}

	// ListBackgroundTasks filtered by conversationId.
	ans, rpcErr, handled := d.routeLanguageServerAdditionalMethods(context.Background(), "ListBackgroundTasks", json.RawMessage(`{"conversationId":"`+convID+`"}`))
	if !handled || rpcErr != nil {
		t.Fatalf("ListBackgroundTasks(filtered): handled=%v err=%v", handled, rpcErr)
	}
	listRaw, err := json.Marshal(ans)
	if err != nil {
		t.Fatalf("json.Marshal(ListBackgroundTasks): %v", err)
	}
	listBody, err := codec.EncodeResponse("ListBackgroundTasks", listRaw)
	if err != nil {
		t.Fatalf("EncodeResponse(ListBackgroundTasks): %v", err)
	}
	listMD, ok := codec.OutputMessage("ListBackgroundTasks")
	if !ok {
		t.Fatal("missing ListBackgroundTasks output descriptor")
	}
	listDecoded := dynamicpb.NewMessage(listMD)
	if err := proto.Unmarshal(listBody, listDecoded); err != nil {
		t.Fatalf("proto.Unmarshal(ListBackgroundTasks): %v", err)
	}
	tasks := listDecoded.Get(fdByName(listMD, "tasks")).List()
	if tasks.Len() != 1 {
		t.Fatalf("filtered ListBackgroundTasks len = %d, want 1", tasks.Len())
	}
	if got := tasks.Get(0).Message().Get(fdByName(tasks.Get(0).Message().Descriptor(), "conversation_id")).String(); got != convID {
		t.Errorf("conversation_id = %q, want %q", got, convID)
	}

	// ListBackgroundTasks unfiltered returns both conversations' tasks.
	allAns, rpcErr, handled := d.routeLanguageServerAdditionalMethods(context.Background(), "ListBackgroundTasks", json.RawMessage(`{}`))
	if !handled || rpcErr != nil {
		t.Fatalf("ListBackgroundTasks(all): handled=%v err=%v", handled, rpcErr)
	}
	allMap, _ := allAns.(map[string]any)
	allTasks, _ := allMap["tasks"].([]any)
	if len(allTasks) < 2 {
		t.Fatalf("unfiltered ListBackgroundTasks len = %d, want >= 2", len(allTasks))
	}
}

func TestLSIncludeBundleMember(t *testing.T) {
	d, err := NewDispatcher(Config{Session: NewSessionState("test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ans, rpcErr, handled := d.routeLanguageServerMethods(context.Background(), "IncludeBundleMember", json.RawMessage(`{"bundlePluginId":"bundle-1","memberCustomizationId":"member-1"}`))
	if !handled || rpcErr != nil {
		t.Fatalf("IncludeBundleMember: handled=%v err=%v", handled, rpcErr)
	}
	raw, err := json.Marshal(ans)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	if _, err := codec.EncodeResponse("IncludeBundleMember", raw); err != nil {
		t.Fatalf("EncodeResponse(IncludeBundleMember): %v", err)
	}
}

func TestLS2340NewRPCs(t *testing.T) {
	d, err := NewDispatcher(Config{Session: NewSessionState("test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}

	// Create a local bare git repo to test CloneGitHubRepo without network.
	srcRepo := filepath.Join(t.TempDir(), "sample-repo")
	if err := os.MkdirAll(srcRepo, 0o755); err != nil {
		t.Fatalf("mkdir srcRepo: %v", err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", srcRepo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(srcRepo, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", srcRepo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	parentDir := t.TempDir()
	cloneReq, _ := json.Marshal(map[string]string{
		"repoSlug":     "file://" + srcRepo,
		"parentDirUri": files.NormalizedURI(parentDir),
	})
	ans, rpcErr, handled := d.routeLanguageServerMethods(context.Background(), "CloneGitHubRepo", cloneReq)
	if !handled || rpcErr != nil {
		t.Fatalf("CloneGitHubRepo: handled=%v err=%v", handled, rpcErr)
	}
	raw, _ := json.Marshal(ans)
	if _, err := codec.EncodeResponse("CloneGitHubRepo", raw); err != nil {
		t.Fatalf("EncodeResponse(CloneGitHubRepo): %v", err)
	}
	clonedDir := filepath.Join(parentDir, "sample-repo")
	if !isGitRepo(clonedDir) {
		t.Fatalf("expected cloned git repo at %s", clonedDir)
	}

	for _, tc := range []struct {
		method string
		params string
	}{
		{"GetProjectTrustStatus", `{"projectId":"p1"}`},
		{"SetWorkspaceTrust", `{"projectId":"p1","trusted":true,"paths":["/tmp"]}`},
		{"GetHubOAuthAccessToken", `{}`},
	} {
		ans, rpcErr, handled := d.routeLanguageServerAdditionalMethods(context.Background(), tc.method, json.RawMessage(tc.params))
		if !handled || rpcErr != nil {
			t.Fatalf("%s: handled=%v err=%v", tc.method, handled, rpcErr)
		}
		raw, _ := json.Marshal(ans)
		if _, err := codec.EncodeResponse(tc.method, raw); err != nil {
			t.Fatalf("EncodeResponse(%s): %v", tc.method, err)
		}
	}
}
