package toolscmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bot/assets"
	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	xdraw "golang.org/x/image/draw"
)

func init() {
	cmds := []types.Command{
		// Image ops
		{Name: "crop", Category: "tools", Description: "Center-crop a replied image to square (1:1).", Execute: cmdImageOp("crop")},
		{Name: "resize", Category: "tools", Description: "Resize a replied image. Usage: .resize <width> <height>", Execute: cmdImageOp("resize")},
		{Name: "rotate", Category: "tools", Description: "Rotate a replied image clockwise (90/180/270). Usage: .rotate 90", Execute: cmdImageOp("rotate")},
		{Name: "flip", Category: "tools", Description: "Flip a replied image horizontally or vertically. Usage: .flip [h|v]", Execute: cmdImageOp("flip")},
		{Name: "blur", Category: "tools", Description: "Blur a replied image.", Execute: cmdImageOp("blur")},
		{Name: "pixelate", Category: "tools", Description: "Pixelate a replied image.", Execute: cmdImageOp("pixelate")},
		{Name: "jadiburik", Category: "tools", Description: "Crush a replied image into ultra-low-res JPEG.", Execute: cmdImageOp("burik")},
		{Name: "stitch", Category: "tools", Description: "Mirror-stitch a replied image side-by-side.", Execute: cmdImageOp("stitch")},
		{Name: "testimage", Category: "tools", Description: "Generate a SMPTE-style test pattern image.", Execute: cmdTestImage},
		{Name: "iqc", Aliases: []string{"pesan"}, Category: "tools", Description: "Generate an iPhone-style quote notification card. Usage: .iqc <text>", Execute: cmdIQC},

		// Audio/Video/PDF ops
		{Name: "tomp3", Category: "tools", Description: "Extract MP3 audio from a replied video/voice note.", Execute: cmdToMP3},
		{Name: "topdf", Category: "tools", Description: "Convert a replied image into a PDF document.", Execute: cmdToPDF},
		{Name: "trim", Category: "tools", Description: "Trim replied video/audio. Usage: .trim <start_sec> <end_sec>", Execute: cmdTrim},
		{Name: "compress", Category: "tools", Description: "Compress a replied video or image for smaller file size.", Execute: cmdCompress},
		{Name: "mediaquality", Category: "tools", Description: "Inspect resolution, codec, bitrate, and duration of replied media.", Execute: cmdMediaQuality},
		{Name: "todocmsg", Aliases: []string{"fakethumbnail"}, Category: "tools", Description: "Re-send a replied media file as a WhatsApp document.", Execute: cmdToDocMsg},
		{Name: "generatelrc", Category: "tools", Description: "Fetch synced .lrc lyrics file for a song. Usage: .generatelrc <song>", Execute: cmdGenerateLRC},

		// Public Info & Speech APIs
		{Name: "cuaca", Category: "tools", Description: "Check live weather forecast for a city. Usage: .cuaca Jakarta", Execute: cmdCuaca},
		{Name: "gempa", Category: "tools", Description: "Get latest Indonesia earthquake info from BMKG.", Execute: cmdGempa},
		{Name: "translate", Aliases: []string{"lang"}, Category: "tools", Description: "Translate text. Usage: .translate <target_lang> <text> (e.g. .translate en halo dunia)", Execute: cmdTranslate},
		{Name: "tts", Category: "tools", Description: "Convert text to speech voice note. Usage: .tts [lang] <text>", Execute: cmdTTS},
		{Name: "upload", Category: "tools", Description: "Upload replied media to temporary Litterbox file host and return URL.", Execute: cmdUpload},

		// WhatsApp utilities
		{Name: "readviewonce", Aliases: []string{"getmedia"}, Category: "tools", Description: "Alias for /get — recover replied View-Once media.", Execute: cmdReadViewOnceAlias},
		{Name: "getmsgid", Category: "tools", Description: "Show the stanza/message ID of a replied message.", Execute: cmdGetMsgID},
		{Name: "getidch", Category: "tools", Description: "Extract channel JID/invite key from a WhatsApp Channel link.", Execute: cmdGetIDCH},
		{Name: "whatmusic", Aliases: []string{"humming"}, Category: "tools", Description: "Identify a song from replied audio/video (alias for /shazam).", Execute: cmdWhatMusicAlias},
		{Name: "whatsappstatus", Category: "tools", Description: "Show current WhatsApp connection and Android Companion status.", Execute: cmdWAStatus},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func decodeRepliedImage(client *whatsmeow.Client, msg *events.Message) (image.Image, []byte, error) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || target.GetImageMessage() == nil {
		return nil, nil, fmt.Errorf("reply to an image first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		return nil, nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, data, err
}

func cmdImageOp(op string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		img, _, err := decodeRepliedImage(client, msg)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "📷 "+err.Error())
			return
		}
		b := img.Bounds()
		w, h := b.Dx(), b.Dy()
		var out image.Image

		switch op {
		case "crop":
			s := w
			if h < s {
				s = h
			}
			x0, y0 := b.Min.X+(w-s)/2, b.Min.Y+(h-s)/2
			dst := image.NewRGBA(image.Rect(0, 0, s, s))
			for y := 0; y < s; y++ {
				for x := 0; x < s; x++ {
					dst.Set(x, y, img.At(x0+x, y0+y))
				}
			}
			out = dst

		case "resize":
			fields := strings.Fields(strings.Join(args, " "))
			nw, nh := 512, 512
			if len(fields) >= 2 {
				if v, e := strconv.Atoi(fields[0]); e == nil && v >= 16 && v <= 4096 {
					nw = v
				}
				if v, e := strconv.Atoi(fields[1]); e == nil && v >= 16 && v <= 4096 {
					nh = v
				}
			} else if len(fields) == 1 {
				if v, e := strconv.Atoi(fields[0]); e == nil && v >= 16 && v <= 4096 {
					nw = v
					nh = max(16, (h*v)/max(1, w))
				}
			}
			dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
			xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
			out = dst

		case "rotate":
			deg := 90
			if len(args) > 0 {
				if v, e := strconv.Atoi(strings.TrimSpace(args[0])); e == nil {
					deg = ((v % 360) + 360) % 360
				}
			}
			switch deg {
			case 180:
				dst := image.NewRGBA(image.Rect(0, 0, w, h))
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						dst.Set(w-1-x, h-1-y, img.At(b.Min.X+x, b.Min.Y+y))
					}
				}
				out = dst
			case 270:
				dst := image.NewRGBA(image.Rect(0, 0, h, w))
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						dst.Set(y, w-1-x, img.At(b.Min.X+x, b.Min.Y+y))
					}
				}
				out = dst
			default: // 90
				dst := image.NewRGBA(image.Rect(0, 0, h, w))
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						dst.Set(h-1-y, x, img.At(b.Min.X+x, b.Min.Y+y))
					}
				}
				out = dst
			}

		case "flip":
			vert := strings.HasPrefix(strings.ToLower(strings.TrimSpace(strings.Join(args, " "))), "v")
			dst := image.NewRGBA(image.Rect(0, 0, w, h))
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if vert {
						dst.Set(x, h-1-y, img.At(b.Min.X+x, b.Min.Y+y))
					} else {
						dst.Set(w-1-x, y, img.At(b.Min.X+x, b.Min.Y+y))
					}
				}
			}
			out = dst

		case "blur":
			// Fast downscale + smooth upscale box-blur
			sw, sh := max(16, w/12), max(16, h/12)
			small := image.NewRGBA(image.Rect(0, 0, sw, sh))
			xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, b, xdraw.Over, nil)
			dst := image.NewRGBA(image.Rect(0, 0, w, h))
			xdraw.BiLinear.Scale(dst, dst.Bounds(), small, small.Bounds(), xdraw.Over, nil)
			out = dst

		case "pixelate":
			sw, sh := max(12, w/20), max(12, h/20)
			small := image.NewRGBA(image.Rect(0, 0, sw, sh))
			xdraw.NearestNeighbor.Scale(small, small.Bounds(), img, b, xdraw.Over, nil)
			dst := image.NewRGBA(image.Rect(0, 0, w, h))
			xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), small, small.Bounds(), xdraw.Over, nil)
			out = dst

		case "burik":
			sw, sh := max(32, w/8), max(32, h/8)
			small := image.NewRGBA(image.Rect(0, 0, sw, sh))
			xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, b, xdraw.Over, nil)
			dst := image.NewRGBA(image.Rect(0, 0, w, h))
			xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), small, small.Bounds(), xdraw.Over, nil)
			var buf bytes.Buffer
			_ = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 8})
			_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, buf.Bytes(), "image/jpeg", "🥔 Jadi Burik")
			return

		case "stitch":
			dst := image.NewRGBA(image.Rect(0, 0, w*2, h))
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					c := img.At(b.Min.X+x, b.Min.Y+y)
					dst.Set(x, y, c)
					dst.Set(w*2-1-x, y, c)
				}
			}
			out = dst
		}

		var buf bytes.Buffer
		_ = png.Encode(&buf, out)
		_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, buf.Bytes(), "image/png", "✅ "+op)
	}
}

