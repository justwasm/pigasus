// errors.go — the typed errors the provider transport produces.
package models

import (
	"errors"
	"fmt"
)

// UpstreamError is a non-2xx answer from the provider's endpoint.
//
// It exists so a caller can recover the HTTP status without parsing the
// message: the verification probe turns 401 into "the credential was
// rejected" and 429 into "the endpoint is rate-limiting", and the two
// call for different fixes.
//
// The message is the one the transport has always produced — "upstream
// 401: …" — so nothing that prints it changes.
type UpstreamError struct {
	// Status is the HTTP status code.
	Status int
	// Body is the upstream's answer, truncated.
	Body string
}

// Error implements error.
func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream %d: %s", e.Status, e.Body)
}

// UpstreamStatus returns the HTTP status an error carries, or 0 when the
// error did not come from an upstream response.
func UpstreamStatus(err error) int {
	var ue *UpstreamError
	if errors.As(err, &ue) {
		return ue.Status
	}
	return 0
}
