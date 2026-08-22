package tool

import (
    "bot/handlers"
    "bot/types"
    "bot/utils"
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "time"

    "go.mau.fi/whatsmeow"
    "go.mau.fi/whatsmeow/types/events"
)

func init() {
    handlers.RegisterCommand(types.Command{Name: "shazam", Category: "media", Execute: executeShazam})
    handlers.RegisterCommand(types.Command{Name: "sz", Category: "media", Execute: executeShazam})
}

func executeShazam(client *whatsmeow.Client, msg *events.Message, args []string) {
    var mediaMsg whatsmeow.DownloadableMessage
    if msg.Message.GetAudioMessage() != nil {
        mediaMsg = msg.Message.GetAudioMessage()
    } else if msg.Message.GetVideoMessage() != nil {
        mediaMsg = msg.Message.GetVideoMessage()
    } else if quoted := msg.Message.GetExtendedTextMessage().GetContextInfo().GetQuotedMessage(); quoted != nil {
        if quoted.GetAudioMessage() != nil {
            mediaMsg = quoted.GetAudioMessage()
        } else if quoted.GetVideoMessage() != nil {
            mediaMsg = quoted.GetVideoMessage()
        }
    }

    if mediaMsg == nil {
        utils.SendReply(client, msg.Info.Chat, "⚠️ Please reply to an audio or video file with `/shazam`.")
        return
    }

    utils.SendReply(client, msg.Info.Chat, "🎧 Analyzing audio fingerprint...")

    data, err := client.Download(context.Background(), mediaMsg)
    if err != nil {
        utils.SendReply(client, msg.Info.Chat, "❌ Failed to download media.")
        return
    }

    tempDir := "temp"
    os.MkdirAll(tempDir, 0755)
    stamp := time.Now().UnixNano()
    rawFile := filepath.Join(tempDir, fmt.Sprintf("shazam_raw_%d.mp4", stamp))
    cleanAudioFile := filepath.Join(tempDir, fmt.Sprintf("shazam_audio_%d.mp3", stamp))
    os.WriteFile(rawFile, data, 0644)
    defer os.Remove(rawFile)
    defer os.Remove(cleanAudioFile)

    ffmpegPath, _, err := utils.GetFFmpegPath()
    if err != nil {
        utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg is missing from the system.")
        return
    }

    if output, err := exec.Command(ffmpegPath, "-i", rawFile, "-q:a", "0", "-map", "a", cleanAudioFile, "-y").CombinedOutput(); err != nil {
        utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Failed to extract audio.\n\n%s", string(output)))
        return
    }

    shazamPath, _, err := utils.GetShazamCliPath()
    if err != nil {
        utils.SendReply(client, msg.Info.Chat, "❌ Shazam binary is missing from the system.")
        return
    }

    // node-shazam normally resolves @ffmpeg-installer/ffmpeg to a packaged
    // snapshot path. The standalone pkg binary cannot execute a native file
    // from that virtual snapshot. Give the CLI the real embedded FFmpeg path.
    cmd := exec.Command(shazamPath, cleanAudioFile)
    cmd.Env = append(os.Environ(), "SHAZAM_FFMPEG_PATH="+ffmpegPath)
    out, err := cmd.CombinedOutput()
    if err != nil {
        errMsg := fmt.Sprintf("❌ The music engine crashed.\n\n*System Error:* %v\n*Engine Log:* %s", err, string(out))
        utils.SendReply(client, msg.Info.Chat, errMsg)
        return
    }

    var result struct {
        Success bool   `json:"success"`
        Title   string `json:"title"`
        Artist  string `json:"artist"`
        URL     string `json:"url"`
    }

    if err := json.Unmarshal(out, &result); err != nil || !result.Success {
        utils.SendReply(client, msg.Info.Chat, "❌ Could not identify the song. Try a clearer audio clip without background noise.")
        return
    }

    caption := fmt.Sprintf("🎵 *Song Identified!*\n\n")
    caption += fmt.Sprintf("👤 *Artist:* %s\n", result.Artist)
    caption += fmt.Sprintf("💿 *Title:* %s\n", result.Title)
    caption += fmt.Sprintf("🔗 *Listen:* %s", result.URL)
    utils.SendReply(client, msg.Info.Chat, caption)
}
