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

	"golang.org/x/image/font"
)

const (
	memeSize      = 512
	memeMaxFontPt = 40
	memeMinFontPt = 18
	memeMargin    = 14
)

func fitMemeFace(text string, maxW, maxH int) (font.Face, []string, error) {
	for pt := float64(memeMaxFontPt); pt >= memeMinFontPt; pt -= 4 {
		face, err := utils.NewFace(assets.FontLiberationSansBold, pt)
		if err != nil {
			return nil, nil, err
		}
		lines := utils.WrapLines(face, text, maxW)
		if len(lines)*face.Metrics().Height.Ceil() <= maxH {
			return face, lines, nil
		}
		face.Close()
	}
	face, err := utils.NewFace(assets.FontLiberationSansBold, memeMinFontPt)
	if err != nil {
		return nil, nil, err
	}
	return face, utils.WrapLines(face, text, maxW), nil
}

// renderMemePNG composites the source image onto a 512x512 canvas and draws
// outlined top/bottom captions locally with auto-fitted typography.
func renderMemePNG(src []byte, top, bottom string) ([]byte, error) {
	decoded, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	canvas := utils.FitSquare(decoded, memeSize)
	maxW := memeSize - 2*memeMargin
	maxRegionH := memeSize/3 - memeMargin

	top = strings.ToUpper(strings.TrimSpace(top))
	bottom = strings.ToUpper(strings.TrimSpace(bottom))

	if top != "" {
		face, lines, err := fitMemeFace(top, maxW, maxRegionH)
		if err != nil {
			return nil, fmt.Errorf("font: %w", err)
		}
		y := memeMargin + face.Metrics().Ascent.Ceil()
		for _, ln := range lines {
			utils.DrawOutlinedCentered(canvas, face, ln, memeSize/2, y, color.White, color.Black, 3)
			y += face.Metrics().Height.Ceil()
		}
		face.Close()
	}

	if bottom != "" {
		face, lines, err := fitMemeFace(bottom, maxW, maxRegionH)
		if err != nil {
			return nil, fmt.Errorf("font: %w", err)
		}
		y := memeSize - memeMargin
		for i := len(lines) - 1; i >= 0; i-- {
			y -= face.Metrics().Descent.Ceil()
			utils.DrawOutlinedCentered(canvas, face, lines[i], memeSize/2, y, color.White, color.Black, 3)
			y -= face.Metrics().Height.Ceil() - face.Metrics().Descent.Ceil()
		}
		face.Close()
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}
