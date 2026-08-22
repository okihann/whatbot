package session

import (
	"bot/types"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const (
	conversationsDir = "db/conversations"
	userDataPath     = "db/userdata.json"
	defaultModel     = "llama-3.1-8b-instant"
	defaultRole      = "You are a helpful assistant."
)

var (
	userDataCache  = make(map[string]*types.UserData)
	sessionCache   = make(map[string]map[string]*types.AISession) // userID -> sessionID -> session
	currentSession = make(map[string]string)                      // userID -> sessionID
	aiMutex        sync.Mutex
)

func ensureDir(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.MkdirAll(path, 0755)
	}
}

func init() {
	ensureDir(conversationsDir)
	loadUserData()
}

// --- User Data Management ---

func loadUserData() {
	aiMutex.Lock()
	defer aiMutex.Unlock()
	if _, err := os.Stat(userDataPath); os.IsNotExist(err) {
		return // No data to load
	}
	data, err := os.ReadFile(userDataPath)
	if err != nil {
		fmt.Println("Error loading user data:", err)
		return
	}
	json.Unmarshal(data, &userDataCache)
}

func SaveUserData() {
	aiMutex.Lock()
	defer aiMutex.Unlock()
	data, err := json.MarshalIndent(userDataCache, "", "  ")
	if err != nil {
		fmt.Println("Error marshalling user data:", err)
		return
	}
	os.WriteFile(userDataPath, data, 0600)
}

// GetUserData is the public, thread-safe function to get user data.
func GetUserData(userID string) *types.UserData {
	aiMutex.Lock()
	defer aiMutex.Unlock()
	return getUserDataInternal(userID)
}

// getUserDataInternal is the private function that assumes a lock is already held.
// This is the key to preventing the deadlock.
func getUserDataInternal(userID string) *types.UserData {
	if _, ok := userDataCache[userID]; !ok {
		userDataCache[userID] = &types.UserData{
			Model: defaultModel,
			Role:  defaultRole,
		}
	}
	return userDataCache[userID]
}

// --- AI Session Management ---

func getSessionFilePath(userID, sessionID string) string {
	userDir := filepath.Join(conversationsDir, userID)
	return filepath.Join(userDir, fmt.Sprintf("%s.json", sessionID))
}

// CreateSession is the public, thread-safe function for creating a new session.
func CreateSession(userID string) *types.AISession {
	aiMutex.Lock()
	defer aiMutex.Unlock()
	return createSessionInternal(userID)
}

// GetCurrentSession is the public, thread-safe function to get the active session.
func GetCurrentSession(userID string) *types.AISession {
	aiMutex.Lock()
	defer aiMutex.Unlock()

	userData := getUserDataInternal(userID) // Use the internal, unlocked function
	sessionID := userData.CurrentSession

	if sessionID != "" {
		// Check memory cache first
		if userSessions, ok := sessionCache[userID]; ok {
			if session, ok := userSessions[sessionID]; ok {
				return session
			}
		}

		// Not in memory, try loading from disk
		if loadedSession, err := loadSessionInternal(userID, sessionID); err == nil {
			if _, ok := sessionCache[userID]; !ok {
				sessionCache[userID] = make(map[string]*types.AISession)
			}
			sessionCache[userID][sessionID] = loadedSession
			return loadedSession
		}
	}

	// No current session found or it failed to load, so create a new one.
	return createSessionInternal(userID)
}

// createSessionInternal creates a new session, assuming a lock is already held.
func createSessionInternal(userID string) *types.AISession {
	userData := getUserDataInternal(userID)

	sessionID := uuid.New().String()[:6]
	session := &types.AISession{
		ID:       sessionID,
		Model:    userData.Model,
		Role:     userData.Role,
		UserID:   userID,
		Messages: []types.ChatMessage{{Role: "system", Content: userData.Role}},
	}

	if _, ok := sessionCache[userID]; !ok {
		sessionCache[userID] = make(map[string]*types.AISession)
	}
	sessionCache[userID][sessionID] = session
	userData.CurrentSession = sessionID

	saveSessionInternal(session)
	// We don't need to call SaveUserData() here because the caller will handle it.
	// However, to be safe, we'll save it via the public method.
	go SaveUserData()

	return session
}

func saveSessionInternal(session *types.AISession) error {
	userDir := filepath.Join(conversationsDir, session.UserID)
	ensureDir(userDir)
	filePath := getSessionFilePath(session.UserID, session.ID)
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0600)
}

func loadSessionInternal(userID, sessionID string) (*types.AISession, error) {
	filePath := getSessionFilePath(userID, sessionID)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session file not found")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var session types.AISession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	session.UserID = userID // Set internal field
	return &session, nil
}

func SaveAISession(session *types.AISession) {
	aiMutex.Lock()
	defer aiMutex.Unlock()
	saveSessionInternal(session)
}

func ListSessions(userID string) ([]string, error) {
	userDir := filepath.Join(conversationsDir, userID)
	files, err := os.ReadDir(userDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // No sessions yet
		}
		return nil, err
	}
	var sessionIDs []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			sessionIDs = append(sessionIDs, strings.TrimSuffix(file.Name(), ".json"))
		}
	}
	return sessionIDs, nil
}

func SwitchSession(userID, sessionID string) (bool, error) {
	aiMutex.Lock()
	defer aiMutex.Unlock()
	if _, err := loadSessionInternal(userID, sessionID); err != nil {
		return false, err
	}
	userData := getUserDataInternal(userID)
	userData.CurrentSession = sessionID
	go SaveUserData()
	return true, nil
}

func DeleteSession(userID, sessionID string) error {
	aiMutex.Lock()
	defer aiMutex.Unlock()

	// Remove from cache if it exists
	if userSessions, ok := sessionCache[userID]; ok {
		delete(userSessions, sessionID)
	}

	filePath := getSessionFilePath(userID, sessionID)
	return os.Remove(filePath)
}