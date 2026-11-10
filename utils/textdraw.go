package utils

import (
	"image"
	"image/color"
	"image/draw"

	xdraw "golang.org/x/image/draw"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// NewFace parses TTF bytes into a font.Face at sizePt (72 DPI).
func NewFace(ttf []byte, sizePt float64) (font.Face, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(parsed, &opentype.FaceOptions{
		Size: sizePt, DPI: 72, Hinting: font.HintingFull,
	})
}

// WrapLines greedily word-wraps text so no line exceeds maxWidth.
func WrapLines(face font.Face, text string, maxWidth int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	lines := []string{words[0]}
	for _, w := range words[1:] {
		cur := lines[len(lines)-1]
		if font.MeasureString(face, cur+" "+w).Ceil() <= maxWidth {
			lines[len(lines)-1] = cur + " " + w
			continue
		}
		lines = append(lines, w)
	}
	return lines
}

// DrawLinesCentered paints lines as one block, horizontally and vertically
// centred inside box.
func DrawLinesCentered(dst draw.Image, face font.Face, lines []string, box image.Rectangle, lineHeight int, ink color.Color) {
	if len(lines) == 0 {
		return
	}
	block := lineHeight * len(lines)
	y := box.Min.Y + (box.Dy()-block)/2 + face.Metrics().Ascent.Ceil()

	d := &font.Drawer{Dst: dst, Src: image.NewUniform(ink), Face: face}
	for i, ln := range lines {
		w := font.MeasureString(face, ln).Ceil()
		d.Dot = fixed.P(box.Min.X+(box.Dx()-w)/2, y+i*lineHeight)
		d.DrawString(ln)
	}
}

// DrawOutlinedCentered draws a single line centred on centerX with a stroke
// outline, the classic meme look.
func DrawOutlinedCentered(dst draw.Image, face font.Face, text string, centerX, baselineY int, fill, stroke color.Color, strokeW int) {
	if strings.TrimSpace(text) == "" {
		return
	}
	x := centerX - font.MeasureString(face, text).Ceil()/2

	if strokeW > 0 {
		sd := &font.Drawer{Dst: dst, Src: image.NewUniform(stroke), Face: face}
		for dx := -strokeW; dx <= strokeW; dx++ {
			for dy := -strokeW; dy <= strokeW; dy++ {
				if dx == 0 && dy == 0 {
					continue
				}
				sd.Dot = fixed.P(x+dx, baselineY+dy)
				sd.DrawString(text)
			}
		}
	}

	fd := &font.Drawer{Dst: dst, Src: image.NewUniform(fill), Face: face}
	fd.Dot = fixed.P(x, baselineY)
	fd.DrawString(text)
}

// FitSquare scales src to fit inside a size x size transparent canvas,
// centred, using high-quality resampling.
func FitSquare(src image.Image, size int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	sb := src.Bounds()
	if sb.Dx() == 0 || sb.Dy() == 0 {
		return out
	}

	scale := float64(size) / float64(max(sb.Dx(), sb.Dy()))
	w := int(float64(sb.Dx()) * scale)
	h := int(float64(sb.Dy()) * scale)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}

	dstRect := image.Rect((size-w)/2, (size-h)/2, (size-w)/2+w, (size-h)/2+h)
	xdraw.CatmullRom.Scale(out, dstRect, src, sb, draw.Over, nil)
	return out
}
