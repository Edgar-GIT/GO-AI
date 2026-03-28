package chat

import (
	"strings"
	"time"

	"gopher-ai/internal/ids"
)

type Chat struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
	ModelUsed string       `json:"modelUsed"`
	Messages  []Message    `json:"messages"`
	Memory    MemoryState  `json:"memory"`
	Metadata  ChatMetadata `json:"metadata"`
}

type Message struct {
	ID          string          `json:"id"`
	Role        string          `json:"role"`
	Content     string          `json:"content"`
	Attachments []AttachmentRef `json:"attachments,omitempty"`
	Model       string          `json:"model,omitempty"`
	LatencyMS   int64           `json:"latency,omitempty"`
	TokensUsed  int             `json:"tokensUsed,omitempty"`
	Timestamp   time.Time       `json:"timestamp"`
}

type AttachmentRef struct {
	ID            string `json:"id"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
	MimeType      string `json:"mimeType"`
	Hash          string `json:"hash"`
	LocalPath     string `json:"localPath,omitempty"`
	Preview       string `json:"preview,omitempty"`
	ThumbnailPath string `json:"thumbnailPath,omitempty"`
}

type MemoryState struct {
	Enabled bool   `json:"enabled"`
	Context string `json:"context,omitempty"`
}

type ChatMetadata struct {
	TotalTokensUsed int `json:"totalTokensUsed"`
	MessageCount    int `json:"messageCount"`
	AttachmentCount int `json:"attachmentCount"`
}

type ChatSummary struct {
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	UpdatedAt          time.Time `json:"updatedAt"`
	ModelUsed          string    `json:"modelUsed"`
	MessageCount       int       `json:"messageCount"`
	LastMessagePreview string    `json:"lastMessagePreview,omitempty"`
}

func New(title, model string) Chat {
	now := time.Now().UTC()
	if strings.TrimSpace(title) == "" {
		title = "New Chat"
	}

	return Chat{
		ID:        ids.New("chat"),
		Title:     strings.TrimSpace(title),
		CreatedAt: now,
		UpdatedAt: now,
		ModelUsed: strings.TrimSpace(model),
		Messages:  []Message{},
		Memory: MemoryState{
			Enabled: true,
		},
		Metadata: ChatMetadata{},
	}
}

func NewMessage(role, content string, attachments []AttachmentRef) Message {
	return Message{
		ID:          ids.New("msg"),
		Role:        strings.TrimSpace(role),
		Content:     strings.TrimSpace(content),
		Attachments: attachments,
		Timestamp:   time.Now().UTC(),
	}
}

func (c *Chat) AddMessage(message Message) {
	c.Messages = append(c.Messages, message)
	c.UpdatedAt = message.Timestamp
	if message.Role == "assistant" && message.Model != "" {
		c.ModelUsed = message.Model
	}
	c.RefreshMetadata()
}

func (c *Chat) RefreshMetadata() {
	totalTokens := 0
	attachmentCount := 0
	for _, message := range c.Messages {
		totalTokens += message.TokensUsed
		attachmentCount += len(message.Attachments)
	}

	c.Metadata = ChatMetadata{
		TotalTokensUsed: totalTokens,
		MessageCount:    len(c.Messages),
		AttachmentCount: attachmentCount,
	}
}

func (c Chat) Summary() ChatSummary {
	lastPreview := ""
	if len(c.Messages) > 0 {
		lastPreview = truncate(c.Messages[len(c.Messages)-1].Content, 80)
	}

	return ChatSummary{
		ID:                 c.ID,
		Title:              c.Title,
		UpdatedAt:          c.UpdatedAt,
		ModelUsed:          c.ModelUsed,
		MessageCount:       len(c.Messages),
		LastMessagePreview: lastPreview,
	}
}

func AutoTitleFromContent(content string) string {
	value := strings.TrimSpace(content)
	if value == "" {
		return "New Chat"
	}

	value = strings.Join(strings.Fields(value), " ")
	return truncate(value, 48)
}

func truncate(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}
