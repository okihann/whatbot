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
