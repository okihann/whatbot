package tool

import (
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:     "traceanime",
		Category: "anime",
		Execute:  executeTraceAnime,
	})
    handlers.RegisterCommand(types.Command{
		Name:     "traceanime",
		Category: "anime",
		Execute:  executeTraceAnime,
	})
}

func executeTraceAnime(client *whatsmeow.Client, msg *events.Message, args []string) {
	mediaMsg := utils.GetMediaMessage(msg.Message)
	if mediaMsg == nil {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Please reply to an image or an image document with `/traceanime`.")
		return
	}

	// --- DOCUMENT SAFEGUARD ---
	// If the user uploaded the photo as a Document, ensure it is actually an image format (JPG/PNG)
	if docMsg := msg.Message.GetDocumentMessage(); docMsg != nil {
		if !strings.HasPrefix(docMsg.GetMimetype(), "image/") {
			utils.SendReply(client, msg.Info.Chat, "⚠️ The document must be an image file (JPG/PNG).")
			return
		}
	} else if quoted := msg.Message.GetExtendedTextMessage().GetContextInfo().GetQuotedMessage(); quoted != nil {
		if docMsg := quoted.GetDocumentMessage(); docMsg != nil {
			if !strings.HasPrefix(docMsg.GetMimetype(), "image/") {
				utils.SendReply(client, msg.Info.Chat, "⚠️ The quoted document must be an image file (JPG/PNG).")
				return
			}
		}
	}

	utils.SendReply(client, msg.Info.Chat, "🔍 Tracing scene...")

	imageData, err := client.Download(context.Background(), mediaMsg)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download image.")
		return
	}

	// Added cutBorders=1 to ignore WhatsApp compression artifacts
	req, _ := http.NewRequest("POST", "https://api.trace.moe/search?cutBorders=1", bytes.NewBuffer(imageData))
	req.Header.Set("Content-Type", "image/jpeg") 

	clientHTTP := &http.Client{}
	resp, err := clientHTTP.Do(req)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Trace API failed.")
		return
	}
	defer resp.Body.Close()

	var result struct {
		Result []struct {
			Filename   string      `json:"filename"`
			Episode    interface{} `json:"episode"`
			Similarity float64     `json:"similarity"`
			From       float64     `json:"from"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Result) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ No matches found. Try a clearer screenshot.")
		return
	}

	top := result.Result[0]
	sim := top.Similarity * 100

	if sim < 85 {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Low confidence match found. This might not be accurate.")
	}

	mins := int(top.From) / 60
	secs := int(top.From) % 60

	ep := "Unknown"
	if top.Episode != nil {
		ep = fmt.Sprintf("%v", top.Episode)
	}

	caption := fmt.Sprintf("🎬 *Match Found!*\n\n")
	caption += fmt.Sprintf("📁 *File:* %s\n", top.Filename)
	caption += fmt.Sprintf("📺 *Episode:* %s\n", ep)
	caption += fmt.Sprintf("⏱ *Timestamp:* %02d:%02d\n", mins, secs)
	caption += fmt.Sprintf("🎯 *Confidence:* %.2f%%", sim)

	utils.SendReply(client, msg.Info.Chat, caption)
}
