package confess

import (
	"path/filepath"
	"testing"

	db "bot/databases"
)

func TestConfessPersistenceAndPrivacyControls(t *testing.T) {
	dir := t.TempDir()
	store, err := db.OpenBotStore(filepath.Join(dir, "botdata.db"))
	if err != nil {
		t.Fatalf("OpenBotStore: %v", err)
	}
	defer store.Close()

	if got := cleanPhone("0812-3456-7890"); got != "6281234567890" {
		t.Fatalf("expected Indonesian 08->628 normalization, got %q", got)
	}

	rec := &db.ConfessionRecord{
		ID:            "CF-ABCD",
		SenderJID:     "62811111111",
		TargetJID:     "62822222222",
		Alias:         "Secret",
		Message:       "Hello!",
		Status:        "open",
		LastTargetMsg: "MSG-TARGET-1",
	}
	if err := store.SaveConfession(rec); err != nil {
		t.Fatalf("SaveConfession: %v", err)
	}

	byID, err := store.GetConfession("#cf-abcd")
	if err != nil || byID == nil || byID.Message != "Hello!" {
		t.Fatalf("GetConfession failed: %+v err=%v", byID, err)
	}
	byMsg, err := store.GetConfessionByMsgID("MSG-TARGET-1")
	if err != nil || byMsg == nil || byMsg.ID != "CF-ABCD" {
		t.Fatalf("GetConfessionByMsgID failed: %+v err=%v", byMsg, err)
	}

	if store.IsConfessBlocked("62822222222", "62811111111") {
		t.Fatalf("should not be blocked initially")
	}
	if err := store.BlockConfessSender("62822222222", "62811111111"); err != nil {
		t.Fatalf("BlockConfessSender: %v", err)
	}
	if !store.IsConfessBlocked("62822222222", "62811111111") {
		t.Fatalf("expected sender to be anonymously blocked")
	}
}
