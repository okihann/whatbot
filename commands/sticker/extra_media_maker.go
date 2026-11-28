package stickercmd

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"google.golang.org/protobuf/proto"
)

func init() {
	cmds := []types.Command{
		// Extra Downloaders & Converters
		{Name: "soundcloud", Aliases: []string{"scdl"}, Category: "media", Description: "Download a SoundCloud track as MP3. Usage: .soundcloud <url>", Execute: cmdSoundCloud},
		{Name: "tiktokmp3", Aliases: []string{"ttmp3"}, Category: "media", Description: "Download TikTok audio as MP3. Usage: .tiktokmp3 <url>", Execute: cmdTikTokMP3},
		{Name: "hd", Aliases: []string{"remini", "swhd"}, Category: "media", Description: "Upscale and sharpen a replied image to 2x HD resolution.", Execute: cmdHD},
		{Name: "tovn", Aliases: []string{"toptt"}, Category: "media", Description: "Convert a replied audio or video into a WhatsApp voice note (PTT).", Execute: cmdToVN},
		{Name: "toviewonce", Aliases: []string{"tovo"}, Category: "media", Description: "Resend a replied photo or video as a View-Once message.", Execute: cmdToViewOnce},
		{Name: "toptv", Category: "media", Description: "Convert a replied short video into a circular WhatsApp Video Note.", Execute: cmdToPTV},

		// Extra Stickers & Watermarks
		{Name: "scircle", Aliases: []string{"circlesticker"}, Category: "media", Description: "Convert a replied image into a circular WebP sticker.", Execute: cmdSCircle},
		{Name: "ttp", Category: "media", Description: "Create a crisp text-to-sticker WebP. Usage: .ttp <text>", Execute: cmdTTP(false)},
		{Name: "attp", Category: "media", Description: "Create an animated rainbow text sticker. Usage: .attp <text>", Execute: cmdTTP(true)},
		{Name: "brat", Aliases: []string{"brat2"}, Category: "media", Description: "Generate a classic lime-green Brat text sticker. Usage: .brat <text>", Execute: cmdBratSticker},
		{Name: "swm", Category: "media", Description: "Change pack/author watermark of a replied sticker. Usage: .swm Pack | Author", Execute: cmdSWM},
		{Name: "setwm", Category: "media", Description: "Save your default sticker pack/author watermark. Usage: .setwm Pack | Author", Execute: cmdSetWM},
		{Name: "resetwm", Category: "media", Description: "Reset your default sticker watermark to NopeBot.", Execute: cmdResetWM},

		// Makers
		{Name: "hartatahta", Category: "media", Description: "Generate a metallic 'HARTA TAHTA <NAME>' graphic. Usage: .hartatahta <name>", Execute: cmdHartaTahta},
		{Name: "iqcpink", Category: "media", Description: "Generate a pink aesthetic iPhone-style quote card. Usage: .iqcpink <text>", Execute: cmdIQCPink},
		{Name: "fakereply", Aliases: []string{"fakechat"}, Category: "media", Description: "Generate a fake WhatsApp chat bubble image. Usage: .fakereply Name | Message", Execute: cmdFakeReply},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func userWatermark(client *whatsmeow.Client, msg *events.Message) (string, string) {
	pack, author := "NopeBot", "NopeBot"
	store := db.GetBotStore()
	if store == nil {
		return pack, author
	}
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	if uid == "" {
		return pack, author
	}
	if val, ok, err := store.GetKV("wm:" + uid); err == nil && ok && val != "" {
		parts := strings.SplitN(val, "|", 2)
		if p := strings.TrimSpace(parts[0]); p != "" {
			pack = p
		}
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			author = strings.TrimSpace(parts[1])
		}
	}
	return pack, author
}

func downloadAudioWithYtDlp(ctx context.Context, rawURL string) ([]byte, error) {
	ytPath, cleanup, err := utils.GetYtDlpPath()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp unavailable: %w", err)
	}
	defer cleanup()
	ffPath, ffClean, err := utils.GetFFmpegPath()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg unavailable: %w", err)
	}
	defer ffClean()

	tmpDir, err := os.MkdirTemp("", "nopebot-dlmp3-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	outTmpl := filepath.Join(tmpDir, "audio.%(ext)s")
	cmd := exec.CommandContext(ctx, ytPath,
		"--no-playlist", "-x", "--audio-format", "mp3", "--audio-quality", "128K",
		"--ffmpeg-location", filepath.Dir(ffPath),
		"-o", outTmpl, rawURL,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	matches, _ := filepath.Glob(filepath.Join(tmpDir, "audio.*"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("no audio file produced")
	}
	return os.ReadFile(matches[0])
}

func cmdSoundCloud(client *whatsmeow.Client, msg *events.Message, args []string) {
	rawURL := strings.TrimSpace(strings.Join(args, " "))
	if rawURL == "" {
		utils.SendReply(client, msg.Info.Chat, "☁️ Usage: `.soundcloud <soundcloud_url>`")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "⏳ Downloading SoundCloud track...")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	mp3, err := downloadAudioWithYtDlp(ctx, rawURL)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ SoundCloud download failed: "+err.Error())
		return
	}
	_, _ = utils.SendAudioData(ctx, client, msg.Info.Chat, mp3, "audio/mpeg", 0, false)
}

func cmdTikTokMP3(client *whatsmeow.Client, msg *events.Message, args []string) {
	rawURL := strings.TrimSpace(strings.Join(args, " "))
	if rawURL == "" {
		utils.SendReply(client, msg.Info.Chat, "🎵 Usage: `.tiktokmp3 <tiktok_url>`")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "⏳ Extracting TikTok audio...")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	mp3, err := downloadAudioWithYtDlp(ctx, rawURL)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ TikTok MP3 extraction failed: "+err.Error())
		return
	}
	_, _ = utils.SendAudioData(ctx, client, msg.Info.Chat, mp3, "audio/mpeg", 0, false)
}

