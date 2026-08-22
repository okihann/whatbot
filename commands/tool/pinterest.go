package tool

import (
	"bot/handlers"
	"bot/session"
	"bot/types"
	"bot/utils"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func init() {
	handlers.RegisterCommand(types.Command{Name: "pinterest", Category: "search", Execute: executePinterest})
	handlers.RegisterCommand(types.Command{Name: "pin", Category: "search", Execute: executePinterest})
	handlers.RegisterInteractiveSessionHandler(handlePinterestInteraction)
}

func fetchPinterestAPI(query string) ([]string, error) {
	apiURL := "https://api.siputzx.my.id/api/s/pinterest?query=" + url.QueryEscape(query)
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Status bool `json:"status"`
		Data   []struct {
			ImageURL string `json:"image_url"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse API: %w", err)
	}

	var urls []string
	for _, item := range result.Data {
		if item.ImageURL != "" {
			urls = append(urls, item.ImageURL)
		}
	}

	if len(urls) == 0 {
		return nil, fmt.Errorf("no images found")
	}

	return urls, nil
}

func executePinterest(client *whatsmeow.Client, msg *events.Message, args []string) {
	senderID := msg.Info.Sender.ToNonAD().String()
	query := strings.Join(args, " ")

	if query == "" {
		utils.SendReply(client, msg.Info.Chat, "  Please provide a search term or a URL.\n*Search:* `/pin cute cats`\n*Download:* `/pin https://pin.it/...` ")
		return
	}

	if strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") {
		utils.SendReply(client, msg.Info.Chat, "📥 Downloading Pinterest media...")
		go func() {
			downloadUrl := query
			
			if strings.Contains(query, "pin.it") {
				resp, err := http.Get(query)
				if err == nil {
					downloadUrl = resp.Request.URL.String()
					resp.Body.Close()
				}
			}

			tempDir := "temp"
			os.MkdirAll(tempDir, 0755)
			stamp := time.Now().UnixNano()
			outputTemplate := filepath.Join(tempDir, fmt.Sprintf("pin_%d.%%(ext)s", stamp))

			ytDlpPath, cleanup, err := utils.GetYtDlpPathForVersionCheck()
			if err != nil {
				ytDlpPath = "yt-dlp"
			} else {
				defer cleanup()
			}

			cmd := exec.Command(ytDlpPath, downloadUrl, 
				"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
				"-f", "best", 
				"-o", outputTemplate, 
				"--no-warnings")
				
			out, err := cmd.CombinedOutput()
			if err != nil {
				if strings.Contains(string(out), "No video formats found") || strings.Contains(string(out), "HTTP Error 404") || strings.Contains(string(out), "Unsupported URL") {
					
					// THE FIX: Added Chrome User-Agent so Pinterest doesn't block the scraper fallback
					req, _ := http.NewRequest("GET", downloadUrl, nil)
					req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36")
					
					resp, httpErr := (&http.Client{Timeout: 15 * time.Second}).Do(req)
					if httpErr == nil {
						defer resp.Body.Close()
						htmlBytes, _ := io.ReadAll(resp.Body)
						htmlStr := string(htmlBytes)
						
						if strings.Contains(htmlStr, `meta property="og:image" content="`) {
							parts := strings.Split(htmlStr, `meta property="og:image" content="`)
							if len(parts) > 1 {
								imgUrl := strings.Split(parts[1], `"`)[0]
								
								imgResp, imgErr := http.Get(imgUrl)
								if imgErr == nil {
									defer imgResp.Body.Close()
									imgData, _ := io.ReadAll(imgResp.Body)
									
									uploaded, _ := client.Upload(context.Background(), imgData, whatsmeow.MediaImage)
									imageMsg := &waE2E.Message{
										ImageMessage: &waE2E.ImageMessage{
											URL:           proto.String(uploaded.URL),
											DirectPath:    proto.String(uploaded.DirectPath),
											MediaKey:      uploaded.MediaKey,
											Mimetype:      proto.String("image/jpeg"),
											FileEncSHA256: uploaded.FileEncSHA256,
											FileSHA256:    uploaded.FileSHA256,
											FileLength:    proto.Uint64(uploaded.FileLength),
											Caption:       proto.String("📍 *Pinterest Image Download*"),
										},
									}
									client.SendMessage(context.Background(), msg.Info.Chat, imageMsg)
									return // Stop here so it doesn't print the yt-dlp error below
								}
							}
						}
					}
				}
				
				utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Failed to download Pinterest link.\n\n*yt-dlp Log:* %s", string(out)))
				return
			}

			files, _ := filepath.Glob(filepath.Join(tempDir, fmt.Sprintf("pin_%d.*", stamp)))
			if len(files) == 0 {
				utils.SendReply(client, msg.Info.Chat, "❌ Could not retrieve media.")
				return
			}

			downloadedFile := files[0]
			defer os.Remove(downloadedFile)

			mediaData, err := os.ReadFile(downloadedFile)
			if err != nil { return }

			ext := strings.ToLower(filepath.Ext(downloadedFile))

			if ext == ".mp4" || ext == ".webm" || ext == ".mov" {
				uploaded, _ := client.Upload(context.Background(), mediaData, whatsmeow.MediaVideo)
				videoMsg := &waE2E.Message{
					VideoMessage: &waE2E.VideoMessage{
						URL:           proto.String(uploaded.URL),
						DirectPath:    proto.String(uploaded.DirectPath),
						MediaKey:      uploaded.MediaKey,
						Mimetype:      proto.String("video/mp4"),
						FileEncSHA256: uploaded.FileEncSHA256,
						FileSHA256:    uploaded.FileSHA256,
						FileLength:    proto.Uint64(uploaded.FileLength),
						Caption:       proto.String("📍 *Pinterest Download*"),
					},
				}
				client.SendMessage(context.Background(), msg.Info.Chat, videoMsg)
			} else {
				uploaded, _ := client.Upload(context.Background(), mediaData, whatsmeow.MediaImage)
				imageMsg := &waE2E.Message{
					ImageMessage: &waE2E.ImageMessage{
						URL:           proto.String(uploaded.URL),
						DirectPath:    proto.String(uploaded.DirectPath),
						MediaKey:      uploaded.MediaKey,
						Mimetype:      proto.String("image/jpeg"),
						FileEncSHA256: uploaded.FileEncSHA256,
						FileSHA256:    uploaded.FileSHA256,
						FileLength:    proto.Uint64(uploaded.FileLength),
						Caption:       proto.String("📍 *Pinterest Download*"),
					},
				}
				client.SendMessage(context.Background(), msg.Info.Chat, imageMsg)
			}
		}()
		return
	}

	if !session.AcquireLock(senderID, "pinterest") {
		utils.SendReply(client, msg.Info.Chat, "  You already have an active Pinterest gallery! Please finish it, or wait 30 seconds.")
		return
	}

	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("  Searching Pinterest for '%s'...", query))

	go func() {
		imageUrls, err := fetchPinterestAPI(query)
		if err != nil {
			session.ReleaseLock(senderID, "pinterest")
			utils.SendReply(client, msg.Info.Chat, "  Sorry, the Pinterest API failed or found no results.")
			return
		}

		sess := &session.InteractiveSession{
			Type:   "pinterest",
			Query:  query,
			Page:   0,
			Data:   imageUrls,
			UserID: senderID,
		}

		SendPinterestPage(client, msg.Info.Chat, sess)
	}()
}

