// Package herdr provides terminal multiplexing with a side tab panel and Herdr socket compatibility.
// It includes client interfaces matching github.com/roman/tincan/pkg/herdr for shared command execution,
// session binding, notifications, and direct Unix domain socket JSON-RPC communication.
package herdr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	ErrWorkspaceNotFound = errors.New("herdr workspace not found")
	ErrNotFound          = errors.New("herdr agent not found")
	ErrCommand           = errors.New("herdr command failed")
	ErrResponse          = errors.New("invalid herdr response")
	ErrAgentNotReady     = errors.New("herdr agent not ready")
)

// Runner executes the Herdr binary. Its arguments exclude the executable name.
type Runner interface {
	Run(context.Context, ...string) ([]byte, []byte, error)
}

// SessionRunner executes commands with an explicit session and no inherited
// Herdr socket, workspace, tab, or pane selection.
type SessionRunner interface {
	RunSession(context.Context, string, ...string) ([]byte, []byte, error)
}

type boundSessionRunner struct {
	session string
	runner  SessionRunner
}

func (runner boundSessionRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	return runner.runner.RunSession(ctx, runner.session, args...)
}

// Client wraps command execution against a Herdr binary or session.
type Client struct {
	Runner        Runner
	SessionRunner SessionRunner
}

// NewClient returns a Client configured to invoke the given executable (defaulting to "herdr").
func NewClient(executable string) Client {
	if executable == "" {
		executable = "herdr"
	}
	runner := commandRunner{executable: executable}
	return Client{Runner: runner, SessionRunner: runner}
}

type commandRunner struct{ executable string }

func (r commandRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, r.executable, args...)
	stdout, err := command.Output()
	if err == nil {
		return stdout, nil, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return stdout, exitError.Stderr, err
	}
	return stdout, nil, err
}

func (r commandRunner) RunSession(
	ctx context.Context,
	session string,
	args ...string,
) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, r.executable, args...)
	var environment []string
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		switch name {
		case "HERDR_SESSION",
			"HERDR_SOCKET_PATH",
			"HERDR_WORKSPACE_ID",
			"HERDR_TAB_ID",
			"HERDR_PANE_ID":
			continue
		}
		environment = append(environment, variable)
	}
	command.Env = append(environment, "HERDR_SESSION="+session)
	stdout, err := command.Output()
	if err == nil {
		return stdout, nil, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return stdout, exitError.Stderr, err
	}
	return stdout, nil, err
}

// ForSession returns a copy bound to a session. The original client is unchanged.
func (client Client) ForSession(session string) (Client, error) {
	if session == "" ||
		strings.Trim(
			session,
			"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._@%+-",
		) != "" {
		return Client{}, fmt.Errorf("%w: invalid Herdr session", ErrResponse)
	}
	if client.SessionRunner == nil {
		return Client{}, fmt.Errorf("%w: no session runner", ErrCommand)
	}
	client.Runner = boundSessionRunner{session: session, runner: client.SessionRunner}
	return client, nil
}

// ResponseEnvelope holds Herdr's common JSON wrapper without prescribing the
// application-specific result schema.
type ResponseEnvelope[T any] struct {
	Result T             `json:"result"`
	Error  ResponseError `json:"error"`
}

// ResponseError describes an error returned by Herdr.
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (response ResponseError) commandError() error {
	detail := response.Message
	if detail == "" {
		detail = response.Code
	}
	switch response.Code {
	case "workspace_not_found":
		return fmt.Errorf("%w: %w: %s", ErrCommand, ErrWorkspaceNotFound, detail)
	case "agent_not_ready":
		return fmt.Errorf("%w: %w: %s", ErrCommand, ErrAgentNotReady, detail)
	case "agent_not_found":
		return fmt.Errorf("%w: %s", ErrNotFound, detail)
	default:
		return fmt.Errorf("%w: %s", ErrCommand, detail)
	}
}

// DecodeResponse decodes a command's result and maps Herdr errors to the same
// sentinel errors as Client.Run. Callers validate fields required by their operation.
func DecodeResponse[T any](data []byte) (T, error) {
	var result T
	var response ResponseEnvelope[json.RawMessage]
	if err := json.Unmarshal(data, &response); err != nil {
		return result, fmt.Errorf("%w: %v", ErrResponse, err)
	}
	if response.Error.Code != "" {
		return result, response.Error.commandError()
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return result, fmt.Errorf("%w: missing result", ErrResponse)
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return result, fmt.Errorf("%w: %v", ErrResponse, err)
	}
	return result, nil
}

// Run executes arbitrary Herdr subcommands and returns stdout. Arguments exclude
// the executable name. A client returned by ForSession uses that session.
func (c Client) Run(ctx context.Context, args ...string) ([]byte, error) {
	if c.Runner == nil {
		return nil, fmt.Errorf("%w: no runner", ErrCommand)
	}
	stdout, stderr, err := c.Runner.Run(ctx, args...)
	if err == nil {
		return stdout, nil
	}
	for _, output := range [][]byte{stdout, stderr} {
		var response ResponseEnvelope[json.RawMessage]
		if json.Unmarshal(output, &response) != nil || response.Error.Code == "" {
			continue
		}
		return nil, response.Error.commandError()
	}
	detail := strings.TrimSpace(string(stderr))
	if detail == "" {
		detail = strings.TrimSpace(string(stdout))
	}
	if detail == "" {
		detail = err.Error()
	}
	return nil, fmt.Errorf("%w: %s", ErrCommand, detail)
}

// Prompt submits a short notification. Submission does not confirm that the
// agent read it; callers must use their own acknowledgement record.
func (c Client) Prompt(ctx context.Context, target, message string) error {
	if target == "" || message == "" {
		return fmt.Errorf("%w: empty prompt target or message", ErrResponse)
	}
	_, err := c.Run(ctx, "agent", "prompt", target, message)
	return err
}

// CallSocket connects to the Herdr Unix domain socket, sends a newline-delimited
// JSON-RPC request, and reads the newline-delimited JSON-RPC response envelope.
func CallSocket(socketPath, method string, params any) ([]byte, error) {
	var paramsRaw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		paramsRaw = data
	} else {
		paramsRaw = json.RawMessage("{}")
	}

	req := map[string]any{
		"id":     fmt.Sprintf("%d", time.Now().UnixNano()),
		"method": method,
		"params": paramsRaw,
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return CallSocketRaw(socketPath, reqBytes)
}

// CallSocketRaw sends arbitrary raw request bytes (appending newline if needed)
// to a Herdr Unix domain socket and reads the single-line response.
func CallSocketRaw(socketPath string, reqData []byte) ([]byte, error) {
	if socketPath == "" {
		return nil, errors.New("empty socket path")
	}
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if !bytes.HasSuffix(reqData, []byte("\n")) {
		reqData = append(reqData, '\n')
	}
	if _, err := conn.Write(reqData); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(io.LimitReader(conn, 1<<20))
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return bytes.TrimSpace(line), nil
}
