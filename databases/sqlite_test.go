package db

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestOpenSequentialSQLitePragmas(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "session.db")

	conn, err := OpenSequentialSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSequentialSQLite failed: %v", err)
	}
	defer func() {
		if err := CheckpointAndClose(conn); err != nil {
			t.Fatalf("CheckpointAndClose failed: %v", err)
		}
	}()

	status := SQLiteStatus(conn)
	if got := strings.ToLower(status["journal_mode"]); got != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", got)
	}
	if got := status["synchronous"]; got != "1" {
		t.Fatalf("expected synchronous=1 (NORMAL), got %q", got)
	}
	if got := status["foreign_keys"]; got != "1" {
		t.Fatalf("expected foreign_keys=1, got %q", got)
	}
	if got := status["busy_timeout"]; got != "10000" {
		t.Fatalf("expected busy_timeout=10000, got %q", got)
	}
}

func TestChatStoreSequentialBatchedWritesAndQueries(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenChatStore(filepath.Join(dir, "webchat.db"), filepath.Join(dir, "webmedia"))
	if err != nil {
		t.Fatalf("OpenChatStore failed: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("store.Close failed: %v", err)
		}
	}()

	const acct = "628111222333"
	const workers = 100
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			chatJID := "628999000111@s.whatsapp.net"
			isGroup := false
			chatName := "Alice"
			isVO := false
			if i%5 == 0 {
				chatJID = "1203630001@g.us"
				isGroup = true
				chatName = "Dev Group"
			}
			if i == 7 {
				isVO = true
			}
			store.RecordMessage(StoredMessage{
				AccountID:   acct,
				MsgID:       fmt.Sprintf("MSG-%03d", i),
				ChatJID:     chatJID,
				SenderJID:   chatJID,
				SenderName:  chatName,
				IsFromMe:    false,
				MsgType:     "text",
				Body:        fmt.Sprintf("hello %d", i),
				IsViewOnce:  isVO,
				Timestamp:   int64(1700000000 + i),
				ChatName:    chatName,
				ChatIsGroup: isGroup,
			})
		}()
	}
	wg.Wait()
	store.Flush()

	allChats, err := store.ListChats(acct, "", "all")
	if err != nil {
		t.Fatalf("ListChats(all) error: %v", err)
	}
	if len(allChats) != 2 {
		t.Fatalf("expected 2 chats, got %d", len(allChats))
	}

	groupChats, err := store.ListChats(acct, "", "groups")
	if err != nil {
		t.Fatalf("ListChats(groups) error: %v", err)
	}
	if len(groupChats) != 1 || !groupChats[0].IsGroup {
		t.Fatalf("expected 1 group chat, got %+v", groupChats)
	}

	voChats, err := store.ListChats(acct, "", "viewonce")
	if err != nil {
		t.Fatalf("ListChats(viewonce) error: %v", err)
	}
	if len(voChats) != 1 || !voChats[0].HasViewOnce {
		t.Fatalf("expected 1 viewonce chat, got %+v", voChats)
	}

	// Mark DM chat read and verify unread filter only leaves group chat.
	store.MarkChatRead(acct, "628999000111@s.whatsapp.net")
	store.Flush()
	unreadChats, err := store.ListChats(acct, "", "unread")
	if err != nil {
		t.Fatalf("ListChats(unread) error: %v", err)
	}
	if len(unreadChats) != 1 || unreadChats[0].ChatJID != "1203630001@g.us" {
		t.Fatalf("expected only group chat unread, got %+v", unreadChats)
	}

	// Mark message deleted & update media paths.
	store.MarkDeleted(acct, "MSG-007")
	store.UpdateMediaPaths(acct, "MSG-007", "628111222333/MSG-007.jpg", "628111222333/MSG-007_audio.ogg", "image/jpeg")
	store.Flush()

	got, err := store.GetMessage(acct, "MSG-007")
	if err != nil || got == nil {
		t.Fatalf("GetMessage failed: %v", err)
	}
	if !got.IsDeleted || !got.IsViewOnce || got.MediaPath == "" || got.AudioPath == "" {
		t.Fatalf("unexpected message state: %+v", got)
	}

	// Verify chronological ordering of ListMessages.
	msgs, err := store.ListMessages(acct, "628999000111@s.whatsapp.net", 200)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 80 {
		t.Fatalf("expected 80 DM messages, got %d", len(msgs))
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Timestamp < msgs[i-1].Timestamp {
			t.Fatalf("messages not in chronological order at index %d: %d < %d", i, msgs[i].Timestamp, msgs[i-1].Timestamp)
		}
	}
}
