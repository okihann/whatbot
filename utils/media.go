package utils

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// UnwrapMessage peels away device-sent, ephemeral, view-once, captioned, and
// edited wrappers so callers always see the underlying concrete message.
func UnwrapMessage(msg *waE2E.Message) *waE2E.Message {
	for i := 0; i < 10 && msg != nil; i++ {
		switch {
		case msg.GetDeviceSentMessage().GetMessage() != nil:
			msg = msg.GetDeviceSentMessage().GetMessage()
		case msg.GetBotInvokeMessage().GetMessage() != nil:
			msg = msg.GetBotInvokeMessage().GetMessage()
		case msg.GetEphemeralMessage().GetMessage() != nil:
			msg = msg.GetEphemeralMessage().GetMessage()
		case msg.GetViewOnceMessage().GetMessage() != nil:
			msg = msg.GetViewOnceMessage().GetMessage()
		case msg.GetViewOnceMessageV2().GetMessage() != nil:
			msg = msg.GetViewOnceMessageV2().GetMessage()
		case msg.GetViewOnceMessageV2Extension().GetMessage() != nil:
			msg = msg.GetViewOnceMessageV2Extension().GetMessage()
		case msg.GetDocumentWithCaptionMessage().GetMessage() != nil:
			msg = msg.GetDocumentWithCaptionMessage().GetMessage()
		case msg.GetLottieStickerMessage().GetMessage() != nil:
			msg = msg.GetLottieStickerMessage().GetMessage()
		case msg.GetAudioStickerMessage().GetMessage() != nil:
			msg = msg.GetAudioStickerMessage().GetMessage()
		case msg.GetGroupMentionedMessage().GetMessage() != nil:
			msg = msg.GetGroupMentionedMessage().GetMessage()
		case msg.GetEditedMessage().GetMessage() != nil:
			msg = msg.GetEditedMessage().GetMessage()
		case msg.GetAssociatedChildMessage().GetMessage() != nil:
			msg = msg.GetAssociatedChildMessage().GetMessage()
		case msg.GetSpoilerMessage().GetMessage() != nil:
			msg = msg.GetSpoilerMessage().GetMessage()
		case msg.GetGroupStatusMentionMessage().GetMessage() != nil:
			msg = msg.GetGroupStatusMentionMessage().GetMessage()
		case msg.GetStatusMentionMessage().GetMessage() != nil:
			msg = msg.GetStatusMentionMessage().GetMessage()
		case msg.GetBotForwardedMessage().GetMessage() != nil:
			msg = msg.GetBotForwardedMessage().GetMessage()
		case msg.GetQuestionMessage().GetMessage() != nil:
			msg = msg.GetQuestionMessage().GetMessage()
		case msg.GetQuestionReplyMessage().GetMessage() != nil:
			msg = msg.GetQuestionReplyMessage().GetMessage()
		case msg.GetLimitSharingMessage().GetMessage() != nil:
			msg = msg.GetLimitSharingMessage().GetMessage()
		case msg.GetPollCreationMessageV4().GetMessage() != nil:
			msg = msg.GetPollCreationMessageV4().GetMessage()
		default:
			return msg
		}
	}
	return msg
}

// IsViewOnceMessage reports whether a message event or protobuf is a view-once
// message (either via wrapper types or via the inner ViewOnce flag).
func IsViewOnceMessage(evt *events.Message, raw *waE2E.Message) bool {
	if evt != nil && (evt.IsViewOnce || evt.IsViewOnceV2 || evt.IsViewOnceV2Extension) {
		return true
	}

	check := raw
	if check == nil && evt != nil {
		if evt.RawMessage != nil {
			check = evt.RawMessage
		} else {
			check = evt.Message
		}
	}

	for i := 0; i < 10 && check != nil; i++ {
		if check.GetViewOnceMessage() != nil ||
			check.GetViewOnceMessageV2() != nil ||
			check.GetViewOnceMessageV2Extension() != nil {
			return true
		}
		switch {
		case check.GetDeviceSentMessage().GetMessage() != nil:
			check = check.GetDeviceSentMessage().GetMessage()
		case check.GetEphemeralMessage().GetMessage() != nil:
			check = check.GetEphemeralMessage().GetMessage()
		case check.GetDocumentWithCaptionMessage().GetMessage() != nil:
			check = check.GetDocumentWithCaptionMessage().GetMessage()
		default:
			goto doneWalk
		}
	}
doneWalk:

	core := UnwrapMessage(check)
	if core == nil {
		return false
	}
	return core.GetImageMessage().GetViewOnce() ||
		core.GetVideoMessage().GetViewOnce() ||
		core.GetAudioMessage().GetViewOnce()
}

