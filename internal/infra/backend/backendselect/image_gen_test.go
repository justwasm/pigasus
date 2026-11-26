package backendselect

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
	"github.com/justwasm/pigasus/internal/infra/backend/failover"
	"github.com/justwasm/pigasus/internal/infra/backend/gemini"
)

type imageTestClient struct{}

func (*imageTestClient) Stream(context.Context, backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	return nil, nil
}
func (*imageTestClient) GenerateImage(context.Context, backend.ImageGenRequest) (backend.ImageGenResult, error) {
	return backend.ImageGenResult{Data: []byte("native")}, nil
}

type imageTestWrapper struct{ inner backend.Client }

func (w *imageTestWrapper) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	return w.inner.Stream(ctx, req)
}
func (w *imageTestWrapper) Unwrap() backend.Client { return w.inner }

type imageUnsupportedClient struct{}

func (*imageUnsupportedClient) Stream(context.Context, backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	return nil, nil
}

// Resolver tests use a sentinel environment token: HasOAuthToken returns
// without reading stored credentials, and resolving does not execute requests.
func isolateImageResolver(t *testing.T) {
	t.Helper()
	t.Setenv("JETSKI_OAUTH_TOKEN", "resolver-test-token")
}

// Some transparent wrappers expose both a primary transport and all targets.
// IsMock follows Unwrap, so it only sees the primary mock, not the real secondary.
type imageTargetWrapper struct {
	*imageTestWrapper
	targets []failover.Target
}

func (w *imageTargetWrapper) Targets() []failover.Target { return w.targets }

func TestImageGeneratorForUnwrapsNativeBackends(t *testing.T) {
	isolateImageResolver(t)
	gen := &imageTestClient{}
	for _, tc := range []struct {
		name   string
		client backend.Client
	}{
		{"native", gen},
		{"one wrapper", &imageTestWrapper{inner: gen}},
		{"nested wrappers", &imageTestWrapper{inner: &imageTestWrapper{inner: gen}}},
		{"account pool", accountrouting.Wrap(gen, nil)},
		{"failover secondary", failover.New([]failover.Target{{Client: &imageUnsupportedClient{}}, {Client: gen}}, nil)},
		{"wrapped failover", &imageTestWrapper{inner: failover.New([]failover.Target{{Client: gen}}, nil)}},
		{"mock primary native secondary", failover.New([]failover.Target{{Client: backend.NewMock()}, {Client: gen}}, nil)},
		{"account wrapped mock primary", accountrouting.Wrap(failover.New([]failover.Target{{Client: backend.NewMock()}, {Client: gen}}, nil), nil)},
		{"primary unwrap mock with native target", &imageTargetWrapper{imageTestWrapper: &imageTestWrapper{inner: backend.NewMock()}, targets: []failover.Target{{Client: backend.NewMock()}, {Client: gen}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ImageGeneratorFor(tc.client); got != gen {
				t.Fatalf("generator for %T = %T, want native generator", tc.client, got)
			}
		})
	}
	cc := cloudcode.NewCloudCodeWithConfig(cloudcode.CloudCodeConfig{Endpoint: "http://unused", AccessToken: "test"})
	if got := ImageGeneratorFor(cc); got != cc {
		t.Fatalf("CloudCode generator = %T", got)
	}
	gc := gemini.NewGeminiWithConfig(gemini.GeminiConfig{BaseURL: "http://unused"})
	if got := ImageGeneratorFor(gc); got != gc.(backend.ImageGenerator) {
		t.Fatalf("Gemini generator = %T", got)
	}
}

func TestImageGeneratorForSkipsMockAndNilEvenWithOAuth(t *testing.T) {
	isolateImageResolver(t)
	for _, tc := range []struct {
		name   string
		client backend.Client
	}{
		{"nil", nil},
		{"mock", backend.NewMock()},
		{"wrapped mock", &imageTestWrapper{inner: backend.NewMock()}},
		{"account wrapped mock", accountrouting.Wrap(backend.NewMock(), nil)},
		{"empty wrapper", &imageTestWrapper{}},
		{"empty failover", failover.New(nil, nil)},
		{"mock only failover", failover.New([]failover.Target{{Client: backend.NewMock()}, {Client: backend.NewMock()}}, nil)},
		{"nil and mock failover", failover.New([]failover.Target{{}, {Client: backend.NewMock()}}, nil)},
		{"account wrapped mock failover", accountrouting.Wrap(failover.New([]failover.Target{{Client: backend.NewMock()}}, nil), nil)},
		{"primary unwrap mock with only mock targets", &imageTargetWrapper{imageTestWrapper: &imageTestWrapper{inner: backend.NewMock()}, targets: []failover.Target{{Client: backend.NewMock()}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if imageBackendNeedsOAuthFallback(tc.client) {
				t.Fatal("mock-only or empty graph must not probe OAuth availability")
			}
			if got := ImageGeneratorFor(tc.client); got != nil {
				t.Fatalf("generator for %T = %T, want nil", tc.client, got)
			}
		})
	}
}

func TestImageGeneratorForOAuthFallback(t *testing.T) {
	isolateImageResolver(t)
	for _, client := range []backend.Client{
		&imageUnsupportedClient{},
		failover.New([]failover.Target{{Client: backend.NewMock()}, {Client: &imageUnsupportedClient{}}}, nil),
		&imageTargetWrapper{imageTestWrapper: &imageTestWrapper{inner: backend.NewMock()}, targets: []failover.Target{{Client: &imageUnsupportedClient{}}}},
	} {
		got := ImageGeneratorFor(client)
		if _, ok := got.(*cloudcode.CloudCodeClient); !ok {
			t.Fatalf("OAuth fallback generator for %T = %T, want CloudCode", client, got)
		}
	}
}

func TestFindImageGeneratorUnsupported(t *testing.T) {
	for _, client := range []backend.Client{nil, &imageUnsupportedClient{}, &imageTestWrapper{}, failover.New([]failover.Target{{Client: backend.NewMock()}}, nil)} {
		if got := findImageGenerator(client); got != nil {
			t.Fatalf("native generator for %T = %T, want nil", client, got)
		}
	}
}
