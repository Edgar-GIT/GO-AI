package inference

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gopher-ai/internal/chat"
	"gopher-ai/internal/config"
	"gopher-ai/internal/gemini"
	"gopher-ai/internal/llama"
)

var ErrNoInferenceProvider = errors.New("no inference provider available")

const ModelGopherAI = "gopher-ai"

type Result struct {
	Message       chat.Message `json:"message"`
	ResolvedModel string       `json:"resolvedModel"`
	FallbackUsed  bool         `json:"fallbackUsed"`
}

type ModelDescriptor struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	Provider         string `json:"provider"`
	Tier             string `json:"tier"`
	Lifecycle        string `json:"lifecycle"`
	Status           string `json:"status"`
	SupportsThinking bool   `json:"supportsThinking"`
}

type StatusSnapshot struct {
	Primary           string            `json:"primary"`
	Fallback          string            `json:"fallback"`
	FallbackSecondary string            `json:"fallbackSecondary"`
	Local             llama.Status      `json:"local"`
	GeminiConfigured  bool              `json:"geminiConfigured"`
	AvailableModels   []ModelDescriptor `json:"availableModels"`
}

type Service struct {
	cfg    config.AppConfig
	llama  *llama.Client
	gemini *gemini.Client
	logger *slog.Logger
}

func NewService(cfg config.AppConfig, llamaClient *llama.Client, geminiClient *gemini.Client, logger *slog.Logger) *Service {
	return &Service{
		cfg:    cfg,
		llama:  llamaClient,
		gemini: geminiClient,
		logger: logger,
	}
}

func (s *Service) GenerateAssistantReply(ctx context.Context, conversation chat.Chat, forceModel string) (Result, error) {
	model, fallbackUsed, err := s.resolveModel(forceModel)
	if err != nil {
		return Result{}, err
	}

	startedAt := time.Now()
	if isLocalModel(model, s.cfg.Llama.ModelAlias) {
		message, err := s.generateWithLocal(ctx, conversation, model, startedAt)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("local inference failed", "model", model, "err", err)
			}

			fallbackModel, fallbackErr := s.firstAvailableGeminiFallback()
			if fallbackErr != nil {
				return Result{}, err
			}
			message, err = s.generateWithGemini(ctx, conversation, fallbackModel, startedAt)
			if err != nil {
				return Result{}, err
			}
			return Result{
				Message:       message,
				ResolvedModel: fallbackModel,
				FallbackUsed:  true,
			}, nil
		}

		return Result{
			Message:       message,
			ResolvedModel: model,
			FallbackUsed:  fallbackUsed,
		}, nil
	}

	message, err := s.generateWithGemini(ctx, conversation, model, startedAt)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Message:       message,
		ResolvedModel: model,
		FallbackUsed:  fallbackUsed,
	}, nil
}

func (s *Service) resolveModel(forceModel string) (string, bool, error) {
	requested := strings.TrimSpace(forceModel)
	if requested == "" {
		requested = strings.TrimSpace(s.cfg.Models.Primary)
	}

	if requested == ModelGopherAI {
		if s.llama != nil {
			return s.cfg.Llama.ModelAlias, false, nil
		}
		fallbackModel, err := s.firstAvailableGeminiFallback()
		if err != nil {
			return "", false, err
		}
		return fallbackModel, true, nil
	}

	if isLocalModel(requested, s.cfg.Llama.ModelAlias) {
		if s.llama == nil {
			return "", false, ErrNoInferenceProvider
		}
		return s.cfg.Llama.ModelAlias, false, nil
	}

	if isGeminiModel(requested) {
		if s.gemini == nil {
			return "", false, ErrNoInferenceProvider
		}
		return requested, false, nil
	}

	for _, candidate := range []string{
		s.cfg.Models.Primary,
		s.cfg.Models.Fallback,
		s.cfg.Models.FallbackSecondary,
	} {
		switch {
		case isLocalModel(candidate, s.cfg.Llama.ModelAlias) && s.llama != nil:
			return s.cfg.Llama.ModelAlias, candidate != requested, nil
		case isGeminiModel(candidate) && s.gemini != nil:
			return candidate, candidate != requested, nil
		}
	}

	return "", false, fmt.Errorf("%w: requested model %q", ErrNoInferenceProvider, requested)
}

