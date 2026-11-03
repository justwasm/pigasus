package loginui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/features/auth"
)

func completeCodeExchange(w io.Writer, code, verifier string) error {
	if strings.TrimSpace(code) == "" {
		return errors.New("authentication interrupted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok, err := auth.ExchangeAuthorizationCode(ctx, code, verifier)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	_ = auth.SaveTokenUnified(tok)
	fmt.Fprintln(w, "Authentication successful!")
	return nil
}
