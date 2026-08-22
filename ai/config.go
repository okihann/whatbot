package ai

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"

	"github.com/joho/godotenv"
)

type ProviderConfig struct {
	Name     string
	BaseURL  string
	APIKeys  []string
	keyIndex uint64
}

func (p *ProviderConfig) GetNextKey() string {
	if len(p.APIKeys) == 0 {
		return ""
	}
	idx := atomic.AddUint64(&p.keyIndex, 1) - 1
	return p.APIKeys[idx%uint64(len(p.APIKeys))]
}

type Config struct {
	ServerSecret string
	ServerPort   string
	JinaKeys     []string
	jinaIndex    uint64
	TavilyKeys   []string
	tavilyIndex  uint64
	Providers    []ProviderConfig
}

func (c *Config) GetNextJinaKey() string {
	if len(c.JinaKeys) == 0 {
		return ""
	}
	idx := atomic.AddUint64(&c.jinaIndex, 1) - 1
	return c.JinaKeys[idx%uint64(len(c.JinaKeys))]
}

func (c *Config) GetNextTavilyKey() string {
	if len(c.TavilyKeys) == 0 {
		return ""
	}
	idx := atomic.AddUint64(&c.tavilyIndex, 1) - 1
	return c.TavilyKeys[idx%uint64(len(c.TavilyKeys))]
}

var AppConfig Config

func parseKeys(envValue string) []string {
	if envValue == "" {
		return nil
	}
	raw := strings.Split(envValue, ",")
	var clean []string
	for _, k := range raw {
		trimmed := strings.TrimSpace(k)
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return clean
}

func MaskKey(key string) string {
	if key == "" {
		return "❌ [EMPTY / MISSING]"
	}
	if len(key) <= 8 {
		return fmt.Sprintf("✅ [LOADED] (Len: %d)", len(key))
	}
	return fmt.Sprintf("✅ [LOADED] (%s...%s)", key[:4], key[len(key)-4:])
}

func LoadEnv() {
	log.Println("🔍 [CONFIG] Loading environment configuration...")

	err := godotenv.Load()
	if err != nil {
		log.Printf("⚠️  [CONFIG] Could not find or read .env file: %v\n", err)
	} else {
		log.Println("✅ [CONFIG] Successfully loaded .env file.")
	}
    
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}
	if port[0] != ':' {
		port = ":" + port
	}

	AppConfig = Config{
		ServerSecret: os.Getenv("MY_SERVER_API_KEY"),
		ServerPort:   port,
		JinaKeys:     parseKeys(os.Getenv("JINA_API_KEYS")),
		TavilyKeys:   parseKeys(os.Getenv("TAVILY_API_KEYS")),
	}

	// DYNAMIC PROVIDER LOADING
	// Scans .env for anything ending in _BASE_URL and auto-registers it
	for _, envStr := range os.Environ() {
		parts := strings.SplitN(envStr, "=", 2)
		if len(parts) == 2 {
			key := parts[0]
			if strings.HasSuffix(key, "_BASE_URL") {
				prefix := strings.TrimSuffix(key, "_BASE_URL")
				providerName := strings.ToLower(prefix)
				
				baseURL := os.Getenv(key)
				keysEnv := os.Getenv(prefix + "_API_KEY")
				if keysEnv == "" {
					keysEnv = os.Getenv(prefix + "_API_KEYS") // Fallback plural
				}

				if baseURL != "" && keysEnv != "" {
					AppConfig.Providers = append(AppConfig.Providers, ProviderConfig{
						Name:    providerName,
						BaseURL: baseURL,
						APIKeys: parseKeys(keysEnv),
					})
				}
			}
		}
	}
    
	log.Println("--------------------------------------------------")
	log.Println("🛠️  [CONFIG DIAGNOSTICS]")
	log.Printf(" 🔌 Server Port         : %s\n", AppConfig.ServerPort)
	log.Printf(" 🔑 Server Secret       : %s\n", MaskKey(AppConfig.ServerSecret))
	log.Printf(" 🔎 Jina Keys Loaded    : %d accounts\n", len(AppConfig.JinaKeys))
	log.Printf(" 🔍 Tavily Keys Loaded  : %d accounts\n", len(AppConfig.TavilyKeys))

	for _, p := range AppConfig.Providers {
		log.Printf(" 🤖 Provider [%s] -> %d API keys loaded\n", p.Name, len(p.APIKeys))
	}
	log.Println("--------------------------------------------------")
}