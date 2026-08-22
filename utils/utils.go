package utils

import (
	"bot/config"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

//go:embed bin/ffmpeg
var ffmpegData []byte

//go:embed bin/ffprobe
var ffprobeData []byte

//go:embed bin/yt-dlp
var ytDlpData []byte

//go:embed bin/shazam-cli
var shazamCliData []byte

type YtDlpResponse struct {
	Title          string          `json:"title"`
	Thumbnail      string          `json:"thumbnail"`
	Filesize       int64           `json:"filesize"`
	FilesizeApprox int64           `json:"filesize_approx"`
    Duration       float64         `json:"duration"`
	Entries        []YtDlpResponse `json:"entries"`
}

type YTSearchResult struct {
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	Duration float64 `json:"duration"`
}

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36"

func getExecCacheDir() (string, error) {
	const dirName = "bin_cache"
	if _, err := os.Stat(dirName); os.IsNotExist(err) {
		if err := os.Mkdir(dirName, 0755); err != nil {
			return "", err
		}
	}
	absPath, err := filepath.Abs(dirName)
	if err != nil {
		return dirName, nil
	}
	return absPath, nil
}

func ExtractAudioFromVideo(videoPath string) (audioPath string, cleanup func(), err error) {
	ffmpegPath, ffmpegCleanup, err := GetFFmpegPath()
	if err != nil {
		return "", nil, fmt.Errorf("could not get ffmpeg path: %w", err)
	}

	execDir, err := getExecCacheDir()
	if err != nil {
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

func GetFFmpegPath() (path string, cleanup func(), err error) {
	execDir, err := getExecCacheDir()
	if err != nil {
		return "", nil, fmt.Errorf("could not get bin_cache dir: %w", err)
	}

	binPath := filepath.Join(execDir, "ffmpeg")

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		if err := os.WriteFile(binPath, ffmpegData, 0755); err != nil {
			return "", nil, fmt.Errorf("could not write ffmpeg to cache: %w", err)
		}
	}
	return binPath, func() {}, nil
}

func GetFFprobePath() (path string, cleanup func(), err error) {
	execDir, err := getExecCacheDir()
	if err != nil {
		return "", nil, fmt.Errorf("could not get bin_cache dir: %w", err)
	}

	binPath := filepath.Join(execDir, "ffprobe")

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		if err := os.WriteFile(binPath, ffprobeData, 0755); err != nil {
			return "", nil, fmt.Errorf("could not write ffprobe to cache: %w", err)
		}
	}
	return binPath, func() {}, nil
}

func getYtDlpPath() (path string, cleanup func(), err error) {
	execDir, err := getExecCacheDir()
	if err != nil {
		return "", nil, fmt.Errorf("could not get bin_cache dir: %w", err)
	}

	binPath := filepath.Join(execDir, "yt-dlp")

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		if err := os.WriteFile(binPath, ytDlpData, 0755); err != nil {
			return "", nil, fmt.Errorf("could not write yt-dlp to cache: %w", err)
		}
	}
	return binPath, func() {}, nil
}

func GetYtDlpPathForVersionCheck() (string, func(), error) {
	return getYtDlpPath()
}

func GetVideoInfo(videoURL, quality string) (*YtDlpResponse, error) {
	if !IsValidMediaURL(videoURL) {
		return nil, fmt.Errorf("invalid URL")
	}
	ytDlpPath, cleanup, err := getYtDlpPath()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	var formatSelector string
	if quality == "best" {
		formatSelector = "bestvideo[ext=mp4]+bestaudio[ext=m4a]/bestvideo[ext=mp4]+bestaudio/best[ext=mp4]/best"
	} else {
		formatSelector = fmt.Sprintf("bestvideo[height<=?%s][ext=mp4]+bestaudio[ext=m4a]/bestvideo[ext=mp4]+bestaudio/best[ext=mp4]/best", quality)
	}

	conf, _ := config.LoadConfig()

	args := []string{
		// videoURL,
		"--dump-single-json",
		"--no-warnings",
		"--force-ipv4",
		"--user-agent", browserUserAgent,
		"-S", "vcodec:h264",
	}

	if conf != nil && conf.Downloader.CookiesFile != "" {
		if _, err := os.Stat(conf.Downloader.CookiesFile); err == nil {
			args = append(args, "--cookies", conf.Downloader.CookiesFile)
		} else {
			fmt.Println("Warning: Cookies file specified in config.json not found at:", conf.Downloader.CookiesFile)
		}
	}

	args = append(args, "-f", formatSelector, "--", videoURL)
	cmd := exec.Command(ytDlpPath, args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		fullCommand := fmt.Sprintf("%s %s", ytDlpPath, strings.Join(args, " "))
		return nil, fmt.Errorf("yt-dlp info error: %s\nFull Command: %s\nOutput: %s", err, fullCommand, string(output))
	}

	var info YtDlpResponse
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	if err := decoder.Decode(&info); err != nil {
		fullCommand := fmt.Sprintf("%s %s", ytDlpPath, strings.Join(args, " "))
		return nil, fmt.Errorf("failed to parse yt-dlp JSON: %w\nFull Command: %s\nRaw output: %s", err, fullCommand, string(output))
	}

	if len(info.Entries) > 0 {
		return &info.Entries[0], nil
	}
	return &info, nil
}