func cmdHD(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || target.GetImageMessage() == nil {
		utils.SendReply(client, msg.Info.Chat, "✨ Reply to an image with `.hd` to upscale and sharpen it.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download image: "+err.Error())
		return
	}
	ffPath, cleanup, err := utils.GetFFmpegPath()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg unavailable.")
		return
	}
	defer cleanup()
	tmpDir, err := os.MkdirTemp("", "nopebot-hd-*")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmpDir)
	inPath := filepath.Join(tmpDir, "in.img")
	outPath := filepath.Join(tmpDir, "out.png")
	_ = os.WriteFile(inPath, data, 0600)

	// 2x Lanczos upscale (clamped to max 2048px) + unsharp mask + subtle contrast boost
	vf := "scale='min(iw*2,2048)':'min(ih*2,2048)':flags=lanczos,unsharp=5:5:1.1:5:5:0.0,eq=contrast=1.04:saturation=1.06"
	cmd := exec.CommandContext(ctx, ffPath, "-y", "-i", inPath, "-vf", vf, outPath)
	if err := cmd.Run(); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ HD upscale failed: "+err.Error())
		return
	}
	outBytes, err := os.ReadFile(outPath)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed reading enhanced image.")
		return
	}
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, outBytes, "image/png", "✨ *HD Enhanced (2x Lanczos + Sharpen)*")
}

func cmdToVN(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || (target.GetAudioMessage() == nil && target.GetVideoMessage() == nil) {
		utils.SendReply(client, msg.Info.Chat, "🎤 Reply to an audio or video with `.tovn` to convert to a Voice Note.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed downloading media: "+err.Error())
		return
	}
	oggBytes, dur, err := utils.ConvertToStickerOpus(data, 180)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Voice note conversion failed: "+err.Error())
		return
	}
	_, _ = utils.SendAudioData(ctx, client, msg.Info.Chat, oggBytes, "audio/ogg; codecs=opus", dur, true)
}

