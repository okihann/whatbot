package utils

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const (
	MaxStaticStickerBytes   = 100 * 1024 // WhatsApp static WebP sticker limit
	MaxAnimatedStickerBytes = 900 * 1024 // WhatsApp animated/sound sticker limit
	DefaultStickerPackName  = "NopeBot"
	DefaultStickerAuthor    = "nopebot"
)

// BuildStickerEXIF creates a WhatsApp-compliant TIFF/EXIF payload carrying
// sticker pack metadata so iOS and Android can save and favorite the sticker.
func BuildStickerEXIF(packName, author string, emojis []string) []byte {
	if strings.TrimSpace(packName) == "" {
		packName = DefaultStickerPackName
	}
	if strings.TrimSpace(author) == "" {
		author = DefaultStickerAuthor
	}
	if len(emojis) == 0 {
		emojis = []string{"✨"}
	}

	payload, _ := json.Marshal(map[string]any{
		"sticker-pack-id":        "com.nopebot.stickers",
		"sticker-pack-name":      packName,
		"sticker-pack-publisher": author,
		"emojis":                 emojis,
	})

	// Standard little-endian TIFF header with 1 IFD tag (0x5741 = "WA").
	header := []byte{
		0x49, 0x49, 0x2A, 0x00, // "II*\0"
		0x08, 0x00, 0x00, 0x00, // offset to first IFD = 8
		0x01, 0x00, // 1 directory entry
		0x41, 0x57, // tag 0x5741
		0x07, 0x00, // type UNDEFINED
		0x00, 0x00, 0x00, 0x00, // length (filled below)
		0x16, 0x00, 0x00, 0x00, // offset to payload = 22
	}
	binary.LittleEndian.PutUint32(header[14:18], uint32(len(payload)))

	out := make([]byte, 0, len(header)+len(payload))
	out = append(out, header...)
	out = append(out, payload...)
	return out
}

// InjectWebPEXIF injects an EXIF chunk directly into a RIFF/WEBP container
// without requiring external webpmux tools, setting the required VP8X flags.
func InjectWebPEXIF(webpData, exifData []byte) []byte {
	if len(webpData) < 16 || string(webpData[0:4]) != "RIFF" || string(webpData[8:12]) != "WEBP" {
		return webpData
	}

	type chunk struct {
		id  string
		raw []byte
	}
	var chunks []chunk
	hasVP8X := false
	isAnim := false
	hasAlpha := false

	offset := 12
	for offset+8 <= len(webpData) {
		id := string(webpData[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(webpData[offset+4 : offset+8]))
		padded := size + (size % 2)
		end := offset + 8 + padded
		if end > len(webpData) {
			end = len(webpData)
		}
		raw := make([]byte, end-offset)
		copy(raw, webpData[offset:end])

		switch id {
		case "VP8X":
			hasVP8X = true
		case "ANIM", "ANMF":
			isAnim = true
		case "ALPH", "VP8L":
			hasAlpha = true
		}
		if id != "EXIF" {
			chunks = append(chunks, chunk{id: id, raw: raw})
		}
		offset = end
	}

	if !hasVP8X {
		vp8x := make([]byte, 18)
		copy(vp8x[0:4], "VP8X")
		binary.LittleEndian.PutUint32(vp8x[4:8], 10)
		var flags byte = 0x08 // EXIF bit
		if hasAlpha {
			flags |= 0x10 // Alpha bit
		}
		if isAnim {
			flags |= 0x02 // Animation bit
		}
		vp8x[8] = flags
		// Canvas width-1 and height-1 (511 = 0x0001FF, 24-bit little-endian).
		vp8x[12], vp8x[13], vp8x[14] = 0xFF, 0x01, 0x00
		vp8x[15], vp8x[16], vp8x[17] = 0xFF, 0x01, 0x00
		chunks = append([]chunk{{id: "VP8X", raw: vp8x}}, chunks...)
	} else {
		for i := range chunks {
			if chunks[i].id == "VP8X" && len(chunks[i].raw) >= 18 {
				chunks[i].raw[8] |= 0x08 // EXIF
				if isAnim {
					chunks[i].raw[8] |= 0x02
				}
				break
			}
		}
	}

	exifChunk := make([]byte, 8+len(exifData)+(len(exifData)%2))
	copy(exifChunk[0:4], "EXIF")
	binary.LittleEndian.PutUint32(exifChunk[4:8], uint32(len(exifData)))
	copy(exifChunk[8:], exifData)
	chunks = append(chunks, chunk{id: "EXIF", raw: exifChunk})

	var body bytes.Buffer
	for _, c := range chunks {
		body.Write(c.raw)
	}

	out := make([]byte, 12+body.Len())
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(body.Len()+4))
	copy(out[8:12], "WEBP")
	copy(out[12:], body.Bytes())
	return out
}

