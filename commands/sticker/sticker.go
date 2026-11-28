package stickercmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"bot/assets"
	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	cmds := []types.Command{
		{Name: "qc", Category: "media", Description: "Generate a WhatsApp quote-bubble sticker. Usage: .qc <text> (or reply to a message)", Execute: cmdQC},
		{Name: "emojimix", Category: "media", Description: "Combine two emojis using Google Emoji Kitchen. Usage: .emojimix 😂+🔥", Execute: cmdEmojiMix},
		{Name: "getexif", Category: "media", Description: "Inspect EXIF pack/author metadata of a replied sticker.", Execute: cmdGetEXIF},
		{Name: "toimage", Aliases: []string{"toimg"}, Category: "media", Description: "Convert a replied WebP sticker into a PNG image.", Execute: cmdToImage},
		{Name: "togif", Aliases: []string{"tovideo", "tovid"}, Category: "media", Description: "Convert a replied animated WebP sticker into MP4 video.", Execute: cmdToVideo},
		{Name: "linesticker", Aliases: []string{"downloadstickerpack", "stickerly", "sigstick", "stickersgg", "fstik"}, Category: "media", Description: "Download stickers from a public LINE Store / Sticker pack URL.", Usage: ".linesticker <store_url>", Execute: cmdStickerPack},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdQC(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	name := strings.TrimSpace(msg.Info.PushName)
	if text == "" {
		if q := utils.ExtractQuotedOrOwnMessage(msg); q != nil {
			text = strings.TrimSpace(utils.GetTextFromMessage(q))
		}
	}
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.qc <text>` or reply to a text message.")
		return
	}
	if name == "" {
		name = utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	}

	pngBytes, err := renderQuoteCardPNG(name, text)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Quote card render failed: "+err.Error())
		return
	}
	webpBytes, err := utils.ConvertImageToWebP(pngBytes, false, "NopeBot QC", name)
	if err != nil {
		_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, pngBytes, "image/png", "")
		return
	}
	_, _ = utils.SendSticker(context.Background(), client, msg.Info.Chat, webpBytes, false, msg)
}

func renderQuoteCardPNG(author, text string) ([]byte, error) {
	const W, H = 512, 512
	canvas := image.NewRGBA(image.Rect(0, 0, W, H))

	nameFace, err := utils.NewFace(assets.FontLiberationSansBold, 28)
	if err != nil {
		return nil, err
	}
	bodyFace, err := utils.NewFace(assets.FontLiberationSansBold, 30)
	if err != nil {
		return nil, err
	}
	lines := utils.WrapLines(bodyFace, text, 340)
	if len(lines) > 7 {
		lines = lines[:7]
	}

	boxH := 86 + len(lines)*38
	if boxH > 460 {
		boxH = 460
	}
	boxY := (H - boxH) / 2

	// Avatar circle on left
	avatarCol := color.RGBA{0x00, 0xa8, 0x84, 0xff}
	drawCircle(canvas, 56, boxY+42, 34, avatarCol)
	initial := "?"
	if len([]rune(author)) > 0 {
		initial = strings.ToUpper(string([]rune(author)[0]))
	}
	utils.DrawLinesCentered(canvas, nameFace, []string{initial}, image.Rect(22, boxY+8, 90, boxY+76), 30, color.White)

	// Dark WhatsApp chat bubble on right
	bubbleCol := color.RGBA{0x20, 0x2c, 0x33, 0xff}
	drawRoundedRect(canvas, image.Rect(102, boxY, 492, boxY+boxH), 20, bubbleCol)

	// Author name
	utils.DrawLinesCentered(canvas, nameFace, []string{author}, image.Rect(118, boxY+12, 476, boxY+52), 30, color.RGBA{0x53, 0xbd, 0xeb, 0xff})

	// Message body
	utils.DrawLinesCentered(canvas, bodyFace, lines, image.Rect(118, boxY+56, 476, boxY+boxH-14), 38, color.RGBA{0xe9, 0xed, 0xef, 0xff})

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func drawCircle(dst draw.Image, cx, cy, r int, col color.Color) {
	r2 := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				dst.Set(x, y, col)
			}
		}
	}
}

func drawRoundedRect(dst draw.Image, rect image.Rectangle, radius int, col color.Color) {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			inCorner := false
			var cx, cy int
			if x < rect.Min.X+radius && y < rect.Min.Y+radius {
				cx, cy, inCorner = rect.Min.X+radius, rect.Min.Y+radius, true
			} else if x >= rect.Max.X-radius && y < rect.Min.Y+radius {
				cx, cy, inCorner = rect.Max.X-radius-1, rect.Min.Y+radius, true
			} else if x < rect.Min.X+radius && y >= rect.Max.Y-radius {
				cx, cy, inCorner = rect.Min.X+radius, rect.Max.Y-radius-1, true
			} else if x >= rect.Max.X-radius && y >= rect.Max.Y-radius {
				cx, cy, inCorner = rect.Max.X-radius-1, rect.Max.Y-radius-1, true
			}
			if inCorner {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy > radius*radius {
					continue
				}
			}
			dst.Set(x, y, col)
		}
	}
}

func emojiToCodepoints(s string) string {
	var parts []string
	for _, r := range strings.TrimSpace(s) {
		if r == 0xfe0f {
			continue
		}
		parts = append(parts, fmt.Sprintf("u%x", r))
	}
	return strings.Join(parts, "-")
}

func cmdEmojiMix(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	parts := strings.Split(raw, "+")
	if len(parts) != 2 {
		parts = strings.Fields(raw)
	}
	if len(parts) != 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.emojimix 😂+🔥`")
		return
	}
	cp1 := emojiToCodepoints(parts[0])
	cp2 := emojiToCodepoints(parts[1])
	dates := []string{"20231113", "20230301", "20220823", "20210831", "20201001"}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	var imgBytes []byte
	for _, d := range dates {
		candidates := []string{
			fmt.Sprintf("https://www.gstatic.com/android/keyboard/emojikitchen/%s/%s/%s_%s.png", d, cp1, cp1, cp2),
			fmt.Sprintf("https://www.gstatic.com/android/keyboard/emojikitchen/%s/%s/%s_%s.png", d, cp2, cp2, cp1),
		}
		for _, u := range candidates {
			data, ct, err := utils.DownloadHTTPBytes(ctx, u, nil)
			if err == nil && strings.HasPrefix(ct, "image/") && len(data) > 500 {
				imgBytes = data
				break
			}
		}
		if len(imgBytes) > 0 {
			break
		}
	}
	if len(imgBytes) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ That emoji combination isn't available in Emoji Kitchen. Try common emojis like `😂+🔥` or `😎+🐱`.")
		return
	}
	webpBytes, err := utils.ConvertImageToWebP(imgBytes, false, "EmojiMix", "NopeBot")
	if err != nil {
		_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, imgBytes, "image/png", "")
		return
	}
	_, _ = utils.SendSticker(ctx, client, msg.Info.Chat, webpBytes, false, msg)
}

