package remotetransport

import (
	"context"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Use the upstream client, both protocols and both codecs, with compressed
// requests. This verifies interoperability rather than mirroring our adapter.
func TestConnectGoClientInteroperability(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()
	input, _ := srv.protoCodec.InputMessage("GetStatus")
	output, _ := srv.protoCodec.OutputMessage("GetStatus")
	for _, grpcWeb := range []bool{false, true} {
		for _, useJSON := range []bool{false, true} {
			name := "connect"
			if grpcWeb {
				name = "grpc-web"
			}
			if useJSON {
				name += "/json"
			} else {
				name += "/proto"
			}
			t.Run(name, func(t *testing.T) {
				opts := []connect.ClientOption{connect.WithSendGzip(), connect.WithCompressMinBytes(0), connect.WithResponseInitializer(func(_ connect.Spec, value any) error {
					*value.(*dynamicpb.Message) = *dynamicpb.NewMessage(output)
					return nil
				})}
				if grpcWeb {
					opts = append(opts, connect.WithGRPCWeb())
				}
				if useJSON {
					opts = append(opts, connect.WithProtoJSON())
				}
				client := connect.NewClient[dynamicpb.Message, dynamicpb.Message](http.DefaultClient, "http://"+srv.Addr().String()+"/exa.language_server_pb.LanguageServerService/GetStatus", opts...)
				reply, err := client.CallUnary(context.Background(), connect.NewRequest(dynamicpb.NewMessage(input)))
				if err != nil {
					t.Fatal(err)
				}
				status := output.Fields().ByName("status")
				if !reply.Msg.Has(status) {
					t.Fatal("response lost its status message")
				}
			})
		}
	}
}
