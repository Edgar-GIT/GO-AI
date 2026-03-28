package gophermobile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	BaseURL            string
	HTTPTimeoutSeconds int
}

type queuedMessagesFile struct {
	PendingMessages []QueuedMessage `json:"pendingMessages"`
}

type QueuedMessage struct {
	TempID     string `json:"tempId"`
	ChatID     string `json:"chatId"`
	Content    string `json:"content"`
	ForceModel string `json:"forceModel,omitempty"`
	QueuedAt   string `json:"queuedAt"`
	Status     string `json:"status"`
}

type SyncResult struct {
	Sent    []QueuedMessage `json:"sent"`
	Failed  []QueuedMessage `json:"failed"`
	Skipped []QueuedMessage `json:"skipped"`
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:            normalizeBaseURL(baseURL),
		HTTPTimeoutSeconds: 45,
	}
}

func (c *Client) SetBaseURL(baseURL string) {
	c.BaseURL = normalizeBaseURL(baseURL)
}

func (c *Client) Bootstrap() (string, error) {
	return c.get("/api/app/bootstrap")
}

func (c *Client) ListChats(search string) (string, error) {
	path := "/api/chats"
	if strings.TrimSpace(search) != "" {
		path += "?search=" + search
	}
	return c.get(path)
}

func (c *Client) GetChat(chatID string) (string, error) {
	return c.get("/api/chats/" + strings.TrimSpace(chatID))
}

func (c *Client) CreateChat(title, model string) (string, error) {
	payload := map[string]string{
		"title": title,
		"model": model,
	}
	return c.postJSON("/api/chats", payload)
}

func (c *Client) SendMessage(chatID, content, forceModel string) (string, error) {
	payload := map[string]any{
		"content":    content,
		"forceModel": forceModel,
	}
	return c.postJSON("/api/chats/"+strings.TrimSpace(chatID)+"/messages", payload)
}

func (c *Client) QueueMessage(queuePath, chatID, content, forceModel string) error {
	state, err := loadQueue(queuePath)
	if err != nil {
		return err
	}

	state.PendingMessages = append(state.PendingMessages, QueuedMessage{
		TempID:     fmt.Sprintf("tmp_%d", time.Now().UTC().UnixNano()),
		ChatID:     strings.TrimSpace(chatID),
		Content:    strings.TrimSpace(content),
		ForceModel: strings.TrimSpace(forceModel),
		QueuedAt:   time.Now().UTC().Format(time.RFC3339),
		Status:     "pending",
	})

	return saveQueue(queuePath, state)
}

func (c *Client) SyncQueuedMessages(queuePath string) (string, error) {
	state, err := loadQueue(queuePath)
	if err != nil {
		return "", err
	}

	result := SyncResult{}
	remaining := make([]QueuedMessage, 0, len(state.PendingMessages))

	for _, item := range state.PendingMessages {
		if strings.TrimSpace(item.ChatID) == "" || strings.TrimSpace(item.Content) == "" {
			item.Status = "skipped"
			result.Skipped = append(result.Skipped, item)
			continue
		}

		if _, err := c.SendMessage(item.ChatID, item.Content, item.ForceModel); err != nil {
			item.Status = "pending"
			result.Failed = append(result.Failed, item)
			remaining = append(remaining, item)
			continue
		}

		item.Status = "sent"
		result.Sent = append(result.Sent, item)
	}

	if err := saveQueue(queuePath, queuedMessagesFile{PendingMessages: remaining}); err != nil {
		return "", err
	}

	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func (c *Client) get(path string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return "", err
	}

	response, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	return decodeResponse(response)
}

func (c *Client) postJSON(path string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	return decodeResponse(response)
}

func (c *Client) httpClient() *http.Client {
	timeout := c.HTTPTimeoutSeconds
	if timeout <= 0 {
		timeout = 45
	}

	return &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}
}

func decodeResponse(response *http.Response) (string, error) {
	data := &bytes.Buffer{}
	if _, err := data.ReadFrom(response.Body); err != nil {
		return "", err
	}

	if response.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("request failed with %d: %s", response.StatusCode, strings.TrimSpace(data.String()))
	}

	return data.String(), nil
}

func normalizeBaseURL(baseURL string) string {
	value := strings.TrimSpace(baseURL)
	if value == "" {
		return "http://127.0.0.1:8080"
	}
	return strings.TrimRight(value, "/")
}

func loadQueue(queuePath string) (queuedMessagesFile, error) {
	file, err := os.Open(queuePath)
	if err != nil {
		if os.IsNotExist(err) {
			return queuedMessagesFile{PendingMessages: []QueuedMessage{}}, nil
		}
		return queuedMessagesFile{}, err
	}
	defer file.Close()

	var state queuedMessagesFile
	if err := json.NewDecoder(file).Decode(&state); err != nil {
		return queuedMessagesFile{}, err
	}

	return state, nil
}

func saveQueue(queuePath string, state queuedMessagesFile) error {
	if err := os.MkdirAll(filepath.Dir(queuePath), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	tempPath := queuePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tempPath, queuePath)
}
