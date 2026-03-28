package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gopher-ai/internal/attachments"
	"gopher-ai/internal/chat"
	"gopher-ai/internal/config"
	"gopher-ai/internal/gemini"
	"gopher-ai/internal/inference"
	"gopher-ai/internal/storage"
	"gopher-ai/internal/training"
)

func TestBootstrapAndModelsEndpoints(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := config.Default(root)
	cfg.Training.AutoRun = false
	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("ensure directories: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	quotaTracker, err := gemini.NewRateLimiter(cfg.Paths.QuotaTrackingFile, gemini.Limits{
		DailyQuota:   cfg.Gemini.DailyQuota,
		RequestLimit: cfg.Gemini.RequestLimitDaily,
		Cooldown:     time.Duration(cfg.Gemini.RequestCooldownSeconds) * time.Second,
	})
	if err != nil {
		t.Fatalf("new quota tracker: %v", err)
	}

	chatStore := storage.NewChatStore(cfg.Paths.ChatsDir, cfg.Paths.TrashDir)
	attachmentService := attachments.NewService(cfg.Paths.AttachmentsTempDir, cfg.Paths.AttachmentsMetaDir, cfg.Features.AttachmentMaxSize)
	inferenceService := inference.NewService(cfg, nil, nil, logger)
	trainingService := training.NewService(cfg, chatStore, nil, logger)
	defer trainingService.Close()
	server := NewServer(cfg, logger, chatStore, attachmentService, inferenceService, trainingService, nil, quotaTracker, nil)

	value := chat.New("Aquarium Test Chat", cfg.Models.Primary)
	value.AddMessage(chat.NewMessage("user", "Can you see me?", nil))
	if err := chatStore.Create(context.Background(), value); err != nil {
		t.Fatalf("create chat: %v", err)
	}

	bootstrapReq := httptest.NewRequest(http.MethodGet, "/api/app/bootstrap", nil)
	bootstrapRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(bootstrapRes, bootstrapReq)

	if bootstrapRes.Code != http.StatusOK {
		t.Fatalf("bootstrap status: got %d want %d", bootstrapRes.Code, http.StatusOK)
	}
	if !strings.Contains(bootstrapRes.Body.String(), "Aquarium Test Chat") {
		t.Fatalf("bootstrap response did not include seeded chat: %s", bootstrapRes.Body.String())
	}

	modelsReq := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	modelsRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(modelsRes, modelsReq)

	if modelsRes.Code != http.StatusOK {
		t.Fatalf("models status: got %d want %d", modelsRes.Code, http.StatusOK)
	}
	if !strings.Contains(modelsRes.Body.String(), "gopher-ai") {
		t.Fatalf("models response did not include Gopher-AI: %s", modelsRes.Body.String())
	}
	if !strings.Contains(modelsRes.Body.String(), cfg.Llama.ModelAlias) {
		t.Fatalf("models response did not include local alias %q: %s", cfg.Llama.ModelAlias, modelsRes.Body.String())
	}
}

func TestSystemSettingsUpdatesGeminiConfiguration(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")

	root := t.TempDir()
	cfg := config.Default(root)
	cfg.Training.AutoRun = false
	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("ensure directories: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	quotaTracker, err := gemini.NewRateLimiter(cfg.Paths.QuotaTrackingFile, gemini.Limits{
		DailyQuota:   cfg.Gemini.DailyQuota,
		RequestLimit: cfg.Gemini.RequestLimitDaily,
		Cooldown:     time.Duration(cfg.Gemini.RequestCooldownSeconds) * time.Second,
	})
	if err != nil {
		t.Fatalf("new quota tracker: %v", err)
	}

	chatStore := storage.NewChatStore(cfg.Paths.ChatsDir, cfg.Paths.TrashDir)
	attachmentService := attachments.NewService(cfg.Paths.AttachmentsTempDir, cfg.Paths.AttachmentsMetaDir, cfg.Features.AttachmentMaxSize)
	inferenceService := inference.NewService(cfg, nil, nil, logger)
	trainingService := training.NewService(cfg, chatStore, nil, logger)
	defer trainingService.Close()
	server := NewServer(cfg, logger, chatStore, attachmentService, inferenceService, trainingService, nil, quotaTracker, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/system/settings", strings.NewReader(`{"apiKeys":{"gemini":"test-key"},"models":{"primary":"gemini-3.1-pro-preview"}}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("settings status: got %d want %d body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"geminiConfigured":true`) {
		t.Fatalf("settings response did not mark gemini configured: %s", res.Body.String())
	}

	saved, err := config.Load(root)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if saved.APIKeys.Gemini != "test-key" {
		t.Fatalf("saved gemini key = %q, want test-key", saved.APIKeys.Gemini)
	}
	if saved.Models.Primary != "gemini-3.1-pro-preview" {
		t.Fatalf("saved primary model = %q", saved.Models.Primary)
	}
}
