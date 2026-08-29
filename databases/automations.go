package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type AutomationConfig struct {
	AntiDeleteOn      bool     `json:"antiDeleteOn"`
	AntiDeleteTargets []string `json:"antiDeleteTargets"`
	AutoGetOn         bool     `json:"autoGetOn"`
	AutoGetTargets    []string `json:"autoGetTargets"`
}

var (
	autoConfigPath = "db/automations.json"
	autoMutex      sync.Mutex
	// Upgraded to a map to support multiple accounts simultaneously
	AutoConfigs    map[string]*AutomationConfig
)

func init() {
	AutoConfigs = make(map[string]*AutomationConfig)
	loadAllConfigs()
}

// Internal function to load the entire JSON map into memory on startup
func loadAllConfigs() {
	autoMutex.Lock()
	defer autoMutex.Unlock()

	data, err := os.ReadFile(autoConfigPath)
	if err == nil {
		json.Unmarshal(data, &AutoConfigs)
	}
}

// GetAutoConfig dynamically fetches or creates a config for a specific bot number
func GetAutoConfig(botID string) *AutomationConfig {
	autoMutex.Lock()
	defer autoMutex.Unlock()

	// If this account already has settings, return them
	if config, exists := AutoConfigs[botID]; exists {
		return config
	}

	// If this is a brand new account connecting for the first time, initialize it
	newConfig := &AutomationConfig{
		AntiDeleteTargets: []string{},
		AutoGetTargets:    []string{},
	}
	AutoConfigs[botID] = newConfig
	return newConfig
}

// SaveAutoConfig saves the entire multi-account map back to JSON
func SaveAutoConfig() error {
	autoMutex.Lock()
	defer autoMutex.Unlock()

	os.MkdirAll(filepath.Dir(autoConfigPath), 0755)
	data, _ := json.MarshalIndent(AutoConfigs, "", "  ")
	return os.WriteFile(autoConfigPath, data, 0644)
}
