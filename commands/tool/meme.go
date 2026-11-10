package tool

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"bot/assets"
	"bot/utils"
)

const (
	memeSize   = 512
	memeFontPt = 38
	memeMargin = 14
)

// renderMemePNG composites the source image onto a 512x512 canvas and draws
// outlined top/bottom captions. All local - no Catbox, no memegen.
func renderMemePNG(src []byte, top, bottom string) ([]byte, error) {
	decoded, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	canvas := utils.FitSquare(decoded, memeSize)

	face, err := utils.NewFace(assets.FontLiberationSansBold, memeFontPt)
	if err != nil {
		return nil, fmt.Errorf("font: %w", err)
	}
	defer face.Close()

	top = strings.ToUpper(strings.TrimSpace(top))
	bottom = strings.ToUpper(strings.TrimSpace(bottom))

	if top != "" {
		lines := utils.WrapLines(face, top, memeSize-2*memeMargin)
		y := memeMargin + face.Metrics().Ascent.Ceil()
		for _, ln := range lines {
			utils.DrawOutlinedCentered(canvas, face, ln, memeSize/2, y, color.White, color.Black, 3)
			y += face.Metrics().Height.Ceil()
		}
	}

	if bottom != "" {
		lines := utils.WrapLines(face, bottom, memeSize-2*memeMargin)
		y := memeSize - memeMargin
		for i := len(lines) - 1; i >= 0; i-- {
			y -= face.Metrics().Descent.Ceil()
			utils.DrawOutlinedCentered(canvas, face, lines[i], memeSize/2, y, color.White, color.Black, 3)
			y -= face.Metrics().Height.Ceil() - face.Metrics().Descent.Ceil()
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}
