package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	ModelGemini31ProPreview       = "gemini-3.1-pro-preview"
	ModelGemini3FlashPreview      = "gemini-3-flash-preview"
	ModelGemini31FlashLitePreview = "gemini-3.1-flash-lite-preview"
)

type ModelInfo struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	Tier             string `json:"tier"`
	Lifecycle        string `json:"lifecycle"`
	SupportsThinking bool   `json:"supportsThinking"`
	SupportsTools    bool   `json:"supportsTools"`
	Status           string `json:"status"`
}

type Client struct {
	httpClient  *http.Client
	apiKey      string
	baseURL     string
	rateLimiter *RateLimiter
}

type GenerateContentRequest struct {
	Model             string    `json:"-"`
	Contents          []Content `json:"contents"`
	SystemInstruction *Content  `json:"system_instruction,omitempty"`
}

type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inline_data,omitempty"`
}

type InlineData struct {
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"`
}

type GenerateContentResponse struct {
	Candidates    []Candidate   `json:"candidates"`
	UsageMetadata UsageMetadata `json:"usageMetadata"`
}

type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason"`
}

type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type APIError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

func NewClient(apiKey, baseURL string, rateLimiter *RateLimiter) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		apiKey:      strings.TrimSpace(apiKey),
		baseURL:     strings.TrimRight(baseURL, "/"),
		rateLimiter: rateLimiter,
	}
}

func (c *Client) GenerateContent(ctx context.Context, req GenerateContentRequest) (GenerateContentResponse, error) {
	if c == nil || c.apiKey == "" {
		return GenerateContentResponse{}, errors.New("gemini client not configured")
	}

	if strings.TrimSpace(req.Model) == "" {
		req.Model = ModelGemini31ProPreview
	}

	if c.rateLimiter != nil {
		if err := c.rateLimiter.CanMakeRequest(); err != nil {
			return GenerateContentResponse{}, err
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return GenerateContentResponse{}, err
	}

	endpoint := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, req.Model)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return GenerateContentResponse{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("x-goog-api-key", c.apiKey)

	startedAt := time.Now()
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if c.rateLimiter != nil {
			_ = c.rateLimiter.LogUsage(req.Model, 0, time.Since(startedAt), false, err)
		}
		return GenerateContentResponse{}, err
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		payload, _ := io.ReadAll(response.Body)
		apiErr := APIError{}
		if json.Unmarshal(payload, &apiErr) == nil && apiErr.Error.Message != "" {
			err = fmt.Errorf("gemini api error (%d): %s", apiErr.Error.Code, apiErr.Error.Message)
		} else {
			err = fmt.Errorf("gemini api error: status %d", response.StatusCode)
		}
		if c.rateLimiter != nil {
			_ = c.rateLimiter.LogUsage(req.Model, 0, time.Since(startedAt), false, err)
		}
		return GenerateContentResponse{}, err
	}

	var parsed GenerateContentResponse
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		if c.rateLimiter != nil {
			_ = c.rateLimiter.LogUsage(req.Model, 0, time.Since(startedAt), false, err)
		}
		return GenerateContentResponse{}, err
	}

	totalTokens := parsed.UsageMetadata.TotalTokenCount
	if totalTokens == 0 {
		totalTokens = parsed.UsageMetadata.PromptTokenCount + parsed.UsageMetadata.CandidatesTokenCount
	}

	if c.rateLimiter != nil {
		_ = c.rateLimiter.LogUsage(req.Model, totalTokens, time.Since(startedAt), true, nil)
	}

	return parsed, nil
}

func (r GenerateContentResponse) Text() string {
	var builder strings.Builder
	for _, candidate := range r.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(part.Text)
		}
		if builder.Len() > 0 {
			break
		}
	}
	return strings.TrimSpace(builder.String())
}

func Catalog(configured bool) []ModelInfo {
	status := "ready"
	if !configured {
		status = "requires_api_key"
	}

	return []ModelInfo{
		{
			ID:               ModelGemini31ProPreview,
			Label:            "Gemini 3.1 Pro Preview",
			Tier:             "advanced",
			Lifecycle:        "preview",
			SupportsThinking: true,
			SupportsTools:    true,
			Status:           status,
		},
		{
			ID:               ModelGemini3FlashPreview,
			Label:            "Gemini 3 Flash Preview",
			Tier:             "balanced",
			Lifecycle:        "preview",
			SupportsThinking: true,
			SupportsTools:    true,
			Status:           status,
		},
		{
			ID:               ModelGemini31FlashLitePreview,
			Label:            "Gemini 3.1 Flash-Lite Preview",
			Tier:             "fast",
			Lifecycle:        "preview",
			SupportsThinking: true,
			SupportsTools:    true,
			Status:           status,
		},
	}
}