func cmdTestImage(client *whatsmeow.Client, msg *events.Message, args []string) {
	colors := []color.RGBA{
		{235, 235, 235, 255}, {235, 235, 16, 255}, {16, 235, 235, 255},
		{16, 235, 16, 255}, {235, 16, 235, 255}, {235, 16, 16, 255}, {16, 16, 235, 255},
	}
	const w, h = 560, 320
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	barW := w / len(colors)
	for i, c := range colors {
		for y := 0; y < h; y++ {
			for x := i * barW; x < (i+1)*barW; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, buf.Bytes(), "image/png", "📺 Test Image Pattern")
}

func cmdIQC(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.iqc <quote text>`")
		return
	}
	const w, h = 640, 360
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		shade := uint8(18 + (y*28)/h)
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{shade, shade + 10, shade + 22, 255})
		}
	}
	// Card box
	for y := 95; y < 285; y++ {
		for x := 48; x < w-48; x++ {
			img.SetRGBA(x, y, color.RGBA{34, 46, 53, 255})
		}
	}
	face, err := utils.NewFace(assets.FontLiberationSansBold, 26)
	if err == nil {
		titleFace, _ := utils.NewFace(assets.FontLiberationSansBold, 20)
		if titleFace != nil {
			utils.DrawLinesCentered(img, titleFace, []string{time.Now().Format("15:04") + "  •  Messages"}, image.Rect(48, 42, w-48, 86), 24, color.RGBA{180, 195, 205, 255})
		}
		lines := utils.WrapLines(face, text, w-140)
		if len(lines) > 5 {
			lines = lines[:5]
		}
		utils.DrawLinesCentered(img, face, lines, image.Rect(68, 112, w-68, 268), 34, color.White)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, buf.Bytes(), "image/png", "")
}

// WrapJPEGAsPDF produces a minimal valid single-page PDF embedding a JPEG image.
func WrapJPEGAsPDF(jpegData []byte, width, height int) []byte {
	var b bytes.Buffer
	offsets := make([]int, 6)
	b.WriteString("%PDF-1.4\n")

	offsets[1] = b.Len()
	b.WriteString("1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n")

	offsets[2] = b.Len()
	b.WriteString("2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n")

	offsets[3] = b.Len()
	fmt.Fprintf(&b, "3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >> endobj\n", width, height)

	offsets[4] = b.Len()
	fmt.Fprintf(&b, "4 0 obj << /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >> stream\n", width, height, len(jpegData))
	b.Write(jpegData)
	b.WriteString("\nendstream endobj\n")

	stream := fmt.Sprintf("q %d 0 0 %d 0 0 cm /Im0 Do Q", width, height)
	offsets[5] = b.Len()
	fmt.Fprintf(&b, "5 0 obj << /Length %d >> stream\n%s\nendstream endobj\n", len(stream), stream)

	xref := b.Len()
	b.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer << /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}

func cmdToPDF(client *whatsmeow.Client, msg *events.Message, args []string) {
	img, _, err := decodeRepliedImage(client, msg)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "📷 Reply to an image with `.topdf`.")
		return
	}
	var jpgBuf bytes.Buffer
	if err := jpeg.Encode(&jpgBuf, img, &jpeg.Options{Quality: 85}); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ JPEG encode failed.")
		return
	}
	pdfBytes := WrapJPEGAsPDF(jpgBuf.Bytes(), img.Bounds().Dx(), img.Bounds().Dy())
	_, _ = utils.SendDocumentData(context.Background(), client, msg.Info.Chat, pdfBytes, "application/pdf", "document.pdf", "📄 Image → PDF")
}

func cmdToMP3(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || (target.GetVideoMessage() == nil && target.GetAudioMessage() == nil) {
		utils.SendReply(client, msg.Info.Chat, "🎵 Reply to a video or voice note with `.tomp3`.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Download failed.")
		return
	}
	ffPath, cleanup, err := utils.GetFFmpegPath()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg unavailable.")
		return
	}
	defer cleanup()
	tmpDir, _ := os.MkdirTemp("", "whatbot-tomp3-*")
	defer os.RemoveAll(tmpDir)
	inPath := filepath.Join(tmpDir, "in.media")
	outPath := filepath.Join(tmpDir, "out.mp3")
	_ = os.WriteFile(inPath, data, 0600)
	if out, err := exec.CommandContext(ctx, ffPath, "-y", "-i", inPath, "-vn", "-c:a", "libmp3lame", "-b:a", "128k", outPath).CombinedOutput(); err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Conversion failed: %v %s", err, string(out)))
		return
	}
	mp3, _ := os.ReadFile(outPath)
	_, _ = utils.SendAudioData(ctx, client, msg.Info.Chat, mp3, "audio/mpeg", 0, false)
}

func cmdTrim(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: reply to video/audio with `.trim <start_sec> <end_sec>`")
		return
	}
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || (target.GetVideoMessage() == nil && target.GetAudioMessage() == nil) {
		utils.SendReply(client, msg.Info.Chat, "✂️ Reply to a video or audio message.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Download failed.")
		return
	}
	ffPath, cleanup, err := utils.GetFFmpegPath()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg unavailable.")
		return
	}
	defer cleanup()
	tmpDir, _ := os.MkdirTemp("", "whatbot-trim-*")
	defer os.RemoveAll(tmpDir)
	inPath := filepath.Join(tmpDir, "in.media")
	_ = os.WriteFile(inPath, data, 0600)

	if target.GetAudioMessage() != nil {
		outPath := filepath.Join(tmpDir, "out.mp3")
		if out, err := exec.CommandContext(ctx, ffPath, "-y", "-ss", fields[0], "-to", fields[1], "-i", inPath, "-c:a", "libmp3lame", outPath).CombinedOutput(); err != nil {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Trim failed: %v %s", err, string(out)))
			return
		}
		outBytes, _ := os.ReadFile(outPath)
		_, _ = utils.SendAudioData(ctx, client, msg.Info.Chat, outBytes, "audio/mpeg", 0, false)
		return
	}
	outPath := filepath.Join(tmpDir, "out.mp4")
	if out, err := exec.CommandContext(ctx, ffPath, "-y", "-ss", fields[0], "-to", fields[1], "-i", inPath, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart", outPath).CombinedOutput(); err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Trim failed: %v %s", err, string(out)))
		return
	}
	outBytes, _ := os.ReadFile(outPath)
	thumb, secs := utils.ExtractVideoMeta(outPath)
	_, _ = utils.SendVideoData(ctx, client, msg.Info.Chat, outBytes, "✂️ Trimmed Video", secs, thumb)
}

func cmdCompress(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || (target.GetImageMessage() == nil && target.GetVideoMessage() == nil) {
		utils.SendReply(client, msg.Info.Chat, "🗜️ Reply to an image or video with `.compress`.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Download failed.")
		return
	}
	if target.GetImageMessage() != nil {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Decode failed.")
			return
		}
		var buf bytes.Buffer
		_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 45})
		_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, buf.Bytes(), "image/jpeg", fmt.Sprintf("🗜️ Compressed: %.1f KB → %.1f KB", float64(len(data))/1024, float64(buf.Len())/1024))
		return
	}
	ffPath, cleanup, err := utils.GetFFmpegPath()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg unavailable.")
		return
	}
	defer cleanup()
	tmpDir, _ := os.MkdirTemp("", "whatbot-comp-*")
	defer os.RemoveAll(tmpDir)
	inPath := filepath.Join(tmpDir, "in.mp4")
	outPath := filepath.Join(tmpDir, "out.mp4")
	_ = os.WriteFile(inPath, data, 0600)
	if out, err := exec.CommandContext(ctx, ffPath, "-y", "-i", inPath, "-vf", "scale='min(720,iw)':-2", "-c:v", "libx264", "-crf", "30", "-preset", "veryfast", "-c:a", "aac", "-b:a", "96k", "-movflags", "+faststart", outPath).CombinedOutput(); err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Compression failed: %v %s", err, string(out)))
		return
	}
	outBytes, _ := os.ReadFile(outPath)
	thumb, secs := utils.ExtractVideoMeta(outPath)
	_, _ = utils.SendVideoData(ctx, client, msg.Info.Chat, outBytes, fmt.Sprintf("🗜️ %.2f MB → %.2f MB", float64(len(data))/1e6, float64(len(outBytes))/1e6), secs, thumb)
}

func cmdMediaQuality(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || !utils.HasDownloadableMedia(target) {
		utils.SendReply(client, msg.Info.Chat, "🔍 Reply to a media message with `.mediaquality`.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, _, mimeType, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Download failed.")
		return
	}
	hasAudio, dur := utils.ProbeMediaStreams(data)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🔍 *Media Inspector*\n• MIME: `%s`\n• Size: `%.2f KB`\n• Has Audio Stream: `%v`\n• Duration: `%.2fs`",
		mimeType, float64(len(data))/1024, hasAudio, dur,
	))
}

func cmdToDocMsg(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || !utils.HasDownloadableMedia(target) {
		utils.SendReply(client, msg.Info.Chat, "📄 Reply to any media message with `.todocmsg [filename]`.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, _, mimeType, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Download failed.")
		return
	}
	name := utils.SanitizeFilename(strings.Join(args, " "))
	if name == "media" {
		name = "file.bin"
		if strings.HasPrefix(mimeType, "image/") {
			name = "image.jpg"
		} else if strings.HasPrefix(mimeType, "video/") {
			name = "video.mp4"
		} else if strings.HasPrefix(mimeType, "audio/") {
			name = "audio.mp3"
		}
	}
	_, _ = utils.SendDocumentData(ctx, client, msg.Info.Chat, data, mimeType, name, "📄 "+name)
}

func cmdGenerateLRC(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.generatelrc <song title>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var results []struct {
		TrackName    string `json:"trackName"`
		ArtistName   string `json:"artistName"`
		SyncedLyrics string `json:"syncedLyrics"`
		PlainLyrics  string `json:"plainLyrics"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://lrclib.net/api/search?q="+url.QueryEscape(q), nil, &results); err != nil || len(results) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ No lyrics found for `"+q+"`.")
		return
	}
	r := results[0]
	content := r.SyncedLyrics
	if content == "" {
		content = r.PlainLyrics
	}
	fname := utils.SanitizeFilename(r.ArtistName+" - "+r.TrackName) + ".lrc"
	_, _ = utils.SendDocumentData(ctx, client, msg.Info.Chat, []byte(content), "text/plain", fname, "🎶 "+r.TrackName+" — "+r.ArtistName)
}