func cmdToViewOnce(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || (target.GetImageMessage() == nil && target.GetVideoMessage() == nil) {
		utils.SendReply(client, msg.Info.Chat, "👁️ Reply to an image or video with `.toviewonce` to resend it as View-Once.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	data, _, mimeType, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed downloading media: "+err.Error())
		return
	}
	caption := strings.TrimSpace(strings.Join(args, " "))
	if target.GetImageMessage() != nil {
		up, err := client.Upload(ctx, data, whatsmeow.MediaImage)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Upload failed: "+err.Error())
			return
		}
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		out := &waE2E.Message{
			ViewOnceMessageV2: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{
					ImageMessage: &waE2E.ImageMessage{
						URL:           proto.String(up.URL),
						DirectPath:    proto.String(up.DirectPath),
						MediaKey:      up.MediaKey,
						Mimetype:      proto.String(mimeType),
						FileSHA256:    up.FileSHA256,
						FileEncSHA256: up.FileEncSHA256,
						FileLength:    proto.Uint64(up.FileLength),
						Caption:       proto.String(caption),
						ViewOnce:      proto.Bool(true),
					},
				},
			},
		}
		_, _ = client.SendMessage(ctx, msg.Info.Chat, out)
		return
	}
	up, err := client.Upload(ctx, data, whatsmeow.MediaVideo)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Upload failed: "+err.Error())
		return
	}
	if mimeType == "" {
		mimeType = "video/mp4"
	}
	out := &waE2E.Message{
		ViewOnceMessageV2: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{
				VideoMessage: &waE2E.VideoMessage{
					URL:           proto.String(up.URL),
					DirectPath:    proto.String(up.DirectPath),
					MediaKey:      up.MediaKey,
					Mimetype:      proto.String(mimeType),
					FileSHA256:    up.FileSHA256,
					FileEncSHA256: up.FileEncSHA256,
					FileLength:    proto.Uint64(up.FileLength),
					Caption:       proto.String(caption),
					ViewOnce:      proto.Bool(true),
				},
			},
		},
	}
	_, _ = client.SendMessage(ctx, msg.Info.Chat, out)
}

func cmdToPTV(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || target.GetVideoMessage() == nil {
		utils.SendReply(client, msg.Info.Chat, "⭕ Reply to a short video with `.toptv` to convert to a circular Video Note.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed downloading video: "+err.Error())
		return
	}
	up, err := client.Upload(ctx, data, whatsmeow.MediaVideo)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Upload failed: "+err.Error())
		return
	}
	_, _ = client.SendMessage(ctx, msg.Info.Chat, &waE2E.Message{
		PtvMessage: &waE2E.VideoMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String("video/mp4"),
			FileSHA256:    up.FileSHA256,
			FileEncSHA256: up.FileEncSHA256,
			FileLength:    proto.Uint64(up.FileLength),
			Seconds:       target.GetVideoMessage().Seconds,
		},
	})
}

func cmdSCircle(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || target.GetImageMessage() == nil {
		utils.SendReply(client, msg.Info.Chat, "⭕ Reply to an image with `.scircle` to create a circular sticker.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed downloading image: "+err.Error())
		return
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not decode image.")
		return
	}
	b := src.Bounds()
	size := b.Dx()
	if b.Dy() < size {
		size = b.Dy()
	}
	ox := b.Min.X + (b.Dx()-size)/2
	oy := b.Min.Y + (b.Dy()-size)/2
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) + 0.5 - r
			dy := float64(y) + 0.5 - r
			if dx*dx+dy*dy <= r*r {
				out.Set(x, y, src.At(ox+x, oy+y))
			}
		}
	}
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, out)
	pack, author := userWatermark(client, msg)
	webp, err := utils.ConvertImageToWebP(pngBuf.Bytes(), true, pack, author)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Sticker conversion failed: "+err.Error())
		return
	}
	_, _ = utils.SendSticker(ctx, client, msg.Info.Chat, webp, false, msg)
}

