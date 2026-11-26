package utils

import (
	"bot/config"
	"context"
	"strings"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// NormalizeJID strips the device suffix and server from a JID, returning just
// the user portion (phone number or LID).
func NormalizeJID(jid waTypes.JID) string {
	return jid.ToNonAD().User
}

// NormalizeTarget normalizes an automation target string ("all" or a phone
// number with optional +, spaces, or dashes).
func NormalizeTarget(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.EqualFold(trimmed, "all") {
		return "all"
	}
	replacer := strings.NewReplacer("+", "", "-", "", " ", "", "(", "", ")", "")
	return replacer.Replace(trimmed)
}

// ResolvePhoneNumber returns the phone number for a JID when available,
// checking primary JID, an optional alternate JID (e.g. SenderAlt/RecipientAlt),
// and the client's LID store. Falls back to primary.ToNonAD().User.
func ResolvePhoneNumber(client *whatsmeow.Client, primary, alt waTypes.JID) string {
	p := primary.ToNonAD()
	if p.Server == waTypes.DefaultUserServer && p.User != "" {
		return p.User
	}

	a := alt.ToNonAD()
	if a.Server == waTypes.DefaultUserServer && a.User != "" {
		return a.User
	}

	if client != nil && client.Store != nil && client.Store.LIDs != nil {
		for _, candidate := range []waTypes.JID{p, a} {
			if candidate.Server == waTypes.HiddenUserServer && candidate.User != "" {
				pn, err := client.Store.LIDs.GetPNForLID(context.Background(), candidate)
				if err == nil && pn.User != "" {
					return pn.ToNonAD().User
				}
			}
		}
	}

	if p.User != "" {
		return p.User
	}
	return a.User
}

// ResolvePrivateChatJID returns the non-AD JID to DM a command sender privately.
func ResolvePrivateChatJID(client *whatsmeow.Client, msg *events.Message) waTypes.JID {
	if msg.Info.IsFromMe && client != nil && client.Store != nil && client.Store.ID != nil {
		return client.Store.ID.ToNonAD()
	}
	sender := msg.Info.Sender.ToNonAD()
	if sender.Server == waTypes.DefaultUserServer && sender.User != "" {
		return sender
	}
	alt := msg.Info.SenderAlt.ToNonAD()
	if alt.Server == waTypes.DefaultUserServer && alt.User != "" {
		return alt
	}
	if sender.Server == waTypes.HiddenUserServer && client != nil && client.Store != nil && client.Store.LIDs != nil {
		if pn, err := client.Store.LIDs.GetPNForLID(context.Background(), sender); err == nil && pn.User != "" {
			return pn.ToNonAD()
		}
	}
	return sender
}

// IsOwner reports whether msg was sent by the bot itself or by a configured owner.
func IsOwner(client *whatsmeow.Client, msg *events.Message, conf *config.Config) bool {
	if msg == nil {
		return false
	}
	if msg.Info.IsFromMe {
		return true
	}
	if conf == nil {
		return false
	}

	senderNum := ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	if senderNum == "" {
		return false
	}
	for _, ownerNum := range conf.Settings.OwnerJIDs {
		if NormalizeTarget(ownerNum) == senderNum {
			return true
		}
	}
	return false
}