func DownloadVideo(videoURL, outputFile, quality string) error {
	if !IsValidMediaURL(videoURL) {
		return fmt.Errorf("invalid URL")
	}

	ytDlpPath, ytDlpCleanup, err := getYtDlpPath()
	if err != nil {
		return err
	}
	defer ytDlpCleanup()

	ffmpegPath, ffmpegCleanup, err := GetFFmpegPath()
	if err != nil {
		return fmt.Errorf("could not extract ffmpeg for yt-dlp: %w", err)
	}
	defer ffmpegCleanup()

	// 1. Define a temporary raw file
	rawFile := outputFile + "_raw.mp4"
	defer os.Remove(rawFile) // Garbage collection: ALWAYS delete raw file when done

	var formatSelector string
	if quality == "best" {
		formatSelector = "bestvideo+bestaudio/best"
	} else {
		formatSelector = fmt.Sprintf("bestvideo[height<=?%s]+bestaudio/best", quality)
	}

	conf, _ := config.LoadConfig()

	// 2. Download raw video via yt-dlp
	ytArgs := []string{
		"--no-warnings",
		"--force-ipv4",
		"--user-agent", browserUserAgent,
		"--ffmpeg-location", ffmpegPath,
		"-S", "vcodec:h264,res,acodec:m4a", // Still politely ask for h264 first
	}

	if conf != nil && conf.Downloader.CookiesFile != "" {
		if _, err := os.Stat(conf.Downloader.CookiesFile); err == nil {
			ytArgs = append(ytArgs, "--cookies", conf.Downloader.CookiesFile)
		} else {
			fmt.Println("Warning: Cookies file specified in config.json not found at:", conf.Downloader.CookiesFile)
		}
	}

	ytArgs = append(ytArgs,
		"-f", formatSelector,
		"-o", rawFile,
		"--merge-output-format", "mp4",
		"--", videoURL,
	)

	ytCmd := exec.Command(ytDlpPath, ytArgs...)
	if output, err := ytCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("yt-dlp download error: %s\nOutput: %s", err, string(output))
	}

	// 3. Force FFMPEG to transcode to H.264 (Video) and AAC (Audio)
	fmt.Printf("Transcoding %s to Android-friendly H.264...\n", rawFile)
	
	ffArgs := []string{
		"-i", rawFile,
		"-c:v", "libx264",
		"-c:a", "aac",
		"-preset", "veryfast", // Saves CPU on your Pterodactyl server
		"-crf", "20",          // Compresses size slightly without destroying quality
		"-y",                  // Overwrite output
		outputFile,
	}

	ffCmd := exec.Command(ffmpegPath, ffArgs...)
	if output, err := ffCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg transcoding error: %s\nOutput: %s", err, string(output))
	}

	fmt.Println("Transcoding complete!")
	return nil
}

