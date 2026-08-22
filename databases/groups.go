package db

import (
	"bot/types"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const groupDataDir = "db/groups"

var (
	groupSettingsCache = make(map[string]*types.GroupSettings)
	groupMutex         sync.Mutex
)

func ensureDir() {
	if _, err := os.Stat(groupDataDir); os.IsNotExist(err) {
		os.MkdirAll(groupDataDir, 0755)
	}
}

func getGroupFilePath(groupID string) string {
	return filepath.Join(groupDataDir, fmt.Sprintf("%s.json", groupID))
}

// LoadGroupSettings loads settings for a specific group, creating defaults if none exist.
func LoadGroupSettings(groupID string) (*types.GroupSettings, error) {
	groupMutex.Lock()
	defer groupMutex.Unlock()

	if settings, found := groupSettingsCache[groupID]; found {
		return settings, nil
	}

	ensureDir()
	filePath := getGroupFilePath(groupID)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// Default settings for a new group.
		defaultSettings := &types.GroupSettings{
			WelcomeEnabled:     false,
			WelcomeMessage:     "Welcome @user to {group}! 🎉 We now have {count} members.", // Default message
			DisabledCategories: make(map[string]bool),
			DisabledCommands:   make(map[string]bool),
		}
		// Save the new default settings
		if err := saveGroupSettingsInternal(groupID, defaultSettings); err != nil {
			return nil, err
		}
		groupSettingsCache[groupID] = defaultSettings
		return defaultSettings, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var settings types.GroupSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	// Ensure maps and message are not nil/empty from older configs
	if settings.DisabledCategories == nil {
		settings.DisabledCategories = make(map[string]bool)
	}
	if settings.DisabledCommands == nil {
		settings.DisabledCommands = make(map[string]bool)
	}
	if settings.WelcomeMessage == "" {
		settings.WelcomeMessage = "Welcome @user to {group}! 🎉 We now have {count} members."
	}

	groupSettingsCache[groupID] = &settings
	return &settings, nil
}

// SaveGroupSettings saves the settings for a group.
func SaveGroupSettings(groupID string, settings *types.GroupSettings) error {
	groupMutex.Lock()
	defer groupMutex.Unlock()
	return saveGroupSettingsInternal(groupID, settings)
}

func saveGroupSettingsInternal(groupID string, settings *types.GroupSettings) error {
	ensureDir()
	filePath := getGroupFilePath(groupID)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	groupSettingsCache[groupID] = settings
	return os.WriteFile(filePath, data, 0644)
}
