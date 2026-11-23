package graphic

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// RenderImage renders an image.Image using the terminal's detected inline
// graphics protocol, falling back to 24-bit ANSI half-block rendering when
// ProtocolNone is active or opts.ForceASCII is true.
func RenderImage(img image.Image, opts ImageOptions, caps Capabilities) (string, error) {
	if img == nil {
		return "", fmt.Errorf("graphic: nil image")
	}
	if opts.ForceASCII || caps.Protocol == ProtocolNone {
		return RenderHalfBlock(img, opts.Cols, opts.Rows), nil
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("graphic: encode png: %w", err)
	}
	return RenderPNG(buf.Bytes(), opts, caps)
}

// RenderPNG renders raw PNG bytes using the terminal's detected inline
// graphics protocol, decoding and falling back to 24-bit ANSI half-block
// rendering when ProtocolNone is active or opts.ForceASCII is true.
func RenderPNG(pngBytes []byte, opts ImageOptions, caps Capabilities) (string, error) {
	if len(pngBytes) == 0 {
		return "", fmt.Errorf("graphic: empty PNG data")
	}
	if opts.ForceASCII || caps.Protocol == ProtocolNone {
		img, err := png.Decode(bytes.NewReader(pngBytes))
		if err != nil {
			return "", fmt.Errorf("graphic: decode png for fallback: %w", err)
		}
		return RenderHalfBlock(img, opts.Cols, opts.Rows), nil
	}

	switch caps.Protocol {
	case ProtocolKitty:
		return EncodeKittyPNG(pngBytes, opts), nil
	case ProtocolITerm2:
		return EncodeITerm2(pngBytes, opts), nil
	default:
		img, err := png.Decode(bytes.NewReader(pngBytes))
		if err != nil {
			return "", fmt.Errorf("graphic: decode png for fallback: %w", err)
		}
		return RenderHalfBlock(img, opts.Cols, opts.Rows), nil
	}
}

// RenderHalfBlock renders img as Unicode half-block characters ('▀' / '▄')
// with 24-bit ANSI truecolor foreground and background escape sequences.
// Each terminal row encodes two vertical pixels. If maxCols or maxRows is > 0,
// the image is scaled down proportionally to fit within those cell bounds.
func RenderHalfBlock(img image.Image, maxCols, maxRows int) string {
	if img == nil {
		return ""
	}
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return ""
	}

	dstW, dstH := fitHalfBlockDimensions(srcW, srcH, maxCols, maxRows)

	var out strings.Builder
	rows := (dstH + 1) / 2
	out.Grow(rows * dstW * 24)

	for y := 0; y < dstH; y += 2 {
		if y > 0 {
			out.WriteByte('\n')
		}
		styled := false
		for x := 0; x < dstW; x++ {
			sx := b.Min.X + (x*srcW)/dstW
			syTop := b.Min.Y + (y*srcH)/dstH
			top := color.NRGBAModel.Convert(img.At(sx, syTop)).(color.NRGBA)
			topOpaque := top.A >= 128

			var bot color.NRGBA
			botOpaque := false
			if y+1 < dstH {
				syBot := b.Min.Y + ((y+1)*srcH)/dstH
				bot = color.NRGBAModel.Convert(img.At(sx, syBot)).(color.NRGBA)
				botOpaque = bot.A >= 128
			}

			switch {
			case topOpaque && botOpaque:
				fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
					top.R, top.G, top.B, bot.R, bot.G, bot.B)
				styled = true
			case topOpaque:
				if styled {
					out.WriteString("\x1b[0m")
				}
				fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm▀", top.R, top.G, top.B)
				styled = true
			case botOpaque:
				if styled {
					out.WriteString("\x1b[0m")
				}
				fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm▄", bot.R, bot.G, bot.B)
				styled = true
			default:
				if styled {
					out.WriteString("\x1b[0m")
					styled = false
				}
				out.WriteByte(' ')
			}
		}
		if styled {
			out.WriteString("\x1b[0m")
		}
	}

	return out.String()
}

func fitHalfBlockDimensions(srcW, srcH, maxCols, maxRows int) (int, int) {
	dstW, dstH := srcW, srcH
	if maxCols > 0 && dstW > maxCols {
		dstH = max(1, (dstH*maxCols)/dstW)
		dstW = maxCols
	}
	if maxRows > 0 {
		maxPxH := maxRows * 2
		if dstH > maxPxH {
			dstW = max(1, (dstW*maxPxH)/dstH)
			dstH = maxPxH
		}
	}
	return dstW, dstH
}

var queenBeeSprite = [12]string{
	".....C.CC.C.....",
	".....CCCCCC.....",
	"..WW..KKKK..WW..",
	".WWWWKYYYYKWWWW.",
	".WWWwKYKKYKWWWW.",
	"..WWwKYYYYKwWW..",
	"....KKKKKKKK....",
	"...KYYYYYYYYK...",
	"...KKKKKKKKKK...",
	"....KYYYYYYK....",
	".....KKKKKK.....",
	".......KK.......",
}

var queenBeePalette = map[byte]color.NRGBA{
	'C': {R: 255, G: 215, B: 0, A: 255},   // Crown gold
	'K': {R: 26, G: 26, B: 26, A: 255},    // Outline / stripes / eyes
	'Y': {R: 250, G: 204, B: 21, A: 255},  // Bee yellow
	'W': {R: 224, G: 242, B: 254, A: 255}, // Wing highlight
	'w': {R: 186, G: 230, B: 253, A: 255}, // Wing shadow
}

// QueenBeeImage returns the crisp 16x12 pixel-art Queen Bee sprite.
func QueenBeeImage() image.Image {
	const w, h = 16, 12
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y, row := range queenBeeSprite {
		for x := 0; x < len(row) && x < w; x++ {
			if c, ok := queenBeePalette[row[x]]; ok {
				img.SetNRGBA(x, y, c)
			}
		}
	}
	return img
}

// RenderQueenBeePixelArt renders the 16x12 pixel-art Queen Bee for /swarm board
// and terminal previews. When the active environment supports Kitty graphics
// (ProtocolKitty), it emits a Kitty PNG sequence; otherwise it renders using
// 24-bit ANSI half-blocks.
func RenderQueenBeePixelArt(cols int) string {
	return RenderQueenBee(cols, DetectEnv())
}

// RenderQueenBee renders the 16x12 pixel-art Queen Bee using the supplied
// terminal capabilities.
func RenderQueenBee(cols int, caps Capabilities) string {
	if cols <= 0 {
		cols = 16
	}
	bee := QueenBeeImage()
	if caps.Protocol == ProtocolKitty {
		var buf bytes.Buffer
		if err := png.Encode(&buf, bee); err == nil {
			return EncodeKittyPNG(buf.Bytes(), ImageOptions{
				Cols:  cols,
				Quiet: true,
			})
		}
	}
	return RenderHalfBlock(bee, cols, 0)
}
