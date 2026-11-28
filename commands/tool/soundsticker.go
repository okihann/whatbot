package tool

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	soundStickerSessionTTL = 60 * time.Second
	maxSoundVideoSeconds   = 10
	maxSoundAudioSeconds   = 25
)

type soundStickerSession struct {
	WaitingFor   string // "audio" or "visual"
	WebP         []byte
	IsAnimated   bool
	Ogg          []byte
	AudioSeconds uint32
	Crop         bool
	MaxSeconds   int
	PackName     string
	Author       string
	PromptMsgID  string
	Timer        *time.Timer
}

var (
	soundSessionsMu sync.Mutex
	soundSessions   = make(map[string]*soundStickerSession)
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "soundsticker",
		Aliases:     []string{"asticker", "sstick", "sa"},
		Description: "Create a Sound Sticker from video with audio, or image + voice note.",
		Category:    "media",
		Execute:     executeSoundSticker,
	})
	handlers.RegisterInteractiveSessionHandler(handleSoundStickerReply)
}

func soundSessionKey(chatJID, senderJID waTypes.JID) string {
	return chatJID.ToNonAD().String() + ":" + senderJID.ToNonAD().String()
}

func getSoundSession(key string) *soundStickerSession {
	soundSessionsMu.Lock()
	defer soundSessionsMu.Unlock()
	return soundSessions[key]
}

func setSoundSession(key string, sess *soundStickerSession) {
	soundSessionsMu.Lock()
	defer soundSessionsMu.Unlock()
	if prev, ok := soundSessions[key]; ok && prev.Timer != nil {
		prev.Timer.Stop()
	}
	sess.Timer = time.AfterFunc(soundStickerSessionTTL, func() {
		clearSoundSession(key)
	})
	soundSessions[key] = sess
}

func clearSoundSession(key string) {
	soundSessionsMu.Lock()
	defer soundSessionsMu.Unlock()
	if prev, ok := soundSessions[key]; ok {
		if prev.Timer != nil {
			prev.Timer.Stop()
		}
		delete(soundSessions, key)
	}
}

type soundStickerOpts struct {
	Cancel     bool
	Crop       bool
	MaxSeconds int
	PackName   string
	Author     string
}

func parseSoundStickerArgs(args []string) soundStickerOpts {
	opts := soundStickerOpts{
		PackName: utils.DefaultStickerPackName,
		Author:   utils.DefaultStickerAuthor,
	}
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		return opts
	}

	lower := strings.ToLower(raw)
	if lower == "cancel" || lower == "stop" || lower == "batal" {
		opts.Cancel = true
		return opts
	}

	// Consume leading option tokens like "crop" or a numeric duration (e.g. "6").
	var remaining []string
	tokens := strings.Fields(raw)
	idx := 0
	for idx < len(tokens) {
		tok := tokens[idx]
		if strings.EqualFold(tok, "crop") {
			opts.Crop = true
			idx++
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSuffix(strings.ToLower(tok), "s")); err == nil && n >= 1 && n <= maxSoundAudioSeconds {
			opts.MaxSeconds = n
			idx++
			continue
		}
		break
	}
	if idx < len(tokens) {
		remaining = tokens[idx:]
	}

	meta := strings.TrimSpace(strings.Join(remaining, " "))
	if meta != "" {
		if strings.Contains(meta, "|") {
			parts := strings.SplitN(meta, "|", 2)
			if p := strings.TrimSpace(parts[0]); p != "" {
				opts.PackName = p
			}
			if a := strings.TrimSpace(parts[1]); a != "" {
				opts.Author = a
			}
		} else {
			opts.PackName = meta
		}
	}
	return opts
}

func resolveSoundStickerMedia(client *whatsmeow.Client, msg *events.Message) (src *waE2E.Message, preCached []byte, kind string) {
	classify := func(m *waE2E.Message) string {
		if m == nil {
			return ""
		}
		switch {
		case m.VideoMessage != nil:
			return "video"
		case m.AudioMessage != nil:
			return "audio"
		case m.ImageMessage != nil:
			return "image"
		case m.StickerMessage != nil:
			return "sticker"
		case m.DocumentMessage != nil:
			mime := strings.ToLower(m.DocumentMessage.GetMimetype())
			switch {
			case strings.HasPrefix(mime, "video/"):
				return "video"
			case strings.HasPrefix(mime, "audio/"):
				return "audio"
			case strings.HasPrefix(mime, "image/gif"):
				return "video"
			case strings.HasPrefix(mime, "image/"):
				return "image"
			}
		}
		return ""
	}

	core := utils.UnwrapMessage(msg.Message)
	if k := classify(core); k != "" {
		return core, nil, k
	}

	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil {
		return nil, nil, ""
	}

	botID := ""
	if client.Store != nil && client.Store.ID != nil {
		botID = client.Store.ID.ToNonAD().User
	}
	if cached, ok := utils.DefaultMessageCache.Get(botID, ctxInfo.GetStanzaID()); ok && cached != nil {
		if k := classify(cached.Message); k != "" && (len(cached.MediaData) > 0 || utils.HasDownloadableMedia(cached.Message)) {
			return cached.Message, cached.MediaData, k
		}
	}

	quoted := utils.UnwrapMessage(ctxInfo.QuotedMessage)
	if k := classify(quoted); k != "" {
		return quoted, nil, k
	}
	return nil, nil, ""
}