// ExtractContextInfo returns the ContextInfo from any common message variant.
func ExtractContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	core := UnwrapMessage(msg)
	if core == nil {
		return nil
	}
	switch {
	case core.GetExtendedTextMessage().GetContextInfo() != nil:
		return core.GetExtendedTextMessage().GetContextInfo()
	case core.GetImageMessage().GetContextInfo() != nil:
		return core.GetImageMessage().GetContextInfo()
	case core.GetVideoMessage().GetContextInfo() != nil:
		return core.GetVideoMessage().GetContextInfo()
	case core.GetAudioMessage().GetContextInfo() != nil:
		return core.GetAudioMessage().GetContextInfo()
	case core.GetDocumentMessage().GetContextInfo() != nil:
		return core.GetDocumentMessage().GetContextInfo()
	case core.GetStickerMessage().GetContextInfo() != nil:
		return core.GetStickerMessage().GetContextInfo()
	case core.GetButtonsResponseMessage().GetContextInfo() != nil:
		return core.GetButtonsResponseMessage().GetContextInfo()
	case core.GetTemplateButtonReplyMessage().GetContextInfo() != nil:
		return core.GetTemplateButtonReplyMessage().GetContextInfo()
	case core.GetInteractiveResponseMessage().GetContextInfo() != nil:
		return core.GetInteractiveResponseMessage().GetContextInfo()
	default:
		return nil
	}
}

func directMediaMessage(msg *waE2E.Message) whatsmeow.DownloadableMessage {
	core := UnwrapMessage(msg)
	if core == nil {
		return nil
	}
	switch {
	case core.ImageMessage != nil:
		return core.ImageMessage
	case core.InstantImageMessage != nil:
		return core.InstantImageMessage
	case core.VideoMessage != nil:
		return core.VideoMessage
	case core.PtvMessage != nil:
		return core.PtvMessage
	case core.AudioMessage != nil:
		return core.AudioMessage
	case core.DocumentMessage != nil:
		return core.DocumentMessage
	case core.StickerMessage != nil:
		return core.StickerMessage
	default:
		return nil
	}
}

// GetMediaMessage safely extracts downloadable media from a message or its
// quoted message, unwrapping view-once / ephemeral wrappers on both.
func GetMediaMessage(msg *waE2E.Message) whatsmeow.DownloadableMessage {
	if direct := directMediaMessage(msg); direct != nil {
		return direct
	}
	if ctxInfo := ExtractContextInfo(msg); ctxInfo != nil && ctxInfo.QuotedMessage != nil {
		return directMediaMessage(ctxInfo.QuotedMessage)
	}
	return nil
}

// IsMediaMessage reports whether an unwrapped message contains media.
func IsMediaMessage(msg *waE2E.Message) bool {
	return directMediaMessage(msg) != nil
}

// HasDownloadableMedia reports whether an unwrapped message contains media with
// a URL or DirectPath that WhatsApp CDN can serve.
func HasDownloadableMedia(msg *waE2E.Message) bool {
	core := UnwrapMessage(msg)
	if core == nil {
		return false
	}
	switch {
	case core.ImageMessage != nil:
		return core.ImageMessage.GetURL() != "" || core.ImageMessage.GetDirectPath() != ""
	case core.InstantImageMessage != nil:
		return core.InstantImageMessage.GetURL() != "" || core.InstantImageMessage.GetDirectPath() != ""
	case core.VideoMessage != nil:
		return core.VideoMessage.GetURL() != "" || core.VideoMessage.GetDirectPath() != ""
	case core.PtvMessage != nil:
		return core.PtvMessage.GetURL() != "" || core.PtvMessage.GetDirectPath() != ""
	case core.AudioMessage != nil:
		return core.AudioMessage.GetURL() != "" || core.AudioMessage.GetDirectPath() != ""
	case core.DocumentMessage != nil:
		return core.DocumentMessage.GetURL() != "" || core.DocumentMessage.GetDirectPath() != ""
	case core.StickerMessage != nil:
		return core.StickerMessage.GetURL() != "" || core.StickerMessage.GetDirectPath() != ""
	default:
		return false
	}
}

