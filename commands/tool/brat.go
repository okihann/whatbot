package tool

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"google.golang.org/protobuf/proto"
)

const (
	bratSize   = 512
	bratPad    = 34
	bratFontPt = 54
)

// renderBratPNG draws lowercase bold text, centred on a white square, in the
// style of the "brat" cover art.
func renderBratPNG(text string) ([]byte, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil, fmt.Errorf("empty text")
	}

	parsed, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse font: %w", err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size: bratFontPt, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("new face: %w", err)
	}
	defer face.Close()

	img := image.NewRGBA(image.Rect(0, 0, bratSize, bratSize))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	maxWidth := bratSize - 2*bratPad
	lines := wrapText(face, text, maxWidth)

	metrics := face.Metrics()
	lineHeight := metrics.Height.Ceil()
	blockHeight := lineHeight * len(lines)
	startY := (bratSize-blockHeight)/2 + metrics.Ascent.Ceil()

	d := &font.Drawer{Dst: img, Src: image.Black, Face: face}
	for i, line := range lines {
		w := font.MeasureString(face, line).Ceil()
		d.Dot = fixed.P((bratSize-w)/2, startY+i*lineHeight)
		d.DrawString(line)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// wrapText greedily breaks text into lines no wider than maxWidth.
func wrapText(face font.Face, text string, maxWidth int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		candidate := current + " " + w
		if font.MeasureString(face, candidate).Ceil() <= maxWidth {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = w
	}
	return append(lines, current)
}

// sendStickerFromPNG converts a PNG to a 512x512 WebP sticker and sends it.
func sendStickerFromPNG(client *whatsmeow.Client, msg *events.Message, pngData []byte) error {
	ffmpegPath, cleanup, err := utils.GetFFmpegPath()
	if err != nil {
		return fmt.Errorf("ffmpeg unavailable: %w", err)
	}
	defer cleanup()

	tempDir := "temp"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}

	stamp := time.Now().UnixNano()
	inPath := filepath.Join(tempDir, fmt.Sprintf("brat_in_%d.png", stamp))
	outPath := filepath.Join(tempDir, fmt.Sprintf("brat_out_%d.webp", stamp))
	defer os.Remove(inPath)
	defer os.Remove(outPath)

	if err := os.WriteFile(inPath, pngData, 0644); err != nil {
		return fmt.Errorf("write png: %w", err)
	}

	filter := "scale=512:512:force_original_aspect_ratio=decrease,format=rgba,pad=512:512:(ow-iw)/2:(oh-ih)/2:color='#00000000'"
	if out, err := exec.Command(ffmpegPath, "-y", "-i", inPath, "-vcodec", "libwebp", "-vf", filter, outPath).CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg convert: %v: %s", err, string(out))
	}

	webpData, err := os.ReadFile(outPath)
	if err != nil {
		return fmt.Errorf("read webp: %w", err)
	}

	uploaded, err := client.Upload(context.Background(), webpData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	_, err = client.SendMessage(context.Background(), msg.Info.Chat, &waE2E.Message{
		StickerMessage: &waE2E.StickerMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String("image/webp"),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
		},
	})
	return err
}

// executeBratSticker renders the given text as a brat-style sticker.
func executeBratSticker(client *whatsmeow.Client, msg *events.Message, text string) {
	pngData, err := renderBratPNG(text)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not render the brat sticker.")
		return
	}
	if err := sendStickerFromPNG(client, msg, pngData); err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Failed to send sticker: %v", err))
	}
}
