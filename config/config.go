package config

import (
	"encoding/json"
	"os"
	"sync"
)

// BotSettings holds general settings for the bot.
type BotSettings struct {
	BotName   string   `json:"botName"`
	Prefix    string   `json:"prefix"`
	OwnerJIDs []string `json:"ownerJIDs"`
}

// HelpMenuTemplate holds the customizable template strings.
type HelpMenuTemplate struct {
	MainMenu     string `json:"mainMenu"`
	CategoryMenu string `json:"categoryMenu"`
}

// NEW: AIModel defines the structure for a model in the selector
type AIModel struct {
	Name string `json:"name"` // User-friendly name (e.g., "Mistral 7B (Free)")
	ID   string `json:"id"`   // API-specific ID (e.g., "mistralai/mistral-7b-instruct:free")
}

// APISettings holds various API keys and model definitions.
type APISettings struct {
	GroqAPIKey       string    `json:"groqApiKey"`
	OpenRouterAPIKey string    `json:"openRouterApiKey"`
	AvailableModels  []AIModel `json:"availableModels"` // NEW: List of models for the web UI
}

// WebServerSettings holds settings for the web server.
// ... (rest of file is unchanged, but provided for completeness) ...
type WebServerSettings struct {
	Host string `json:"host"`
	Port string `json:"port"`
}

// NEW: DownloaderSettings holds settings for yt-dlp.
type DownloaderSettings struct {
	CookiesFile string `json:"cookiesFile"`
}

// Config is the top-level structure for the entire config.json file.
type Config struct {
	Settings         BotSettings        `json:"botSettings"`
	HelpMenuTemplate HelpMenuTemplate   `json:"helpMenuTemplate"`
	APIs             APISettings        `json:"apis"`
	WebServer        WebServerSettings  `json:"webServer"`
	Downloader       DownloaderSettings `json:"downloader"` // NEW
}

const configPath = "config.json"

var (
	configCache *Config
	configMutex sync.Mutex
)

// defaultConfig provides the initial structure if config.json is missing.
func defaultConfig() *Config {
	return &Config{
		Settings: BotSettings{
			BotName:   "Nopebot",
			Prefix:    "/",
			OwnerJIDs: []string{"628999778909"},
		},
		HelpMenuTemplate: HelpMenuTemplate{
			MainMenu: `*${botName}*

Hello, ${userName} 👋
I am an automated system (BOT) that can help to do something, search and get data / information only through WhatsApp.

╭───「 *INFO* 」
│➣ *Name*: ${botName}
│➣ *Platform*: ${platform}
│➣ *Type*: Go-WA
│➣ *Uptime*: ${uptime}
│➣ *Date*: ${date}
│➣ *Time*: ${time}
╰─────────────
${readMore}
${menuList}

Reply with the number of the menu you want to view.`,
			CategoryMenu: "╭─「 *${emoji} ${categoryName} Menu* 」\n${commandList}\n╰────",
		},
		APIs: APISettings{
			GroqAPIKey:       "YOUR_GROQ_API_KEY_HERE",
			OpenRouterAPIKey: "YOUR_OPENROUTER_API_KEY_HERE",
			// NEW: Default model list
			AvailableModels: []AIModel{
				{Name: "Mistral 7B (Free)", ID: "mistralai/mistral-7b-instruct:free"},
				{Name: "Gemini Flash 1.5", ID: "google/gemini-flash-1.5"},
				{Name: "Claude 3 Haiku", ID: "anthropic/claude-3-haiku"},
				{Name: "Llama 3 8B", ID: "meta-llama/llama-3-8b-instruct"},
				{Name: "GPT-4o mini", ID: "openai/gpt-4o-mini"},
			},
		},
		WebServer: WebServerSettings{
			Host: "0.0.0.0",
			Port: "6064",
		},
		// NEW: Add default downloader settings
		Downloader: DownloaderSettings{
			CookiesFile: "", // Default is no cookies file
		},
	}
}

// LoadConfig reads the configuration from config.json, creating it if it doesn't exist.
func LoadConfig() (*Config, error) {
	configMutex.Lock()
	defer configMutex.Unlock()

	if configCache != nil {
		return configCache, nil
	}

	file, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			defaultConf := defaultConfig()
			data, err := json.MarshalIndent(defaultConf, "", "  ") // Use 2 spaces for standard JSON
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(configPath, data, 0644); err != nil {
				return nil, err
			}
			configCache = defaultConf
			return configCache, nil
		}
		return nil, err
	}

	var conf Config
	if err := json.Unmarshal(file, &conf); err != nil {
		return nil, err
	}

	// --- THIS IS THE FIX ---
	// Check if the Downloader field is uninitialized (i.e., it's an old config)
	if conf.Downloader == (DownloaderSettings{}) {
		// If it is, populate it with the default values
		conf.Downloader = defaultConfig().Downloader
	}

	// --- MODIFIED THIS CHECK ---
	// We can't compare structs with slices. Instead, we'll check the fields
	// individually. This ensures that old configs are properly updated.

	// If GroqAPIKey is missing, add the default
	if conf.APIs.GroqAPIKey == "" {
		conf.APIs.GroqAPIKey = defaultConfig().APIs.GroqAPIKey
	}

	// If OpenRouterAPIKey is missing, add the default
	if conf.APIs.OpenRouterAPIKey == "" {
		conf.APIs.OpenRouterAPIKey = defaultConfig().APIs.OpenRouterAPIKey // Add just the missing key
	}

	// If AvailableModels is missing, add the defaults
	if len(conf.APIs.AvailableModels) == 0 {
		conf.APIs.AvailableModels = defaultConfig().APIs.AvailableModels // Add default models
	}
	// --- END OF FIX ---

	configCache = &conf
	return configCache, nil
}

func SaveConfig(conf *Config) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	data, err := json.MarshalIndent(conf, "", "  ") // Use 2 spaces for standard JSON
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return err
	}
	configCache = conf
	return nil
}