const soundStickerUsage = "🔊 *WhatsApp Sound Sticker*\n\n" +
	"• Send or reply to a *video with sound* with `/sa`\n" +
	"• Or combine in 2 steps: reply `/sa` to a *photo/sticker/GIF*, then reply to my prompt with an *audio/voice note* (or audio first, then photo!)\n" +
	"• Options: `/sa crop`, `/sa 6` (seconds), `/sa Pack | Author`, `/sa cancel`"

func executeSoundSticker(client *whatsmeow.Client, msg *events.Message, args []string) {
	key := soundSessionKey(msg.Info.Chat, msg.Info.Sender)
	opts := parseSoundStickerArgs(args)

	if opts.Cancel {
		clearSoundSession(key)
		utils.SendReply(client, msg.Info.Chat, "✅ Sound sticker session cancelled.")
		return
	}

	srcMsg, preCached, kind := resolveSoundStickerMedia(client, msg)
	if srcMsg == nil {
		utils.SendReply(client, msg.Info.Chat, soundStickerUsage)
		return
	}

	go processSoundStickerStep(client, msg, key, srcMsg, preCached, kind, opts)
}

// handleSoundStickerReply allows the user to simply reply to the bot's prompt
// with the missing audio or image/video without needing to re-type /sa.
func handleSoundStickerReply(client *whatsmeow.Client, msg *events.Message) bool {
	key := soundSessionKey(msg.Info.Chat, msg.Info.Sender)
	sess := getSoundSession(key)
	if sess == nil {
		return false
	}

	// Only intercept direct media messages or replies to our prompt.
	ctxInfo := utils.ExtractContextInfo(msg.Message)
	isReplyToPrompt := ctxInfo != nil && sess.PromptMsgID != "" && ctxInfo.GetStanzaID() == sess.PromptMsgID

	core := utils.UnwrapMessage(msg.Message)
	isDirectMedia := utils.IsMediaMessage(core)
	if !isReplyToPrompt && !isDirectMedia {
		return false
	}

	// Let explicit commands (starting with prefix) go through normal command handling.
	text := strings.TrimSpace(utils.GetTextFromMessage(msg.Message))
	if strings.HasPrefix(text, "/") {
		return false
	}

	srcMsg, preCached, kind := resolveSoundStickerMedia(client, msg)
	if srcMsg == nil {
		return false
	}

	if (sess.WaitingFor == "audio" && kind != "audio" && kind != "video") ||
		(sess.WaitingFor == "visual" && kind != "image" && kind != "sticker" && kind != "video") {
		return false
	}

	opts := soundStickerOpts{
		Crop:       sess.Crop,
		MaxSeconds: sess.MaxSeconds,
		PackName:   sess.PackName,
		Author:     sess.Author,
	}
	go processSoundStickerStep(client, msg, key, srcMsg, preCached, kind, opts)
	return true
}

