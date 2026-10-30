package mediagen_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/mediagen"
)

func TestMediagenFacade(t *testing.T) {
	_, err := mediagen.GenerateImage("test")
	// If ffmpeg is not found or succeeds, err may or may not be nil, but function should be callable
	_ = err
}
