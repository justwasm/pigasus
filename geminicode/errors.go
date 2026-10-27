package geminicode

import (
	"fmt"
	"net/http"
)

// HTTPError is a non-2xx response from the CloudCode endpoint.
//
// It keeps the raw body so the caller can classify the failure: the
// reference tells a quota exhaustion ("Individual quota reached") apart
// from an expired sign-in, and it renders an HTML error page from a
// corporate proxy as its title rather than as raw markup.
//
// The package does not decide what a given status means — that is the
// application's policy — so this type carries the three facts the
// decision needs: the status, the body and the URL that produced them.
type HTTPError struct {
	StatusCode int
	Body       []byte
	URL        string
}

// Error renders the failure as one line.
func (e *HTTPError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("geminicode: upstream %d: %s", e.StatusCode, http.StatusText(e.StatusCode))
}