// MediaFileLength returns the declared file length in bytes for a media message.
func MediaFileLength(msg *waE2E.Message) uint64 {
	core := UnwrapMessage(msg)
	if core == nil {
		return 0
	}
	switch {
	case core.ImageMessage != nil:
		return core.ImageMessage.GetFileLength()
	case core.InstantImageMessage != nil:
		return core.InstantImageMessage.GetFileLength()
	case core.VideoMessage != nil:
		return core.VideoMessage.GetFileLength()
	case core.PtvMessage != nil:
		return core.PtvMessage.GetFileLength()
	case core.AudioMessage != nil:
		return core.AudioMessage.GetFileLength()
	case core.DocumentMessage != nil:
		return core.DocumentMessage.GetFileLength()
	case core.StickerMessage != nil:
		return core.StickerMessage.GetFileLength()
	default:
		return 0
	}
}

// MediaCaption returns the original caption from a media message, if any.
func MediaCaption(msg *waE2E.Message) string {
	core := UnwrapMessage(msg)
	if core == nil {
		return ""
	}
	switch {
	case core.ImageMessage != nil:
		return core.ImageMessage.GetCaption()
	case core.InstantImageMessage != nil:
		return core.InstantImageMessage.GetCaption()
	case core.VideoMessage != nil:
		return core.VideoMessage.GetCaption()
	case core.PtvMessage != nil:
		return core.PtvMessage.GetCaption()
	case core.DocumentMessage != nil:
		return core.DocumentMessage.GetCaption()
	default:
		return ""
	}
}

// MediaTypeForMessage returns the whatsmeow MediaType for uploading the given
// message's media payload.
func MediaTypeForMessage(msg *waE2E.Message) (whatsmeow.MediaType, bool) {
	core := UnwrapMessage(msg)
	if core == nil {
		return "", false
	}
	switch {
	case core.ImageMessage != nil, core.InstantImageMessage != nil, core.StickerMessage != nil:
		return whatsmeow.MediaImage, true
	case core.VideoMessage != nil, core.PtvMessage != nil:
		return whatsmeow.MediaVideo, true
	case core.AudioMessage != nil:
		return whatsmeow.MediaAudio, true
	case core.DocumentMessage != nil:
		return whatsmeow.MediaDocument, true
	default:
		return "", false
	}
}

// DownloadMedia downloads the media from an unwrapped WhatsApp message.
// Non-fatal ErrFileLengthMismatch warnings with non-empty decrypted bytes are
// accepted so valid view-once / mobile media is not rejected unnecessarily.
func DownloadMedia(ctx context.Context, client *whatsmeow.Client, msg *waE2E.Message) ([]byte, whatsmeow.MediaType, string, error) {
	core := UnwrapMessage(msg)
	dl := directMediaMessage(core)
	if dl == nil {
		return nil, "", "", fmt.Errorf("message contains no downloadable media")
	}
	mediaType, _ := MediaTypeForMessage(core)
	origCaption := MediaCaption(core)

	data, err := client.Download(ctx, dl)
	if err != nil {
		if errors.Is(err, whatsmeow.ErrFileLengthMismatch) && len(data) > 0 {
			fmt.Printf("  [MEDIA] Ignoring non-fatal file length mismatch (%v), got %d bytes\n", err, len(data))
			return data, mediaType, origCaption, nil
		}
		return nil, mediaType, origCaption, err
	}
	return data, mediaType, origCaption, nil
}

// BuildPermanentMediaMessage constructs a clean non-view-once outgoing message
// from newly uploaded media bytes, preserving original metadata (mimetype,
// duration, PTT, filename, thumbnail).
func BuildPermanentMediaMessage(templateMsg *waE2E.Message, uploaded whatsmeow.UploadResponse, caption string) (*waE2E.Message, bool) {
	core := UnwrapMessage(templateMsg)
	if core == nil {
		return nil, false
	}

	switch {
	case core.ImageMessage != nil:
		mime := core.ImageMessage.GetMimetype()
		if mime == "" {
			mime = "image/jpeg"
		}
		return &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      proto.String(mime),
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Caption:       proto.String(caption),
				JPEGThumbnail: core.ImageMessage.JPEGThumbnail,
			},
		}, true

	case core.VideoMessage != nil:
		mime := core.VideoMessage.GetMimetype()
		if mime == "" {
			mime = "video/mp4"
		}
		return &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      proto.String(mime),
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Caption:       proto.String(caption),
				Seconds:       core.VideoMessage.Seconds,
				GifPlayback:   core.VideoMessage.GifPlayback,
				JPEGThumbnail: core.VideoMessage.JPEGThumbnail,
			},
		}, true

	case core.AudioMessage != nil:
		mime := core.AudioMessage.GetMimetype()
		if mime == "" {
			mime = "audio/ogg; codecs=opus"
		}
		return &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      proto.String(mime),
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Seconds:       core.AudioMessage.Seconds,
				PTT:           core.AudioMessage.PTT,
			},
		}, false

	case core.StickerMessage != nil:
		mime := core.StickerMessage.GetMimetype()
		if mime == "" {
			mime = "image/webp"
		}
		return &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      proto.String(mime),
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				IsAnimated:    core.StickerMessage.IsAnimated,
			},
		}, false

	case core.DocumentMessage != nil:
		return &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      core.DocumentMessage.Mimetype,
				FileName:      core.DocumentMessage.FileName,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Caption:       proto.String(caption),
			},
		}, true
	}

	return nil, false
}

