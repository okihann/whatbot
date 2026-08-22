package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"bot/types"
)

func GenerateAIResponse(sess *types.AISession) (string, error) {
	// 🛑 LOOP BREAKER: 10 Tools (2 messages per tool) + User + System = ~25 Max
	if len(sess.Messages) > 25 {
		return "⚠️ **System Override:** AI execution terminated. Maximum autonomous tool limit (10) reached.", nil
	}

	// 🧠 INJECT SYSTEM PROMPT
	hasSystem := false
	for _, m := range sess.Messages {
		if m.Role == "system" {
			hasSystem = true
			break
		}
	}
	if !hasSystem {
		sysMsg := types.ChatMessage{
			Role:    "system",
			Content: "You are an advanced AI Agent operating in a sandboxed Linux environment. You have permission to use up to 10 tool calls per request to solve the user's problem. Always think step-by-step and utilize the provided tools when necessary.",
		}
		sess.Messages = append([]types.ChatMessage{sysMsg}, sess.Messages...)
	}

	cached, err := GetCachedModel(sess.Model)
	if err != nil {
		return "", err
	}

	provider := cached.Provider
	actualModelID := cached.ActualID

	payload := map[string]interface{}{
		"model":       actualModelID,
		"messages":    sess.Messages,
		"tools":       []interface{}{WebSearchTool, RunCommandTool, UnzipTool},
		"tool_choice": "auto",
	}

	payloadBytes, _ := json.Marshal(payload)
	endpoint := provider.BaseURL
	if endpoint[len(endpoint)-1] == '/' {
		endpoint = endpoint[:len(endpoint)-1]
	}

	var lastResponseBody string
	keyAttempts := len(provider.APIKeys)
	if keyAttempts == 0 {
		keyAttempts = 1
	}

	apiPath := "/chat/completions"

	for attempt := 0; attempt < keyAttempts; attempt++ {
		apiKey := provider.GetNextKey()

		req, _ := http.NewRequest("POST", endpoint+apiPath, bytes.NewBuffer(payloadBytes))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
        req.Header.Set("User-Agent", "Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
		req.Header.Set("Accept", "application/json")

		// Timeout bumped to 60s per request
		resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
		if err != nil {
			log.Printf("❌ [PROVIDER NET ERROR] %s: %v\n", provider.Name, err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 200 {
			var aiResult map[string]interface{}
			json.Unmarshal(body, &aiResult)

			choices, ok := aiResult["choices"].([]interface{})
			if !ok || len(choices) == 0 {
				return "", fmt.Errorf("invalid API response format")
			}

			message := choices[0].(map[string]interface{})["message"].(map[string]interface{})

			// --- TOOL EXECUTION BLOCK ---
			if rawToolCalls, ok := message["tool_calls"].([]interface{}); ok && len(rawToolCalls) > 0 {
				toolCallMap := rawToolCalls[0].(map[string]interface{})
				funcData := toolCallMap["function"].(map[string]interface{})
				funcName := funcData["name"].(string)

				var args map[string]string
				json.Unmarshal([]byte(funcData["arguments"].(string)), &args)

				var toolOutput string
				switch funcName {
				case "web_search":
					log.Printf("🔍 [AI TOOL] Web search: '%s'\n", args["query"])
					toolOutput = PerformWebSearch(args["query"])
				case "run_command":
					log.Printf("💻 [AI TOOL] Command execution: '%s'\n", args["command"])
					toolOutput = ExecuteSandboxedCommand(args["command"])
				case "unzip_file":
					log.Printf("📦 [AI TOOL] Unzipping: '%s'\n", args["zip_filename"])
					toolOutput = ExtractZip(args["zip_filename"])
				}

				typedToolCalls := []types.ToolCall{
					{
						ID:   toolCallMap["id"].(string),
						Type: "function",
						Function: types.FunctionCall{
							Name:      funcName,
							Arguments: funcData["arguments"].(string),
						},
					},
				}

				sess.Messages = append(sess.Messages, types.ChatMessage{
					Role:      "assistant",
					Content:   "",
					ToolCalls: typedToolCalls,
				})
				sess.Messages = append(sess.Messages, types.ChatMessage{
					Role:       "tool",
					Content:    toolOutput,
					ToolCallID: toolCallMap["id"].(string),
				})

				return GenerateAIResponse(sess)
			}

			// --- UI LOG INJECTION ---
			if content, ok := message["content"].(string); ok {
				var uiLogs string
				for _, m := range sess.Messages {
					if len(m.ToolCalls) > 0 {
						funcName := m.ToolCalls[0].Function.Name
						args := m.ToolCalls[0].Function.Arguments
						args = strings.ReplaceAll(args, "\n", "")
						uiLogs += fmt.Sprintf("> ⚙️ **Executed `%s`**\n> `%s`\n\n", funcName, args)
					}
				}

				if uiLogs != "" {
					return uiLogs + "---\n" + content, nil
				}
				
				return content, nil
			}
			return "Response processed.", nil
		}

		lastResponseBody = string(body)
		
		if apiPath == "/chat/completions" && strings.Contains(lastResponseBody, "run on POST /v1/chat/") {
			log.Printf("🔄 [AI] Dynamic route detected! Switching %s to /chat/...", actualModelID)
			apiPath = "/chat/"
			attempt-- 
			continue
		}

		if strings.Contains(lastResponseBody, "concurrency limit exceeded") {
			time.Sleep(2500 * time.Millisecond)
			continue
		}
	}

	return "", fmt.Errorf("API failed across all keys: %s", lastResponseBody)
}