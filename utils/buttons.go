package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// QuickReply is a single tappable button shown to the user.
type QuickReply struct {
	ID   string
	Text string
}

type buttonParams struct {
	DisplayText string `json:"display_text"`
	ID          string `json:"id"`
}

// SendButtons sends a WhatsApp interactive message with quick-reply buttons.
// Returns the sent message (so the caller can track replies) along with an error.
// If the user's client doesn't support buttons, WhatsApp normally falls back to
// a text listing handled by the caller — but we also ship a plain-text mirror.
func SendButtons(client *whatsmeow.Client, jid waTypes.JID, title, body, footer string, buttons []QuickReply) (whatsmeow.SendResponse, error) {
	if len(buttons) == 0 {
		return whatsmeow.SendResponse{}, fmt.Errorf("no buttons provided")
	}
	if len(buttons) > 3 {
		buttons = buttons[:3]
	}

	nfButtons := make([]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton, 0, len(buttons))
	for _, b := range buttons {
		params, _ := json.Marshal(buttonParams{DisplayText: b.Text, ID: b.ID})
		nfButtons = append(nfButtons, &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			Name:             proto.String("quick_reply"),
			ButtonParamsJSON: proto.String(string(params)),
		})
	}

	msg := &waE2E.Message{
		InteractiveMessage: &waE2E.InteractiveMessage{
			Header: &waE2E.InteractiveMessage_Header{Title: proto.String(title)},
			Body:   &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
			Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String(footer)},
			InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
				NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
					Buttons: nfButtons,
				},
			},
		},
	}

	return client.SendMessage(context.Background(), jid, msg)
}

// RenderButtonsAsText is the fallback used by callers when a user replies with
// plain text instead of tapping a button.
func RenderButtonsAsText(buttons []QuickReply) string {
	var b strings.Builder
	for i, br := range buttons {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, br.Text))
	}
	return strings.TrimRight(b.String(), "\n")
}

// ExtractButtonID pulls the chosen button ID out of a button/interactive reply.
// Returns "" if the message is not a button response.
func ExtractButtonID(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if ir := msg.GetInteractiveResponseMessage(); ir != nil {
		if nf := ir.GetNativeFlowResponseMessage(); nf != nil {
			var payload struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal([]byte(nf.GetParamsJSON()), &payload); err == nil && payload.ID != "" {
				return payload.ID
			}
		}
	}
	if br := msg.GetButtonsResponseMessage(); br != nil {
		return br.GetSelectedButtonID()
	}
	if tr := msg.GetTemplateButtonReplyMessage(); tr != nil {
		return tr.GetSelectedID()
	}
	return ""
}
