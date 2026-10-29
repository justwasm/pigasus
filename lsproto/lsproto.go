// Package lsproto — the Connect dialect bridge for
// `exa.language_server_pb.LanguageServerService`.
//
// The Antigravity web UI speaks Connect-RPC against the language
// server, and in the v2 "binary unary" protocol it speaks *binary
// protobuf*: the request carries `content-type: application/proto` and
// the answer has to be a binary protobuf body. The clone's dispatcher
// speaks JSON, so without a bridge the UI cannot decode the answer —
// it reports "invalid end group tag" and "premature EOF" and the
// remote-control page spins forever on "Connecting to instance…".
//
// This package owns that bridge. It loads the descriptor closure the
// frontend bundle already embeds (the schema is closed-source; the
// bytes are extracted, never regenerated) and converts between the two
// dialects:
//
//	binary protobuf  →  proto-JSON  →  the dispatcher's JSON
//	the dispatcher's JSON  →  proto-JSON  →  binary protobuf
//
// Evidence:
//   - out/ide-language-server/extract_ls_protoset.py 【一手】
//   - the CLI web bundle's "[XChannelTransport] Unary mode: binary
//     (proto)" log line, observed live in the cloud remote-control page
package lsproto

import (
	"fmt"
	"sort"
	"sync"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	_ "embed"
)

//go:embed descriptor/language_server.protoset
var protoset []byte

// ServiceName is the fully-qualified protobuf service name the web UI
// addresses. It is also the path prefix of every Connect-RPC call:
// "/exa.language_server_pb.LanguageServerService/GetStatus".
const ServiceName = "exa.language_server_pb.LanguageServerService"

// Codec converts between the binary-protobuf and JSON dialects of the
// language-server service.
//
// The zero value is not usable — call New.
type Codec struct {
	files   *protoregistry.Files
	methods map[string]protoreflect.MethodDescriptor
}

// New loads the embedded descriptor closure and indexes the service's
// methods by name.
//
// The descriptor set is parsed once and cached; the web UI makes a few
// hundred calls per session, so the lookup has to be cheap.
func New() (*Codec, error) {
	fds := &descriptorpb.FileDescriptorSet{}
	if err := unmarshalProtoset(protoset, fds); err != nil {
		return nil, fmt.Errorf("lsproto: parse descriptor set: %w", err)
	}
	files, err := newFiles(fds)
	if err != nil {
		return nil, fmt.Errorf("lsproto: build registry: %w", err)
	}
	c := &Codec{files: files, methods: map[string]protoreflect.MethodDescriptor{}}
	svc, err := files.FindDescriptorByName(protoreflect.FullName(ServiceName))
	if err != nil {
		return nil, fmt.Errorf("lsproto: service %s not found in descriptor set", ServiceName)
	}
	service, ok := svc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("lsproto: %s is not a service", ServiceName)
	}
	for i := 0; i < service.Methods().Len(); i++ {
		m := service.Methods().Get(i)
		c.methods[string(m.Name())] = m
	}
	return c, nil
}

// HasMethod reports whether the descriptor set knows the method. An
// unknown method falls back to the JSON dialect.
func (c *Codec) HasMethod(name string) bool {
	if c == nil {
		return false
	}
	_, ok := c.methods[name]
	return ok
}

// MethodCount returns the number of methods the service declares. The
// reference's service has 356.
func (c *Codec) MethodCount() int {
	if c == nil {
		return 0
	}
	return len(c.methods)
}

// InputMessage returns the request message descriptor of a method.
func (c *Codec) InputMessage(method string) (protoreflect.MessageDescriptor, bool) {
	m, ok := c.methods[method]
	if !ok {
		return nil, false
	}
	return m.Input(), true
}

// IsServerStreaming reports whether the method is a server-streaming
// method.
//
// The gRPC-Web protocol does not mark a stream in the request: its
// content type is the same for a unary and a server-streaming call, so
// the server has to know from the method's own descriptor which one it
// is serving.
func (c *Codec) IsServerStreaming(method string) bool {
	if c == nil {
		return false
	}
	m, ok := c.methods[method]
	if !ok {
		return false
	}
	return m.IsStreamingServer()
}

// ServerStreamingMethods returns the names of every server-streaming
// method of the service, sorted.
//
// The list is the schema's own answer to "which calls are streams";
// a hand-written list of them drifts from it.
func (c *Codec) ServerStreamingMethods() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.methods))
	for name, m := range c.methods {
		if m.IsStreamingServer() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// OutputMessage returns the response message descriptor of a method.
func (c *Codec) OutputMessage(method string) (protoreflect.MessageDescriptor, bool) {
	m, ok := c.methods[method]
	if !ok {
		return nil, false
	}
	return m.Output(), true
}

// Files exposes the registry so callers can resolve cross-package
// message types.
func (c *Codec) Files() *protoregistry.Files {
	if c == nil {
		return nil
	}
	return c.files
}

var (
	sharedOnce sync.Once
	shared     *Codec
	sharedErr  error
)

// Shared returns the process-wide codec, building it on first use.
// Every handler that needs the bridge uses this instance so the
// descriptor set is parsed exactly once.
func Shared() (*Codec, error) {
	sharedOnce.Do(func() {
		shared, sharedErr = New()
	})
	return shared, sharedErr
}