func cmdCuaca(client *whatsmeow.Client, msg *events.Message, args []string) {
	city := strings.TrimSpace(strings.Join(args, " "))
	if city == "" {
		city = "Jakarta"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var wttr struct {
		CurrentCondition []struct {
			TempC       string `json:"temp_C"`
			FeelsLikeC  string `json:"FeelsLikeC"`
			Humidity    string `json:"humidity"`
			WindspeedKm string `json:"windspeedKmph"`
			WeatherDesc []struct {
				Value string `json:"value"`
			} `json:"weatherDesc"`
		} `json:"current_condition"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://wttr.in/"+url.PathEscape(city)+"?format=j1", nil, &wttr); err != nil || len(wttr.CurrentCondition) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch weather for `"+city+"`.")
		return
	}
	c := wttr.CurrentCondition[0]
	desc := ""
	if len(c.WeatherDesc) > 0 {
		desc = c.WeatherDesc[0].Value
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🌤️ *Weather — %s*\n• Condition: *%s*\n• Temp: `%s°C` (Feels like `%s°C`)\n• Humidity: `%s%%`\n• Wind: `%s km/h`",
		city, desc, c.TempC, c.FeelsLikeC, c.Humidity, c.WindspeedKm,
	))
}

func cmdGempa(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var bmkg struct {
		Infogempa struct {
			Gempa struct {
				Tanggal   string `json:"Tanggal"`
				Jam       string `json:"Jam"`
				Magnitude string `json:"Magnitude"`
				Kedalaman string `json:"Kedalaman"`
				Wilayah   string `json:"Wilayah"`
				Potensi   string `json:"Potensi"`
				Dirasakan string `json:"Dirasakan"`
				Shakemap  string `json:"Shakemap"`
			} `json:"gempa"`
		} `json:"Infogempa"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://data.bmkg.go.id/DataMKG/TEWS/autogempa.json", nil, &bmkg); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to query BMKG: "+err.Error())
		return
	}
	g := bmkg.Infogempa.Gempa
	caption := fmt.Sprintf(
		"🌋 *BMKG Latest Earthquake*\n• Time: `%s %s`\n• Magnitude: *%s* | Depth: `%s`\n• Region: %s\n• Potential: %s\n• Felt: %s",
		g.Tanggal, g.Jam, g.Magnitude, g.Kedalaman, g.Wilayah, g.Potensi, g.Dirasakan,
	)
	if g.Shakemap != "" {
		if img, ct, err := utils.DownloadHTTPBytes(ctx, "https://data.bmkg.go.id/DataMKG/TEWS/"+g.Shakemap, nil); err == nil {
			_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, ct, caption)
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, caption)
}

func cmdTranslate(client *whatsmeow.Client, msg *events.Message, args []string) {
	parts := strings.SplitN(strings.TrimSpace(strings.Join(args, " ")), " ", 2)
	if len(parts) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.translate <lang_code> <text>` (e.g. `.translate en selamat pagi`)")
		return
	}
	lang, text := parts[0], parts[1]
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	apiURL := fmt.Sprintf("https://translate.googleapis.com/translate_a/single?client=gtx&sl=auto&tl=%s&dt=t&q=%s", url.QueryEscape(lang), url.QueryEscape(text))
	raw, _, err := utils.DownloadHTTPBytes(ctx, apiURL, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Translate failed: "+err.Error())
		return
	}
	var parsed []any
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not parse translation.")
		return
	}
	var out strings.Builder
	if segs, ok := parsed[0].([]any); ok {
		for _, seg := range segs {
			if pair, ok := seg.([]any); ok && len(pair) > 0 {
				if s, ok := pair[0].(string); ok {
					out.WriteString(s)
				}
			}
		}
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🌐 *Translation (%s):*\n%s", lang, out.String()))
}

func cmdTTS(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.tts [lang] <text>` (e.g. `.tts id halo semuanya`)")
		return
	}
	lang := "id"
	text := raw
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) == 2 && len(parts[0]) == 2 {
		lang = strings.ToLower(parts[0])
		text = parts[1]
	}
	if len(text) > 200 {
		text = text[:200]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ttsURL := fmt.Sprintf("https://translate.google.com/translate_tts?ie=UTF-8&tl=%s&client=tw-ob&q=%s", url.QueryEscape(lang), url.QueryEscape(text))
	mp3, _, err := utils.DownloadHTTPBytes(ctx, ttsURL, nil)
	if err != nil || len(mp3) < 200 {
		utils.SendReply(client, msg.Info.Chat, "❌ TTS failed.")
		return
	}
	_, _ = utils.SendAudioData(ctx, client, msg.Info.Chat, mp3, "audio/mpeg", 0, true)
}

