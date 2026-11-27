package utils

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	waTypes "go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type fakeLIDStore struct {
	store.LIDStore
	pnToLID map[waTypes.JID]waTypes.JID
	lidToPN map[waTypes.JID]waTypes.JID
}

func (f *fakeLIDStore) GetManyLIDsForPNs(ctx context.Context, pns []waTypes.JID) (map[waTypes.JID]waTypes.JID, error) {
	out := make(map[waTypes.JID]waTypes.JID)
	for _, pn := range pns {
		if lid, ok := f.pnToLID[pn.ToNonAD()]; ok {
			out[pn.ToNonAD()] = lid
		}
	}
	return out, nil
}

func (f *fakeLIDStore) GetPNForLID(ctx context.Context, lid waTypes.JID) (waTypes.JID, error) {
	return f.lidToPN[lid.ToNonAD()], nil
}

func TestResolveLIDStatusRecipientsAllPrivacyModes(t *testing.T) {
	ownLID := waTypes.NewJID("9990001", waTypes.HiddenUserServer)
	pnAlice := waTypes.NewJID("6281110001", waTypes.DefaultUserServer)
	lidAlice := waTypes.NewJID("10000001", waTypes.HiddenUserServer)
	pnBob := waTypes.NewJID("6281110002", waTypes.DefaultUserServer)
	lidBob := waTypes.NewJID("10000002", waTypes.HiddenUserServer)
	pnUnmapped := waTypes.NewJID("6281110003", waTypes.DefaultUserServer)
	lidCarol := waTypes.NewJID("10000003", waTypes.HiddenUserServer)
	newsletter := waTypes.NewJID("120363999", waTypes.NewsletterServer)

	contacts := map[waTypes.JID]waTypes.ContactInfo{
		pnAlice:    {Found: true, FullName: "Alice"},
		pnBob:      {Found: true, FullName: "Bob"},
		pnUnmapped: {Found: true, FullName: "No LID"},
		lidCarol:   {Found: true, FullName: "Carol LID"},
		newsletter: {Found: true, FullName: "Channel"},
	}
	lids := &fakeLIDStore{
		pnToLID: map[waTypes.JID]waTypes.JID{
			pnAlice: lidAlice,
			pnBob:   lidBob,
		},
		lidToPN: map[waTypes.JID]waTypes.JID{
			lidAlice: pnAlice,
			lidBob:   pnBob,
		},
	}

	// 1. My Contacts: Alice (resolved LID), Bob (resolved LID), Carol (LID), Own LID -> 4 pure @lid recipients
	recContacts, err := ResolveLIDStatusRecipients(context.Background(), ownLID, waTypes.StatusPrivacy{Type: waTypes.StatusPrivacyTypeContacts}, contacts, lids)
	if err != nil {
		t.Fatalf("Contacts mode error: %v", err)
	}
	if len(recContacts) != 4 {
		t.Fatalf("expected 4 LID recipients in Contacts mode, got %d: %v", len(recContacts), recContacts)
	}
	for _, r := range recContacts {
		if r.Server != waTypes.HiddenUserServer {
			t.Fatalf("non-LID recipient in Contacts mode: %s", r)
		}
	}

	// 2. My Contacts Except... (Blacklist with PN pnBob AND lidCarol): only Alice + Own LID remain
	recBL, err := ResolveLIDStatusRecipients(context.Background(), ownLID, waTypes.StatusPrivacy{
		Type: waTypes.StatusPrivacyTypeBlacklist,
		List: []waTypes.JID{pnBob, lidCarol},
	}, contacts, lids)
	if err != nil {
		t.Fatalf("Blacklist mode error: %v", err)
	}
	if len(recBL) != 2 {
		t.Fatalf("expected 2 LID recipients (Alice + Own) after Blacklist, got %d: %v", len(recBL), recBL)
	}
	for _, r := range recBL {
		if r == lidBob || r == lidCarol {
			t.Fatalf("blacklisted contact leaked: %s", r)
		}
	}

	// 3. Only Share With... (Whitelist with mixed PN pnAlice + LID lidCarol): resolves both to @lid + Own LID -> 3
	recWL, err := ResolveLIDStatusRecipients(context.Background(), ownLID, waTypes.StatusPrivacy{
		Type: waTypes.StatusPrivacyTypeWhitelist,
		List: []waTypes.JID{pnAlice, lidCarol},
	}, contacts, lids)
	if err != nil {
		t.Fatalf("Whitelist mode error: %v", err)
	}
	if len(recWL) != 3 {
		t.Fatalf("expected 3 LID recipients in Whitelist mode, got %d: %v", len(recWL), recWL)
	}
	for _, r := range recWL {
		if r.Server != waTypes.HiddenUserServer {
			t.Fatalf("non-LID recipient in Whitelist mode: %s", r)
		}
	}
}

func TestApplyStatusReshareableAndHostedDeviceFilter(t *testing.T) {
	vid := &waE2E.Message{
		VideoMessage: &waE2E.VideoMessage{Caption: proto.String("hi")},
	}
	ApplyStatusReshareable(vid)
	if !vid.VideoMessage.GetContextInfo().GetFeatureEligibilities().GetCanBeReshared() {
		t.Fatalf("expected VideoMessage CanBeReshared=true")
	}

	txt := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("hello")},
	}
	ApplyStatusReshareable(txt)
	if !txt.ExtendedTextMessage.GetContextInfo().GetFeatureEligibilities().GetCanBeReshared() {
		t.Fatalf("expected ExtendedTextMessage CanBeReshared=true")
	}

	img := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{Caption: proto.String("pic")},
	}
	ApplyStatusReshareable(img)
	if !img.ImageMessage.GetContextInfo().GetFeatureEligibilities().GetCanBeReshared() {
		t.Fatalf("expected ImageMessage CanBeReshared=true")
	}

	// Hosted and PN devices must be stripped from LID status device list
	devs := []waTypes.JID{
		{User: "1001", Device: 0, Server: waTypes.HiddenUserServer},
		{User: "1001", Device: 99, Server: waTypes.HostedLIDServer},
		{User: "628111", Device: 0, Server: waTypes.DefaultUserServer},
		{User: "628111", Device: 99, Server: waTypes.HostedServer},
	}
	filtered := filterLIDStatusDevices(devs)
	if len(filtered) != 1 || filtered[0].Server != waTypes.HiddenUserServer {
		t.Fatalf("expected only 1 non-hosted LID device, got %v", filtered)
	}
}
