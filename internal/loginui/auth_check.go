package loginui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// RunPrintModeAuthIfNeeded checks if OAuth credentials exist before running
// a headless prompt.
func RunPrintModeAuthIfNeeded(w io.Writer, r io.Reader) error {
	if auth.HasOAuthToken() || os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("AGY_ADC_AUTH") != "" {
		return nil
	}
	verifier, challenge, state, err := auth.GeneratePKCE()
	if err != nil {
		return err
	}
	authURL := auth.BuildAuthorizationURL(challenge, state)
	fmt.Fprintf(w, "Authentication required. Please visit the URL to log in:\n  %s\n\nWaiting for authentication (timeout 60s)...\nOr, paste the authorization code here and press Enter:\n", authURL)
	_ = auth.OpenBrowser(authURL)

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)
	go func() {
		code, rerr := auth.ReadAuthorizationCode(r)
		if rerr != nil {
			errChan <- rerr
			return
		}
		codeChan <- code
	}()

	select {
	case code := <-codeChan:
		return completeCodeExchange(w, code, verifier)
	case err := <-errChan:
		return fmt.Errorf("authentication failed: %w", err)
	case <-time.After(60 * time.Second):
		return errors.New("authentication timed out")
	}
}

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