func processSoundStickerStep(
	client *whatsmeow.Client,
	msg *events.Message,
	key string,
	srcMsg *waE2E.Message,
	preCached []byte,
	kind string,
	opts soundStickerOpts,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	data := preCached
	if len(data) == 0 {
		var err error
		data, _, _, err = utils.DownloadMedia(ctx, client, srcMsg)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Failed to download media: "+err.Error())
			return
		}
	}

	existing := getSoundSession(key)

	// Case 1: One-step video with audio (unless user already saved visual and is supplying audio from a video).
	if kind == "video" && (existing == nil || existing.WaitingFor != "audio") {
		hasAudio, _ := utils.ProbeMediaStreams(data)
		if hasAudio && existing == nil {
			utils.SendReply(client, msg.Info.Chat, "🔊 Creating Sound Sticker from video...")
			vidSecs := opts.MaxSeconds
			if vidSecs <= 0 || vidSecs > maxSoundVideoSeconds {
				vidSecs = 7
			}
			audSecs := opts.MaxSeconds
			if audSecs <= 0 || audSecs > maxSoundAudioSeconds {
				audSecs = vidSecs
			}

			webpData, err := utils.ConvertVideoToAnimatedWebP(data, vidSecs, opts.Crop, opts.PackName, opts.Author)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "❌ Could not convert video to animated sticker: "+err.Error())
				return
			}
			oggData, secs, err := utils.ConvertToStickerOpus(data, audSecs)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "❌ Could not extract audio track: "+err.Error())
				return
			}

			if _, err := utils.SendAudioSticker(ctx, client, msg.Info.Chat, webpData, true, oggData, secs, msg); err != nil {
				fmt.Printf("[SOUND-STICKER] Send error: %v\n", err)
				utils.SendReply(client, msg.Info.Chat, "❌ Failed to send sound sticker: "+err.Error())
			}
			return
		}
	}

	// Case 2: Completing an active 2-step session.
	if existing != nil {
		if existing.WaitingFor == "audio" && (kind == "audio" || kind == "video") {
			clearSoundSession(key)
			utils.SendReply(client, msg.Info.Chat, "🔊 Combining sticker + audio...")

			audSecs := existing.MaxSeconds
			if opts.MaxSeconds > 0 {
				audSecs = opts.MaxSeconds
			}
			if audSecs <= 0 || audSecs > maxSoundAudioSeconds {
				audSecs = 10
			}

			oggData, secs, err := utils.ConvertToStickerOpus(data, audSecs)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "❌ Could not process audio track: "+err.Error())
				return
			}
			if _, err := utils.SendAudioSticker(ctx, client, msg.Info.Chat, existing.WebP, existing.IsAnimated, oggData, secs, msg); err != nil {
				fmt.Printf("[SOUND-STICKER] Send error: %v\n", err)
				utils.SendReply(client, msg.Info.Chat, "❌ Failed to send sound sticker: "+err.Error())
			}
			return
		}

		if existing.WaitingFor == "visual" && (kind == "image" || kind == "sticker" || kind == "video") {
			clearSoundSession(key)
			utils.SendReply(client, msg.Info.Chat, "🔊 Combining visual + saved audio...")

			webpData, isAnim, err := convertVisualForSoundSticker(srcMsg, data, kind, existing.MaxSeconds, existing.Crop, existing.PackName, existing.Author)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "❌ Could not convert visual to sticker: "+err.Error())
				return
			}
			if _, err := utils.SendAudioSticker(ctx, client, msg.Info.Chat, webpData, isAnim, existing.Ogg, existing.AudioSeconds, msg); err != nil {
				fmt.Printf("[SOUND-STICKER] Send error: %v\n", err)
				utils.SendReply(client, msg.Info.Chat, "❌ Failed to send sound sticker: "+err.Error())
			}
			return
		}
	}

	// Case 3: Start a new 2-step session.
	if kind == "audio" {
		audSecs := opts.MaxSeconds
		if audSecs <= 0 || audSecs > maxSoundAudioSeconds {
			audSecs = 10
		}
		oggData, secs, err := utils.ConvertToStickerOpus(data, audSecs)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Could not convert audio: "+err.Error())
			return
		}
		resp, _ := utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎵 *Audio saved (%ds)!*\nNow send a *photo, sticker, GIF, or video* (or reply to one with `/sa`) within 60s.\nType `/sa cancel` to abort.", secs))
		setSoundSession(key, &soundStickerSession{
			WaitingFor:   "visual",
			Ogg:          oggData,
			AudioSeconds: secs,
			Crop:         opts.Crop,
			MaxSeconds:   opts.MaxSeconds,
			PackName:     opts.PackName,
			Author:       opts.Author,
			PromptMsgID:  resp.ID,
		})
		return
	}

	// Visual-only (image, sticker, GIF, or silent video) -> wait for audio.
	webpData, isAnim, err := convertVisualForSoundSticker(srcMsg, data, kind, opts.MaxSeconds, opts.Crop, opts.PackName, opts.Author)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not prepare sticker: "+err.Error())
		return
	}
	resp, _ := utils.SendReply(client, msg.Info.Chat, "🖼️ *Sticker saved!*\nNow send a *voice note or audio* (or reply to one with `/sa`) within 60s to attach sound.\nType `/sa cancel` to abort.")
	setSoundSession(key, &soundStickerSession{
		WaitingFor:  "audio",
		WebP:        webpData,
		IsAnimated:  isAnim,
		Crop:        opts.Crop,
		MaxSeconds:  opts.MaxSeconds,
		PackName:    opts.PackName,
		Author:      opts.Author,
		PromptMsgID: resp.ID,
	})
}

func convertVisualForSoundSticker(
	srcMsg *waE2E.Message,
	data []byte,
	kind string,
	maxSecs int,
	crop bool,
	packName, author string,
) ([]byte, bool, error) {
	if kind == "sticker" && srcMsg.GetStickerMessage().GetIsAnimated() {
		return utils.InjectWebPEXIF(data, utils.BuildStickerEXIF(packName, author, []string{"🔊", "✨"})), true, nil
	}
	if kind == "video" {
		vidSecs := maxSecs
		if vidSecs <= 0 || vidSecs > maxSoundVideoSeconds {
			vidSecs = 7
		}
		webpData, err := utils.ConvertVideoToAnimatedWebP(data, vidSecs, crop, packName, author)
		return webpData, true, err
	}
	webpData, err := utils.ConvertImageToWebP(data, crop, packName, author)
	return webpData, false, err
}