func cmdUpload(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || !utils.HasDownloadableMedia(target) {
		utils.SendReply(client, msg.Info.Chat, "☁️ Reply to a media message with `.upload`.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	data, _, mimeType, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Download failed.")
		return
	}
	ext := ".bin"
	if strings.HasPrefix(mimeType, "image/png") {
		ext = ".png"
	} else if strings.HasPrefix(mimeType, "image/") {
		ext = ".jpg"
	} else if strings.HasPrefix(mimeType, "video/") {
		ext = ".mp4"
	} else if strings.HasPrefix(mimeType, "audio/") {
		ext = ".mp3"
	}
	link, err := utils.UploadToLitterbox(data, "whatbot"+ext)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Upload failed: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, "☁️ *Uploaded (24h):*\n"+link)
}

func cmdReadViewOnceAlias(client *whatsmeow.Client, msg *events.Message, args []string) {
	if cmd, ok := handlers.Commands["get"]; ok {
		cmd.Execute(client, msg, args)
		return
	}
	utils.SendReply(client, msg.Info.Chat, "Use `/get` while replying to a view-once message.")
}

func cmdWhatMusicAlias(client *whatsmeow.Client, msg *events.Message, args []string) {
	if cmd, ok := handlers.Commands["shazam"]; ok {
		cmd.Execute(client, msg, args)
		return
	}
	utils.SendReply(client, msg.Info.Chat, "Use `/shazam` while replying to audio or video.")
}