func cmdGetEXIF(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || target.GetStickerMessage() == nil {
		utils.SendReply(client, msg.Info.Chat, "↩️ Reply to a sticker with `.getexif`.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download sticker.")
		return
	}
	exifJSON := extractWebPEXIFJSON(data)
	if exifJSON == "" {
		utils.SendReply(client, msg.Info.Chat, "ℹ️ This sticker has no WhatsApp EXIF metadata.")
		return
	}
	var pretty map[string]any
	if json.Unmarshal([]byte(exifJSON), &pretty) == nil {
		out, _ := json.MarshalIndent(pretty, "", "  ")
		utils.SendReply(client, msg.Info.Chat, "🏷️ *Sticker EXIF Metadata:*\n```json\n"+string(out)+"\n```")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "🏷️ *Sticker EXIF:*\n"+exifJSON)
}

func extractWebPEXIFJSON(webp []byte) string {
	if len(webp) < 16 || string(webp[:4]) != "RIFF" || string(webp[8:12]) != "WEBP" {
		return ""
	}
	pos := 12
	for pos+8 <= len(webp) {
		fourCC := string(webp[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(webp[pos+4 : pos+8]))
		start := pos + 8
		end := start + size
		if end > len(webp) {
			break
		}
		if fourCC == "EXIF" {
			chunk := webp[start:end]
			if idx := bytes.IndexByte(chunk, '{'); idx >= 0 {
				if last := bytes.LastIndexByte(chunk, '}'); last > idx {
					return string(chunk[idx : last+1])
				}
			}
		}
		pos = end + (size % 2)
	}
	return ""
}

func cmdToImage(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || target.GetStickerMessage() == nil {
		utils.SendReply(client, msg.Info.Chat, "↩️ Reply to a sticker with `.toimage`.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download sticker.")
		return
	}
	ffPath, cleanup, err := utils.GetFFmpegPath()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg unavailable.")
		return
	}
	defer cleanup()
	tmpDir, _ := os.MkdirTemp("", "nopebot-toimg-*")
	defer os.RemoveAll(tmpDir)
	inPath := filepath.Join(tmpDir, "in.webp")
	outPath := filepath.Join(tmpDir, "out.png")
	_ = os.WriteFile(inPath, data, 0600)
	if out, err := exec.Command(ffPath, "-y", "-i", inPath, "-frames:v", "1", outPath).CombinedOutput(); err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Conversion failed: %v %s", err, string(out)))
		return
	}
	pngBytes, _ := os.ReadFile(outPath)
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, pngBytes, "image/png", "🖼️ Sticker → Image")
}

