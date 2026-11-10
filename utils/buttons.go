package utils

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// QuickReply is a single tappable button.
type QuickReply struct {
	ID   string
	Text string
}

// SendButtons sends a legacy ButtonsMessage.
//
// whatsmeow only wraps an outgoing message in WhatsApp's interactive envelope
// when getButtonTypeFromMessage recognises the type (send.go). InteractiveMessage
// is not in that switch, ButtonsMessage is - so that is what we send.
func SendButtons(client *whatsmeow.Client, jid waTypes.JID, title, body, footer string, buttons []QuickReply) (whatsmeow.SendResponse, error) {
	if len(buttons) == 0 {
		return whatsmeow.SendResponse{}, fmt.Errorf("no buttons provided")
	}
	if len(buttons) > 3 {
		buttons = buttons[:3]
	}

	btns := make([]*waE2E.ButtonsMessage_Button, 0, len(buttons))
	for _, b := range buttons {
		btns = append(btns, &waE2E.ButtonsMessage_Button{
			ButtonID:   proto.String(b.ID),
			ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String(b.Text)},
			Type:       waE2E.ButtonsMessage_Button_RESPONSE.Enum(),
		})
	}

	msg := &waE2E.Message{
		ButtonsMessage: &waE2E.ButtonsMessage{
			HeaderType:  waE2E.ButtonsMessage_TEXT.Enum(),
			Header:      &waE2E.ButtonsMessage_Text{Text: title},
			ContentText: proto.String(body),
			FooterText:  proto.String(footer),
			Buttons:     btns,
		},
	}
	return client.SendMessage(context.Background(), jid, msg)
}

// RenderButtonsAsText is the fallback listing for clients without button support.
func RenderButtonsAsText(buttons []QuickReply) string {
	var b strings.Builder
	for i, br := range buttons {
		fmt.Fprintf(&b, "%d. %s\n", i+1, br.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ExtractButtonID returns the chosen button ID, or "" for non-button messages.
func ExtractButtonID(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if br := msg.GetButtonsResponseMessage(); br != nil {
		return br.GetSelectedButtonID()
	}
	if tr := msg.GetTemplateButtonReplyMessage(); tr != nil {
		return tr.GetSelectedID()
	}
	if ir := msg.GetInteractiveResponseMessage(); ir != nil {
		if nf := ir.GetNativeFlowResponseMessage(); nf != nil {
			return nf.GetParamsJSON()
		}
	}
	return ""
}
