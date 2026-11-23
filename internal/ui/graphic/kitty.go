package graphic

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const kittyMaxChunkSize = 4096

// ImageOptions configures inline image encoding and fallback rendering.
type ImageOptions struct {
	ID          uint32
	PlacementID uint32
	Cols        int
	Rows        int
	Quiet       bool
	ZIndex      int
	ForceASCII  bool
	Name        string
}

// EncodeKittyPNG encodes PNG bytes into one or more Kitty Graphics Protocol
// APC escape sequences, chunking the base64 payload into <= 4096-byte chunks.
func EncodeKittyPNG(pngBytes []byte, opts ImageOptions) string {
	if len(pngBytes) == 0 {
		return ""
	}

	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	total := len(encoded)

	var b strings.Builder
	// Pre-size roughly for encoded payload + APC framing overhead.
	b.Grow(total + (total/kittyMaxChunkSize+1)*48)

	for offset, chunkIdx := 0, 0; offset < total; offset, chunkIdx = offset+kittyMaxChunkSize, chunkIdx+1 {
		end := offset + kittyMaxChunkSize
		more := 1
		if end >= total {
			end = total
			more = 0
		}
		chunk := encoded[offset:end]

		b.WriteString("\x1b_G")
		if chunkIdx == 0 {
			b.WriteString("a=T,f=100")
			if opts.ID > 0 {
				b.WriteString(",i=")
				b.WriteString(strconv.FormatUint(uint64(opts.ID), 10))
			}
			if opts.PlacementID > 0 {
				b.WriteString(",p=")
				b.WriteString(strconv.FormatUint(uint64(opts.PlacementID), 10))
			}
			if opts.Cols > 0 {
				b.WriteString(",c=")
				b.WriteString(strconv.Itoa(opts.Cols))
			}
			if opts.Rows > 0 {
				b.WriteString(",r=")
				b.WriteString(strconv.Itoa(opts.Rows))
			}
			if opts.Quiet {
				b.WriteString(",q=2")
			}
			if opts.ZIndex != 0 {
				b.WriteString(",z=")
				b.WriteString(strconv.Itoa(opts.ZIndex))
			}
			b.WriteString(",m=")
			b.WriteString(strconv.Itoa(more))
		} else {
			b.WriteString("m=")
			b.WriteString(strconv.Itoa(more))
		}
		b.WriteByte(';')
		b.WriteString(chunk)
		b.WriteString("\x1b\\")
	}

	return b.String()
}

// DeleteKittyImage returns the Kitty Graphics Protocol sequence to delete
// the image with the given ID quietly (q=2).
func DeleteKittyImage(id uint32) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}