func stickerVideoFilter(crop bool, fps int) string {
	var base string
	if crop {
		base = "crop=min(iw\\,ih):min(iw\\,ih):(iw-min(iw\\,ih))/2:(ih-min(iw\\,ih))/2,scale=512:512,format=yuva420p"
	} else {
		base = "scale=512:512:force_original_aspect_ratio=decrease,format=rgba,pad=512:512:(ow-iw)/2:(oh-ih)/2:color=0x00000000,format=yuva420p"
	}
	if fps > 0 {
		return fmt.Sprintf("fps=%d,%s", fps, base)
	}
	return base
}

// ConvertImageToWebP converts PNG/JPEG/GIF/WebP bytes into a 512x512 WhatsApp
// static WebP sticker under 100 KB with embedded EXIF metadata.
func ConvertImageToWebP(srcData []byte, crop bool, packName, author string) ([]byte, error) {
	ffmpegPath, cleanup, err := GetFFmpegPath()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg unavailable: %w", err)
	}
	defer cleanup()

	workDir, err := os.MkdirTemp("", "nopebot-stk-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	inPath := filepath.Join(workDir, "in.img")
	outPath := filepath.Join(workDir, "out.webp")
	if err := os.WriteFile(inPath, srcData, 0644); err != nil {
		return nil, err
	}

	exif := BuildStickerEXIF(packName, author, []string{"✨"})
	filter := stickerVideoFilter(crop, 0)

	qualities := []int{80, 65, 50, 35, 20}
	var lastWebP []byte
	var lastErr error

	for _, q := range qualities {
		cmd := exec.Command(ffmpegPath,
			"-y",
			"-i", inPath,
			"-vframes", "1",
			"-vf", filter,
			"-vcodec", "libwebp",
			"-lossless", "0",
			"-q:v", strconv.Itoa(q),
			"-preset", "default",
			"-an",
			outPath,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("ffmpeg webp encode failed: %v: %s", err, string(out))
			continue
		}
		raw, err := os.ReadFile(outPath)
		if err != nil {
			lastErr = err
			continue
		}
		withExif := InjectWebPEXIF(raw, exif)
		lastWebP = withExif
		if len(withExif) <= MaxStaticStickerBytes {
			return withExif, nil
		}
	}

	if len(lastWebP) > 0 {
		return lastWebP, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("could not convert image to WebP")
}

// ConvertVideoToAnimatedWebP converts a video or animated GIF into a 512x512
// looping animated WebP sticker within WhatsApp's size limits.
func ConvertVideoToAnimatedWebP(srcData []byte, maxSeconds int, crop bool, packName, author string) ([]byte, error) {
	if maxSeconds <= 0 || maxSeconds > 10 {
		maxSeconds = 7
	}

	ffmpegPath, cleanup, err := GetFFmpegPath()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg unavailable: %w", err)
	}
	defer cleanup()

	workDir, err := os.MkdirTemp("", "nopebot-astk-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	inPath := filepath.Join(workDir, "in.media")
	outPath := filepath.Join(workDir, "out.webp")
	if err := os.WriteFile(inPath, srcData, 0644); err != nil {
		return nil, err
	}

	exif := BuildStickerEXIF(packName, author, []string{"🔊", "✨"})

	steps := []struct {
		q   int
		fps int
	}{
		{60, 15},
		{45, 12},
		{30, 10},
		{18, 8},
	}

	var lastErr error
	for _, step := range steps {
		cmd := exec.Command(ffmpegPath,
			"-y",
			"-i", inPath,
			"-t", strconv.Itoa(maxSeconds),
			"-vf", stickerVideoFilter(crop, step.fps),
			"-vcodec", "libwebp",
			"-lossless", "0",
			"-q:v", strconv.Itoa(step.q),
			"-preset", "default",
			"-loop", "0",
			"-an",
			outPath,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("ffmpeg animated webp failed: %v: %s", err, string(out))
			continue
		}
		raw, err := os.ReadFile(outPath)
		if err != nil {
			lastErr = err
			continue
		}
		withExif := InjectWebPEXIF(raw, exif)
		if len(withExif) <= MaxAnimatedStickerBytes {
			return withExif, nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("animated sticker exceeds %d KB even at lowest quality (try a shorter clip)", MaxAnimatedStickerBytes/1024)
}

// ProbeMediaStreams checks whether mediaBytes contains an audio stream and
// returns its duration in seconds.
func ProbeMediaStreams(mediaBytes []byte) (hasAudio bool, duration float64) {
	ffprobePath, cleanup, err := GetFFprobePath()
	if err != nil {
		return false, 0
	}
	defer cleanup()

	workDir, err := os.MkdirTemp("", "nopebot-probe-*")
	if err != nil {
		return false, 0
	}
	defer os.RemoveAll(workDir)

	path := filepath.Join(workDir, "media")
	if err := os.WriteFile(path, mediaBytes, 0644); err != nil {
		return false, 0
	}

	out, err := exec.Command(ffprobePath,
		"-v", "error",
		"-show_entries", "stream=codec_type:format=duration",
		"-of", "json",
		path,
	).Output()
	if err != nil {
		return false, 0
	}

	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return false, 0
	}
	for _, s := range parsed.Streams {
		if s.CodecType == "audio" {
			hasAudio = true
			break
		}
	}
	if d, err := strconv.ParseFloat(strings.TrimSpace(parsed.Format.Duration), 64); err == nil && d > 0 {
		duration = d
	}
	return hasAudio, duration
}

// ConvertToStickerOpus encodes audio (or a video's audio track) into the exact
// OGG/Opus format required for WhatsApp Sound Stickers to play on phones.
func ConvertToStickerOpus(srcData []byte, maxSeconds int) ([]byte, uint32, error) {
	if maxSeconds <= 0 || maxSeconds > 25 {
		maxSeconds = 10
	}

	ffmpegPath, cleanup, err := GetFFmpegPath()
	if err != nil {
		return nil, 0, fmt.Errorf("ffmpeg unavailable: %w", err)
	}
	defer cleanup()

	workDir, err := os.MkdirTemp("", "nopebot-sopus-*")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(workDir)

	inPath := filepath.Join(workDir, "in.media")
	outPath := filepath.Join(workDir, "out.ogg")
	if err := os.WriteFile(inPath, srcData, 0644); err != nil {
		return nil, 0, err
	}

	cmd := exec.Command(ffmpegPath,
		"-y",
		"-i", inPath,
		"-t", strconv.Itoa(maxSeconds),
		"-vn",
		"-c:a", "libopus",
		"-b:a", "64k",
		"-ac", "1",
		"-ar", "48000",
		"-vbr", "on",
		"-compression_level", "10",
		"-frame_duration", "20",
		"-avoid_negative_ts", "make_zero",
		"-f", "ogg",
		outPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, 0, fmt.Errorf("ffmpeg opus encode failed: %v: %s", err, string(out))
	}

	oggBytes, err := os.ReadFile(outPath)
	if err != nil {
		return nil, 0, err
	}
	if len(oggBytes) == 0 {
		return nil, 0, fmt.Errorf("produced empty audio track")
	}

	_, dur := ProbeMediaStreams(oggBytes)
	secs := uint32(math.Round(dur))
	if secs == 0 {
		secs = 1
	}
	return oggBytes, secs, nil
}

func buildReplyContext(quoted *events.Message) *waE2E.ContextInfo {
	if quoted == nil || quoted.Info.ID == "" {
		return nil
	}
	return &waE2E.ContextInfo{
		StanzaID:      proto.String(quoted.Info.ID),
		Participant:   proto.String(quoted.Info.Sender.ToNonAD().String()),
		QuotedMessage: quoted.Message,
	}
}

// SendSticker uploads a WebP sticker and sends a StickerMessage.
func SendSticker(ctx context.Context, client *whatsmeow.Client, chatJID waTypes.JID, webpData []byte, isAnimated bool, quoted *events.Message) (whatsmeow.SendResponse, error) {
	uploaded, err := client.Upload(ctx, webpData, whatsmeow.MediaImage)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("upload sticker: %w", err)
	}

	stk := &waE2E.StickerMessage{
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		Mimetype:      proto.String("image/webp"),
		Width:         proto.Uint32(512),
		Height:        proto.Uint32(512),
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uploaded.FileLength),
		IsAnimated:    proto.Bool(isAnimated),
		ContextInfo:   buildReplyContext(quoted),
	}

	return client.SendMessage(ctx, chatJID, &waE2E.Message{StickerMessage: stk})
}