func cmdToVideo(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || target.GetStickerMessage() == nil {
		utils.SendReply(client, msg.Info.Chat, "↩️ Reply to an animated sticker with `.tovideo` / `.togif`.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download sticker.")
		return
	}
	ffPath, cleanup, err := utils.GetFFmpegPath()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg unavailable.")
		return
	}
	defer cleanup()
	tmpDir, _ := os.MkdirTemp("", "nopebot-tovid-*")
	defer os.RemoveAll(tmpDir)
	inPath := filepath.Join(tmpDir, "in.webp")
	outPath := filepath.Join(tmpDir, "out.mp4")
	_ = os.WriteFile(inPath, data, 0600)
	cmd := exec.Command(ffPath, "-y", "-i", inPath, "-movflags", "faststart", "-pix_fmt", "yuv420p", "-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Conversion failed: %v\n%s", err, string(out)))
		return
	}
	mp4Bytes, _ := os.ReadFile(outPath)
	thumb, secs := utils.ExtractVideoMeta(outPath)
	_, _ = utils.SendVideoData(ctx, client, msg.Info.Chat, mp4Bytes, "🎞️ Sticker → Video", secs, thumb)
}

func cmdStickerPack(client *whatsmeow.Client, msg *events.Message, args []string) {
	rawURL := utils.ExtractURL(strings.Join(args, " "))
	if rawURL == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.linesticker <LINE Store or public sticker pack URL>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	page, _, err := utils.DownloadHTTPBytes(ctx, rawURL, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to fetch sticker pack page: "+err.Error())
		return
	}
	re := regexp.MustCompile(`https://[^"'\s<>)]+\.(?:png|webp)(?:\?[^"'\s<>)]*)?`)
	matches := re.FindAllString(string(page), -1)
	seen := map[string]bool{}
	var urls []string
	for _, m := range matches {
		if strings.Contains(m, "icon") || strings.Contains(m, "logo") || strings.Contains(m, "banner") {
			continue
		}
		if !seen[m] {
			seen[m] = true
			urls = append(urls, m)
		}
		if len(urls) >= 8 {
			break
		}
	}
	if len(urls) == 0 {
		u, _ := url.Parse(rawURL)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Could not find direct sticker images on `%s`.", u.Host))
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📦 Sending `%d` stickers from pack...", len(urls)))
	for _, u := range urls {
		img, _, err := utils.DownloadHTTPBytes(ctx, u, nil)
		if err != nil || len(img) < 500 {
			continue
		}
		webp, err := utils.ConvertImageToWebP(img, false, "StickerPack", "NopeBot")
		if err != nil {
			continue
		}
		_, _ = utils.SendSticker(ctx, client, msg.Info.Chat, webp, false, nil)
		time.Sleep(400 * time.Millisecond)
	}
}
