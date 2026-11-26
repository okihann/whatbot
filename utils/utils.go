package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// ExtractAudioFromVideo extracts an Opus voice-note OGG track from videoPath.
func ExtractAudioFromVideo(videoPath string) (audioPath string, cleanup func(), err error) {
	ffmpegPath, ffmpegCleanup, err := GetFFmpegPath()
	if err != nil {
		return "", nil, fmt.Errorf("could not get ffmpeg path: %w", err)
	}

	execDir, err := getExecCacheDir()
	if err != nil {
		ffmpegCleanup()
		return "", nil, fmt.Errorf("could not get bin_cache dir: %w", err)
	}
	tempDir, err := os.MkdirTemp(execDir, "wa-audio-extract-*")
	if err != nil {
		ffmpegCleanup()
		return "", nil, fmt.Errorf("could not create temp dir for audio extraction: %w", err)
	}

	outputAudioPath := filepath.Join(tempDir, "extracted_audio.ogg")

	cmd := exec.Command(ffmpegPath,
		"-i", videoPath,
		"-vn",
		"-c:a", "libopus",
		"-b:a", "16k",
		"-vbr", "on",
		outputAudioPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		ffmpegCleanup()
		os.RemoveAll(tempDir)
		return "", nil, fmt.Errorf("ffmpeg audio extraction failed. Error: %v\nOutput: %s", err, string(output))
	}

	cleanup = func() {
		ffmpegCleanup()
		os.RemoveAll(tempDir)
	}

	return outputAudioPath, cleanup, nil
}

// GetTextFromMessage extracts human-readable text or caption from any message,
// unwrapping ephemeral / view-once / device-sent containers first.
func GetTextFromMessage(msg *waE2E.Message) string {
	core := UnwrapMessage(msg)
	if core == nil {
		return ""
	}
	switch {
	case core.ReactionMessage != nil:
		if text := core.ReactionMessage.GetText(); text != "" {
			return text
		}
		return "[Reaction]"
	case core.Conversation != nil:
		return *core.Conversation
	case core.ExtendedTextMessage != nil && core.ExtendedTextMessage.Text != nil:
		return *core.ExtendedTextMessage.Text
	case core.TemplateButtonReplyMessage != nil && core.TemplateButtonReplyMessage.SelectedDisplayText != nil:
		return *core.TemplateButtonReplyMessage.SelectedDisplayText
	case core.ButtonsResponseMessage != nil && core.ButtonsResponseMessage.SelectedButtonID != nil:
		return *core.ButtonsResponseMessage.SelectedButtonID
	case core.TemplateMessage != nil && core.TemplateMessage.HydratedTemplate != nil && core.TemplateMessage.HydratedTemplate.HydratedContentText != nil:
		return *core.TemplateMessage.HydratedTemplate.HydratedContentText
	case core.ImageMessage != nil && core.ImageMessage.Caption != nil:
		return *core.ImageMessage.Caption
	case core.VideoMessage != nil && core.VideoMessage.Caption != nil:
		return *core.VideoMessage.Caption
	case core.DocumentMessage != nil && core.DocumentMessage.Caption != nil:
		return *core.DocumentMessage.Caption
	case core.StickerMessage != nil:
		return "[Sticker]"
	case core.AudioMessage != nil:
		return "[Audio]"
	case core.VideoMessage != nil:
		return "[Video]"
	case core.ImageMessage != nil:
		return "[Image]"
	case core.DocumentMessage != nil:
		return "[Document]"
	default:
		return ""
	}
}

func SendReply(client *whatsmeow.Client, jid types.JID, text string) (whatsmeow.SendResponse, error) {
	return client.SendMessage(context.Background(), jid, &waE2E.Message{
		Conversation: proto.String(text),
	})
}

func SendVoiceNote(client *whatsmeow.Client, jid types.JID, mp3Data []byte) {
	ffmpegPath, cleanup, err := GetFFmpegPath()
	if err != nil {
		fmt.Printf("Failed to get ffmpeg path: %v\n", err)
		SendReply(client, jid, "[TTS Error: Could not prepare audio converter.]")
		return
	}
	defer cleanup()

	execDir, err := getExecCacheDir()
	if err != nil {
		fmt.Println("Failed to create temp dir:", err)
		SendReply(client, jid, "[TTS Error: Could not create temp directory]")
		return
	}
	tempDir, err := os.MkdirTemp(execDir, "wa-audio-")
	if err != nil {
		fmt.Println("Failed to create temp dir:", err)
		SendReply(client, jid, "[TTS Error: Could not create temp directory]")
		return
	}
	defer os.RemoveAll(tempDir)

	mp3Path := filepath.Join(tempDir, "output.mp3")
	oggPath := filepath.Join(tempDir, "output.ogg")

	if err := os.WriteFile(mp3Path, mp3Data, 0600); err != nil {
		fmt.Println("Failed to write temp mp3 file:", err)
		SendReply(client, jid, "[TTS Error: Could not write temp file]")
		return
	}

	cmd := exec.Command(ffmpegPath,
		"-i", mp3Path,
		"-c:a", "libopus",
		"-b:a", "16k",
		"-vbr", "on",
		"-compression_level", "10",
		oggPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Embedded FFmpeg conversion failed.\nError: %v\nOutput: %s\n", err, string(output))
		SendReply(client, jid, "[TTS Error: Could not convert audio. Please check server logs.]")
		return
	}

	var durationSeconds uint32 = 10
	re := regexp.MustCompile(`Duration: (\d{2}):(\d{2}):(\d{2})\.\d{2}`)
	if matches := re.FindStringSubmatch(string(output)); len(matches) == 4 {
		hours, _ := strconv.Atoi(matches[1])
		minutes, _ := strconv.Atoi(matches[2])
		seconds, _ := strconv.Atoi(matches[3])
		durationSeconds = uint32((hours * 3600) + (minutes * 60) + seconds)
	}

	oggData, err := os.ReadFile(oggPath)
	if err != nil {
		fmt.Println("Failed to read converted ogg file:", err)
		SendReply(client, jid, "[TTS Error: Could not read converted file]")
		return
	}

	if _, err := SendAudioData(context.Background(), client, jid, oggData, "audio/ogg; codecs=opus", durationSeconds, true); err != nil {
		fmt.Printf("Failed to send audio message: %v\n", err)
		SendReply(client, jid, "[TTS Error: Failed to upload audio]")
	}
}
