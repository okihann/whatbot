package types

import (
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

type Command struct {
	Name        string
	Description string
	Category    string
	Execute     func(client *whatsmeow.Client, msg *events.Message, args []string)
}

type GroupSettings struct {
	WelcomeEnabled     bool            `json:"welcomeEnabled"`
	WelcomeMessage     string          `json:"welcomeMessage"`
	DisabledCategories map[string]bool `json:"disabledCategories"`
	DisabledCommands   map[string]bool `json:"disabledCommands"`
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type OpenAIChatRequest struct {
	Model            string        `json:"model"`
	Messages         []ChatMessage `json:"messages"`
	Tools            interface{}   `json:"tools,omitempty"`
	WebSearchEnabled bool          `json:"webSearchEnabled,omitempty"`
	ThinkEnabled     bool          `json:"thinkEnabled,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	LogProbs     interface{} `json:"logprobs"`
	FinishReason string      `json:"finish_reason"`
}

type OpenAIChatResponse struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
	Choices           []OpenAIChoice `json:"choices"`
	Usage             Usage          `json:"usage"`
	Reasoning         string         `json:"reasoning,omitempty"`
	SearchOutput      string         `json:"search_output,omitempty"`
}

type AISession struct {
	ID       string        `json:"id"`
	Model    string        `json:"model"`
	Role     string        `json:"role"`
	Messages []ChatMessage `json:"messages"`
	UserID   string        `json:"-"`
	Tools    interface{}   `json:"tools,omitempty"`
}

type UserData struct {
	Model          string `json:"model"`
	Role           string `json:"role"`
	CurrentSession string `json:"currentSession"`
}
