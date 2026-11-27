package utils

import (
	"context"
	"os/exec"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	waTypes "go.mau.fi/whatsmeow/types"
)

type statusPNOnlyKey struct{}

// WithStatusPNOnlyContacts tags ctx so StatusSafeContactStore returns only
// @s.whatsapp.net (PN) JIDs when whatsmeow builds status@broadcast recipients.
// This prevents WhatsApp's "server returned error 400" caused by mixing PN and
// LID recipients in status broadcasts (tulir/whatsmeow#1293) with zero races.
func WithStatusPNOnlyContacts(ctx context.Context) context.Context {
	return context.WithValue(ctx, statusPNOnlyKey{}, true)
}

type statusSafeContactStore struct {
	store.ContactStore
	lids store.LIDStore
}

func (s *statusSafeContactStore) GetAllContacts(ctx context.Context) (map[waTypes.JID]waTypes.ContactInfo, error) {
	all, err := s.ContactStore.GetAllContacts(ctx)
	if err != nil {
		return nil, err
	}
	if ctx == nil || ctx.Value(statusPNOnlyKey{}) != true {
		return all, nil
	}

	filtered := make(map[waTypes.JID]waTypes.ContactInfo, len(all))
	for jid, info := range all {
		clean := jid.ToNonAD()
		switch clean.Server {
		case waTypes.DefaultUserServer:
			filtered[clean] = info
		case waTypes.HiddenUserServer:
			// Resolve @lid contact to its @s.whatsapp.net PN so all recipients match ownID's PN domain.
			if s.lids != nil {
				if pn, err := s.lids.GetPNForLID(ctx, clean); err == nil && !pn.IsEmpty() && pn.Server == waTypes.DefaultUserServer {
					pnClean := pn.ToNonAD()
					if _, exists := filtered[pnClean]; !exists {
						filtered[pnClean] = info
					}
				}
			}
		}
	}
	return filtered, nil
}

// WrapDeviceForStatusBroadcast wraps device.Contacts once (before creating the
// whatsmeow.Client) so status@broadcast sends never mix PN and LID recipients.
func WrapDeviceForStatusBroadcast(device *store.Device) {
	if device == nil || device.Contacts == nil {
		return
	}
	if _, already := device.Contacts.(*statusSafeContactStore); already {
		return
	}
	device.Contacts = &statusSafeContactStore{
		ContactStore: device.Contacts,
		lids:         device.LIDs,
	}
}

// SendStatusBroadcast sends a WhatsApp Status (Story) message to status@broadcast
// using a PN-normalized contact context to avoid WhatsApp server error 400.
func SendStatusBroadcast(ctx context.Context, client *whatsmeow.Client, msg *waE2E.Message) (whatsmeow.SendResponse, error) {
	return client.SendMessage(WithStatusPNOnlyContacts(ctx), waTypes.StatusBroadcastJID, msg)
}

// EnsureWhatsAppStatusMP4 converts non-MP4 video files (e.g. .mov, .mkv, .webm)
// into a WhatsApp-compatible H.264/AAC MP4 at dstPath. Returns true if converted.
func EnsureWhatsAppStatusMP4(srcPath, dstPath, mimeType string) bool {
	lower := strings.ToLower(strings.TrimSpace(mimeType))
	if lower == "video/mp4" {
		return false
	}
	ffmpegPath, cleanup, err := GetFFmpegPath()
	if err != nil {
		return false
	}
	defer cleanup()
	cmd := exec.Command(ffmpegPath,
		"-y", "-i", srcPath,
		"-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		dstPath,
	)
	return cmd.Run() == nil
}