// CloneSanitizedMediaMessage clones a media message for fallback forwarding
// when CDN download fails, explicitly stripping ViewOnce and ContextInfo so
// WhatsApp displays it as a normal permanent message.
func CloneSanitizedMediaMessage(templateMsg *waE2E.Message, caption string) (*waE2E.Message, bool) {
	core := UnwrapMessage(templateMsg)
	if core == nil {
		return nil, false
	}

	switch {
	case core.ImageMessage != nil:
		img := proto.Clone(core.ImageMessage).(*waE2E.ImageMessage)
		img.ViewOnce = proto.Bool(false)
		img.ContextInfo = nil
		img.Caption = proto.String(caption)
		return &waE2E.Message{ImageMessage: img}, true

	case core.VideoMessage != nil:
		vid := proto.Clone(core.VideoMessage).(*waE2E.VideoMessage)
		vid.ViewOnce = proto.Bool(false)
		vid.ContextInfo = nil
		vid.Caption = proto.String(caption)
		return &waE2E.Message{VideoMessage: vid}, true

	case core.AudioMessage != nil:
		aud := proto.Clone(core.AudioMessage).(*waE2E.AudioMessage)
		aud.ViewOnce = proto.Bool(false)
		aud.ContextInfo = nil
		return &waE2E.Message{AudioMessage: aud}, false

	case core.StickerMessage != nil:
		stk := proto.Clone(core.StickerMessage).(*waE2E.StickerMessage)
		stk.ContextInfo = nil
		return &waE2E.Message{StickerMessage: stk}, false

	case core.DocumentMessage != nil:
		doc := proto.Clone(core.DocumentMessage).(*waE2E.DocumentMessage)
		doc.ContextInfo = nil
		doc.Caption = proto.String(caption)
		return &waE2E.Message{DocumentMessage: doc}, true
	}

	return nil, false
}

// ForwardPermanentMedia downloads (or uses preDownloaded bytes), re-uploads,
// and sends media to targetJID as permanent non-view-once media. If download
// fails, it falls back to forwarding a sanitized clone when URL/DirectPath are
// present. For audio/sticker messages that cannot hold captions, headerText is
// sent as a preceding text message when non-empty.
func ForwardPermanentMedia(
	ctx context.Context,
	client *whatsmeow.Client,
	targetJID waTypes.JID,
	templateMsg *waE2E.Message,
	preDownloaded []byte,
	caption string,
	headerForCaptionless string,
) error {
	core := UnwrapMessage(templateMsg)
	mediaType, ok := MediaTypeForMessage(core)
	if !ok {
		return fmt.Errorf("unsupported media message")
	}

	data := preDownloaded
	var dlErr error
	if len(data) == 0 {
		data, mediaType, _, dlErr = DownloadMedia(ctx, client, core)
	}

	var outMsg *waE2E.Message
	var supportsCaption bool

	if dlErr == nil && len(data) > 0 {
		uploaded, upErr := client.Upload(ctx, data, mediaType)
		if upErr == nil {
			outMsg, supportsCaption = BuildPermanentMediaMessage(core, uploaded, caption)
		} else {
			fmt.Printf("  [MEDIA] Re-upload failed (%v), trying sanitized fallback...\n", upErr)
			dlErr = upErr
		}
	}

	if outMsg == nil {
		if !HasDownloadableMedia(core) {
			if dlErr != nil {
				return dlErr
			}
			return fmt.Errorf("media has no downloadable URL or direct path")
		}
		fmt.Printf("  [MEDIA] Falling back to sanitized media forward (%v)\n", dlErr)
		outMsg, supportsCaption = CloneSanitizedMediaMessage(core, caption)
	}

	if outMsg == nil {
		return fmt.Errorf("could not construct outgoing media message")
	}

	if !supportsCaption && headerForCaptionless != "" {
		_, _ = SendReply(client, targetJID, headerForCaptionless)
	}

	_, err := client.SendMessage(ctx, targetJID, outMsg)
	return err
}

