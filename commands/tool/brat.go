package tool

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"bot/assets"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	bratSize      = 512
	bratPad       = 38
	bratMaxFontPt = 48
	bratMinFontPt = 20
	bratLineRatio = 0.90 // tight leading, like the real covers
)

// renderBratPNG draws lowercase bold text centred on a white 512x512 square,
// stepping the font size down automatically so long text never clips.
func renderBratPNG(text string) ([]byte, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil, fmt.Errorf("empty text")
	}

	box := image.Rect(bratPad, bratPad, bratSize-bratPad, bratSize-bratPad)

	var chosenPt float64 = bratMinFontPt
	for pt := float64(bratMaxFontPt); pt >= bratMinFontPt; pt -= 4 {
		face, err := utils.NewFace(assets.FontLiberationSansBold, pt)
		if err != nil {
			return nil, fmt.Errorf("font: %w", err)
		}
		lines := utils.WrapLines(face, text, box.Dx())
		lineHeight := int(float64(face.Metrics().Height.Ceil()) * bratLineRatio)
		fits := len(lines)*lineHeight <= box.Dy()
		face.Close()
		if fits {
			chosenPt = pt
			break
		}
	}

	face, err := utils.NewFace(assets.FontLiberationSansBold, chosenPt)
	if err != nil {
		return nil, fmt.Errorf("font: %w", err)
	}
	defer face.Close()

	img := image.NewRGBA(image.Rect(0, 0, bratSize, bratSize))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	lines := utils.WrapLines(face, text, box.Dx())
	lineHeight := int(float64(face.Metrics().Height.Ceil()) * bratLineRatio)
	utils.DrawLinesCentered(img, face, lines, box, lineHeight, color.Black)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// sendStickerPNG converts image bytes to a 512x512 WebP sticker with EXIF
// metadata and sends it to the chat.
func sendStickerPNG(client *whatsmeow.Client, msg *events.Message, imgData []byte) error {
	webpData, err := utils.ConvertImageToWebP(imgData, false, utils.DefaultStickerPackName, utils.DefaultStickerAuthor)
	if err != nil {
		return err
	}
	_, err = utils.SendSticker(context.Background(), client, msg.Info.Chat, webpData, false, msg)
	return err
}

// executeBratSticker renders text as a brat-style sticker.
func executeBratSticker(client *whatsmeow.Client, msg *events.Message, text string) {
	pngData, err := renderBratPNG(text)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not render the brat sticker.")
		return
	}
	if err := sendStickerPNG(client, msg, pngData); err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Failed to send sticker: %v", err))
	}
}