func SendPinterestPage(client *whatsmeow.Client, chatJID waTypes.JID, sess *session.InteractiveSession) {
	urls := sess.Data.([]string)
	currentURL := urls[sess.Page]

	resp, err := http.Get(currentURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	uploaded, err := client.Upload(context.Background(), imageData, whatsmeow.MediaImage)
	if err != nil {
		return
	}

	caption := fmt.Sprintf("  *Pinterest:* %s\n  *Image:* %d of %d\n\nReply to this message with:\n*+* : Next Image\n*-* : Previous Image\n*y* : Done\n\n_(Closes automatically in 30s)_", sess.Query, sess.Page+1, len(urls))

	imageMsg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String("image/jpeg"),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Caption:       proto.String(caption),
		},
	}

	sentMsg, err := client.SendMessage(context.Background(), chatJID, imageMsg)
	if err == nil {
		session.StoreInteractiveSession(sentMsg.ID, sess)
	}
}

func handlePinterestInteraction(client *whatsmeow.Client, msg *events.Message) bool {
	ctxInfo := msg.Message.GetExtendedTextMessage().GetContextInfo()
	if ctxInfo == nil || ctxInfo.StanzaID == nil {
		return false
	}

	targetMsgID := *ctxInfo.StanzaID
	sess, found := session.GetInteractiveSession(targetMsgID)
	if !found || sess.Type != "pinterest" {
		return false
	}

	if sess.UserID != msg.Info.Sender.ToNonAD().String() {
		utils.SendReply(client, msg.Info.Chat, "  This is not your search session!")
		return true
	}

	userText := strings.ToLower(strings.TrimSpace(utils.GetTextFromMessage(msg.Message)))
	urls := sess.Data.([]string)

	if userText == "+" {
		session.ResetSessionTimer(targetMsgID)
		if sess.Page < len(urls)-1 {
			sess.Page++
			SendPinterestPage(client, msg.Info.Chat, sess)
		} else {
			utils.SendReply(client, msg.Info.Chat, "  No more images found.")
		}
		return true
	}

	if userText == "-" {
		session.ResetSessionTimer(targetMsgID)
		if sess.Page > 0 {
			sess.Page--
			SendPinterestPage(client, msg.Info.Chat, sess)
		} else {
			utils.SendReply(client, msg.Info.Chat, "  You are on the first image.")
		}
		return true
	}

	if userText == "y" {
		utils.SendReply(client, msg.Info.Chat, "  Image selected! Closing gallery.")
		session.ClearInteractiveSession(targetMsgID)
		return true
	}

	return false
}