// SendImageData uploads raw image bytes and sends an ImageMessage.
func SendImageData(ctx context.Context, client *whatsmeow.Client, chatJID waTypes.JID, data []byte, mime, caption string) (whatsmeow.SendResponse, error) {
	if mime == "" {
		mime = "image/jpeg"
	}
	uploaded, err := client.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("upload image: %w", err)
	}
	return client.SendMessage(ctx, chatJID, &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(mime),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Caption:       proto.String(caption),
		},
	})
}

// SendVideoData uploads raw MP4 bytes and sends a VideoMessage.
func SendVideoData(ctx context.Context, client *whatsmeow.Client, chatJID waTypes.JID, data []byte, caption string, seconds uint32, jpegThumb []byte) (whatsmeow.SendResponse, error) {
	uploaded, err := client.Upload(ctx, data, whatsmeow.MediaVideo)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("upload video: %w", err)
	}
	vid := &waE2E.VideoMessage{
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		Mimetype:      proto.String("video/mp4"),
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uploaded.FileLength),
		Caption:       proto.String(caption),
	}
	if seconds > 0 {
		vid.Seconds = proto.Uint32(seconds)
	}
	if len(jpegThumb) > 0 {
		vid.JPEGThumbnail = jpegThumb
	}
	return client.SendMessage(ctx, chatJID, &waE2E.Message{VideoMessage: vid})
}

// SendDocumentData uploads raw bytes and sends a DocumentMessage.
func SendDocumentData(ctx context.Context, client *whatsmeow.Client, chatJID waTypes.JID, data []byte, mime, fileName, caption string) (whatsmeow.SendResponse, error) {
	if mime == "" {
		mime = "application/octet-stream"
	}
	uploaded, err := client.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("upload document: %w", err)
	}
	return client.SendMessage(ctx, chatJID, &waE2E.Message{
		DocumentMessage: &waE2E.DocumentMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(mime),
			FileName:      proto.String(fileName),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Caption:       proto.String(caption),
		},
	})
}

// SendAudioData uploads raw audio bytes and sends an AudioMessage.
func SendAudioData(ctx context.Context, client *whatsmeow.Client, chatJID waTypes.JID, data []byte, mime string, seconds uint32, ptt bool) (whatsmeow.SendResponse, error) {
	if mime == "" {
		mime = "audio/mpeg"
	}
	uploaded, err := client.Upload(ctx, data, whatsmeow.MediaAudio)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("upload audio: %w", err)
	}
	aud := &waE2E.AudioMessage{
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		Mimetype:      proto.String(mime),
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uploaded.FileLength),
		PTT:           proto.Bool(ptt),
	}
	if seconds > 0 {
		aud.Seconds = proto.Uint32(seconds)
	}
	return client.SendMessage(ctx, chatJID, &waE2E.Message{AudioMessage: aud})
}

// ExtractVideoMeta extracts a small JPEG thumbnail and duration in seconds from
// a local video file using the embedded ffmpeg and ffprobe binaries.
func ExtractVideoMeta(videoPath string) ([]byte, uint32) {
	var thumb []byte
	var seconds uint32

	if ffmpegPath, cleanup, err := GetFFmpegPath(); err == nil {
		defer cleanup()
		if out, err := exec.Command(ffmpegPath,
			"-i", videoPath,
			"-ss", "00:00:00",
			"-vframes", "1",
			"-vf", "scale=320:-1",
			"-f", "image2",
			"-vcodec", "mjpeg",
			"-",
		).Output(); err == nil && len(out) > 0 {
			thumb = out
		}
	}

	if ffprobePath, cleanup, err := GetFFprobePath(); err == nil {
		defer cleanup()
		if out, err := exec.Command(ffprobePath,
			"-v", "error",
			"-show_entries", "format=duration",
			"-of", "default=noprint_wrappers=1:nokey=1",
			videoPath,
		).Output(); err == nil {
			if f, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); err == nil && f > 0 {
				seconds = uint32(f)
			}
		}
	}

	return thumb, seconds
}
