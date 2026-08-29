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
	msgCache         = make(map[string]*events.Message)
	autoGetDoneCache = make(map[string]bool)
	cacheMu          sync.Mutex
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

func resolvePN(client *whatsmeow.Client, jid waTypes.JID) string {
	nonAD := jid.ToNonAD()
	if nonAD.Server == "lid" {
		pn, err := client.Store.LIDs.GetPNForLID(context.Background(), nonAD)
		if err == nil && pn.User != "" {
			return pn.User
		}
	}
	return nonAD.User
}

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

func extractContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	if msg == nil {
		return nil
	}
	if ext := msg.GetExtendedTextMessage(); ext != nil {
		return ext.GetContextInfo()
	}
	if img := msg.GetImageMessage(); img != nil {
		return img.GetContextInfo()
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return vid.GetContextInfo()
	}
	if aud := msg.GetAudioMessage(); aud != nil {
		return aud.GetContextInfo()
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		return doc.GetContextInfo()
	}
	return nil
}

// --- COMMAND LOGIC (STRICT OWNER ONLY) ---

func manageAutomation(client *whatsmeow.Client, msg *events.Message, args []string, feature string) {
	conf, err := config.LoadConfig()
	if err != nil {
		return
	}

	isOwner := msg.Info.IsFromMe
	if !isOwner {
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

	// --- 1. ANTI-DELETE TRIGGER ---
	if msg.Message.GetProtocolMessage() != nil && msg.Message.GetProtocolMessage().GetType() == waE2E.ProtocolMessage_REVOKE {
		targetID := msg.Message.GetProtocolMessage().GetKey().GetID()

		cacheMu.Lock()
		cachedMsg, found := msgCache[targetID]
		cacheMu.Unlock()

		if found && autoConf.AntiDeleteOn {
			senderNum := resolvePN(client, cachedMsg.Info.Sender)

			if isTargeted(autoConf.AntiDeleteTargets, senderNum) {
				
				// FEATURE UPDATE: Accurately label Statuses, Groups, and DMs
				chatName := "Private Chat"
				if cachedMsg.Info.Chat.Server == "broadcast" {
					chatName = "WhatsApp Status"
				} else if cachedMsg.Info.IsGroup {
					chatName = fmt.Sprintf("Group (%s)", cachedMsg.Info.Chat.String())
				}

				alertText := fmt.Sprintf("🚨 *Anti-Delete Triggered*\n*Target:* %s\n*Chat:* %s\n\n_Recovered Message:_\n", senderNum, chatName)
				coreMsg := unwrapMessage(cachedMsg.Message)

				isText := coreMsg.Conversation != nil || coreMsg.ExtendedTextMessage != nil

				if isText && coreMsg.ImageMessage == nil && coreMsg.VideoMessage == nil && coreMsg.DocumentMessage == nil && coreMsg.AudioMessage == nil && coreMsg.StickerMessage == nil {
					originalText := utils.GetTextFromMessage(cachedMsg.Message)
					finalText := alertText + originalText
					client.SendMessage(context.Background(), ownerJID, &waE2E.Message{
						ExtendedTextMessage: &waE2E.ExtendedTextMessage{
							Text: proto.String(finalText),
						},
					})
				} else if coreMsg.ImageMessage != nil {
					img := proto.Clone(coreMsg.ImageMessage).(*waE2E.ImageMessage)
					origCap := ""
					if img.Caption != nil {
						origCap = *img.Caption
					}
					img.Caption = proto.String(alertText + origCap)
					img.ViewOnce = proto.Bool(false)
					client.SendMessage(context.Background(), ownerJID, &waE2E.Message{ImageMessage: img})
				} else if coreMsg.VideoMessage != nil {
					vid := proto.Clone(coreMsg.VideoMessage).(*waE2E.VideoMessage)
					origCap := ""
					if vid.Caption != nil {
						origCap = *vid.Caption
					}
					vid.Caption = proto.String(alertText + origCap)
					img.ViewOnce = proto.Bool(false)
					client.SendMessage(context.Background(), ownerJID, &waE2E.Message{VideoMessage: vid})
				} else if coreMsg.DocumentMessage != nil {
					doc := proto.Clone(coreMsg.DocumentMessage).(*waE2E.DocumentMessage)
					origCap := ""
					if doc.Caption != nil {
						origCap = *doc.Caption
					}
					doc.Caption = proto.String(alertText + origCap)
					client.SendMessage(context.Background(), ownerJID, &waE2E.Message{DocumentMessage: doc})
				} else {
					utils.SendReply(client, ownerJID, alertText)
					client.SendMessage(context.Background(), ownerJID, coreMsg)
				}
			}
		}
		return
	}

	// Cache direct incoming messages (and statuses) for 24 hours
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

	// --- 2. QUOTE INSPECTOR (FIXED FOR 1-ON-1 PRIVATE CHATS) ---
	ctxInfo := extractContextInfo(msg.Message)
	if ctxInfo != nil && ctxInfo.QuotedMessage != nil && ctxInfo.StanzaID != nil {
		quotedID := *ctxInfo.StanzaID
		quotedMedia := unwrapMessage(ctxInfo.QuotedMessage)

		var quotedSenderJID waTypes.JID
		if ctxInfo.Participant != nil && *ctxInfo.Participant != "" {
			quotedSenderJID, _ = waTypes.ParseJID(*ctxInfo.Participant)
		} else if !msg.Info.IsGroup {
			if msg.Info.IsFromMe {
				quotedSenderJID = msg.Info.Chat
			} else {
				quotedSenderJID = msg.Info.Sender
			}
		} else {
			quotedSenderJID = msg.Info.Sender
		}

		quotedSenderNum := resolvePN(client, quotedSenderJID)

		cacheMu.Lock()
		if existing, found := msgCache[quotedID]; found {
			existing.Message = quotedMedia
		} else {
			msgCache[quotedID] = &events.Message{
				Info: waTypes.MessageInfo{
					MessageSource: waTypes.MessageSource{
						Sender:  quotedSenderJID,
						Chat:    msg.Info.Chat,
						IsGroup: msg.Info.IsGroup,
					},
					ID:        quotedID,
					Timestamp: msg.Info.Timestamp,
				},
				Message: quotedMedia,
			}
		}
		alreadyGotten := autoGetDoneCache[quotedID]
		cacheMu.Unlock()

		if autoConf.AutoGetOn && !alreadyGotten && isTargeted(autoConf.AutoGetTargets, quotedSenderNum) {
			isMedia := quotedMedia.GetImageMessage() != nil ||
				quotedMedia.GetVideoMessage() != nil ||
				quotedMedia.GetAudioMessage() != nil ||
				quotedMedia.GetDocumentMessage() != nil

			if isMedia {
				cacheMu.Lock()
				autoGetDoneCache[quotedID] = true
				cacheMu.Unlock()

				go processAutoGet(client, quotedMedia, ownerJID, quotedSenderNum)
			}
		}
	}

	// --- 3. DIRECT INCOMING MEDIA AUTO-GET LOGIC ---
	// BUGFIX: Added `msg.Info.Chat.Server != "broadcast"` so it doesn't auto-download every status
	if !msg.Info.IsGroup && msg.Info.Chat.Server != "broadcast" && !msg.Info.IsFromMe && autoConf.AutoGetOn {
		senderNum := resolvePN(client, msg.Info.Sender)

		if isTargeted(autoConf.AutoGetTargets, senderNum) {
			coreMsg := unwrapMessage(msg.Message)
			
			isMedia := coreMsg.GetImageMessage() != nil || 
				coreMsg.GetVideoMessage() != nil || 
				coreMsg.GetAudioMessage() != nil || 
				coreMsg.GetDocumentMessage() != nil

			cacheMu.Lock()
			alreadyGotten := autoGetDoneCache[msg.Info.ID]
			cacheMu.Unlock()

			if isMedia && !alreadyGotten {
				cacheMu.Lock()
				autoGetDoneCache[msg.Info.ID] = true
				cacheMu.Unlock()

				go processAutoGet(client, coreMsg, ownerJID, senderNum)
			}
		}
	}
}

func processAutoGet(client *whatsmeow.Client, coreMsg *waE2E.Message, ownerJID waTypes.JID, senderNum string) {
	captionHeader := fmt.Sprintf("📥 *Auto-Get*\nReceived media from: %s\n\n", senderNum)

	if coreMsg.ImageMessage != nil {
		img := proto.Clone(coreMsg.ImageMessage).(*waE2E.ImageMessage)
		if img.Caption != nil {
			img.Caption = proto.String(captionHeader + *img.Caption)
		} else {
			img.Caption = proto.String(strings.TrimSpace(captionHeader))
		}
		// The Magic Bullet: Strip the self-destruct flag
		img.ViewOnce = proto.Bool(false)
		
		_, err := client.SendMessage(context.Background(), ownerJID, &waE2E.Message{ImageMessage: img})
		if err != nil {
			fmt.Printf("  [AUTO-GET] Failed to route image: %v\n", err)
		}

	} else if coreMsg.VideoMessage != nil {
		vid := proto.Clone(coreMsg.VideoMessage).(*waE2E.VideoMessage)
		if vid.Caption != nil {
			vid.Caption = proto.String(captionHeader + *vid.Caption)
		} else {
			vid.Caption = proto.String(strings.TrimSpace(captionHeader))
		}
		// The Magic Bullet: Strip the self-destruct flag
		vid.ViewOnce = proto.Bool(false)
		
		_, err := client.SendMessage(context.Background(), ownerJID, &waE2E.Message{VideoMessage: vid})
		if err != nil {
			fmt.Printf("  [AUTO-GET] Failed to route video: %v\n", err)
		}

	} else if coreMsg.AudioMessage != nil {
		aud := proto.Clone(coreMsg.AudioMessage).(*waE2E.AudioMessage)
		_, err := client.SendMessage(context.Background(), ownerJID, &waE2E.Message{AudioMessage: aud})
		if err != nil {
			fmt.Printf("  [AUTO-GET] Failed to route audio: %v\n", err)
		}

	} else if coreMsg.DocumentMessage != nil {
		doc := proto.Clone(coreMsg.DocumentMessage).(*waE2E.DocumentMessage)
		if doc.Caption != nil {
			doc.Caption = proto.String(captionHeader + *doc.Caption)
		} else {
			doc.Caption = proto.String(strings.TrimSpace(captionHeader))
		}
		_, err := client.SendMessage(context.Background(), ownerJID, &waE2E.Message{DocumentMessage: doc})
		if err != nil {
			fmt.Printf("  [AUTO-GET] Failed to route document: %v\n", err)
		}
	}
}
