package tool

import (
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func init() {
	handlers.RegisterCommand(types.Command{Name: "smeme", Category: "media", Execute: executeStickerMeme})
}

func executeStickerMeme(client *whatsmeow.Client, msg *events.Message, args []string) {
	// THE FIX: Add your user hash here so Catbox authorizes the deletion request.
	// You can get this by logging into catbox.moe and checking your account page.
	CatboxUserHash := "8b3453f86d3dbb67e47f6a8c1"

	mediaMsg := utils.GetMediaMessage(msg.Message)
	if mediaMsg == nil || len(args) == 0 {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Reply to an image with `/smeme TOP TEXT | BOTTOM TEXT`")
		return
	}

	fullText := strings.Join(args, " ")
	parts := strings.SplitN(fullText, "|", 2)

	top := strings.TrimSpace(parts[0])
	bottom := "_"
	if len(parts) > 1 {
		bottom = strings.TrimSpace(parts[1])
	}
	if top == "" { top = "_" }
	if bottom == "" { bottom = "_" }

	utils.SendReply(client, msg.Info.Chat, "🎨 Generating meme...")

	data, err := client.Download(context.Background(), mediaMsg)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download original image.")
		return
	}

	// 1. Upload to Standard Catbox API
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("reqtype", "fileupload")
	
	if CatboxUserHash != "8b3453f86d3dbb67e47f6a8c1" {
		writer.WriteField("userhash", CatboxUserHash)
	}

	part, _ := writer.CreateFormFile("fileToUpload", "meme.png")
	io.Copy(part, bytes.NewReader(data))
	writer.Close()

	req, _ := http.NewRequest("POST", "https://catbox.moe/user/api.php", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	
	catboxResp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil { 
		utils.SendReply(client, msg.Info.Chat, "❌ Network error reaching Catbox API.")
		return 
	}
	defer catboxResp.Body.Close()
	catboxURLBytes, _ := io.ReadAll(catboxResp.Body)
	
	catboxURL := strings.TrimSpace(string(catboxURLBytes))
	
	if !strings.HasPrefix(catboxURL, "http") {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Failed to upload to Catbox.\n\n*Response:* %s", catboxURL))
		return
	}

	// Extract filename from URL (e.g. https://files.catbox.moe/xxxxx.png -> xxxxx.png)
	urlParts := strings.Split(catboxURL, "/")
	catboxFilename := urlParts[len(urlParts)-1]

	// 2. Format Memegen URL
	topURL := url.PathEscape(strings.ReplaceAll(top, " ", "_"))
	botURL := url.PathEscape(strings.ReplaceAll(bottom, " ", "_"))
	memeLink := fmt.Sprintf("https://api.memegen.link/images/custom/%s/%s.png?background=%s", topURL, botURL, catboxURL)

	// 3. Download from Memegen
	memeResp, err := http.Get(memeLink)
	if err != nil || memeResp.StatusCode != 200 {
		utils.SendReply(client, msg.Info.Chat, "❌ Memegen API failed to generate the meme.")
		return
	}
	defer memeResp.Body.Close()
	memeData, _ := io.ReadAll(memeResp.Body)

	// 4. Convert and Send as WebP Sticker
	tempDir := "temp"
	os.MkdirAll(tempDir, 0755)
	stamp := time.Now().UnixNano()
	inputPath := filepath.Join(tempDir, fmt.Sprintf("in_meme_%d.png", stamp))
	outputPath := filepath.Join(tempDir, fmt.Sprintf("out_meme_%d.webp", stamp))

	os.WriteFile(inputPath, memeData, 0644)
	defer os.Remove(inputPath)
	defer os.Remove(outputPath)

	ffmpegPath, cleanup, err := utils.GetFFmpegPath()
	if err != nil { return }
	defer cleanup()

	convertCmd := exec.Command(ffmpegPath, "-i", inputPath, "-vcodec", "libwebp", "-vf", "scale=512:512:force_original_aspect_ratio=decrease,format=rgba,pad=512:512:(ow-iw)/2:(oh-ih)/2:color='#00000000'", outputPath)
	if err := convertCmd.Run(); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg failed to convert meme to sticker.")
		return
	}

	webpData, _ := os.ReadFile(outputPath)
	uploaded, _ := client.Upload(context.Background(), webpData, whatsmeow.MediaImage)

	stickerMsg := &waE2E.Message{
		StickerMessage: &waE2E.StickerMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String("image/webp"),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
		},
	}
	client.SendMessage(context.Background(), msg.Info.Chat, stickerMsg)

	// 5. Fire a background request to delete the original image from Catbox Servers
	if CatboxUserHash != "8b3453f86d3dbb67e47f6a8c1" && catboxFilename != "" {
		go func() {
			delBody := &bytes.Buffer{}
			delWriter := multipart.NewWriter(delBody)
			delWriter.WriteField("reqtype", "deletefiles")
			delWriter.WriteField("userhash", CatboxUserHash)
			delWriter.WriteField("files", catboxFilename)
			delWriter.Close()

			delReq, _ := http.NewRequest("POST", "https://catbox.moe/user/api.php", delBody)
			delReq.Header.Set("Content-Type", delWriter.FormDataContentType())
			delReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
			
			// Execute silently in the background
			(&http.Client{Timeout: 10 * time.Second}).Do(delReq)
		}()
	}
}
