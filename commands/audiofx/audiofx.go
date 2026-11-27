package audiofx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

var audioEffects = map[string]string{
	"bass":      "equalizer=f=54:width_type=o:width=2:g=18",
	"blown":     "acrusher=.1:1:64:0:log",
	"deep":      "atempo=4/4,asetrate=44500*2/3",
	"earrape":   "volume=12",
	"fast":      "atempo=1.63,asetrate=44100",
	"fat":       "atempo=1.6,asetrate=22100",
	"nightcore": "aresample=48000,asetrate=48000*1.25",
	"reverse":   "areverse",
	"robot":     "afftfilt=real='hypot(re,im)*sin(0)':imag='hypot(re,im)*cos(0)':win_size=512:overlap=0.75",
	"slow":      "atempo=0.7,asetrate=44100",
	"smooth":    "minterpolate='mi_mode=mci:mc_mode=aobmc:vsbmc=1:fps=120'",
	"squirrel":  "atempo=0.5,asetrate=65100",
	"8d":        "apulsator=hz=0.125",
	"echo":      "aecho=0.8:0.9:1000:0.3",
}

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "audioeffect",
		Aliases:     []string{"audiofx", "afx"},
		Category:    "audiofx",
		Description: "Apply studio FFmpeg audio effects to a replied audio/video. Usage: .audioeffect <bass|blown|deep|earrape|fast|fat|nightcore|reverse|robot|slow|squirrel|8d|echo>",
		Execute:     cmdAudioEffect,
	})
}

func cmdAudioEffect(client *whatsmeow.Client, msg *events.Message, args []string) {
	effect := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	filter, ok := audioEffects[effect]
	if !ok || effect == "smooth" {
		names := make([]string, 0, len(audioEffects))
		for k := range audioEffects {
			if k != "smooth" {
				names = append(names, k)
			}
		}
		sort.Strings(names)
		utils.SendReply(client, msg.Info.Chat, "🎛️ *Audio Effects*\nReply to an audio/voice note or video with:\n`.audioeffect <preset>`\n\n*Presets:* `"+strings.Join(names, "`, `")+"`")
		return
	}

	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || (target.GetAudioMessage() == nil && target.GetVideoMessage() == nil && target.GetDocumentMessage() == nil) {
		utils.SendReply(client, msg.Info.Chat, "🎵 Reply to an audio, voice note, or video message.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download media: "+err.Error())
		return
	}

	ffPath, cleanup, err := utils.GetFFmpegPath()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg unavailable.")
		return
	}
	defer cleanup()

	tmpDir, err := os.MkdirTemp("", "whatbot-afx-*")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmpDir)

	inPath := filepath.Join(tmpDir, "in.media")
	outPath := filepath.Join(tmpDir, "out.mp3")
	_ = os.WriteFile(inPath, data, 0600)

	cmd := exec.CommandContext(ctx, ffPath, "-y", "-i", inPath, "-vn", "-af", filter, "-c:a", "libmp3lame", "-b:a", "128k", outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Audio filter failed: %v\n%s", err, string(out)))
		return
	}
	mp3Bytes, err := os.ReadFile(outPath)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to read processed audio.")
		return
	}
	_, _ = utils.SendAudioData(ctx, client, msg.Info.Chat, mp3Bytes, "audio/mpeg", 0, false)
}
