// Package clipboard is a backward-compatible facade over internal/features/tools/clipboard.
package clipboard

import (
	featureclip "github.com/justwasm/pigasus/internal/features/tools/clipboard"
)

var (
	Reader = featureclip.Reader
	Writer = featureclip.Writer
	Read   = featureclip.Read
	Write  = featureclip.Write
)

type Media = featureclip.Media

var (
	ErrNoMedia      = featureclip.ErrNoMedia
	ErrSSHClipboard = featureclip.ErrSSHClipboard
)

type ToolMissingError = featureclip.ToolMissingError

var (
	ReadMedia      = featureclip.ReadMedia
	MediaFromPath  = featureclip.MediaFromPath
	SupportsOSC52  = featureclip.SupportsOSC52
	WriteOSC52     = featureclip.WriteOSC52
)

type PasteImage = featureclip.PasteImage
type Options = featureclip.Options

var DefaultOptions = featureclip.DefaultOptions

type ImageSource = featureclip.ImageSource

var (
	BytesSource   = featureclip.BytesSource
	ReadImage     = featureclip.ReadImage
	AcceptPaste   = featureclip.AcceptPaste
	NormalizeText = featureclip.NormalizeText
)