func GetTextFromMessage(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if msg.ReactionMessage != nil {
		if text := msg.ReactionMessage.GetText(); text != "" {
			return text
		}
		return "[Reaction]"
	}
	if msg.Conversation != nil {
		return *msg.Conversation
	}
	if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Text != nil {
		return *msg.ExtendedTextMessage.Text
	}
	if msg.TemplateButtonReplyMessage != nil && msg.TemplateButtonReplyMessage.SelectedDisplayText != nil {
		return *msg.TemplateButtonReplyMessage.SelectedDisplayText
	}
	if msg.ButtonsResponseMessage != nil && msg.ButtonsResponseMessage.SelectedButtonID != nil {
		return *msg.ButtonsResponseMessage.SelectedButtonID
	}
	if msg.TemplateMessage != nil && msg.TemplateMessage.HydratedTemplate != nil && msg.TemplateMessage.HydratedTemplate.HydratedContentText != nil {
		return *msg.TemplateMessage.HydratedTemplate.HydratedContentText
	}
	if msg.ImageMessage != nil && msg.ImageMessage.Caption != nil {
		return *msg.ImageMessage.Caption
	}
	if msg.VideoMessage != nil && msg.VideoMessage.Caption != nil {
		return *msg.VideoMessage.Caption
	}
	if msg.DocumentMessage != nil && msg.DocumentMessage.Caption != nil {
		return *msg.DocumentMessage.Caption
	}
	if msg.EphemeralMessage != nil && msg.EphemeralMessage.Message != nil {
		return GetTextFromMessage(msg.EphemeralMessage.Message)
	}
	if msg.ViewOnceMessage != nil && msg.ViewOnceMessage.Message != nil {
		return GetTextFromMessage(msg.ViewOnceMessage.Message)
	}
	if msg.ViewOnceMessageV2 != nil && msg.ViewOnceMessageV2.Message != nil {
		return GetTextFromMessage(msg.ViewOnceMessageV2.Message)
	}
	if msg.StickerMessage != nil {
		return "[Sticker]"
	}
	if msg.AudioMessage != nil {
		return "[Audio]"
	}
	if msg.VideoMessage != nil {
		return "[Video]"
	}
	if msg.ImageMessage != nil {
		return "[Image]"
	}
	return ""
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
	tempDir, err := os.MkdirTemp(execDir, "wa-audio-") // Use current dir
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
	matches := re.FindStringSubmatch(string(output))
	if len(matches) == 4 {
		hours, _ := strconv.Atoi(matches[1])
		minutes, _ := strconv.Atoi(matches[2])
		seconds, _ := strconv.Atoi(matches[3])
		totalSeconds := (hours * 3600) + (minutes * 60) + seconds
		durationSeconds = uint32(totalSeconds)
	}

	oggData, err := os.ReadFile(oggPath)
	if err != nil {
		fmt.Println("Failed to read converted ogg file:", err)
		SendReply(client, jid, "[TTS Error: Could not read converted file]")
		return
	}

	uploaded, err := client.Upload(context.Background(), oggData, whatsmeow.MediaAudio)
	if err != nil {
		fmt.Printf("Failed to upload audio: %v\n", err)
		SendReply(client, jid, "[TTS Error: Failed to upload audio]")
		return
	}

	msg := &waE2E.Message{
		AudioMessage: &waE2E.AudioMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String("audio/ogg; codecs=opus"),
			FileSHA256:    uploaded.FileSHA256,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Seconds:       proto.Uint32(durationSeconds),
			PTT:           proto.Bool(true),
		},
	}

	_, err = client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		fmt.Printf("Failed to send audio message: %v\n", err)
	}
}

func NormalizeJID(jid types.JID) string {
	return strings.Split(jid.ToNonAD().String(), "@")[0]
}

func IsValidMediaURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	// Only allow http / https
	return u.Scheme == "http" || u.Scheme == "https"
}

func GetShazamCliPath() (path string, cleanup func(), err error) {
	execDir, err := getExecCacheDir()
	if err != nil {
		return "", nil, fmt.Errorf("could not get bin_cache dir: %w", err)
	}

	binPath := filepath.Join(execDir, "shazam-cli")

	// Check if the binary is already unpacked in the cache
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		// If not, write the embedded bytes to the filesystem
		if err := os.WriteFile(binPath, shazamCliData, 0755); err != nil {
			return "", nil, fmt.Errorf("could not write shazam-cli to cache: %w", err)
		}
	}
	return binPath, func() {}, nil
}

// GetMediaMessage safely extracts downloadable media from a message or a quoted message
func GetMediaMessage(msg *waE2E.Message) whatsmeow.DownloadableMessage {
	if msg == nil {
		return nil
	}
	
	// Check direct messages
	if msg.ImageMessage != nil {
		return msg.ImageMessage
	} else if msg.VideoMessage != nil {
		return msg.VideoMessage
	} else if msg.AudioMessage != nil {
		return msg.AudioMessage
	} else if msg.DocumentMessage != nil {
		return msg.DocumentMessage
	} else if msg.StickerMessage != nil {
		return msg.StickerMessage
	}

	// Check quoted messages
	if quoted := msg.GetExtendedTextMessage().GetContextInfo().GetQuotedMessage(); quoted != nil {
		if quoted.ImageMessage != nil {
			return quoted.ImageMessage
		} else if quoted.VideoMessage != nil {
			return quoted.VideoMessage
		} else if quoted.AudioMessage != nil {
			return quoted.AudioMessage
		} else if quoted.DocumentMessage != nil {
			return quoted.DocumentMessage
		} else if quoted.StickerMessage != nil {
			return quoted.StickerMessage
		}
	}

	return nil
}