// SendAudioSticker uploads both the WebP sticker and its Opus audio track and
// sends a native WhatsApp Sound Sticker (AudioStickerMessage).
func SendAudioSticker(
	ctx context.Context,
	client *whatsmeow.Client,
	chatJID waTypes.JID,
	webpData []byte,
	isAnimated bool,
	oggData []byte,
	audioSeconds uint32,
	quoted *events.Message,
) (whatsmeow.SendResponse, error) {
	uploadedStk, err := client.Upload(ctx, webpData, whatsmeow.MediaImage)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("upload sticker: %w", err)
	}
	uploadedAud, err := client.Upload(ctx, oggData, whatsmeow.MediaAudio)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("upload sticker audio: %w", err)
	}

	if audioSeconds == 0 {
		audioSeconds = 1
	}

	stk := &waE2E.StickerMessage{
		URL:           proto.String(uploadedStk.URL),
		DirectPath:    proto.String(uploadedStk.DirectPath),
		MediaKey:      uploadedStk.MediaKey,
		Mimetype:      proto.String("image/webp"),
		Width:         proto.Uint32(512),
		Height:        proto.Uint32(512),
		FileEncSHA256: uploadedStk.FileEncSHA256,
		FileSHA256:    uploadedStk.FileSHA256,
		FileLength:    proto.Uint64(uploadedStk.FileLength),
		IsAnimated:    proto.Bool(isAnimated),
		ContextInfo:   buildReplyContext(quoted),
		Audio: &waE2E.StickerMessage_AudioMessage{
			AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(uploadedAud.URL),
				DirectPath:    proto.String(uploadedAud.DirectPath),
				MediaKey:      uploadedAud.MediaKey,
				Mimetype:      proto.String("audio/ogg; codecs=opus"),
				FileEncSHA256: uploadedAud.FileEncSHA256,
				FileSHA256:    uploadedAud.FileSHA256,
				FileLength:    proto.Uint64(uploadedAud.FileLength),
				Seconds:       proto.Uint32(audioSeconds),
			},
		},
	}

	msg := &waE2E.Message{
		AudioStickerMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{
				StickerMessage: stk,
			},
		},
	}

	return client.SendMessage(ctx, chatJID, msg)
}