func cmdGetMsgID(client *whatsmeow.Client, msg *events.Message, args []string) {
	id := string(msg.Info.ID)
	if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil && ctxInfo.GetStanzaID() != "" {
		id = ctxInfo.GetStanzaID()
	}
	utils.SendReply(client, msg.Info.Chat, "🆔 Message ID: `"+id+"`")
}

func cmdGetIDCH(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.getidch https://whatsapp.com/channel/<invite_key>`")
		return
	}
	key := raw
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		key = raw[idx+1:]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	meta, err := client.GetNewsletterInfoWithInvite(ctx, key)
	if err != nil || meta == nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not resolve channel: "+fmt.Sprint(err))
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📢 *Channel Info*\n• Name: *%s*\n• JID: `%s`\n• Subscribers: `%d`", meta.ThreadMeta.Name.Text, meta.ID.String(), meta.ThreadMeta.SubscriberCount))
}

func cmdWAStatus(client *whatsmeow.Client, msg *events.Message, args []string) {
	id := "unknown"
	if client != nil && client.Store != nil && client.Store.ID != nil {
		id = client.Store.ID.String()
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📱 *WhatsApp Status*\n• Account: `%s`\n• Connected: `%v`\n• Mode: Android Companion (View-Once Ready)", id, client.IsConnected()))
}
