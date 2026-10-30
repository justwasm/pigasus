// Package mediagen is a backward-compatible facade over internal/features/tools/media.
package mediagen

import (
	"github.com/justwasm/pigasus/internal/features/tools/media"
)

var (
	GenerateImage = media.GenerateImage
	GenerateAudio = media.GenerateAudio
	GenerateVideo = media.GenerateVideo
)
