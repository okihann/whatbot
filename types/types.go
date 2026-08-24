// --- types/types.go (Updated) ---
package types

import (
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/types/events"
)

// Command defines the structure for all bot commands
type Command struct {
	Name        string
	Description string
	Category    string
	Execute     func(client *whatsmeow.Client, msg *events.Message, args []string)
}

// --- Player and Character Structs ---

type Resource string

const (
	Yen        Resource = "yen"
	Iron       Resource = "iron"
	Meat       Resource = "meat"
	Vegetables Resource = "vegetables"
	Fruit      Resource = "fruit"
	Emerald    Resource = "emerald"
	Diamond    Resource = "diamond"
	Gold       Resource = "gold"
)

// Player represents a user in the RPG game
type Player struct {
	ID                  string             `json:"id"`
	Name                string             `json:"name"`
	Age                 int                `json:"age"`
	Level               int                `json:"level"`
	NetWorth            int64              `json:"netWorth"`
	Currency            map[Resource]int64 `json:"currency"`
	Resources           map[Resource]int64 `json:"resources"`
	Energy              Energy             `json:"energy"`
	Buildings           []Building         `json:"buildings"`
	Units               []Unit             `json:"units"`
	CombatStats         CombatStats        `json:"combatStats"`
	Character           Character          `json:"character"`
	Bank                Bank               `json:"bank"`
	Cooldowns           Cooldowns          `json:"cooldowns"`
	RaidProtectionUntil int64              `json:"raidProtectionUntil"`
	ActiveWorksiteID    string             `json:"activeWorksiteId"`
}

type Energy struct {
	Current int `json:"current"`
	Max     int `json:"max"`
}

type GroupSettings struct {
	WelcomeEnabled     bool            `json:"welcomeEnabled"`
	WelcomeMessage     string          `json:"welcomeMessage"` // The custom welcome message for the group
	DisabledCategories map[string]bool `json:"disabledCategories"`
	DisabledCommands   map[string]bool `json:"disabledCommands"`
}

type CombatStats struct {
	Attack  int `json:"attack"`
	Defense int `json:"defense"`
}

type Bank struct {
	Balance  int64 `json:"balance"`
	Capacity int64 `json:"capacity"`
}

type Cooldowns struct {
	LastRaid int64 `json:"lastRaid"`
	LastJob  int64 `json:"lastJob"`
}

// Character represents the player's avatar for duels
type Character struct {
	Level       int       `json:"level"`
	XP          int       `json:"xp"`
	Stats       CharStats `json:"stats"`
	Skills      []Skill   `json:"skills"`
	InDuel      bool      `json:"inDuel"`
	IsDefending bool      `json:"isDefending"`
}

type CharStats struct {
	Strength     int `json:"strength"`
	Dexterity    int `json:"dexterity"`
	Intelligence int `json:"intelligence"`
	Health       int `json:"health"`
	MaxHealth    int `json:"maxHealth"`
	Mana         int `json:"mana"`
	MaxMana      int `json:"maxMana"`
}

type Skill struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Power    int    `json:"power"`
	ManaCost int    `json:"manaCost"`
}

// --- Building and Unit Structs ---

type Building struct {
	InstanceID       string     `json:"instanceId"`
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Level            int        `json:"level"`
	Status           string     `json:"status"` // "active" or "under_construction"
	CurrentLabor     int        `json:"currentLabor"`
	RequiredLabor    int        `json:"requiredLabor"`
	Production       Production `json:"production"`
	StoredIncome     float64    `json:"storedIncome"`
	MaxStorage       int        `json:"maxStorage"`
	LastIncomeUpdate int64      `json:"lastIncomeUpdate"`
	DefenseBonus     int        `json:"defenseBonus"`
}

type Production struct {
	Resource Resource `json:"resource"`
	Rate     float64  `json:"rate"`
}

type Unit struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Attack   int    `json:"attack"`
	Defense  int    `json:"defense"`
}

// --- Game Interaction Structs ---

type ActiveWorksite struct {
	PlayerID   string
	MessageKey *waCommon.MessageKey
}

type ShopItem struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"` // "resource" or "unit"
	BuyPrice    int64    `json:"buyPrice"`
	SellPrice   int64    `json:"sellPrice"`
	Resource    Resource `json:"resource,omitempty"`
	UnitID      string   `json:"unitId,omitempty"`
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"` // Always "model"
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelList struct {
	Object string  `json:"object"` // Always "list"
	Data   []Model `json:"data"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
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
	Temperature      *float64      `json:"temperature,omitempty"`
	TopP             *float64      `json:"top_p,omitempty"`
	N                *int          `json:"n,omitempty"`
	Stream           bool          `json:"stream,omitempty"`
	Stop             interface{}   `json:"stop,omitempty"`
	MaxTokens        *int          `json:"max_tokens,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
	User             string        `json:"user,omitempty"`
	Tools            interface{}   `json:"tools,omitempty"`
	ToolChoice       interface{}   `json:"tool_choice,omitempty"`

	WebSearchEnabled bool `json:"webSearchEnabled,omitempty"`
	ThinkEnabled     bool `json:"thinkEnabled,omitempty"`
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
	Object            string         `json:"object"` // "chat.completion"
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
	Choices           []OpenAIChoice `json:"choices"`
	Usage             Usage          `json:"usage"`
	Reasoning         string         `json:"reasoning,omitempty"`      // NEW
	SearchOutput      string         `json:"search_output,omitempty"`  // NEW
}

// Streaming SSE Types
type ChunkDelta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ChunkChoice struct {
	Index        int         `json:"index"`
	Delta        ChunkDelta  `json:"delta"`
	LogProbs     interface{} `json:"logprobs"`
	FinishReason *string     `json:"finish_reason"`
}

type OpenAIChatChunk struct {
	ID                string        `json:"id"`
	Object            string        `json:"object"` // "chat.completion.chunk"
	Created           int64         `json:"created"`
	Model             string        `json:"model"`
	SystemFingerprint string        `json:"system_fingerprint,omitempty"`
	Choices           []ChunkChoice `json:"choices"`
}

// --- Session & Bot Types ---

type AISession struct {
	ID       string        `json:"id"`
	Model    string        `json:"model"`
	Role     string        `json:"role"`
	Messages []ChatMessage `json:"messages"`
	UserID   string        `json:"-"`
	Tools    interface{}   `json:"tools,omitempty"` // NEW
}

type UserData struct {
	Model          string `json:"model"`
	Role           string `json:"role"`
	CurrentSession string `json:"currentSession"`
}
