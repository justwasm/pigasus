package rctunnel

import (
	"crypto/rand"
	"fmt"
	"io"
)

// Instance represents a running language server on loopback.
type Instance struct {
	PID       int
	Port      int
	CSRFToken string
}

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
	Payload     []byte            `json:"payload,omitempty"`
	StatusCode  int               `json:"statusCode,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	IsFinal     bool              `json:"isFinal,omitempty"`
	PayloadText string            `json:"payloadText,omitempty"`
}

// BaseURL returns the local HTTPS origin the instance serves.
func (i *Instance) BaseURL() string {
	return fmt.Sprintf("https://127.0.0.1:%d", i.Port)
}

// Config specifies the runtime tunnel and supervisor options.
type Config struct {
	BinaryPath   string
	GeminiDir    string
	Addr         string
	Name         string
	Account      string
	CSRFToken    string
	NoLogin      bool
	LogWriter    io.Writer
}

// NewCSRFToken generates a UUID for the language server --csrf_token flag.
func NewCSRFToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "pigasus-rc-csrf-token"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
