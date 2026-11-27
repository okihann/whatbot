package utils

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	waTypes "go.mau.fi/whatsmeow/types"
)

type statusCtxKey struct{}

type statusFilterConfig struct {
	pnOnly    bool
	blacklist map[waTypes.JID]struct{}
}

// WithStatusPNOnlyContacts tags ctx so StatusSafeContactStore returns only
// normalized @s.whatsapp.net (PN) JIDs when whatsmeow builds status@broadcast
// recipients, optionally excluding any JIDs in extraBlacklist (covering both
// raw PN and LID-resolved entries for "My Contacts Except..." privacy).
func WithStatusPNOnlyContacts(ctx context.Context, extraBlacklist ...map[waTypes.JID]struct{}) context.Context {
	cfg := statusFilterConfig{pnOnly: true}
	if len(extraBlacklist) > 0 && extraBlacklist[0] != nil {
		cfg.blacklist = extraBlacklist[0]
	}
	return context.WithValue(ctx, statusCtxKey{}, cfg)
}

// PrepareStatusContext queries the account's live status privacy settings and
// returns a context that normalizes PN/LID contacts AND strictly honors
// "My Contacts Except..." (Blacklist) even when WhatsApp returns @lid entries
// in the blacklist. It also reports if Whitelist mode contains @lid entries.
func PrepareStatusContext(ctx context.Context, client *whatsmeow.Client) (statusCtx context.Context, whitelistHasLID bool) {
	blacklist := make(map[waTypes.JID]struct{})
	if client != nil && client.IsConnected() {
		if opts, err := client.GetStatusPrivacy(ctx); err == nil && len(opts) > 0 {
			priv := opts[0]
			switch priv.Type {
			case waTypes.StatusPrivacyTypeBlacklist:
				for _, raw := range priv.List {
					clean := raw.ToNonAD()
					blacklist[clean] = struct{}{}
					if clean.Server == waTypes.HiddenUserServer && client.Store != nil && client.Store.LIDs != nil {
						if pn, err := client.Store.LIDs.GetPNForLID(ctx, clean); err == nil && !pn.IsEmpty() {
							blacklist[pn.ToNonAD()] = struct{}{}
						}
					}
				}
			case waTypes.StatusPrivacyTypeWhitelist:
				for _, raw := range priv.List {
					if raw.Server == waTypes.HiddenUserServer {
						whitelistHasLID = true
						break
					}
				}
			}
		}
	}
	return WithStatusPNOnlyContacts(ctx, blacklist), whitelistHasLID
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
	if ctx == nil {
		return all, nil
	}
	cfg, ok := ctx.Value(statusCtxKey{}).(statusFilterConfig)
	if !ok || !cfg.pnOnly {
		return all, nil
	}

	isBlacklisted := func(j waTypes.JID) bool {
		if len(cfg.blacklist) == 0 {
			return false
		}
		_, blocked := cfg.blacklist[j.ToNonAD()]
		return blocked
	}

	filtered := make(map[waTypes.JID]waTypes.ContactInfo, len(all))
	for jid, info := range all {
		clean := jid.ToNonAD()
		if isBlacklisted(clean) {
			continue
		}
		switch clean.Server {
		case waTypes.DefaultUserServer:
			filtered[clean] = info
		case waTypes.HiddenUserServer:
			// Resolve @lid contact to its @s.whatsapp.net PN so all recipients match ownID's PN domain,
			// while still honoring Blacklist ("My Contacts Except...") on both the LID and resolved PN.
			if s.lids != nil {
				if pn, err := s.lids.GetPNForLID(ctx, clean); err == nil && !pn.IsEmpty() && pn.Server == waTypes.DefaultUserServer {
					pnClean := pn.ToNonAD()
					if isBlacklisted(pnClean) {
						continue
					}
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
// using a PN-normalized contact context (preserving Blacklist privacy) to avoid
// WhatsApp server error 400.
func SendStatusBroadcast(ctx context.Context, client *whatsmeow.Client, msg *waE2E.Message) (whatsmeow.SendResponse, error) {
	statusCtx, whitelistHasLID := PrepareStatusContext(ctx, client)
	resp, err := client.SendMessage(statusCtx, waTypes.StatusBroadcastJID, msg)
	if err != nil && whitelistHasLID && strings.Contains(err.Error(), "400") {
		return resp, fmt.Errorf("%w (WhatsApp returned @lid entries in 'Only Share With' whitelist; switch status privacy to 'My Contacts' or 'My Contacts Except...' until whatsmeow#1293 is merged upstream)", err)
	}
	return resp, err
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
