package ai

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
    "log"
)

const WorkspaceDir = "./workspace"
const BinDir = "./bin"

func init() {
	os.MkdirAll(WorkspaceDir, 0755)
	os.MkdirAll(BinDir, 0755)
}

var WebSearchTool = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        "web_search",
		"description": "Searches the internet for real-time information.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "Search query"},
			},
			"required": []string{"query"},
		},
	},
}

var RunCommandTool = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        "run_command",
		"description": "Executes shell commands strictly inside the sandboxed workspace directory.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]string{"type": "string", "description": "Bash command"},
			},
			"required": []string{"command"},
		},
	},
}

var UnzipTool = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        "unzip_file",
		"description": "Extracts a .zip file in the workspace directory.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"zip_filename": map[string]string{"type": "string", "description": "Zip file name"},
			},
			"required": []string{"zip_filename"},
		},
	},
}

func ExecuteSandboxedCommand(cmdStr string) string {
	absWorkspace, _ := filepath.Abs(WorkspaceDir)
	absBin, _ := filepath.Abs(BinDir)

	// STRICT SANDBOX BLOCKLIST
	blocked := []string{
		"rm -rf /", "mkfs", "dd ", ":(){:|:&};:", "shutdown", "reboot", 
		"..", "cd /", "~", "env", "export", "config.json", ".db", "sqlite3",
	}
	
	for _, b := range blocked {
		if strings.Contains(cmdStr, b) {
			log.Printf("🚨 [SECURITY] Blocked rogue AI command: %s\n", cmdStr)
			return "Security Error: Command blocked by system policy. You are restricted to the current directory."
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = absWorkspace

	pathEnv := os.Getenv("PATH")
	cmd.Env = append(os.Environ(), fmt.Sprintf("PATH=%s:%s", absBin, pathEnv))

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "Error: Command timed out (20s limit)."
	}

	res := string(out)
	if err != nil && res == "" {
		res = fmt.Sprintf("Error: %v", err)
	}
	if len(res) > 3000 {
		res = res[:3000] + "\n...(truncated)"
	}
	return res
}

func ExtractZip(zipFilename string) string {
	zipPath := filepath.Join(WorkspaceDir, filepath.Base(zipFilename))
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Sprintf("Failed to open zip: %v", err)
	}
	defer r.Close()

	var extracted []string
	for _, f := range r.File {
		fpath := filepath.Join(WorkspaceDir, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(WorkspaceDir)+string(os.PathSeparator)) {
			return "Security Error: Invalid file path."
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Sprintf("Error extracting: %v", err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Sprintf("Error reading entry: %v", err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return fmt.Sprintf("Error writing file: %v", err)
		}
		extracted = append(extracted, f.Name)
	}

	return fmt.Sprintf("Successfully extracted %d files:\n- %s", len(extracted), strings.Join(extracted, "\n- "))
}

func EnsureBinary(binaryName string, downloadURL string) string {
	absBin, _ := filepath.Abs(BinDir)
	targetPath := filepath.Join(absBin, binaryName)

	if _, err := exec.LookPath(binaryName); err == nil {
		return "Binary exists in PATH."
	}
	if _, err := os.Stat(targetPath); err == nil {
		return "Binary exists in local bin."
	}

	resp, err := http.Get(downloadURL)
	if err != nil || resp.StatusCode != 200 {
		return fmt.Sprintf("Failed download: %v", err)
	}
	defer resp.Body.Close()

	out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return fmt.Sprintf("Failed to save: %v", err)
	}
	defer out.Close()

	io.Copy(out, resp.Body)
	return fmt.Sprintf("Installed binary %s.", binaryName)
}

func PerformWebSearch(query string) string {
	if len(AppConfig.JinaKeys) > 0 {
		for i := 0; i < len(AppConfig.JinaKeys); i++ {
			apiKey := AppConfig.GetNextJinaKey()
			res, err := searchJina(query, apiKey)
			if err == nil {
				return res
			}
		}
	}
	if len(AppConfig.TavilyKeys) > 0 {
		for i := 0; i < len(AppConfig.TavilyKeys); i++ {
			apiKey := AppConfig.GetNextTavilyKey()
			res, err := searchTavily(query, apiKey)
			if err == nil {
				return res
			}
		}
	}
	return "Error: All search engines unavailable."
}

func searchJina(query, apiKey string) (string, error) {
	searchURL := fmt.Sprintf("https://s.jina.ai/%s", url.PathEscape(query))
	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	res := string(body)
	if len(res) > 3000 {
		res = res[:3000] + "\n...(truncated)"
	}
	return res, nil
}

func searchTavily(query, apiKey string) (string, error) {
	payload := map[string]interface{}{"query": query, "search_depth": "basic", "include_answer": true}
	payloadBytes, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api.tavily.com/search", bytes.NewBuffer(payloadBytes))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if answer, ok := result["answer"].(string); ok && answer != "" {
		return answer, nil
	}
	return "", fmt.Errorf("no search results")
}