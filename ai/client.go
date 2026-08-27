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

func GenerateAIResponseWithTrace(sess *types.AISession, process *strings.Builder, searchOutput *strings.Builder) (string, error) {
	if len(sess.Messages) > 25 {
		return "⚠️ **System Override:** AI execution terminated. Maximum autonomous tool limit (10) reached.", nil
	}

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
		"model":    actualModelID,
		"messages": sess.Messages,
	}
	if sess.Tools != nil {
		payload["tools"] = sess.Tools
		payload["tool_choice"] = "auto"
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

			if rawToolCalls, ok := message["tool_calls"].([]interface{}); ok && len(rawToolCalls) > 0 {
				assistantMsg := types.ChatMessage{
					Role:      "assistant",
					Content:   "",
					ToolCalls: []types.ToolCall{},
				}
				for _, raw := range rawToolCalls {
					tc := raw.(map[string]interface{})
					funcData := tc["function"].(mfunc GenerateAIResponseWithTrace(sess *types.AISession, reasoning *strings.Builder, searchOutput *strings.Builder) (types.ChatMessage, error) {
	if len(sess.Messages) > 25 {
		return types.ChatMessage{Content: "⚠️ **System Override:** AI execution terminated. Maximum autonomous tool limit (10) reached."}, nil
	}

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
		return types.ChatMessage{}, err
	}

	provider := cached.Provider
	actualModelID := cached.ActualID

	payload := map[string]interface{}{
		"model":    actualModelID,
		"messages": sess.Messages,
	}
	if sess.Tools != nil {
		payload["tools"] = sess.Tools
		payload["tool_choice"] = "auto"
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
				return types.ChatMessage{}, fmt.Errorf("invalid API response format")
			}

			message := choices[0].(map[string]interface{})["message"].(map[string]interface{})

			// Build assistant message from response
			assistantMsg := types.ChatMessage{
				Role: "assistant",
			}
			if content, ok := message["content"].(string); ok {
				assistantMsg.Content = content
			}

			if rawToolCalls, ok := message["tool_calls"].([]interface{}); ok && len(rawToolCalls) > 0 {
				assistantMsg.ToolCalls = []types.ToolCall{}
				for _, raw := range rawToolCalls {
					tc := raw.(map[string]interface{})
					funcData := tc["function"].(map[string]interface{})
					assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, types.ToolCall{
						ID:   tc["id"].(string),
						Type: "function",
						Function: types.FunctionCall{
							Name:      funcData["name"].(string),
							Arguments: funcData["arguments"].(string),
						},
					})
				}

				// Append assistant message to session
				sess.Messages = append(sess.Messages, assistantMsg)

				unknownToolFound := false

				// Execute each tool call (only known tools)
				for _, toolCall := range assistantMsg.ToolCalls {
					var args map[string]string
					json.Unmarshal([]byte(toolCall.Function.Arguments), &args)

					var toolOutput string
					known := true
					switch toolCall.Function.Name {
					case "web_search":
						log.Printf("🔍 [AI TOOL] Web search: '%s'\n", args["query"])
						toolOutput = PerformWebSearch(args["query"])
						searchOutput.WriteString(fmt.Sprintf("Query: %s\n%s\n\n", args["query"], toolOutput))
						reasoning.WriteString(fmt.Sprintf("🔍 Searching for: %s\n", args["query"]))
					case "run_command":
						log.Printf("💻 [AI TOOL] Command execution: '%s'\n", args["command"])
						toolOutput = ExecuteSandboxedCommand(args["command"])
						reasoning.WriteString(fmt.Sprintf("💻 Executing command: %s\n", args["command"]))
					case "unzip_file":
						log.Printf("📦 [AI TOOL] Unzipping: '%s'\n", args["zip_filename"])
						toolOutput = ExtractZip(args["zip_filename"])
						reasoning.WriteString(fmt.Sprintf("📦 Unzipping: %s\n", args["zip_filename"]))
					default:
						known = false
						unknownToolFound = true
						// Do not execute unknown tools; let Worker handle them
						toolOutput = "" // will not append tool result
					}

					if known {
						// Append tool result message
						sess.Messages = append(sess.Messages, types.ChatMessage{
							Role:       "tool",
							Content:    toolOutput,
							ToolCallID: toolCall.ID,
						})
					}
				}

				// If any unknown tool was present, return assistant message with tool calls
				if unknownToolFound {
					return assistantMsg, nil
				}

				// All tools were known, so call AI again recursively
				return GenerateAIResponseWithTrace(sess, reasoning, searchOutput)
			}

			// No tool calls, return final content
			return assistantMsg, nil
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

	return types.ChatMessage{}, fmt.Errorf("API failed across all keys: %s", lastResponseBody)
}
