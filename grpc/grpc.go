// Package grpc is the recovered v4 antigravity-recovered gRPC service
// registry. It exposes the 6 gRPC services that the v4 binary advertises
// on its gRPC reflection surface (or via static dispatcher tables):
//
//   - exa.api_server_pb.ApiServerService
//   - exa.extension_server_pb.ExtensionServerService
//   - google.gca.aicode.v1main.JetskiRemoteAgentProxyService
//   - exa.knowledge_base_pb.KnowledgeBaseService
//   - exa.language_server_pb.LanguageServerService
//   - exa.seat_management_pb.SeatManagementService
//
// Each service exposes a Name() and Description() method via the Service
// interface. The AllServices slice and ListServiceNames() helper make it
// trivial to dump the gRPC surface from a subcommand (e.g.
// `antigravity --services`).
//
// The merged package consolidates the cycle-25 codeassistclient (5 .go)
// and language_server_pb (2 .go) packages. The codeassistclient logic now
// lives under api_server.go (it is the client that dials the
// exa.api_server_pb service); the language_server_pb logic lives under
// language_server.go (it is the recovered binding for
// exa.language_server_pb).
package grpc

// Service is the recovered v4 service descriptor. Every gRPC service
// stub in this package implements Name() and Description(), so the
// surface can be enumerated uniformly.
type Service interface {
	// Name returns the fully-qualified proto service name
	// (e.g. "exa.api_server_pb.ApiServerService").
	Name() string
	// Description returns a short human-readable description of the
	// service for the CLI surface (e.g. `antigravity --services`).
	Description() string
}

// AllServices is the v4-aligned registry. The order matches the order in
// the v4 binary's gRPC service table.
var AllServices = []Service{
	&ApiServerService{},
	&ExtensionServerService{},
	&JetskiRemoteAgentProxyService{},
	&KnowledgeBaseService{},
	&LanguageServerService{},
	&SeatManagementService{},
}

// ListServiceNames returns the fully-qualified proto service names of
// every registered service. Useful for the `antigravity --services`
// CLI output.
func ListServiceNames() []string {
	names := make([]string, len(AllServices))
	for i, s := range AllServices {
		names[i] = s.Name()
	}
	return names
}
