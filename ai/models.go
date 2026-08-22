package ai

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type CachedModel struct {
	Provider *ProviderConfig
	ActualID string
}

var modelCache = make(map[string]CachedModel)
var cacheMu sync.RWMutex

func RefreshModels() {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	log.Println("🔄 [MODELS] Refreshing active models concurrently...")
	modelCache = make(map[string]CachedModel)
	
	var wg sync.WaitGroup
	var localMu sync.Mutex

	for i := range AppConfig.Providers {
		provider := &AppConfig.Providers[i]
		if len(provider.APIKeys) == 0 {
			continue
		}

		wg.Add(1)
		go func(p *ProviderConfig) {
			defer wg.Done()

			endpoint := p.BaseURL
			if endpoint[len(endpoint)-1] == '/' {
				endpoint = endpoint[:len(endpoint)-1]
			}
			endpoint += "/models"

			apiKey := p.GetNextKey()
			req, _ := http.NewRequest("GET", endpoint, nil)
			req.Header.Set("Authorization", "Bearer "+apiKey)
            req.Header.Set("User-Agent", "Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
			req.Header.Set("Accept", "application/json, text/plain, */*")
			req.Header.Set("Accept-Language", "en-US,en;q=0.9")

			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("❌ [MODELS] Failed to reach provider [%s]: %v\n", p.Name, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				log.Printf("❌ [MODELS] Provider [%s] returned status code: %d\n", p.Name, resp.StatusCode)
				return
			}

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				var rawModels []interface{}

				if data, ok := result["data"].([]interface{}); ok {
					rawModels = data
				} else if models, ok := result["models"].([]interface{}); ok {
					rawModels = models
				}

				count := 0
				for _, rm := range rawModels {
					if m, ok := rm.(map[string]interface{}); ok {
						
						isFree := true
						
						if pricingMap, ok := m["pricing"].(map[string]interface{}); ok {
							promptPrice := fmt.Sprintf("%v", pricingMap["prompt"])
							if promptPrice != "0" && promptPrice != "0.0" && promptPrice != "0.00" && promptPrice != "<nil>" {
								isFree = false
							}
						}
						if inputPrice, ok := m["input_price"].(float64); ok && inputPrice > 0 {
							isFree = false
						}
						if outputPrice, ok := m["output_price"].(float64); ok && outputPrice > 0 {
							isFree = false
						}

						// ✨ Bypass pricing filter for purely free providers like Groq
						if p.Name == "groq" {
							isFree = true
						}

						if !isFree {
							continue
						}

						id, _ := m["id"].(string)
						if id == "" {
							id, _ = m["name"].(string)
						}

						if id != "" {
							prefixedName := fmt.Sprintf("%s/%s", p.Name, id)
							
							localMu.Lock()
							modelCache[prefixedName] = CachedModel{
								Provider: p,
								ActualID: id,
							}
							localMu.Unlock()
							count++
						}
					}
				}
				log.Printf("✅ [MODELS] Provider [%s] loaded %d FREE models successfully.\n", p.Name, count)
			}
		}(provider)
	}
	
	wg.Wait()
	log.Printf("🎉 [MODELS] Total active prefixed FREE models registered: %d\n", len(modelCache))
}

func GetCachedModel(prefixedModelName string) (CachedModel, error) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	cached, exists := modelCache[prefixedModelName]
	if !exists {
		return CachedModel{}, fmt.Errorf("model '%s' not found. Check /models for available prefixed names", prefixedModelName)
	}
	return cached, nil
}