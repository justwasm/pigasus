package herdr_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/justwasm/pigasus/herdr"
)

type fakeRunner struct {
	responses []runResponse
	calls     [][]string
}

type runResponse struct {
	stdout []byte
	stderr []byte
	err    error
}

func (r *fakeRunner) Run(_ context.Context, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, args)
	if len(r.responses) == 0 {
		panic("unexpected command")
	}
	response := r.responses[0]
	r.responses = r.responses[1:]
	return response.stdout, response.stderr, response.err
}

func TestPrompt(t *testing.T) {
	runner := &fakeRunner{responses: []runResponse{{stdout: []byte(`{"result":{}}`)}}}
	err := (herdr.Client{Runner: runner}).Prompt(context.Background(), "worker", "Inbox has messages.")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"agent", "prompt", "worker", "Inbox has messages."}
	if !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("call = %v, want %v", runner.calls[0], want)
	}
}

func TestCommandRunnerSessionEnvironment(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "/wrong/herdr.sock")
	t.Setenv("HERDR_WORKSPACE_ID", "w99")
	t.Setenv("HERDR_TAB_ID", "w99:t1")
	t.Setenv("HERDR_PANE_ID", "w99:p1")
	executable := filepath.Join(t.TempDir(), "herdr-probe")
	script := "#!/bin/sh\nprintf '%s\n' \"$HERDR_SESSION\" \"$HERDR_SOCKET_PATH\" \"$HERDR_WORKSPACE_ID\" \"$HERDR_TAB_ID\" \"$HERDR_PANE_ID\" \"$@\"\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	client := herdr.NewClient(executable)
	bound, err := client.ForSession("crew")
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := bound.Run(
		context.Background(),
		"pane",
		"get",
		"pane-1",
		"--session",
		"crew",
	)
	want := "crew\n\n\n\n\npane\nget\npane-1\n--session\ncrew\n"
	if err != nil || string(stdout) != want {
		t.Fatalf("session command = %q, want %q, error: %v", stdout, want, err)
	}
}

func TestPromptPreservesTypedAdapterFailures(t *testing.T) {
	for _, check := range []struct {
		code string
		want error
	}{
		{"agent_not_found", herdr.ErrNotFound},
		{"agent_not_ready", herdr.ErrAgentNotReady},
		{"workspace_not_found", herdr.ErrWorkspaceNotFound},
		{"server_unavailable", herdr.ErrCommand},
	} {
		t.Run(check.code, func(t *testing.T) {
			runner := &fakeRunner{
				responses: []runResponse{
					{
						stderr: []byte(`{"error":{"code":"` + check.code + `"}}`),
						err:    errors.New("exit 1"),
					},
				},
			}
			err := (herdr.Client{Runner: runner}).Prompt(context.Background(), "worker", "Inbox pending")
			if !errors.Is(err, check.want) {
				t.Fatalf("prompt error=%v, want %v", err, check.want)
			}
		})
	}
}

func TestDecodeResponse(t *testing.T) {
	for _, trial := range []struct {
		name string
		data string
		want error
	}{
		{"result", `{"result":{}}`, nil},
		{"malformed", `{`, herdr.ErrResponse},
		{"missing result", `{}`, herdr.ErrResponse},
		{"null result", `{"result":null}`, herdr.ErrResponse},
		{"wrong result type", `{"result":[]}`, herdr.ErrResponse},
		{"missing agent", `{"error":{"code":"agent_not_found","message":"gone"}}`, herdr.ErrNotFound},
		{"missing workspace", `{"error":{"code":"workspace_not_found"}}`, herdr.ErrWorkspaceNotFound},
		{"not ready", `{"error":{"code":"agent_not_ready"}}`, herdr.ErrAgentNotReady},
		{"other error", `{"error":{"code":"server_unavailable"}}`, herdr.ErrCommand},
	} {
		t.Run(trial.name, func(t *testing.T) {
			_, err := herdr.DecodeResponse[struct{}]([]byte(trial.data))
			if !errors.Is(err, trial.want) {
				t.Fatalf("error = %v, want %v", err, trial.want)
			}
		})
	}
}
