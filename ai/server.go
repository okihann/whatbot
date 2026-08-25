package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bot/types"
)

func StartServer() {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/models", authMiddleware(handleGetModels))
	mux.HandleFunc("/v1/chat/completions", authMiddleware(handleChat))

	// Serve workspace files for download
	fileServer := http.FileServer(http.Dir(WorkspaceDir))
	mux.Handle("/files/", http.StripPrefix("/files/", fileServer))

	// Direct upload endpoint into sandbox workspace
	mux.HandleFunc("/upload", authMiddleware(handleFileUpload))
	mux.HandleFunc("/v1/upload", authMiddleware(handleFileUpload)) // fallback

	log.Printf("🌐 [SERVER] API Server started on port %s...\n", AppConfig.ServerPort)

	go RefreshModels()

	if err := http.ListenAndServe(AppConfig.ServerPort, mux); err != nil {
		log.Fatalf("💥 [SERVER FATAL] Failed to start HTTP server: %v", err)
	}
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")

		// If a server secret exists, enforce it
		if AppConfig.ServerSecret != "" && token != AppConfig.ServerSecret {
			http.Error(w, `{"error": {"message": "Unauthorized"}}`, http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	safeFilename := filepath.Base(header.Filename)
	targetPath := filepath.Join(WorkspaceDir, safeFilename)

	out, err := os.Create(targetPath)
	if err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer out.Close()

	io.Copy(out, file)

	log.Printf("📁 [FILE UPLOAD] Uploaded '%s' to sandbox.\n", safeFilename)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"filename": safeFilename,
		"file_url": fmt.Sprintf("/files/%s", safeFilename),
		"status":   "success",
	})
}

func handleGetModels(w http.ResponseWriter, r *http.Request) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	var data []types.Model
	now := time.Now().Unix()

	for name := range modelCache {
		data = append(data, types.Model{
			ID:      name,
			Object:  "model",
			Created: now,
			OwnedBy: "custom-provider",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.ModelList{Object: "list", Data: data})
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var req types.OpenAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": {"message": "Invalid JSON"}}`, http.StatusBadRequest)
		return
	}

	// Build tools based on user toggle
	var tools []interface{}
	tools = append(tools, RunCommandTool, UnzipTool)
	if req.WebSearchEnabled {
		tools = append(tools, WebSearchTool)
	}

	session := &types.AISession{
		ID:       "web-client-session",
		Model:    req.Model,
		Messages: req.Messages,
		Tools:    tools,
	}

	if req.ThinkEnabled {
		systemMsg := types.ChatMessage{
			Role:    "system",
			Content: "You are in deep-think mode. Think step by step, reason carefully, and provide a detailed answer.",
		}
		session.Messages = append([]types.ChatMessage{systemMsg}, session.Messages...)
	}

	var processBuilder strings.Builder
	var searchOutputBuilder strings.Builder

	aiResponse, err := GenerateAIResponseWithTrace(session, &processBuilder, &searchOutputBuilder)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": {"message": "%s"}}`, err.Error()), http.StatusInternalServerError)
		return
	}

	var finalContent strings.Builder
	if processBuilder.Len() > 0 {
		finalContent.WriteString("<process>")
		finalContent.WriteString(processBuilder.String())
		finalContent.WriteString("</process>\n")
	}
	if searchOutputBuilder.Len() > 0 {
		finalContent.WriteString("<search_output>")
		finalContent.WriteString(searchOutputBuilder.String())
		finalContent.WriteString("</search_output>\n")
	}
	finalContent.WriteString(aiResponse)

	timestamp := time.Now().Unix()
	baseID := fmt.Sprintf("chatcmpl-%d", timestamp)

	resp := types.OpenAIChatResponse{
		ID:      baseID,
		Object:  "chat.completion",
		Created: timestamp,
		Model:   req.Model,
		Choices: []types.OpenAIChoice{
			{
				Index:        0,
				Message:      types.ChatMessage{Role: "assistant", Content: finalContent.String()},
				FinishReason: "stop",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
