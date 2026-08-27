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
	AutoConfig     *AutomationConfig
)

func init() {
	LoadAutoConfig()
}

func LoadAutoConfig() *AutomationConfig {
	autoMutex.Lock()
	defer autoMutex.Unlock()

	if AutoConfig != nil {
		return AutoConfig
	}

	data, err := os.ReadFile(autoConfigPath)
	if err != nil {
		AutoConfig = &AutomationConfig{AntiDeleteTargets: []string{}, AutoGetTargets: []string{}}
		return AutoConfig
	}
	json.Unmarshal(data, &AutoConfig)
	return AutoConfig
}

func SaveAutoConfig() error {
	autoMutex.Lock()
	defer autoMutex.Unlock()

	os.MkdirAll(filepath.Dir(autoConfigPath), 0755)
	data, _ := json.MarshalIndent(AutoConfig, "", "  ")
	return os.WriteFile(autoConfigPath, data, 0644)
}
