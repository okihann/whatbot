package owner

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestTargetMatchingAndExactCheck(t *testing.T) {
	targets := []string{"+62 812-345", "all"}

	if !isTargeted(targets, "628123456789") {
		t.Fatalf("expected prefix/all target match")
	}
	if !isTargeted([]string{"+62812"}, "6281299999") {
		t.Fatalf("expected normalized prefix match")
	}
	if hasExactTarget([]string{"all"}, "628123456") {
		t.Fatalf("hasExactTarget must not treat 'all' as matching a specific number")
	}
	if !hasExactTarget([]string{"+62 812-345"}, "62812345") {
		t.Fatalf("expected normalized exact match")
	}
}

func TestIsRevokeMessage(t *testing.T) {
	revokeProto := &waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		},
	}
	if !isRevokeMessage(nil, revokeProto) {
		t.Fatalf("expected ProtocolMessage_REVOKE to be detected")
	}

	editEvt := &events.Message{
		Info: waTypes.MessageInfo{
			Edit: waTypes.EditAttributeSenderRevoke,
		},
	}
	if !isRevokeMessage(editEvt, nil) {
		t.Fatalf("expected EditAttributeSenderRevoke to be detected")
	}

	normal := &waE2E.Message{}
	if isRevokeMessage(&events.Message{}, normal) {
		t.Fatalf("normal message should not be detected as revoke")
	}
}

func TestShouldAutoGetMediaViewOnceOnly(t *testing.T) {
	img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}
	vid := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{}}
	aud := &waE2E.Message{AudioMessage: &waE2E.AudioMessage{}}
	sticker := &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}
	text := &waE2E.Message{}

	// Regular DM media and stickers must NOT be captured by auto-get.
	if shouldAutoGetMedia(img, false) {
		t.Fatalf("regular image must not trigger auto-get")
	}
	if shouldAutoGetMedia(sticker, false) {
		t.Fatalf("sticker must not trigger auto-get")
	}
	if shouldAutoGetMedia(vid, false) {
		t.Fatalf("regular video must not trigger auto-get")
	}

	// View-once media MUST be captured.
	if !shouldAutoGetMedia(img, true) || !shouldAutoGetMedia(vid, true) || !shouldAutoGetMedia(aud, true) {
		t.Fatalf("view-once image/video/audio must trigger auto-get")
	}
	if shouldAutoGetMedia(text, true) {
		t.Fatalf("non-media message must not trigger auto-get")
	}
}
