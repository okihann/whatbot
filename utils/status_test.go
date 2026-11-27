package utils

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/store"
	waTypes "go.mau.fi/whatsmeow/types"
)

type fakeContactStore struct {
	store.ContactStore
	contacts map[waTypes.JID]waTypes.ContactInfo
}

func (f *fakeContactStore) GetAllContacts(ctx context.Context) (map[waTypes.JID]waTypes.ContactInfo, error) {
	return f.contacts, nil
}

type fakeLIDStore struct {
	store.LIDStore
	lidToPN map[waTypes.JID]waTypes.JID
}

func (f *fakeLIDStore) GetPNForLID(ctx context.Context, lid waTypes.JID) (waTypes.JID, error) {
	return f.lidToPN[lid], nil
}

func TestStatusSafeContactStoreFiltersMixedPNAndLID(t *testing.T) {
	pn1 := waTypes.NewJID("6281110001", waTypes.DefaultUserServer)
	pn2 := waTypes.NewJID("6281110002", waTypes.DefaultUserServer)
	lidMapped := waTypes.NewJID("10000001", waTypes.HiddenUserServer)
	lidUnmapped := waTypes.NewJID("10000002", waTypes.HiddenUserServer)
	newsletter := waTypes.NewJID("120363999", waTypes.NewsletterServer)

	dev := &store.Device{
		Contacts: &fakeContactStore{
			contacts: map[waTypes.JID]waTypes.ContactInfo{
				pn1:         {Found: true, FullName: "Alice"},
				lidMapped:   {Found: true, FullName: "Bob LID"},
				lidUnmapped: {Found: true, FullName: "Unmapped LID"},
				newsletter:  {Found: true, FullName: "Some Channel"},
			},
		},
		LIDs: &fakeLIDStore{
			lidToPN: map[waTypes.JID]waTypes.JID{
				lidMapped: pn2,
			},
		},
	}

	WrapDeviceForStatusBroadcast(dev)

	// Normal context preserves all contacts untouched.
	normal, err := dev.Contacts.GetAllContacts(context.Background())
	if err != nil || len(normal) != 4 {
		t.Fatalf("expected 4 contacts in normal ctx, got %d err=%v", len(normal), err)
	}

	// Status context returns strictly @s.whatsapp.net PN JIDs (pn1 + resolved pn2), zero @lid or @newsletter.
	statusContacts, err := dev.Contacts.GetAllContacts(WithStatusPNOnlyContacts(context.Background()))
	if err != nil {
		t.Fatalf("GetAllContacts(status): %v", err)
	}
	if len(statusContacts) != 2 {
		t.Fatalf("expected 2 PN-only contacts for status, got %d: %+v", len(statusContacts), statusContacts)
	}
	for jid := range statusContacts {
		if jid.Server != waTypes.DefaultUserServer {
			t.Fatalf("unexpected non-PN server in status recipients: %s", jid)
		}
	}
	if _, ok := statusContacts[pn1]; !ok {
		t.Fatalf("missing pn1 in status recipients")
	}
	if _, ok := statusContacts[pn2]; !ok {
		t.Fatalf("expected lidMapped to resolve to pn2 in status recipients")
	}

	// Blacklist ("My Contacts Except..."): if lidMapped is blacklisted, its resolved PN (pn2) must also be excluded!
	blacklist := map[waTypes.JID]struct{}{
		lidMapped: {},
	}
	blContacts, err := dev.Contacts.GetAllContacts(WithStatusPNOnlyContacts(context.Background(), blacklist))
	if err != nil {
		t.Fatalf("GetAllContacts(blacklist): %v", err)
	}
	if len(blContacts) != 1 {
		t.Fatalf("expected 1 contact after excluding blacklisted LID, got %d: %+v", len(blContacts), blContacts)
	}
	if _, leaked := blContacts[pn2]; leaked {
		t.Fatalf("blacklisted contact pn2 leaked into status recipients!")
	}
}
