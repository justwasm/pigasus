package mockremoteagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a Go client for interacting with JetskiRemoteAgentProxyService,
// matching the behavior and method signatures of agy's CodeAssistClient.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new remote agent client targeting the given base URL.
func NewClient(baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) rpcURL(method string) string {
	return fmt.Sprintf("%s/google.gca.aicode.v1main.JetskiRemoteAgentProxyService/%s", c.baseURL, method)
}

func (c *Client) post(ctx context.Context, method string, req any, resp any) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL(method), bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("remote agent RPC %s failed with status %d: %s", method, httpResp.StatusCode, string(body))
	}

	if resp != nil {
		return json.NewDecoder(httpResp.Body).Decode(resp)
	}
	return nil
}

// EnsureEnvironment calls EnsureEnvironment on the remote agent.
func (c *Client) EnsureEnvironment(ctx context.Context, req EnsureEnvironmentRequest) (*EnsureEnvironmentResponse, error) {
	var resp EnsureEnvironmentResponse
	err := c.post(ctx, "EnsureEnvironment", req, &resp)
	return &resp, err
}

// ListEnvironments calls ListEnvironments on the remote agent.
func (c *Client) ListEnvironments(ctx context.Context) (*ListEnvironmentsResponse, error) {
	var resp ListEnvironmentsResponse
	err := c.post(ctx, "ListEnvironments", map[string]any{}, &resp)
	return &resp, err
}

// ListAvailableModels calls ListAvailableModels on the remote agent.
func (c *Client) ListAvailableModels(ctx context.Context) (*ListAvailableModelsResponse, error) {
	var resp ListAvailableModelsResponse
	err := c.post(ctx, "ListAvailableModels", map[string]any{}, &resp)
	return &resp, err
}

// ListSlashCommands calls ListSlashCommands on the remote agent.
func (c *Client) ListSlashCommands(ctx context.Context) (*ListSlashCommandsResponse, error) {
	var resp ListSlashCommandsResponse
	err := c.post(ctx, "ListSlashCommands", map[string]any{}, &resp)
	return &resp, err
}

// StartCascade starts a new cascade session on the remote agent.
func (c *Client) StartCascade(ctx context.Context, req StartCascadeRequest) (*StartCascadeResponse, error) {
	var resp StartCascadeResponse
	err := c.post(ctx, "StartCascade", req, &resp)
	return &resp, err
}

// SendUserCascadeMessage sends a user prompt message to the remote cascade session.
func (c *Client) SendUserCascadeMessage(ctx context.Context, cascadeID, text string) (*SendUserCascadeMessageResponse, error) {
	req := SendUserCascadeMessageRequest{
		CascadeID: cascadeID,
		Text:      text,
	}
	var resp SendUserCascadeMessageResponse
	err := c.post(ctx, "SendUserCascadeMessage", req, &resp)
	return &resp, err
}

// CancelCascade cancels an active cascade turn on the remote agent.
func (c *Client) CancelCascade(ctx context.Context, cascadeID string) (*CancelCascadeResponse, error) {
	req := CancelCascadeRequest{CascadeID: cascadeID}
	var resp CancelCascadeResponse
	err := c.post(ctx, "CancelCascade", req, &resp)
	return &resp, err
}

// StreamAgentStateUpdates connects to the server-streaming state updates endpoint and
// delivers trajectory update frames via a channel until the context is cancelled.
func (c *Client) StreamAgentStateUpdates(ctx context.Context, conversationID string) (<-chan StreamAgentStateUpdatesResponse, <-chan error, error) {
	req := StreamAgentStateUpdatesRequest{ConversationID: conversationID}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}

	url := fmt.Sprintf("%s?conversationId=%s", c.rpcURL("StreamAgentStateUpdates"), conversationID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	// Use custom client with no timeout for long-lived stream
	streamClient := &http.Client{}
	httpResp, err := streamClient.Do(httpReq)
	if err != nil {
		return nil, nil, err
	}

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, nil, fmt.Errorf("StreamAgentStateUpdates failed with %d: %s", httpResp.StatusCode, string(body))
	}

	frames := make(chan StreamAgentStateUpdatesResponse, 16)
	errs := make(chan error, 1)

	go func() {
		defer httpResp.Body.Close()
		defer close(frames)
		defer close(errs)

		scanner := bufio.NewScanner(httpResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				payload := strings.TrimPrefix(line, "data: ")
				var frame StreamAgentStateUpdatesResponse
				if err := json.Unmarshal([]byte(payload), &frame); err == nil {
					select {
					case <-ctx.Done():
						return
					case frames <- frame:
					}
				}
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errs <- err
		}
	}()

	return frames, errs, nil
}

// GetConversation fetches the complete trajectory of a conversation.
func (c *Client) GetConversation(ctx context.Context, conversationID string) (*GetConversationResponse, error) {
	req := GetConversationRequest{ConversationID: conversationID}
	var resp GetConversationResponse
	err := c.post(ctx, "GetConversation", req, &resp)
	return &resp, err
}

// ListConversations lists active conversations on the remote agent.
func (c *Client) ListConversations(ctx context.Context) (*ListConversationsResponse, error) {
	var resp ListConversationsResponse
	err := c.post(ctx, "ListConversations", map[string]any{}, &resp)
	return &resp, err
}

// ReadFile reads a file from the remote agent's environment.
func (c *Client) ReadFile(ctx context.Context, path string) (*ReadFileResponse, error) {
	req := ReadFileRequest{Path: path}
	var resp ReadFileResponse
	err := c.post(ctx, "ReadFile", req, &resp)
	return &resp, err
}

// ReadDir reads directory entries from the remote agent's environment.
func (c *Client) ReadDir(ctx context.Context, path string) (*ReadDirResponse, error) {
	req := ReadDirRequest{Path: path}
	var resp ReadDirResponse
	err := c.post(ctx, "ReadDir", req, &resp)
	return &resp, err
}
