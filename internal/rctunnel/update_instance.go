package rctunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
)

// updateInstanceMetadata is the metadata half of the rename request.
//
// 【反编译】 the reference's own proto — devtools/jetski/boq/api/proto/
// api.proto, recovered from the FileDescriptorProto the 1.4.11 binary
// carries at file offset 0x3a2c566 — declares InstanceMetadata with
// name (1), version (2) and supports_chunking (3). The reference
// builds it with the name alone: version and supports_chunking stay
// unset, so the JSON carries just the name.
type updateInstanceMetadata struct {
	Name string `json:"name,omitempty"`
}

// updateInstanceRequest is the body of the rename call.
//
// 【反编译】 the same proto declares UpdateInstanceRequest with
// instance_id (1), metadata (2) and update_mask (3), and the reference
// marshals it with protojson. A google.protobuf.FieldMask is written
// by protojson as a comma-separated string, so the update mask travels
// as the literal "metadata.name" — and protojson writes the fields in
// field-number order, which is the order of the struct below.
//
// The wire body is therefore
//
//	{"instanceId":"<id>","metadata":{"name":"<name>"},"updateMask":"metadata.name"}
type updateInstanceRequest struct {
	InstanceID string                 `json:"instanceId,omitempty"`
	Metadata   *updateInstanceMetadata `json:"metadata,omitempty"`
	UpdateMask string                 `json:"updateMask,omitempty"`
}

// updateInstanceTimeout bounds the rename call.
//
// The tunnel's own client has no timeout — it streams — so the rename
// gets its own. Thirty seconds is longer than the cloud answers in
// and short enough that a wedged call does not hold the watcher.
const updateInstanceTimeout = 30 * time.Second

// UpdateInstanceName renames the instance on the cloud.
//
// The rename travels over the API rather than through the tunnel: the
// reference posts it to "{proxyURL}/v1internal:updateInstance" while
// the tunnel session stays up, so the dashboard picks the new name up
// without the instance dropping off and re-registering.
//
// 【反编译】 the reference's own words, all of them reproduced here:
//
//	newName is empty
//	proxyURL is empty
//	failed to marshal request: %w
//	failed to create HTTP request: %w
//	failed to send request: %w
//	request failed with status %d: %s
//
// 【反编译】 the reference treats HTTP 200 as success and reads
// nothing else out of the body; any other status is the failure
// above.
func (c *Client) UpdateInstanceName(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("newName is empty")
	}
	target := c.updateInstanceURL()
	if target == "" {
		return errors.New("proxyURL is empty")
	}

	body, err := json.Marshal(updateInstanceRequest{
		InstanceID: c.opts.InstanceID,
		Metadata:   &updateInstanceMetadata{Name: name},
		UpdateMask: "metadata.name",
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	token, err := c.getAccessToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: updateInstanceTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(answer)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// updateInstanceURL returns the URL the rename posts to.
func (c *Client) updateInstanceURL() string {
	if v := strings.TrimSpace(c.opts.UpdateInstanceEndpoint); v != "" {
		return v
	}
	return endpoints.UpdateInstanceURLFor(c.opts.Endpoint)
}
