package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// ExtractQuotedOrOwnMessage returns the quoted message if present (and carrying
// media/content), otherwise returns the unwrapped incoming message itself.
func ExtractQuotedOrOwnMessage(msg *events.Message) *waE2E.Message {
	if msg == nil || msg.Message == nil {
		return nil
	}
	if ctxInfo := ExtractContextInfo(msg.Message); ctxInfo != nil && ctxInfo.GetQuotedMessage() != nil {
		return UnwrapMessage(ctxInfo.GetQuotedMessage())
	}
	return UnwrapMessage(msg.Message)
}

// ExtractTargetJIDs extracts mentioned JIDs, quoted participant JID, or phone
// numbers passed in args.
func ExtractTargetJIDs(msg *events.Message, args []string) []waTypes.JID {
	seen := map[string]bool{}
	var out []waTypes.JID
	add := func(j waTypes.JID) {
		j = j.ToNonAD()
		if j.User == "" {
			return
		}
		if j.Server == "" {
			j.Server = waTypes.DefaultUserServer
		}
		k := j.String()
		if !seen[k] {
			seen[k] = true
			out = append(out, j)
		}
	}

	if msg != nil && msg.Message != nil {
		if ctxInfo := ExtractContextInfo(msg.Message); ctxInfo != nil {
			for _, m := range ctxInfo.GetMentionedJID() {
				if j, err := waTypes.ParseJID(m); err == nil {
					add(j)
				}
			}
			if len(out) == 0 && ctxInfo.GetParticipant() != "" {
				if j, err := waTypes.ParseJID(ctxInfo.GetParticipant()); err == nil {
					add(j)
				}
			}
		}
	}

	for _, tok := range strings.Fields(strings.Join(args, " ")) {
		clean := strings.TrimPrefix(tok, "@")
		if strings.Contains(clean, "@") {
			if j, err := waTypes.ParseJID(clean); err == nil {
				add(j)
				continue
			}
		}
		digits := NormalizeTarget(clean)
		if len(digits) >= 7 && digits != "all" {
			allDigits := true
			for _, r := range digits {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				add(waTypes.NewJID(digits, waTypes.DefaultUserServer))
			}
		}
	}
	return out
}

// SendMentionReply sends a text message with MentionedJID populated so WhatsApp
// highlights @mentions properly.
func SendMentionReply(client *whatsmeow.Client, chatJID waTypes.JID, text string, mentions []string) (whatsmeow.SendResponse, error) {
	return client.SendMessage(context.Background(), chatJID, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waE2E.ContextInfo{
				MentionedJID: mentions,
			},
		},
	})
}

// HTTPGetBytes is a convenience alias to DownloadHTTPBytes.
func HTTPGetBytes(ctx context.Context, rawURL string, extraHeaders map[string]string) ([]byte, string, error) {
	return DownloadHTTPBytes(ctx, rawURL, extraHeaders)
}

// HTTPGetJSON fetches a JSON URL and unmarshals into dst.
func HTTPGetJSON(ctx context.Context, rawURL string, extraHeaders map[string]string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", BrowserUserAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(dst)
}
