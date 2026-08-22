package session

import (
	"sync"
	"time"
)

type HelpSession struct {
	MessageID  string
	Categories []string
}

var (
	activeHelpMenus = make(map[string]HelpSession)
	mu              sync.Mutex
)

func StoreHelpSession(jid, messageID string, categories []string) {
	mu.Lock()
	defer mu.Unlock()
	activeHelpMenus[jid] = HelpSession{
		MessageID:  messageID,
		Categories: categories,
	}
}

func GetHelpSession(jid string) (HelpSession, bool) {
	mu.Lock()
	defer mu.Unlock()
	session, found := activeHelpMenus[jid]
	return session, found
}

func ClearHelpSession(jid string) {
	mu.Lock()
	defer mu.Unlock()
	delete(activeHelpMenus, jid)
}

type InteractiveSession struct {
	Type      string
	Query     string
	Page      int
	Data      interface{}
	UserID    string      // The JID of the user who started the session
	TargetCmd string      // e.g., "ytmp4" or "ytmp3"
	Timer     *time.Timer // Auto-close timer
}

var (
	activeInteractiveSessions = make(map[string]*InteractiveSession)
	userSessionLocks          = make(map[string]bool) // Prevents spamming: "UserID_Type"
	interactiveMutex          sync.Mutex
)

// AcquireLock checks if a user is already running this command and locks it if not.
func AcquireLock(userID, sessionType string) bool {
	interactiveMutex.Lock()
	defer interactiveMutex.Unlock()
	key := userID + "_" + sessionType
	if userSessionLocks[key] {
		return false // They already have an active session
	}
	userSessionLocks[key] = true
	return true
}

// ReleaseLock frees the user so they can run the command again.
func ReleaseLock(userID, sessionType string) {
	key := userID + "_" + sessionType
	delete(userSessionLocks, key)
}

func StoreInteractiveSession(messageID string, sess *InteractiveSession) {
	interactiveMutex.Lock()
	defer interactiveMutex.Unlock()

	activeInteractiveSessions[messageID] = sess

	// Set a 30-second auto-destruct timer
	sess.Timer = time.AfterFunc(30*time.Second, func() {
		ClearInteractiveSession(messageID)
	})
}

func GetInteractiveSession(messageID string) (*InteractiveSession, bool) {
	interactiveMutex.Lock()
	defer interactiveMutex.Unlock()
	sess, found := activeInteractiveSessions[messageID]
	return sess, found
}

// ResetSessionTimer is called whenever a user interacts (+, -) to keep it alive.
func ResetSessionTimer(messageID string) {
	interactiveMutex.Lock()
	defer interactiveMutex.Unlock()
	if sess, found := activeInteractiveSessions[messageID]; found {
		if sess.Timer != nil {
			sess.Timer.Stop()
		}
		sess.Timer = time.AfterFunc(30*time.Second, func() {
			ClearInteractiveSession(messageID)
		})
	}
}

// ClearInteractiveSession stops the timer, releases the user lock, and deletes the memory.
func ClearInteractiveSession(messageID string) {
	interactiveMutex.Lock()
	defer interactiveMutex.Unlock()

	if sess, found := activeInteractiveSessions[messageID]; found {
		if sess.Timer != nil {
			sess.Timer.Stop()
		}
		ReleaseLock(sess.UserID, sess.Type)
		delete(activeInteractiveSessions, messageID)
	}
}