func (s *Service) Snapshot(ctx context.Context) StatusSnapshot {
	localStatus := llama.Status{Status: "disabled"}
	if s.llama != nil {
		statusCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		localStatus = s.llama.Status(statusCtx)
		cancel()
	}

	models := []ModelDescriptor{
		{
			ID:               ModelGopherAI,
			Label:            "Gopher-AI",
			Provider:         "adaptive",
			Tier:             "default",
			Lifecycle:        "local-first",
			Status:           gopherAIStatus(localStatus, s.gemini != nil),
			SupportsThinking: s.gemini != nil,
		},
		{
			ID:               s.cfg.Llama.ModelAlias,
			Label:            "Local llama.cpp",
			Provider:         "llama.cpp",
			Tier:             "local",
			Lifecycle:        "self-hosted",
			Status:           localStatus.Status,
			SupportsThinking: false,
		},
	}

	for _, item := range gemini.Catalog(s.gemini != nil) {
		models = append(models, ModelDescriptor{
			ID:               item.ID,
			Label:            item.Label,
			Provider:         "gemini",
			Tier:             item.Tier,
			Lifecycle:        item.Lifecycle,
			Status:           item.Status,
			SupportsThinking: item.SupportsThinking,
		})
	}

	return StatusSnapshot{
		Primary:           s.cfg.Models.Primary,
		Fallback:          s.cfg.Models.Fallback,
		FallbackSecondary: s.cfg.Models.FallbackSecondary,
		Local:             localStatus,
		GeminiConfigured:  s.gemini != nil,
		AvailableModels:   models,
	}
}

func (s *Service) generateWithLocal(ctx context.Context, conversation chat.Chat, model string, startedAt time.Time) (chat.Message, error) {
	if s.llama == nil {
		return chat.Message{}, ErrNoInferenceProvider
	}

	localCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.Inference.LocalTimeoutMS)*time.Millisecond)
	defer cancel()

	response, err := s.llama.GenerateChatCompletion(localCtx, llama.ChatCompletionRequest{
		Model:       model,
		Messages:    s.localMessages(conversation),
		Temperature: 0.7,
		MaxTokens:   s.cfg.Llama.MaxTokens,
		Stream:      false,
	})
	if err != nil {
		return chat.Message{}, err
	}

	message := chat.NewMessage("assistant", response.Text(), nil)
	message.Model = model
	message.Timestamp = time.Now().UTC()
	message.LatencyMS = time.Since(startedAt).Milliseconds()
	message.TokensUsed = response.Usage.TotalTokens
	if message.TokensUsed == 0 {
		message.TokensUsed = response.Usage.PromptTokens + response.Usage.CompletionTokens
	}

	return message, nil
}

func (s *Service) generateWithGemini(ctx context.Context, conversation chat.Chat, model string, startedAt time.Time) (chat.Message, error) {
	if s.gemini == nil {
		return chat.Message{}, ErrNoInferenceProvider
	}

	request := gemini.GenerateContentRequest{
		Model:             model,
		SystemInstruction: s.systemInstruction(conversation),
		Contents:          s.contentsFromMessages(conversation.Messages),
	}

	response, err := s.gemini.GenerateContent(ctx, request)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("gemini generation failed", "model", model, "err", err)
		}
		return chat.Message{}, err
	}

	message := chat.NewMessage("assistant", response.Text(), nil)
	message.Model = model
	message.Timestamp = time.Now().UTC()
	message.LatencyMS = time.Since(startedAt).Milliseconds()
	message.TokensUsed = response.UsageMetadata.TotalTokenCount
	if message.TokensUsed == 0 {
		message.TokensUsed = response.UsageMetadata.PromptTokenCount + response.UsageMetadata.CandidatesTokenCount
	}

	return message, nil
}

