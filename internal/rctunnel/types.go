package rctunnel

import (
	"encoding/base64"
	"encoding/json"
)

// InstanceMetadata represents metadata about this instance sent during registration.
type InstanceMetadata struct {
	Name             string `json:"name,omitempty"`
	Version          string `json:"version,omitempty"`
	SupportsChunking bool   `json:"supportsChunking,omitempty"`
}

// ConnectInstanceRequestV2 is the outer envelope sent over WebChannel to Google Cloud Tunnel.
type ConnectInstanceRequestV2 struct {
	Metadata            *InstanceMetadata `json:"metadata,omitempty"`
	InstanceID          string            `json:"instanceId,omitempty"`
	ProxyResponse       *ProxyResponseV2  `json:"proxyResponse,omitempty"`
	SupportsPayloadText bool              `json:"supportsPayloadText,omitempty"`
}

// ConnectInstanceResponseV2 is the outer envelope received from Google Cloud Tunnel.
type ConnectInstanceResponseV2 struct {
	InstanceUUID string          `json:"instanceUuid,omitempty"`
	ProxyRequest *ProxyRequestV2 `json:"proxyRequest,omitempty"`
}

// ProxyRequestV2 represents an inbound RPC routed from Google Cloud Tunnel.
type ProxyRequestV2 struct {
	RequestID   string            `json:"requestId,omitempty"`
	RPCPath     string            `json:"rpcPath,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	PayloadText string            `json:"payloadText,omitempty"`
	IsFinal     bool              `json:"isFinal,omitempty"`
}

// ProxyResponseV2 represents an outbound RPC response returned to Google Cloud Tunnel.
type ProxyResponseV2 struct {
	RequestID   string            `json:"requestId,omitempty"`
	Payload     []byte            `json:"-"`
	StatusCode  int               `json:"statusCode,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	IsFinal     bool              `json:"isFinal,omitempty"`
	PayloadText string            `json:"-"`
}

// MarshalJSON ensures that when Payload is non-nil (even if 0-length bytes),
// it is serialized as "payload": "<base64>" (or "payload": "") rather than
// omitted by omitempty. The reference's protojson descriptor treats payload and
// payloadText as a oneof where even an empty 0-byte payload preserves oneof presence.
func (r *ProxyResponseV2) MarshalJSON() ([]byte, error) {
	type raw ProxyResponseV2
	aux := struct {
		*raw
		Payload     *string `json:"payload,omitempty"`
		PayloadText *string `json:"payloadText,omitempty"`
	}{
		raw: (*raw)(r),
	}
	if r.Payload != nil {
		b64 := base64.StdEncoding.EncodeToString(r.Payload)
		aux.Payload = &b64
	}
	if r.PayloadText != "" {
		aux.PayloadText = &r.PayloadText
	}
	return json.Marshal(aux)
}
