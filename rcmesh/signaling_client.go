package rcmesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// PollRequest mirrors api.proto's poll_target oneof. Exactly one target is required.
type PollRequest struct {
	SessionID             string `json:"sessionId,omitempty"`
	InstanceID            string `json:"instanceId,omitempty"`
	AfterTimestamp        string `json:"afterTimestamp,omitempty"`
	IncludeAuthorizedKeys bool   `json:"includeAuthorizedKeys,omitempty"`
}
type AuthorizedKeyBundle struct {
	WebCryptoPubKeys []string `json:"webCryptoPubKeys,omitempty"`
	Delivered        bool     `json:"delivered,omitempty"`
}
type SignalingMessageDetails struct {
	SessionID string `json:"sessionId,omitempty"`
	Payload   string `json:"payload,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}
type PollResponse struct {
	Messages            []SignalingMessageDetails `json:"messages,omitempty"`
	AuthorizedKeyBundle *AuthorizedKeyBundle      `json:"authorizedKeyBundle,omitempty"`
}
type SendRequest struct {
	SessionID   string `json:"sessionId,omitempty"`
	Payload     string `json:"payload,omitempty"`
	MessageID   string `json:"messageId,omitempty"`
	SenderID    string `json:"senderId,omitempty"`
	RecipientID string `json:"recipientId,omitempty"`
	PayloadType string `json:"payloadType,omitempty"`
}

// SignalingClient receives complete method URLs and an authenticated HTTP client
// or TokenFunc from its host. It has no compiled-in endpoints, token storage or
// retry policy. Redirects are refused so credentials cannot follow an endpoint redirect.
type SignalingClient struct {
	HTTPClient *http.Client
	TokenFunc  func(context.Context) (string, error)
	PollURL    string
	SendURL    string
}

func (c SignalingClient) Poll(ctx context.Context, request PollRequest) (PollResponse, error) {
	var response PollResponse
	if (request.SessionID == "") == (request.InstanceID == "") {
		return response, errors.New("rcmesh: exactly one polling target is required")
	}
	if request.AfterTimestamp != "" {
		if _, err := time.Parse(time.RFC3339Nano, request.AfterTimestamp); err != nil {
			return response, errors.New("rcmesh: invalid polling timestamp")
		}
	}
	err := c.call(ctx, c.PollURL, request, &response)
	return response, err
}

// Send posts one signaling message.
//
// 【反编译】 the request's payload member carries the *bare* content, never
// the envelope: postSignalingMessage (0x1025a6dc0) marshals the
// SendSignalingMessageRequest with protojson and posts it as it stands, and
// the two callers fill the payload with what they marshalled themselves —
// sendSignalingAnswer (0x1025a6690) puts the marshalled pion
// SessionDescription there ("%s Failed to marshal SDP answer: %v"), and
// sendSignalingCandidate (0x1025a6b00) puts the marshalled ICECandidateInit
// there ("%s Failed to unmarshal ICE candidate: %v" on the inbound side).
// The routing lives in the request's own members (message_id, sender_id,
// recipient_id, payload_type) — the envelope only exists on the inbound
// path, where SignalingMessageDetails.payload *is* the envelope.
func (c SignalingClient) Send(ctx context.Context, request SendRequest) error {
	if request.SessionID == "" || request.Payload == "" {
		return errors.New("rcmesh: session and signaling payload are required")
	}
	if !json.Valid([]byte(request.Payload)) {
		return errors.New("rcmesh: signaling payload is not valid JSON")
	}
	return c.call(ctx, c.SendURL, request, new(struct{}))
}
func (c SignalingClient) call(ctx context.Context, endpoint string, input, output any) error {
	target, err := url.Parse(endpoint)
	if err != nil || target.Host == "" || (target.Scheme != "https" && target.Scheme != "http") || target.User != nil || target.Fragment != "" {
		return errors.New("rcmesh: invalid signaling endpoint")
	}
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if c.TokenFunc != nil {
		tok, err := c.TokenFunc(callCtx)
		if err != nil {
			return fmt.Errorf("rcmesh: authorization token failed: %w", err)
		}
		if tok != "" {
			request.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	client := http.Client{}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("rcmesh: signaling request failed: %w", err)
	}
	defer response.Body.Close()
	// 【反编译】 the reference treats HTTP 200 as the only success for the
	// signaling calls: the poll path checks "status != 200" and answers
	// "pollSignalingMessages returned status %d", and the send path answers
	// "%s unexpected status code: %d".
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("rcmesh: signaling HTTP status %d", response.StatusCode)
	}
	const limit = 4 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return err
	}
	if len(body) > limit {
		return errors.New("rcmesh: signaling response too large")
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("rcmesh: invalid signaling response: %w", err)
	}
	return nil
}