func renderTextCardPNG(text string, bg, fg color.RGBA, scale int) []byte {
	const size = 512
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, bg)
		}
	}
	if scale < 1 {
		scale = 3
	}
	// Draw onto small canvas then nearest-neighbor scale up for crisp big text
	smallSize := size / scale
	small := image.NewRGBA(image.Rect(0, 0, smallSize, smallSize))
	for y := 0; y < smallSize; y++ {
		for x := 0; x < smallSize; x++ {
			small.SetRGBA(x, y, bg)
		}
	}
	maxChars := (smallSize - 16) / 7
	if maxChars < 6 {
		maxChars = 6
	}
	var lines []string
	for _, w := range strings.Fields(text) {
		if len(lines) == 0 || len(lines[len(lines)-1])+len(w)+1 > maxChars {
			lines = append(lines, w)
		} else {
			lines[len(lines)-1] += " " + w
		}
	}
	d := &font.Drawer{
		Dst:  small,
		Src:  image.NewUniform(fg),
		Face: basicfont.Face7x13,
	}
	totalH := len(lines) * 16
	startY := (smallSize-totalH)/2 + 12
	for i, line := range lines {
		wPx := len(line) * 7
		x := (smallSize - wPx) / 2
		if x < 4 {
			x = 4
		}
		d.Dot = fixed.P(x, startY+i*16)
		d.DrawString(line)
	}
	for y := 0; y < size; y++ {
		sy := y / scale
		if sy >= smallSize {
			sy = smallSize - 1
		}
		for x := 0; x < size; x++ {
			sx := x / scale
			if sx >= smallSize {
				sx = smallSize - 1
			}
			img.SetRGBA(x, y, small.RGBAAt(sx, sy))
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func cmdTTP(animated bool) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		text := strings.TrimSpace(strings.Join(args, " "))
		if text == "" {
			utils.SendReply(client, msg.Info.Chat, "✨ Usage: `.ttp <text>` or `.attp <text>`")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pack, author := userWatermark(client, msg)
		if !animated {
			pngBytes := renderTextCardPNG(text, color.RGBA{0, 0, 0, 0}, color.RGBA{255, 255, 255, 255}, 4)
			webp, err := utils.ConvertImageToWebP(pngBytes, true, pack, author)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "❌ Failed creating sticker: "+err.Error())
				return
			}
			_, _ = utils.SendSticker(ctx, client, msg.Info.Chat, webp, false, msg)
			return
		}

		// Animated rainbow text sticker via FFmpeg
		ffPath, cleanup, err := utils.GetFFmpegPath()
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg unavailable.")
			return
		}
		defer cleanup()
		tmpDir, err := os.MkdirTemp("", "nopebot-attp-*")
		if err != nil {
			return
		}
		defer os.RemoveAll(tmpDir)
		palette := []color.RGBA{
			{255, 80, 80, 255}, {255, 190, 50, 255}, {80, 240, 120, 255},
			{70, 200, 255, 255}, {180, 110, 255, 255}, {255, 100, 210, 255},
		}
		for i, col := range palette {
			frame := renderTextCardPNG(text, color.RGBA{18, 18, 22, 255}, col, 4)
			_ = os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("f%02d.png", i)), frame, 0600)
		}
		mp4Path := filepath.Join(tmpDir, "attp.mp4")
		cmd := exec.CommandContext(ctx, ffPath, "-y", "-framerate", "5", "-i", filepath.Join(tmpDir, "f%02d.png"),
			"-c:v", "libx264", "-pix_fmt", "yuv420p", mp4Path)
		if err := cmd.Run(); err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Animated sticker render failed.")
			return
		}
		mp4Bytes, _ := os.ReadFile(mp4Path)
		webp, err := utils.ConvertVideoToAnimatedWebP(mp4Bytes, 4, true, pack, author)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ WebP encode failed: "+err.Error())
			return
		}
		_, _ = utils.SendSticker(ctx, client, msg.Info.Chat, webp, true, msg)
	}
}

func cmdBratSticker(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "💚 Usage: `.brat <text>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	// Classic Brat lime-green #8ACE00 background with crisp black lowercase text
	pngBytes := renderTextCardPNG(strings.ToLower(text), color.RGBA{138, 206, 0, 255}, color.RGBA{12, 12, 12, 255}, 4)
	pack, author := userWatermark(client, msg)
	webp, err := utils.ConvertImageToWebP(pngBytes, true, pack, author)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Brat sticker failed: "+err.Error())
		return
	}
	_, _ = utils.SendSticker(ctx, client, msg.Info.Chat, webp, false, msg)
}

func cmdSWM(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || (target.GetStickerMessage() == nil && target.GetImageMessage() == nil) {
		utils.SendReply(client, msg.Info.Chat, "🏷️ Reply to a sticker or image with `.swm Pack | Author`")
		return
	}
	raw := strings.TrimSpace(strings.Join(args, " "))
	pack, author := userWatermark(client, msg)
	if raw != "" {
		parts := strings.SplitN(raw, "|", 2)
		if p := strings.TrimSpace(parts[0]); p != "" {
			pack = p
		}
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			author = strings.TrimSpace(parts[1])
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed downloading media: "+err.Error())
		return
	}
	webp, err := utils.ConvertImageToWebP(data, false, pack, author)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed applying watermark: "+err.Error())
		return
	}
	_, _ = utils.SendSticker(ctx, client, msg.Info.Chat, webp, target.GetStickerMessage().GetIsAnimated(), msg)
}

func cmdSetWM(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		utils.SendReply(client, msg.Info.Chat, "🏷️ Usage: `.setwm PackName | AuthorName`")
		return
	}
	store := db.GetBotStore()
	if store == nil {
		return
	}
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	_ = store.SetKV("wm:"+uid, raw)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Default sticker watermark saved: `%s`", raw))
}