func (s *Service) firstAvailableGeminiFallback() (string, error) {
	if s.gemini == nil {
		return "", ErrNoInferenceProvider
	}

	for _, candidate := range []string{
		s.cfg.Models.Fallback,
		s.cfg.Models.FallbackSecondary,
		gemini.ModelGemini31FlashLitePreview,
	} {
		if isGeminiModel(candidate) {
			return candidate, nil
		}
	}

	return "", ErrNoInferenceProvider
}

func (s *Service) systemInstruction(conversation chat.Chat) *gemini.Content {
	lines := []string{
		"You are Gopher AI, a local-first assistant focused on software work, practical help, and clean technical explanations.",
		"Keep answers grounded, concise, and useful.",
	}

	if conversation.Memory.Enabled && strings.TrimSpace(conversation.Memory.Context) != "" {
		lines = append(lines, "Known user context: "+strings.TrimSpace(conversation.Memory.Context))
	}

	return &gemini.Content{
		Parts: []gemini.Part{
			{Text: strings.Join(lines, "\n")},
		},
	}
}

func (s *Service) contentsFromMessages(messages []chat.Message) []gemini.Content {
	if len(messages) == 0 {
		return []gemini.Content{}
	}

	start := 0
	if len(messages) > 12 {
		start = len(messages) - 12
	}

	contents := make([]gemini.Content, 0, len(messages)-start)
	for _, message := range messages[start:] {
		role := "user"
		if message.Role == "assistant" {
			role = "model"
		}

		text := strings.TrimSpace(message.Content)
		if len(message.Attachments) > 0 {
			var attachmentNotes []string
			for _, attachment := range message.Attachments {
				note := fmt.Sprintf("- %s (%s, %d bytes)", attachment.Filename, attachment.MimeType, attachment.Size)
				if attachment.Preview != "" {
					note += "\nPreview: " + attachment.Preview
				}
				attachmentNotes = append(attachmentNotes, note)
			}
			if text != "" {
				text += "\n\n"
			}
			text += "Attachments:\n" + strings.Join(attachmentNotes, "\n")
		}

		if text == "" {
			continue
		}

		contents = append(contents, gemini.Content{
			Role: role,
			Parts: []gemini.Part{
				{Text: text},
			},
		})
	}

	return contents
}

func isGeminiModel(model string) bool {
	return strings.HasPrefix(strings.TrimSpace(model), "gemini-")
}

func isLocalModel(model, alias string) bool {
	value := strings.TrimSpace(model)
	return value == "" || strings.HasPrefix(value, "local") || value == strings.TrimSpace(alias)
}

func gopherAIStatus(localStatus llama.Status, geminiConfigured bool) string {
	if localStatus.Status == "ready" {
		return "ready"
	}
	if geminiConfigured {
		return "fallback_ready"
	}
	if localStatus.Status != "" && localStatus.Status != "disabled" {
		return localStatus.Status
	}
	return "degraded"
}

func (s *Service) localMessages(conversation chat.Chat) []llama.ChatMessage {
	contents := s.contentsFromMessages(conversation.Messages)
	messages := make([]llama.ChatMessage, 0, len(contents)+1)

	if system := s.systemInstruction(conversation); system != nil {
		var parts []string
		for _, part := range system.Parts {
			if part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
		if len(parts) > 0 {
			messages = append(messages, llama.ChatMessage{
				Role:    "system",
				Content: strings.Join(parts, "\n"),
			})
		}
	}

	for _, item := range contents {
		role := item.Role
		if role == "" {
			role = "user"
		}

		var parts []string
		for _, part := range item.Parts {
			if part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
		if len(parts) == 0 {
			continue
		}

		messages = append(messages, llama.ChatMessage{
			Role:    role,
			Content: strings.Join(parts, "\n"),
		})
	}

	return messages
}
