package owner

import (
	"bot/config"
	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

var (
	msgCache = make(map[string]*events.Message)
	cacheMu  sync.Mutex
)

func init() {
	handlers.RegisterCommand(types.Command{Name: "anti-delete", Category: "owner", Execute: executeAntiDelete})
	handlers.RegisterCommand(types.Command{Name: "auto-get", Category: "owner", Execute: executeAutoGet})
}

// --- HELPER FUNCTIONS ---

func isTargeted(targets []string, num string) bool {
	for _, t := range targets {
		if t == num || t == "all" {
			return true
		}
	}
	return false
}

// resolvePN converts a Local ID (LID) into a Phone Number (PN) so matching works flawlessly.
func resolvePN(client *whatsmeow.Client, jid waTypes.JID) string {
	nonAD := jid.ToNonAD()
	if nonAD.Server == "lid" {
		pn, err := client.Store.LIDs.GetPNForLID(context.Background(), nonAD)
		if err == nil && pn.User != "" {
			return pn.User // Returns just the number (e.g. 628999...)
		}
	}
	return nonAD.User // .User cleanly strips the @s.whatsapp.net part
}

// Deep unwrap to extract core media out of ViewOnce, Ephemeral, and Documents
func unwrapMessage(msg *waE2E.Message) *waE2E.Message {
	if msg == nil {
		return nil
	}
	if msg.EphemeralMessage != nil && msg.EphemeralMessage.Message != nil {
		return unwrapMessage(msg.EphemeralMessage.Message)
	}
	if msg.ViewOnceMessage != nil && msg.ViewOnceMessage.Message != nil {
		return unwrapMessage(msg.ViewOnceMessage.Message)
	}
	if msg.ViewOnceMessageV2 != nil && msg.ViewOnceMessageV2.Message != nil {
		return unwrapMessage(msg.ViewOnceMessageV2.Message)
	}
	if msg.ViewOnceMessageV2Extension != nil && msg.ViewOnceMessageV2Extension.Message != nil {
		return unwrapMessage(msg.ViewOnceMessageV2Extension.Message)
	}
	if msg.DocumentWithCaptionMessage != nil && msg.DocumentWithCaptionMessage.Message != nil {
		return unwrapMessage(msg.DocumentWithCaptionMessage.Message)
	}
	return msg
}

// --- COMMAND LOGIC (STRICT OWNER ONLY) ---

func manageAutomation(client *whatsmeow.Client, msg *events.Message, args []string, feature string) {
	conf, err := config.LoadConfig()
	if err != nil {
		return
	}

	isOwner := msg.Info.IsFromMe
	if !isOwner {
		// Use our new resolvePN helper for the owner check too!
		senderNum := resolvePN(client, msg.Info.Sender)
		for _, ownerNum := range conf.Settings.OwnerJIDs {
			if senderNum == ownerNum {
				isOwner = true
				break
			}
		}
	}

	if !isOwner {
		utils.SendReply(client, msg.Info.Chat, "🚫 Access Denied: This command is restricted to the bot owner.")
		return
	}

	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Usage: `/%s [on|off|add|remove|list] [number/all]`", feature))
		return
	}

	parts := strings.Fields(args[0])
	action := strings.ToLower(parts[0])
	target := ""

	if len(parts) > 1 {
		target = strings.TrimSpace(parts[1])
	}

	autoConf := db.LoadAutoConfig()

	switch action {
	case "list":
		if feature == "anti-delete" {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📋 *Anti-Delete Status*\n*Global Switch:* %v\n*Targets:* %v", autoConf.AntiDeleteOn, autoConf.AntiDeleteTargets))
		} else {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📋 *Auto-Get Status*\n*Global Switch:* %v\n*Targets:* %v", autoConf.AutoGetOn, autoConf.AutoGetTargets))
		}
	case "on":
		if feature == "anti-delete" {
			autoConf.AntiDeleteOn = true
		} else {
			autoConf.AutoGetOn = true
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ *%s* is now ON.", feature))
	case "off":
		if feature == "anti-delete" {
			autoConf.AntiDeleteOn = false
		} else {
			autoConf.AutoGetOn = false
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ *%s* is now OFF.", feature))
	case "add":
		if target == "" {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Please provide a number to add. (e.g. `/%s add all` or `/%s add 628...`)", feature, feature))
			return
		}
		if feature == "anti-delete" {
			if !isTargeted(autoConf.AntiDeleteTargets, target) {
				autoConf.AntiDeleteTargets = append(autoConf.AntiDeleteTargets, target)
			}
		} else {
			if !isTargeted(autoConf.AutoGetTargets, target) {
				autoConf.AutoGetTargets = append(autoConf.AutoGetTargets, target)
			}
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Added `%s` to *%s* targets.", target, feature))
	case "remove":
		var newList []string
		list := autoConf.AntiDeleteTargets
		if feature == "auto-get" {
			list = autoConf.AutoGetTargets
		}

		for _, v := range list {
			if v != target {
				newList = append(newList, v)
			}
		}

		if feature == "anti-delete" {
			autoConf.AntiDeleteTargets = newList
		} else {
			autoConf.AutoGetTargets = newList
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗑️ Removed `%s` from *%s* targets.", target, feature))
	default:
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Unknown action: `%s`\nUsage: `/%s [on|off|add|remove|list] [number]`", action, feature))
	}

	db.SaveAutoConfig()
}

func executeAntiDelete(client *whatsmeow.Client, msg *events.Message, args []string) {
	manageAutomation(client, msg, args, "anti-delete")
}

func executeAutoGet(client *whatsmeow.Client, msg *events.Message, args []string) {
	manageAutomation(client, msg, args, "auto-get")
}

// --- EVENT INTERCEPTOR (THE BACKGROUND WORKER) ---

func HandleAutomations(client *whatsmeow.Client, msg *events.Message) {
	if msg.Message == nil {
		return
	}

	botConf, _ := config.LoadConfig()
	if len(botConf.Settings.OwnerJIDs) == 0 {
		return
	}

	ownerStr := botConf.Settings.OwnerJIDs[0]
	if !strings.Contains(ownerStr, "@") {
		ownerStr += "@s.whatsapp.net"
	}
	ownerJID, _ := waTypes.ParseJID(ownerStr)

	autoConf := db.LoadAutoConfig()

	// --- 1. ANTI-DELETE LOGIC ---
	if msg.Message.GetProtocolMessage() != nil && msg.Message.GetProtocolMessage().GetType() == waE2E.ProtocolMessage_REVOKE {
		targetID := msg.Message.GetProtocolMessage().GetKey().GetID()

		cacheMu.Lock()
		cachedMsg, found := msgCache[targetID]
		cacheMu.Unlock()

		if found && autoConf.AntiDeleteOn {
			// FIX: Resolve the cached sender LID back to a real number
			senderNum := resolvePN(client, cachedMsg.Info.Sender)

			if isTargeted(autoConf.AntiDeleteTargets, senderNum) {
				chatName := "Private Chat"
				if cachedMsg.Info.IsGroup {
					chatName = fmt.Sprintf("Group (%s)", cachedMsg.Info.Chat.String())
				}

				alert := fmt.Sprintf("🚨 *Anti-Delete Triggered*\n*Target:* %s\n*Chat:* %s\n\n_Recovered Message:_ ", senderNum, chatName)
				utils.SendReply(client, ownerJID, alert)
				
				coreMsg := unwrapMessage(cachedMsg.Message)
				client.SendMessage(context.Background(), ownerJID, coreMsg)
			}
		}
		return
	}

	// Always cache incoming messages for 24h
	if msg.Info.ID != "" {
		cacheMu.Lock()
		msgCache[msg.Info.ID] = msg
		cacheMu.Unlock()
		go func(msgID string) {
			time.Sleep(24 * time.Hour)
			cacheMu.Lock()
			delete(msgCache, msgID)
			cacheMu.Unlock()
		}(msg.Info.ID)
	}

	// --- 2. AUTO-GET LOGIC ---
	if !msg.Info.IsGroup && !msg.Info.IsFromMe && autoConf.AutoGetOn {
		// FIX: Resolve the incoming sender LID back to a real number
		senderNum := resolvePN(client, msg.Info.Sender)

		if isTargeted(autoConf.AutoGetTargets, senderNum) {
			coreMsg := unwrapMessage(msg.Message)
			isMedia := coreMsg.GetImageMessage() != nil || coreMsg.GetVideoMessage() != nil || coreMsg.GetAudioMessage() != nil || coreMsg.GetDocumentMessage() != nil || coreMsg.GetStickerMessage() != nil

			if isMedia {
				go processAutoGet(client, coreMsg, ownerJID, senderNum)
			}
		}
	}
}

// --- MEDIA PROCESSOR ---

func processAutoGet(client *whatsmeow.Client, coreMsg *waE2E.Message, ownerJID waTypes.JID, senderNum string) {
	var mediaType whatsmeow.MediaType
	var data []byte
	var err error

	if coreMsg.ImageMessage != nil {
		mediaType = whatsmeow.MediaImage
		data, err = client.Download(context.Background(), coreMsg.ImageMessage)
	} else if coreMsg.VideoMessage != nil {
		mediaType = whatsmeow.MediaVideo
		data, err = client.Download(context.Background(), coreMsg.VideoMessage)
	} else if coreMsg.AudioMessage != nil {
		mediaType = whatsmeow.MediaAudio
		data, err = client.Download(context.Background(), coreMsg.AudioMessage)
	} else if coreMsg.DocumentMessage != nil {
		mediaType = whatsmeow.MediaDocument
		data, err = client.Download(context.Background(), coreMsg.DocumentMessage)
	} else if coreMsg.StickerMessage != nil {
		mediaType = whatsmeow.MediaImage
		data, err = client.Download(context.Background(), coreMsg.StickerMessage)
	} else {
		return
	}

	if err != nil {
		fmt.Printf("  [AUTO-GET] Download failed: %v\n", err)
		return
	}

	uploaded, err := client.Upload(context.Background(), data, mediaType)
	if err != nil {
		fmt.Printf("  [AUTO-GET] Upload failed: %v\n", err)
		return
	}

	caption := fmt.Sprintf("📥 *Auto-Get*\nReceived media from: %s", senderNum)
	var finalMsg waE2E.Message

	if coreMsg.ImageMessage != nil {
		finalMsg.ImageMessage = &waE2E.ImageMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      coreMsg.ImageMessage.Mimetype,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Caption:       proto.String(caption),
		}
	} else if coreMsg.VideoMessage != nil {
		finalMsg.VideoMessage = &waE2E.VideoMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      coreMsg.VideoMessage.Mimetype,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Caption:       proto.String(caption),
		}
	} else if coreMsg.AudioMessage != nil {
		finalMsg.AudioMessage = &waE2E.AudioMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      coreMsg.AudioMessage.Mimetype,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			PTT:           coreMsg.AudioMessage.PTT,
		}
	} else if coreMsg.DocumentMessage != nil {
		finalMsg.DocumentMessage = &waE2E.DocumentMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      coreMsg.DocumentMessage.Mimetype,
			FileName:      coreMsg.DocumentMessage.FileName,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Caption:       proto.String(caption),
		}
	} else if coreMsg.StickerMessage != nil {
		finalMsg.StickerMessage = &waE2E.StickerMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      coreMsg.StickerMessage.Mimetype,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
		}
	}

	_, err = client.SendMessage(context.Background(), ownerJID, &finalMsg)
	if err != nil {
		fmt.Printf("  [AUTO-GET] Failed to forward to owner: %v\n", err)
	}
}