func cmdResetWM(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	if store == nil {
		return
	}
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	_ = store.SetKV("wm:"+uid, "")
	utils.SendReply(client, msg.Info.Chat, "✅ Sticker watermark reset to default (`NopeBot`).")
}

func cmdHartaTahta(client *whatsmeow.Client, msg *events.Message, args []string) {
	name := strings.ToUpper(strings.TrimSpace(strings.Join(args, " ")))
	if name == "" {
		utils.SendReply(client, msg.Info.Chat, "✨ Usage: `.hartatahta <NAME>`")
		return
	}
	const size = 640
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	// Dark obsidian background
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, color.RGBA{12, 12, 18, 255})
		}
	}
	// Render HARTA / TAHTA / NAME on a small mask and apply holographic rainbow gradient!
	const scale = 6
	smallW, smallH := size/scale, size/scale
	mask := image.NewAlpha(image.Rect(0, 0, smallW, smallH))
	d := &font.Drawer{
		Dst:  mask,
		Src:  image.Opaque,
		Face: basicfont.Face7x13,
	}
	lines := []string{"HARTA", "TAHTA", name}
	for i, line := range lines {
		if len(line) > 13 {
			line = line[:13]
		}
		x := (smallW - len(line)*7) / 2
		if x < 2 {
			x = 2
		}
		d.Dot = fixed.P(x, 32+i*24)
		d.DrawString(line)
	}
	for y := 0; y < size; y++ {
		sy := y / scale
		if sy >= smallH {
			sy = smallH - 1
		}
		for x := 0; x < size; x++ {
			sx := x / scale
			if sx >= smallW {
				sx = smallW - 1
			}
			if mask.AlphaAt(sx, sy).A > 0 {
				t := float64(x+y) / float64(size*2)
				r := uint8(155 + 100*math.Sin(t*6.28))
				g := uint8(155 + 100*math.Sin(t*6.28+2.09))
				b := uint8(155 + 100*math.Sin(t*6.28+4.18))
				img.SetRGBA(x, y, color.RGBA{r, g, b, 255})
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, buf.Bytes(), "image/png", "✨ *HARTA TAHTA "+name+"*")
}

func cmdIQCPink(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "🌸 Usage: `.iqcpink <text>`")
		return
	}
	pngBytes := renderTextCardPNG(text, color.RGBA{255, 214, 232, 255}, color.RGBA{120, 20, 70, 255}, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, pngBytes, "image/png", "🌸 *Pink Quote Card*")
}

func cmdFakeReply(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	parts := strings.SplitN(raw, "|", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		utils.SendReply(client, msg.Info.Chat, "💬 Usage: `.fakereply Name | Message text`")
		return
	}
	name := strings.TrimSpace(parts[0])
	body := strings.TrimSpace(parts[1])
	const W, H = 760, 260
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	bg := color.RGBA{11, 20, 26, 255}
	bubble := color.RGBA{32, 44, 51, 255}
	nameCol := color.RGBA{0, 168, 132, 255}
	textCol := color.RGBA{233, 237, 239, 255}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			img.SetRGBA(x, y, bg)
		}
	}
	for y := 40; y < H-40; y++ {
		for x := 50; x < W-50; x++ {
			img.SetRGBA(x, y, bubble)
		}
	}
	// Draw scaled text into bubble
	small := image.NewRGBA(image.Rect(0, 0, W/2, H/2))
	for y := 0; y < H/2; y++ {
		for x := 0; x < W/2; x++ {
			small.SetRGBA(x, y, bg)
		}
	}
	for y := 20; y < H/2-20; y++ {
		for x := 25; x < W/2-25; x++ {
			small.SetRGBA(x, y, bubble)
		}
	}
	dn := &font.Drawer{Dst: small, Src: image.NewUniform(nameCol), Face: basicfont.Face7x13, Dot: fixed.P(36, 42)}
	dn.DrawString(name)
	dt := &font.Drawer{Dst: small, Src: image.NewUniform(textCol), Face: basicfont.Face7x13, Dot: fixed.P(36, 68)}
	if len(body) > 44 {
		dt.DrawString(body[:44])
		dt.Dot = fixed.P(36, 86)
		rem := body[44:]
		if len(rem) > 44 {
			rem = rem[:41] + "..."
		}
		dt.DrawString(rem)
	} else {
		dt.DrawString(body)
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			img.SetRGBA(x, y, small.RGBAAt(x/2, y/2))
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, buf.Bytes(), "image/png", "")
}
