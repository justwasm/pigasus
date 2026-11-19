//go:build js || wasm

package agentturn

import (
	"context"
	"errors"

	"github.com/justwasm/pigasus/agentlink"
)

type AHPRemote struct{}

func (a *AHPRemote) Turn(ctx context.Context, t Turn) (<-chan agentlink.Frame, error) {
	return nil, errors.New("ahp remote engine is not supported on wasm")
}

func (a *AHPRemote) Close() error {
	return nil
}

func (a *AHPRemote) DefaultModel() string {
	return ""
}

func (a *AHPRemote) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return nil, nil
}

type AHPRemoteOptions struct {
	SessionURI     string
	ChatURI        string
	Model          string
	CWD            string
	Token          string
	DisposeOnClose bool
}

func DialAHPRemote(ctx context.Context, rawURL, token, cwd string, opts ...AHPRemoteOptions) (*AHPRemote, error) {
	return nil, errors.New("ahp remote engine is not supported on wasm")
}
