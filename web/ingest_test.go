package web

import (
	"path/filepath"
	"testing"
	"time"

	db "bot/databases"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func newTestFakeClient(user string) *whatsmeow.Client {
	jid := waTypes.NewJID(user, waTypes.DefaultUserServer)
	return &whatsmeow.Client{
		Store: &store.Device{
			ID: &jid,
		},
	}
}

func TestIngestMessageFixes(t *testing.T) {
	dir := t.TempDir()
	chatStore, err := db.OpenChatStore(filepath.Join(dir, "webchat.db"), filepath.Join(dir, "webmedia"))
	if err != nil {
		t.Fatalf("OpenChatStore: %v", err)
	}
	defer chatStore.Close()

	hub := NewHub(chatStore, nil, &mockAccountManager{})
	cli := newTestFakeClient("628111000")

	// 1. Group message bundled with SenderKeyDistributionMessage MUST NOT be dropped!
	groupJID := waTypes.NewJID("120363000111", waTypes.GroupServer)
	senderJID := waTypes.NewJID("628222", waTypes.DefaultUserServer)
	hub.IngestMessage(cli, &events.Message{
		Info: waTypes.MessageInfo{
			MessageSource: waTypes.MessageSource{
				Chat:    groupJID,
				Sender:  senderJID,
				IsGroup: true,
			},
			ID:        "GRP-SKDM-1",
			PushName:  "Alice",
			Timestamp: time.Unix(1700001000, 0),
		},
		Message: &waE2E.Message{
			SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{
				GroupID: proto.String(groupJID.String()),
			},
			Conversation: proto.String("Hello group with key distribution!"),
		},
	})

	// Immediately query WITHOUT calling chatStore.Flush() — verifies zero 30ms write-queue race!
	grpMsgs, err := chatStore.ListMessages("628111000", groupJID.String(), 50)
	if err != nil {
		t.Fatalf("ListMessages group: %v", err)
	}
	if len(grpMsgs) != 1 || grpMsgs[0].Body != "Hello group with key distribution!" {
		t.Fatalf("expected group message bundled with SenderKeyDistributionMessage to be stored, got %+v", grpMsgs)
	}

	// 2. Album photo wrapped in AssociatedChildMessage is unwrapped and stored.
	hub.IngestMessage(cli, &events.Message{
		Info: waTypes.MessageInfo{
			MessageSource: waTypes.MessageSource{
				Chat:    groupJID,
				Sender:  senderJID,
				IsGroup: true,
			},
			ID:        "ALBUM-CHILD-1",
			PushName:  "Alice",
			Timestamp: time.Unix(1700001010, 0),
		},
		Message: &waE2E.Message{
			AssociatedChildMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{
					ImageMessage: &waE2E.ImageMessage{
						Caption:  proto.String("Album photo 1"),
						Mimetype: proto.String("image/jpeg"),
					},
				},
			},
		},
	})
	grpMsgs, _ = chatStore.ListMessages("628111000", groupJID.String(), 50)
	if len(grpMsgs) != 2 || grpMsgs[1].MsgType != "image" || grpMsgs[1].Caption != "Album photo 1" {
		t.Fatalf("expected AssociatedChildMessage album image to be stored, got %+v", grpMsgs)
	}

	// 3. Message edit updates the existing stored message body in place.
	hub.IngestMessage(cli, &events.Message{
		Info: waTypes.MessageInfo{
			MessageSource: waTypes.MessageSource{
				Chat:    groupJID,
				Sender:  senderJID,
				IsGroup: true,
			},
			ID:        "GRP-EDIT-1",
			Timestamp: time.Unix(1700001020, 0),
		},
		Message: &waE2E.Message{
			EditedMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{
					ProtocolMessage: &waE2E.ProtocolMessage{
						Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
						Key: &waCommon.MessageKey{
							ID: proto.String("GRP-SKDM-1"),
						},
						EditedMessage: &waE2E.Message{
							Conversation: proto.String("Hello group (edited)!"),
						},
					},
				},
			},
		},
	})
	editedMsg, err := chatStore.GetMessage("628111000", "GRP-SKDM-1")
	if err != nil || editedMsg == nil || editedMsg.Body != "Hello group (edited)!" {
		t.Fatalf("expected edited message body to update in place, got %+v (err=%v)", editedMsg, err)
	}

	// 4. DM @lid message with SenderAlt=@s.whatsapp.net canonicalizes and merges prior @lid rows.
	lidJID := waTypes.NewJID("998877665544", waTypes.HiddenUserServer)
	pnJID := waTypes.NewJID("628999000111", waTypes.DefaultUserServer)

	// Pre-seed an older orphaned @lid message (simulating previous split DB state).
	chatStore.RecordMessage(db.StoredMessage{
		AccountID:  "628111000",
		MsgID:      "OLD-LID-MSG",
		ChatJID:    lidJID.String(),
		SenderJID:  lidJID.String(),
		SenderName: "Bob",
		MsgType:    "text",
		Body:       "Older message trapped in @lid",
		Timestamp:  1700000500,
		ChatName:   "Bob",
	})

	// New incoming DM arrives addressed via @lid with SenderAlt carrying the real PN.
	hub.IngestMessage(cli, &events.Message{
		Info: waTypes.MessageInfo{
			MessageSource: waTypes.MessageSource{
				Chat:      lidJID,
				Sender:    lidJID,
				SenderAlt: pnJID,
				IsGroup:   false,
			},
			ID:        "NEW-DM-MSG",
			PushName:  "Bob",
			Timestamp: time.Unix(1700002000, 0),
		},
		Message: &waE2E.Message{
			Conversation: proto.String("Brand new DM message!"),
		},
	})

	// Both the older orphaned @lid message AND the brand-new message must appear under the PN JID!
	dmMsgs, err := chatStore.ListMessages("628111000", pnJID.String(), 50)
	if err != nil {
		t.Fatalf("ListMessages PN: %v", err)
	}
	if len(dmMsgs) != 2 {
		t.Fatalf("expected 2 merged DM messages under %s, got %d: %+v", pnJID.String(), len(dmMsgs), dmMsgs)
	}
	if dmMsgs[0].MsgID != "OLD-LID-MSG" || dmMsgs[1].MsgID != "NEW-DM-MSG" {
		t.Fatalf("unexpected merged DM order: %+v", dmMsgs)
	}
	lidsRemaining, _ := chatStore.ListLIDChatJIDs("628111000")
	if len(lidsRemaining) != 0 {
		t.Fatalf("expected orphaned @lid chat to be merged away, still have %v", lidsRemaining)
	}

	// 5. HistorySync uses PhoneNumberToLidMappings and does NOT inflate unread_count.
	histLID := waTypes.NewJID("554433221100", waTypes.HiddenUserServer)
	histPN := waTypes.NewJID("628777888999", waTypes.DefaultUserServer)
	hub.IngestHistorySync(cli, &events.HistorySync{
		Data: &waHistorySync.HistorySync{
			SyncType: waHistorySync.HistorySync_RECENT.Enum(),
			PhoneNumberToLidMappings: []*waHistorySync.PhoneNumberToLIDMapping{
				{
					LidJID: proto.String(histLID.String()),
					PnJID:  proto.String(histPN.String()),
				},
			},
			Conversations: []*waHistorySync.Conversation{
				{
					ID:                    proto.String(histLID.String()),
					Name:                  proto.String("Charlie"),
					UnreadCount:           proto.Uint32(0),
					ConversationTimestamp: proto.Uint64(1700003000),
					Messages: []*waHistorySync.HistorySyncMsg{
						{
							Message: &waWeb.WebMessageInfo{
								Key: &waCommon.MessageKey{
									ID:     proto.String("HIST-1"),
									FromMe: proto.Bool(false),
								},
								MessageTimestamp: proto.Uint64(1700002900),
								PushName:         proto.String("Charlie"),
								Message: &waE2E.Message{
									Conversation: proto.String("Historical message 1"),
								},
							},
						},
						{
							Message: &waWeb.WebMessageInfo{
								Key: &waCommon.MessageKey{
									ID:     proto.String("HIST-2"),
									FromMe: proto.Bool(false),
								},
								MessageTimestamp: proto.Uint64(1700003000),
								PushName:         proto.String("Charlie"),
								Message: &waE2E.Message{
									Conversation: proto.String("Historical message 2"),
								},
							},
						},
					},
				},
			},
		},
	})

	chats, err := chatStore.ListChats("628111000", "", "all")
	if err != nil {
		t.Fatalf("ListChats: %v", err)
	}
	var charlieChat *db.StoredChat
	for i := range chats {
		if chats[i].ChatJID == histPN.String() {
			charlieChat = &chats[i]
			break
		}
	}
	if charlieChat == nil {
		t.Fatalf("expected HistorySync @lid conversation to resolve to %s, chats=%+v", histPN.String(), chats)
	}
	if charlieChat.UnreadCount != 0 {
		t.Fatalf("expected HistorySync unread_count=0 (not inflated by historical messages), got %d", charlieChat.UnreadCount)
	}
	histMsgs, _ := chatStore.ListMessages("628111000", histPN.String(), 50)
	if len(histMsgs) != 2 {
		t.Fatalf("expected 2 HistorySync messages under %s, got %d", histPN.String(), len(histMsgs))
	}
}
