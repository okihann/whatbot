package utils

import (
	"bot/config"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestUnwrapAndViewOnceDetection(t *testing.T) {
	innerImg := &waE2E.ImageMessage{
		URL:      proto.String("https://mmg.whatsapp.net/img"),
		Mimetype: proto.String("image/jpeg"),
		Caption:  proto.String("secret"),
		ViewOnce: proto.Bool(true),
		ContextInfo: &waE2E.ContextInfo{
			Expiration: proto.Uint32(86400),
		},
	}

	wrapped := &waE2E.Message{
		EphemeralMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{
				ViewOnceMessageV2: &waE2E.FutureProofMessage{
					Message: &waE2E.Message{
						ImageMessage: innerImg,
					},
				},
			},
		},
	}

	unwrapped := UnwrapMessage(wrapped)
	if unwrapped == nil || unwrapped.ImageMessage == nil {
		t.Fatalf("expected unwrapped ImageMessage, got %+v", unwrapped)
	}
	if !IsViewOnceMessage(nil, wrapped) {
		t.Fatalf("expected wrapped ViewOnceMessageV2 to be detected as view-once")
	}
	if !IsViewOnceMessage(nil, &waE2E.Message{ImageMessage: innerImg}) {
		t.Fatalf("expected inner ImageMessage.ViewOnce=true to be detected as view-once")
	}

	evt := &events.Message{IsViewOnce: true, Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}}
	if !IsViewOnceMessage(evt, nil) {
		t.Fatalf("expected events.Message.IsViewOnce=true to be detected")
	}

	// Sanitized clone must strip ViewOnce and ContextInfo without mutating original.
	cloned, supportsCaption := CloneSanitizedMediaMessage(wrapped, "recovered caption")
	if !supportsCaption || cloned == nil || cloned.ImageMessage == nil {
		t.Fatalf("expected cloned ImageMessage supporting caption")
	}
	if cloned.ImageMessage.GetViewOnce() {
		t.Fatalf("cloned ImageMessage must have ViewOnce=false")
	}
	if cloned.ImageMessage.ContextInfo != nil {
		t.Fatalf("cloned ImageMessage must clear ContextInfo")
	}
	if cloned.ImageMessage.GetCaption() != "recovered caption" {
		t.Fatalf("unexpected caption: %q", cloned.ImageMessage.GetCaption())
	}
	if !innerImg.GetViewOnce() || innerImg.ContextInfo == nil {
		t.Fatalf("CloneSanitizedMediaMessage mutated the original message")
	}
}

func TestMessageCacheDoesNotClobberGoodMessageWithStubQuote(t *testing.T) {
	cache := NewMessageCache()

	fullImg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			URL:        proto.String("https://mmg.whatsapp.net/real-media"),
			DirectPath: proto.String("/v/t62.7118-24/real"),
			MediaKey:   []byte{1, 2, 3},
			ViewOnce:   proto.Bool(true),
		},
	}
	stubQuote := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			ViewOnce: proto.Bool(true),
		},
	}

	cache.Put("bot1", "MSG1", &CachedMessage{
		Message:    fullImg,
		IsViewOnce: true,
		StoredAt:   time.Now(),
	})
	cache.SetMediaData("bot1", "MSG1", []byte("decrypted-jpeg-bytes"))

	// A reply arrives with a stripped quote stub; cache MUST retain fullImg + MediaData.
	cache.PutQuotedIfBetter("bot1", "MSG1", &CachedMessage{
		Message:    stubQuote,
		IsViewOnce: true,
		StoredAt:   time.Now(),
	})

	got, ok := cache.Get("bot1", "MSG1")
	if !ok {
		t.Fatalf("expected cached entry for MSG1")
	}
	if !HasDownloadableMedia(got.Message) {
		t.Fatalf("good cached media was overwritten by stripped quote stub")
	}
	if string(got.MediaData) != "decrypted-jpeg-bytes" {
		t.Fatalf("expected pre-cached media bytes to be preserved, got %q", string(got.MediaData))
	}

	// Conversely, if original was a stub and a quote HAS downloadable media, upgrade it.
	cache.Put("bot1", "MSG2", &CachedMessage{
		Message:  stubQuote,
		StoredAt: time.Now(),
	})
	cache.PutQuotedIfBetter("bot1", "MSG2", &CachedMessage{
		Message:    fullImg,
		IsViewOnce: true,
		StoredAt:   time.Now(),
	})
	got2, ok := cache.Get("bot1", "MSG2")
	if !ok || !HasDownloadableMedia(got2.Message) || !got2.IsViewOnce {
		t.Fatalf("expected stub entry MSG2 to be upgraded by quote with downloadable media")
	}
}

func TestMessageCacheAutoGetMarkAndPrune(t *testing.T) {
	cache := NewMessageCache()
	if !cache.TryMarkAutoGet("bot1", "M1") {
		t.Fatalf("first TryMarkAutoGet should return true")
	}
	if cache.TryMarkAutoGet("bot1", "M1") {
		t.Fatalf("second TryMarkAutoGet should return false")
	}
	cache.UnmarkAutoGet("bot1", "M1")
	if !cache.TryMarkAutoGet("bot1", "M1") {
		t.Fatalf("TryMarkAutoGet after UnmarkAutoGet should return true")
	}

	cache.Put("bot1", "OLD", &CachedMessage{
		Message:  &waE2E.Message{Conversation: proto.String("old")},
		StoredAt: time.Now().Add(-25 * time.Hour),
	})
	cache.Prune(time.Now())
	if _, ok := cache.Get("bot1", "OLD"); ok {
		t.Fatalf("expected expired message to be pruned")
	}
}

func TestJIDAndOwnerResolution(t *testing.T) {
	lidJID := waTypes.NewJID("123456789012345", waTypes.HiddenUserServer)
	pnJID := waTypes.NewJID("628123456789", waTypes.DefaultUserServer)

	if got := ResolvePhoneNumber(nil, lidJID, pnJID); got != "628123456789" {
		t.Fatalf("expected SenderAlt PN to resolve from LID, got %q", got)
	}
	if got := ResolvePhoneNumber(nil, pnJID, waTypes.EmptyJID); got != "628123456789" {
		t.Fatalf("expected direct PN, got %q", got)
	}

	conf := &config.Config{
		Settings: config.BotSettings{
			OwnerJIDs: []string{"+62 812-3456-789"},
		},
	}
	msg := &events.Message{
		Info: waTypes.MessageInfo{
			MessageSource: waTypes.MessageSource{
				Sender:    lidJID,
				SenderAlt: pnJID,
			},
		},
	}
	if !IsOwner(nil, msg, conf) {
		t.Fatalf("expected owner with LID Sender + PN SenderAlt to match normalized owner number")
	}
}

func TestExtractURLAndSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"Check this out https://vt.tiktok.com/ZS12345/!":    "https://vt.tiktok.com/ZS12345/",
		"(https://pin.it/abc123)":                           "https://pin.it/abc123",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=10s": "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=10s",
		"no link here": "",
	}
	for in, want := range cases {
		if got := ExtractURL(in); got != want {
			t.Errorf("ExtractURL(%q) = %q, want %q", in, got, want)
		}
	}

	if got := SanitizeFilename("My: Video / Test? *2026*"); got != "My_ Video _ Test_ _2026_" {
		t.Errorf("unexpected SanitizeFilename result: %q", got)
	}
}
